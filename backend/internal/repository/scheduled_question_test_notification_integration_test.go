//go:build integration

package repository

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionNotificationSettings struct {
	service.SettingRepository
	values map[string]string
}

func (s scheduledQuestionNotificationSettings) GetValue(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s scheduledQuestionNotificationSettings) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	return s.values, nil
}

type scheduledQuestionNotificationQuestions struct {
	service.AccountTestQuestionRepository
	questions map[int64]*model.AccountTestQuestion
	prepare   func(context.Context) error
}

func (q scheduledQuestionNotificationQuestions) GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error) {
	if id == 1 && q.prepare != nil {
		if err := q.prepare(ctx); err != nil {
			return nil, err
		}
	}
	return q.questions[id], nil
}

func scheduledQuestionSMTPBarrier(t *testing.T) (string, string, <-chan struct{}, func(), <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		_ = listener.Close()
	})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		done <- serveScheduledQuestionSMTPBarrier(conn, entered, release)
	}()
	return host, port, entered, unblock, done
}

func serveScheduledQuestionSMTPBarrier(conn net.Conn, entered chan<- struct{}, release <-chan struct{}) error {
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	write := func(reply string) error {
		_, err := fmt.Fprint(conn, reply)
		return err
	}
	if err := write("220 localhost ESMTP\r\n"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		command := scanner.Text()
		reply := "250 OK\r\n"
		switch {
		case strings.HasPrefix(command, "EHLO"):
			reply = "250-localhost\r\n250 AUTH PLAIN\r\n"
		case strings.HasPrefix(command, "AUTH"):
			reply = "235 Authenticated\r\n"
		case command == "DATA":
			if err := write("354 Send message\r\n"); err != nil {
				return err
			}
			for scanner.Scan() && scanner.Text() != "." {
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			close(entered)
			select {
			case <-release:
			case <-time.After(15 * time.Second):
				return fmt.Errorf("notification barrier timed out")
			}
		case command == "QUIT":
			return write("221 Bye\r\n")
		}
		if err := write(reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func TestScheduledQuestionTestRepositoryIntegrationVerdictSingleConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, true)
	previousLimit := integrationDB.Stats().MaxOpenConnections
	integrationDB.SetMaxOpenConns(1)
	defer integrationDB.SetMaxOpenConns(previousLimit)
	action, err := repo.ApplyAccountVerdict(ctx, plan.ID, account.ID, "alert", true, false, time.Now(), func(actionCtx context.Context, value bool) error {
		return accounts.SetSchedulable(actionCtx, account.ID, value)
	})
	require.NoError(t, err, "scheduling must reuse the verdict transaction instead of acquiring a second connection")
	require.Equal(t, "paused", action)
	loaded, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, loaded.Schedulable)
}

func TestScheduledQuestionTestRepositoryIntegrationServiceNotificationBarrier(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(strconv.FormatBool(before), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, before)
			_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra = '{"synthetic_ui_test":true}'::jsonb WHERE id = $1`, account.ID)
			require.NoError(t, err)
			repo := NewScheduledQuestionTestRepository(integrationDB)
			plans := make([]*service.ScheduledQuestionTestPlan, 0, 2)
			for i := 1; i <= 2; i++ {
				plan, createErr := repo.Create(ctx, &service.ScheduledQuestionTestPlan{
					Name: fmt.Sprintf("notification-barrier-%d-%t", i, before), AccountIDs: []int64{account.ID}, QuestionIDs: []int64{int64(i)},
					CronExpression: "0 * * * *", Enabled: true, MinPassRatio: 100, CooldownMinutes: 360,
					AutoPauseEnabled: true, AutoResumeEnabled: true, NotifyEnabled: i == 1, EmailEnabled: true,
				})
				require.NoError(t, createErr)
				plans = append(plans, plan)
				t.Cleanup(func() { require.NoError(t, repo.Delete(context.Background(), plan.ID)) })
				_, err = integrationDB.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states (plan_id, account_id, state) VALUES ($1,$2,'ok')`, plan.ID, account.ID)
				require.NoError(t, err)
			}
			host, port, entered, unblock, mailDone := scheduledQuestionSMTPBarrier(t)
			settings := scheduledQuestionNotificationSettings{values: map[string]string{
				service.SettingKeySiteName: "Integration", service.SettingKeyAccountQuotaNotifyEmails: `[{"email":"receiver@example.com","verified":true}]`,
				service.SettingKeySMTPHost: host, service.SettingKeySMTPPort: port,
				service.SettingKeySMTPUsername: "sender@example.com", service.SettingKeySMTPPassword: "test-password", service.SettingKeySMTPFrom: "sender@example.com",
			}}
			questions := service.NewAccountTestQuestionService(scheduledQuestionNotificationQuestions{
				questions: map[int64]*model.AccountTestQuestion{
					1: {ID: 1, Name: "failed", Content: "test", Enabled: true, AnswerPatterns: []string{"never-matches-this-synthetic-answer"}},
					2: {ID: 2, Name: "passed", Content: "test", Enabled: true, AnswerPatterns: []string{"Synthetic"}},
				},
				// Prepare runs after the service has captured schedulable_before.
				prepare: func(prepareCtx context.Context) error {
					_, updateErr := integrationDB.ExecContext(prepareCtx, "UPDATE accounts SET schedulable = false WHERE id = $1", account.ID)
					return updateErr
				},
			})
			tester := service.NewAccountTestService(accounts, nil, nil, nil, nil, nil, &config.Config{}, nil)
			batch := service.NewAccountBatchTestService(accounts, tester, questions, nil)
			svc := service.NewScheduledQuestionTestService(repo, accounts, questions, batch, settings, service.NewEmailService(settings, nil), nil, nil)
			t.Cleanup(func() { unblock(); svc.Stop() })
			_, err = svc.RunNow(ctx, plans[0].ID)
			require.NoError(t, err)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("service did not enter SMTP notification barrier: ", ctx.Err())
			}
			states, err := repo.ListStates(ctx, plans[0].ID)
			require.NoError(t, err)
			require.Len(t, states, 1)
			require.Equal(t, "alert", states[0].State)
			require.Nil(t, states[0].LastAlertAt, "cooldown must wait for notification success")
			_, err = svc.RunNow(ctx, plans[1].ID)
			require.NoError(t, err)
			awaitRun := func(planID int64) *service.ScheduledQuestionTestRun {
				var completed *service.ScheduledQuestionTestRun
				require.Eventually(t, func() bool {
					runs, listErr := repo.ListRuns(ctx, planID, 10)
					if listErr != nil || len(runs) != 1 {
						return false
					}
					completed = runs[0]
					return true
				}, 5*time.Second, 10*time.Millisecond)
				return completed
			}
			passing := awaitRun(plans[1].ID)
			require.Equal(t, "success", passing.Status)
			require.Equal(t, "passed", passing.Summary[0].Result)
			require.Equal(t, "resume_blocked", passing.Summary[0].ScheduleAction)
			unblock()
			require.NoError(t, <-mailDone)
			failing := awaitRun(plans[0].ID)
			require.Equal(t, "success", failing.Status)
			require.Equal(t, "failed", failing.Summary[0].Result)
			require.Empty(t, failing.Summary[0].ScheduleAction)
			require.Equal(t, &before, failing.Summary[0].SchedulableBefore)
			require.Len(t, failing.NotificationResults, 1)
			require.True(t, failing.NotificationResults[0].Success)
			loaded, err := accounts.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.False(t, loaded.Schedulable)
			states, err = repo.ListStates(ctx, plans[0].ID)
			require.NoError(t, err)
			require.Equal(t, "alert", states[0].State)
			require.NotNil(t, states[0].LastAlertAt)
		})
	}
}
