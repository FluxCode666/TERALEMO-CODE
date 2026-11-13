package admin

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Only settings and a loopback SMTP server are used. No user's SMTP account is read.
type massEmailReviewSettings struct {
	service.SettingRepository
	values map[string]string
}

func (s *massEmailReviewSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return s.values, nil
}
func (s *massEmailReviewSettings) GetValue(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

// The in-memory store exercises the real handler/service/runner/SMTP implementations.
// SQL transaction/claim behavior is checked separately in repository tests.
type massEmailReviewStore struct {
	service.MassEmailRepository
	mu         sync.Mutex
	campaigns  map[int64]domain.MassEmailCampaign
	recipients map[int64][]domain.MassEmailRecipient
	openErr    error
}

func newMassEmailReviewStore() *massEmailReviewStore {
	return &massEmailReviewStore{campaigns: map[int64]domain.MassEmailCampaign{}, recipients: map[int64][]domain.MassEmailRecipient{}}
}
func (s *massEmailReviewStore) CreateCampaign(_ context.Context, c *domain.MassEmailCampaign) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = int64(len(s.campaigns) + 1)
	s.campaigns[c.ID] = *c
	return nil
}
func (s *massEmailReviewStore) GetCampaign(_ context.Context, id int64) (*domain.MassEmailCampaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return nil, domain.ErrMassEmailCampaignNotFound
	}
	return &c, nil
}
func (s *massEmailReviewStore) DeleteCampaignIfNotSending(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return false, domain.ErrMassEmailCampaignNotFound
	}
	if c.Status == domain.MassEmailStatusSending {
		return false, nil
	}
	delete(s.campaigns, id)
	delete(s.recipients, id)
	return true, nil
}
func (s *massEmailReviewStore) ReplaceRecipients(_ context.Context, id int64, recipients []domain.MassEmailRecipient) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range recipients {
		recipients[i].ID = int64(i + 1)
	}
	s.recipients[id] = recipients
	return len(recipients), nil
}
func (s *massEmailReviewStore) TransitionCampaignStatus(_ context.Context, id int64, from []string, to string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.campaigns[id]
	for _, status := range from {
		if c.Status == status {
			c.Status = to
			s.campaigns[id] = c
			return true, nil
		}
	}
	return false, nil
}
func (s *massEmailReviewStore) ListDueScheduledCampaigns(_ context.Context, now time.Time, _ int) ([]domain.MassEmailCampaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []domain.MassEmailCampaign
	for _, c := range s.campaigns {
		if c.Status == domain.MassEmailStatusScheduled && c.ScheduledAt != nil && !c.ScheduledAt.After(now) {
			result = append(result, c)
		}
	}
	return result, nil
}
func (s *massEmailReviewStore) ListCampaigns(_ context.Context, _ pagination.PaginationParams, filters domain.MassEmailCampaignListFilters) ([]domain.MassEmailCampaign, *pagination.PaginationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []domain.MassEmailCampaign
	for _, c := range s.campaigns {
		for _, status := range filters.Statuses {
			if c.Status == status {
				result = append(result, c)
				break
			}
		}
	}
	return result, &pagination.PaginationResult{Total: int64(len(result)), Page: 1, PageSize: 50}, nil
}
func (s *massEmailReviewStore) ClaimPendingRecipients(_ context.Context, id int64, limit int, lease time.Duration, now time.Time) ([]domain.MassEmailRecipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []domain.MassEmailRecipient
	for i := range s.recipients[id] {
		r := &s.recipients[id][i]
		if r.Status != domain.MassEmailRecipientStatusPending || (r.ClaimedAt != nil && now.Sub(*r.ClaimedAt) < lease) {
			continue
		}
		r.ClaimedAt = &now
		r.Attempts++
		result = append(result, *r)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}
func (s *massEmailReviewStore) MarkRecipientSent(_ context.Context, id, rid int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recipients[id] {
		r := &s.recipients[id][i]
		if r.ID == rid {
			r.Status = "sent"
			r.SentAt = &now
			r.ClaimedAt = nil
			return nil
		}
	}
	return domain.ErrMassEmailRecipientNotFound
}
func (s *massEmailReviewStore) MarkRecipientFailed(_ context.Context, id, rid int64, reason string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recipients[id] {
		r := &s.recipients[id][i]
		if r.ID == rid {
			r.Status = "failed"
			r.Error = reason
			r.ClaimedAt = nil
			return nil
		}
	}
	return domain.ErrMassEmailRecipientNotFound
}
func (s *massEmailReviewStore) ReleaseClaimedRecipients(_ context.Context, id int64, ids []int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.recipients[id] {
		r := &s.recipients[id][i]
		for _, rid := range ids {
			if r.ID == rid && r.Status == "pending" {
				r.ClaimedAt = nil
				r.Attempts--
				n++
			}
		}
	}
	return n, nil
}
func (s *massEmailReviewStore) RecountCampaign(_ context.Context, id int64) (*domain.MassEmailCampaignStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.campaigns[id]; !ok {
		return nil, domain.ErrMassEmailCampaignNotFound
	}
	stats := &domain.MassEmailCampaignStats{}
	for _, r := range s.recipients[id] {
		stats.Total++
		switch r.Status {
		case "pending":
			stats.Pending++
		case "sent":
			stats.Sent++
		case "failed":
			stats.Failed++
		}
		if r.OpenCount > 0 {
			stats.Opened++
		}
	}
	c := s.campaigns[id]
	c.TotalCount = stats.Total
	c.SentCount = stats.Sent
	c.FailedCount = stats.Failed
	c.OpenedCount = stats.Opened
	s.campaigns[id] = c
	return stats, nil
}
func (s *massEmailReviewStore) RecordRecipientOpen(_ context.Context, token string, now time.Time) (*domain.MassEmailRecipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return nil, s.openErr
	}
	for id := range s.recipients {
		for i := range s.recipients[id] {
			r := &s.recipients[id][i]
			if r.TrackingToken == token {
				r.OpenCount++
				if r.FirstOpenedAt == nil {
					r.FirstOpenedAt = &now
				}
				r.LastOpenedAt = &now
				clone := *r
				return &clone, nil
			}
		}
	}
	return nil, domain.ErrMassEmailRecipientNotFound
}

type massEmailReviewLock struct {
	mu    sync.Mutex
	owner string
}

func (l *massEmailReviewLock) TryAcquireLeaderLock(_ context.Context, _, owner string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != "" {
		return false, nil
	}
	l.owner = owner
	return true, nil
}
func (l *massEmailReviewLock) RenewLeaderLock(_ context.Context, _, owner string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.owner == owner, nil
}
func (l *massEmailReviewLock) ReleaseLeaderLock(_ context.Context, _, owner string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner == owner {
		l.owner = ""
	}
	return nil
}

func massEmailReviewRouter(svc *service.MassEmailService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewMassEmailHandler(svc, nil)
	r.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7}) })
	r.POST("/mass-emails", h.Create)
	r.PUT("/mass-emails/:id", h.Update)
	r.POST("/mass-emails/preview-recipients", h.Preview)
	r.POST("/mass-emails/test", h.Test)
	r.GET("/mass-emails/:id", h.Get)
	r.DELETE("/mass-emails/:id", h.Delete)
	r.POST("/mass-emails/:id/send", h.Send)
	r.GET("/api/v1/mail/o/:token.gif", h.Pixel)
	return r
}
func massEmailReviewRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestMassEmailReviewHandlerErrors(t *testing.T) {
	store := newMassEmailReviewStore()
	store.campaigns[1] = domain.MassEmailCampaign{ID: 1, Status: "sending"}
	store.campaigns[2] = domain.MassEmailCampaign{ID: 2, Status: "draft", TotalCount: 1}
	r := massEmailReviewRouter(service.NewMassEmailService(store, nil, nil, nil))
	for _, tc := range []struct {
		method, path, body, reason string
		code                       int
	}{
		{"GET", "/mass-emails/999", "", "MASS_EMAIL_NOT_FOUND", 404},
		{"DELETE", "/mass-emails/1", "", "CAMPAIGN_SENDING", 409},
		{"POST", "/mass-emails/2/send", "", "EMAIL_NOT_CONFIGURED", 503},
		{"PUT", "/mass-emails/1", `{"subject":"s","body_html":"<p>x</p>"}`, "CAMPAIGN_SENDING", 409},
		{"POST", "/mass-emails", `{"subject":" ","body_html":"<p>x</p>"}`, "INVALID_SUBJECT", 400},
		{"POST", "/mass-emails", `{"subject":"s","body_html":"<script>x</script>"}`, "INVALID_BODY", 400},
		{"POST", "/mass-emails/preview-recipients", `{"recipient_filter":{"enterprise_filter":{"enterprise_ids":[1],"scope":"invalid"}}}`, "INVALID_RECIPIENT_FILTER", 400},
	} {
		t.Run(tc.method+tc.path+tc.reason, func(t *testing.T) {
			w := massEmailReviewRequest(r, tc.method, tc.path, tc.body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			var envelope struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, tc.reason, envelope.Reason)
		})
	}
}

func TestMassEmailReviewPixelDoesNotDiscloseStorageFailure(t *testing.T) {
	store := newMassEmailReviewStore()
	store.openErr = errors.New("private database error")
	r := massEmailReviewRouter(service.NewMassEmailService(store, nil, nil, nil))
	for _, token := range []string{"invalid", strings.Repeat("a", 32)} {
		w := massEmailReviewRequest(r, "GET", "/api/v1/mail/o/"+token+".gif", "")
		require.Equal(t, 200, w.Code)
		require.Equal(t, massEmailTransparentGIF, w.Body.Bytes())
		require.Equal(t, "image/gif", w.Header().Get("Content-Type"))
		require.Equal(t, "0", w.Header().Get("Expires"))
		require.Equal(t, "no-cache", w.Header().Get("Pragma"))
		require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
	}
}

func TestMassEmailReviewCreateWorkerPixelEndToEnd(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	messages := make(chan string, 4)
	var serverWG sync.WaitGroup
	serverWG.Add(1)
	go func() {
		defer serverWG.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			serveMassEmailReviewSMTP(conn, messages)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); serverWG.Wait() })
	settings := &massEmailReviewSettings{values: map[string]string{
		service.SettingKeySMTPHost: "127.0.0.1", service.SettingKeySMTPPort: fmt.Sprint(listener.Addr().(*net.TCPAddr).Port),
		service.SettingKeySMTPUsername: "local", service.SettingKeySMTPPassword: "local", service.SettingKeySMTPFrom: "sender@example.com",
		service.SettingKeySMTPUseTLS: "false", service.SettingKeyFrontendURL: "https://site.example", service.SettingKeySiteName: "Review & Site",
	}}
	store := newMassEmailReviewStore()
	email := service.NewEmailService(settings, nil)
	svc := service.NewMassEmailService(store, email, service.NewSettingService(settings, nil), settings)
	router := massEmailReviewRouter(svc)
	created := massEmailReviewRequest(router, "POST", "/mass-emails", `{"subject":"Notice {{email}}","body_html":"<p>Hello {{email}} from {{site_name}}</p><script>bad()</script>","recipient_filter":{"manual_emails":["Alice@Example.com,alice@example.com"]},"send_now":true}`)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.NotContains(t, created.Body.String(), "tracking_token")
	var response struct {
		Data struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Total  int    `json:"total_count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
	require.Equal(t, "sending", response.Data.Status)
	require.Equal(t, 1, response.Data.Total)
	lock := &massEmailReviewLock{}
	runner := service.NewMassEmailRunner(store, svc, email, lock, nil)
	peer := service.NewMassEmailRunner(store, svc, email, lock, nil)
	runner.Start()
	defer runner.Stop()
	peer.Start()
	defer peer.Stop()
	var raw string
	select {
	case raw = <-messages:
	case <-time.After(9 * time.Second):
		t.Fatal("worker did not deliver to loopback SMTP")
	}
	message, err := mail.ReadMessage(strings.NewReader(raw))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Notice alice@example.com", subject)
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	require.NoError(t, err)
	require.Contains(t, string(body), "Hello alice@example.com from Review &amp; Site")
	require.NotContains(t, string(body), "script")
	require.Eventually(t, func() bool {
		c, _ := store.GetCampaign(context.Background(), response.Data.ID)
		return c.Status == "completed" && c.SentCount == 1
	}, time.Second, 10*time.Millisecond)
	store.mu.Lock()
	token := store.recipients[response.Data.ID][0].TrackingToken
	store.mu.Unlock()
	require.Contains(t, string(body), "https://site.example/api/v1/mail/o/"+token+".gif")
	for i := 0; i < 2; i++ {
		pixel := massEmailReviewRequest(router, "GET", "/api/v1/mail/o/"+token+".gif", "")
		require.Equal(t, 200, pixel.Code)
		require.Equal(t, massEmailTransparentGIF, pixel.Body.Bytes())
	}
	detail := massEmailReviewRequest(router, "GET", fmt.Sprintf("/mass-emails/%d", response.Data.ID), "")
	require.Equal(t, 200, detail.Code)
	require.NotContains(t, detail.Body.String(), "tracking_token")
	var opened struct {
		Data struct {
			Stats struct {
				Sent, Opened int
				OpenRate     float64 `json:"open_rate"`
			} `json:"stats"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &opened))
	require.Equal(t, 1, opened.Data.Stats.Sent)
	require.Equal(t, 1, opened.Data.Stats.Opened)
	require.Equal(t, 1.0, opened.Data.Stats.OpenRate)
	store.mu.Lock()
	recipient := store.recipients[response.Data.ID][0]
	store.mu.Unlock()
	require.Equal(t, 2, recipient.OpenCount)
	require.NotNil(t, recipient.FirstOpenedAt)
	require.NotNil(t, recipient.LastOpenedAt)
	select {
	case <-messages:
		t.Fatal("duplicate delivery")
	default:
	}
}

func serveMassEmailReviewSMTP(conn net.Conn, messages chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(conn)
	_, _ = fmt.Fprint(conn, "220 localhost ESMTP\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			_, _ = fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(command, "AUTH"):
			_, _ = fmt.Fprint(conn, "235 authenticated\r\n")
		case command == "DATA":
			_, _ = fmt.Fprint(conn, "354 continue\r\n")
			var body strings.Builder
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(line) == "." {
					break
				}
				body.WriteString(line)
			}
			messages <- body.String()
			_, _ = fmt.Fprint(conn, "250 accepted\r\n")
		case command == "QUIT":
			_, _ = fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			_, _ = fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}

// This opt-in test must point to a fresh, disposable database. It exercises real
// SQL rather than sqlmock, including the migration and the atomic edit path.
func TestMassEmailReviewPostgresRepository(t *testing.T) {
	dsn := os.Getenv("MASS_EMAIL_REVIEW_TEST_DSN")
	if dsn == "" {
		t.Skip("set MASS_EMAIL_REVIEW_TEST_DSN to a disposable local PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	migration, err := os.ReadFile("../../../migrations/266_mass_email_campaigns.sql")
	require.NoError(t, err)
	// The feature migration must also work without any pre-existing user or
	// enterprise tables. Rollback removes the isolated schema and both tables.
	emptyTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = emptyTx.Rollback() })
	_, err = emptyTx.ExecContext(ctx, `CREATE SCHEMA mass_email_review_empty; SET LOCAL search_path TO mass_email_review_empty`)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = emptyTx.ExecContext(ctx, string(migration))
		require.NoError(t, err, "feature migration must work in an empty schema and be repeatable")
	}
	require.NoError(t, emptyTx.Rollback())
	// Only the selected user display fields are needed by this feature.
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (id BIGINT PRIMARY KEY, email TEXT, username TEXT, deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(migration))
		if err != nil {
			_ = tx.Rollback()
		}
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := repository.NewMassEmailRepository(client, db)
	svc := service.NewMassEmailService(repo, nil, nil, nil)
	campaign, err := svc.Create(ctx, 7, service.MassEmailCreateInput{
		Subject: "SQL create", BodyHTML: "<p>Hello</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{ManualEmails: []string{"Alice@Example.com,alice@example.com", "bob@example.com"}},
	})
	require.NoError(t, err, "real SQL must insert the same number of values as target columns")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM mass_email_campaign WHERE id=$1", campaign.ID)
	})
	require.Equal(t, 2, campaign.TotalCount)
	claimed, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	secondClaim, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Empty(t, secondClaim, "a second claimant must not take active leases")
	recovered, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now().Add(domain.MassEmailClaimLease+time.Second))
	require.NoError(t, err)
	require.Len(t, recovered, 2, "after a restart the expired leases must become claimable")
	for _, row := range recovered {
		require.Equal(t, 2, row.Attempts)
	}
	_, err = repo.ReleaseClaimedRecipients(ctx, campaign.ID, []int64{claimed[0].ID, claimed[1].ID})
	require.NoError(t, err)
	updated, err := svc.Update(ctx, campaign.ID, service.MassEmailUpdateInput{
		Subject: "SQL atomic edit", BodyHTML: "<p>Changed</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{ManualEmails: []string{"new@example.com"}},
	})
	require.NoError(t, err, "recipient_filter JSON must be converted to a SQL driver value")
	require.Equal(t, "SQL atomic edit", updated.Subject)
	require.Equal(t, 1, updated.TotalCount)
	// A failed recipient insert must roll back the content update and deletion
	// in the same transaction. A token beyond the schema limit forces that path.
	invalidUpdate := *updated
	invalidUpdate.Subject = "must roll back"
	_, err = repo.UpdateCampaignWithRecipients(ctx, &invalidUpdate, []domain.MassEmailRecipient{{
		Email: "rollback@example.com", TrackingToken: strings.Repeat("x", 33),
	}})
	require.Error(t, err)
	afterRollback, err := repo.GetCampaign(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, "SQL atomic edit", afterRollback.Subject)
	claimed, err = repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "new@example.com", claimed[0].Email)
	require.NoError(t, repo.MarkRecipientSent(ctx, campaign.ID, claimed[0].ID, time.Now()))
	svc.RecordOpen(ctx, claimed[0].TrackingToken)
	svc.RecordOpen(ctx, claimed[0].TrackingToken)
	detail, err := svc.Get(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, 1, detail.Stats.Sent)
	require.Equal(t, 1, detail.Stats.Opened)
	recipient, err := repo.GetRecipient(ctx, campaign.ID, claimed[0].ID)
	require.NoError(t, err)
	require.Equal(t, 2, recipient.OpenCount)
	// An edit racing with a send transition must leave recipients untouched.
	_, err = repo.TransitionCampaignStatus(ctx, campaign.ID, []string{"draft"}, "sending", time.Now())
	require.NoError(t, err)
	deleted, err := repo.DeleteCampaignIfNotSending(ctx, campaign.ID)
	require.NoError(t, err)
	require.False(t, deleted, "the SQL delete must refuse a campaign that has started sending")
	updated.Status = "draft"
	ok, err := repo.UpdateCampaignWithRecipients(ctx, updated, []domain.MassEmailRecipient{{Email: "wrong@example.com", TrackingToken: strings.Repeat("f", 32)}})
	require.NoError(t, err)
	require.False(t, ok)
	stillSent, err := repo.GetRecipient(ctx, campaign.ID, claimed[0].ID)
	require.NoError(t, err)
	require.Equal(t, "sent", stillSent.Status)
}
