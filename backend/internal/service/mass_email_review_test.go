package service

import (
	"context"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMassEmailReviewTestSendUsesCurrentSMTPAndExampleVariables(t *testing.T) {
	smtp := startNotificationEmailTestSMTPServer(t)
	settings := newNotificationEmailMemorySettingRepo()
	require.NoError(t, settings.SetMultiple(context.Background(), smtp.settings()))
	email := NewEmailService(settings, nil)
	svc := NewMassEmailService(newFakeMassEmailRepo(), email, nil, settings)
	to, err := svc.TestSend(context.Background(), "admin@example.com", MassEmailTestInput{
		Subject:  "Test {{name}} & {{email}}",
		BodyHTML: "<p>{{name}} / {{email}} / {{enterprise_name}} / {{site_name}}</p>",
	})
	require.NoError(t, err)
	require.Equal(t, "admin@example.com", to)
	require.EqualValues(t, 1, smtp.messageCount())
	require.Contains(t, smtp.lastMessageBody(t), "测试用户 / admin@example.com / 示例企业 / Sub2API")
	message, err := mail.ReadMessage(strings.NewReader(smtp.lastMessage()))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Test 测试用户 & admin@example.com", subject)
}

// The repository signals after taking a real-looking lease. This lets tests
// cancel a task/leadership during the limiter wait, rather than before drain.
type reviewMassEmailClaimRepo struct {
	*fakeMassEmailRepo
	claimed chan struct{}
}

func (r *reviewMassEmailClaimRepo) ClaimPendingRecipients(ctx context.Context, id int64, limit int, lease time.Duration, now time.Time) ([]domain.MassEmailRecipient, error) {
	rows, err := r.fakeMassEmailRepo.ClaimPendingRecipients(ctx, id, limit, lease, now)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	for _, row := range r.recipients[id] {
		if row.Status == "pending" {
			row.ClaimedAt = &now
			row.Attempts++
		}
	}
	r.mu.Unlock()
	select {
	case r.claimed <- struct{}{}:
	default:
	}
	return rows, nil
}

func TestMassEmailReviewStopsAfterCancellationDuringRateWait(t *testing.T) {
	for _, loseLock := range []bool{false, true} {
		t.Run(map[bool]string{false: "campaign-cancelled", true: "leadership-lost"}[loseLock], func(t *testing.T) {
			smtp := startNotificationEmailTestSMTPServer(t)
			settings := newNotificationEmailMemorySettingRepo()
			require.NoError(t, settings.SetMultiple(context.Background(), smtp.settings()))
			base := newFakeMassEmailRepo()
			base.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: "sending", BodyHTML: "<p>Hello</p>", Subject: "Notice"}
			base.recipients[1] = []*domain.MassEmailRecipient{{ID: 1, CampaignID: 1, Email: "local@example.com", Status: "pending"}}
			repo := &reviewMassEmailClaimRepo{fakeMassEmailRepo: base, claimed: make(chan struct{}, 1)}
			email := NewEmailService(settings, nil)
			svc := NewMassEmailService(repo, email, nil, settings)
			runner := NewMassEmailRunner(repo, svc, email, nil, nil)
			require.True(t, runner.ensureLeader(context.Background()))
			runner.limiter.mu.Lock()
			runner.limiter.next = time.Now().Add(200 * time.Millisecond)
			runner.limiter.mu.Unlock()
			done := make(chan struct{})
			go func() { defer close(done); runner.drain(runner.epochCtx(), 1) }()
			select {
			case <-repo.claimed:
			case <-time.After(time.Second):
				t.Fatal("worker did not claim")
			}
			if loseLock {
				runner.leaderMu.Lock()
				runner.loseLeadershipLocked()
				runner.leaderMu.Unlock()
			} else {
				base.mu.Lock()
				base.campaigns[1].Status = "cancelled"
				base.mu.Unlock()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			require.Zero(t, smtp.messageCount(), "state must be rechecked after waiting, before SMTP")
			base.mu.Lock()
			row := *base.recipients[1][0]
			base.mu.Unlock()
			require.Nil(t, row.ClaimedAt)
			require.Zero(t, row.Attempts)
		})
	}
}

func TestMassEmailReviewRetryFailureResumesWithoutResendingSuccess(t *testing.T) {
	smtp := startNotificationEmailTestSMTPServer(t)
	settings := newNotificationEmailMemorySettingRepo()
	require.NoError(t, settings.SetMultiple(context.Background(), smtp.settings()))
	base := newFakeMassEmailRepo()
	base.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: "completed", BodyHTML: "<p>Hello {{email}}</p>", Subject: "Notice"}
	base.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 1, CampaignID: 1, Email: "failed@example.com", Status: "failed", Attempts: 1, Error: "temporary error"},
		{ID: 2, CampaignID: 1, Email: "sent@example.com", Status: "sent", Attempts: 1},
	}
	email := NewEmailService(settings, nil)
	svc := NewMassEmailService(base, email, nil, settings)
	reset, campaign, err := svc.RetryFailed(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, reset)
	require.Equal(t, "sending", campaign.Status)
	runner := NewMassEmailRunner(base, svc, email, nil, nil)
	require.True(t, runner.ensureLeader(context.Background()))
	runner.drain(runner.epochCtx(), 1)
	require.EqualValues(t, 1, smtp.messageCount())
	require.Contains(t, smtp.lastMessageBody(t), "failed@example.com")
	detail, err := svc.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "completed", detail.Campaign.Status)
	require.Equal(t, 2, detail.Stats.Sent)
	require.Zero(t, detail.Stats.Failed)
	reset, _, err = svc.RetryFailed(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, reset)
	require.EqualValues(t, 1, smtp.messageCount(), "retry with zero failures must not resend delivered recipients")
}
