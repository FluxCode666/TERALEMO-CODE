package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// newMassEmailRepositoryTestRepo 构造只走原生 SQL 的仓储实例（ent 客户端用于谓词构造，单测里不查库）。
func newMassEmailRepositoryTestRepo(t *testing.T) (*massEmailRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return &massEmailRepository{client: client, db: db}, mock
}

func massEmailRecipientRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "campaign_id", "email", "name", "user_id", "enterprise_id", "status", "error",
		"attempts", "claimed_at", "sent_at", "tracking_token", "first_opened_at", "last_opened_at",
		"open_count", "created_at", "updated_at",
	})
}

func TestMassEmailClaimPendingRecipientsUsesLeaseAndSkipLocked(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	now := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	lease := 5 * time.Minute
	claimedAt := now
	rows := massEmailRecipientRows().AddRow(
		11, 3, "alice@example.com", "Alice", 88, nil, "pending", "",
		1, claimedAt, nil, "token-11", nil, nil, 0, now, now,
	)
	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE SKIP LOCKED")).
		WithArgs(int64(3), now, now.Add(-lease), 20).
		WillReturnRows(rows)

	out, err := repo.ClaimPendingRecipients(context.Background(), 3, 20, lease, now)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, int64(11), out[0].ID)
	require.Equal(t, 1, out[0].Attempts)
	require.NotNil(t, out[0].ClaimedAt)
	require.Equal(t, "alice@example.com", out[0].Email)
	require.NotNil(t, out[0].UserID)
	require.Equal(t, int64(88), *out[0].UserID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailClaimPendingRecipientsEmpty(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE SKIP LOCKED")).
		WithArgs(int64(3), sqlmock.AnyArg(), sqlmock.AnyArg(), 20).
		WillReturnRows(massEmailRecipientRows())

	out, err := repo.ClaimPendingRecipients(context.Background(), 3, 0, 0, now)
	require.NoError(t, err)
	require.Empty(t, out)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailMarkRecipientSentClearsClaim(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	sentAt := time.Date(2026, 1, 2, 9, 0, 12, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`SET status = 'sent', sent_at = $3, claimed_at = NULL`)).
		WithArgs(int64(3), int64(11), sentAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.MarkRecipientSent(context.Background(), 3, 11, sentAt))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailMarkRecipientSentMissingRecipient(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta(`SET status = 'sent'`)).
		WithArgs(int64(3), int64(11), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.MarkRecipientSent(context.Background(), 3, 11, time.Now())
	require.ErrorIs(t, err, domain.ErrMassEmailRecipientNotFound)
}

func TestMassEmailMarkRecipientFailedTruncatesReason(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	long := make([]byte, 0, 5000)
	for i := 0; i < 5000; i++ {
		long = append(long, 'x')
	}
	failedAt := time.Date(2026, 1, 2, 9, 0, 12, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`SET status = 'failed', "error" = $3`)).
		WithArgs(int64(3), int64(11), sqlmock.AnyArg(), failedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.MarkRecipientFailed(context.Background(), 3, 11, string(long), failedAt))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailResetFailedRecipients(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta(`WHERE campaign_id = $1 AND status = 'failed'`)).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 4))

	reset, err := repo.ResetFailedRecipients(context.Background(), 3, nil)
	require.NoError(t, err)
	require.Equal(t, 4, reset)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailResetFailedRecipientsSingle(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	recipientID := int64(11)
	mock.ExpectExec(regexp.QuoteMeta(`AND id = $2`)).
		WithArgs(int64(3), recipientID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	reset, err := repo.ResetFailedRecipients(context.Background(), 3, &recipientID)
	require.NoError(t, err)
	require.Equal(t, 1, reset)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailRecordRecipientOpenIncrementsCounter(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	openedAt := time.Date(2026, 1, 2, 9, 41, 0, 0, time.UTC)
	firstOpened := openedAt
	rows := massEmailRecipientRows().AddRow(
		11, 3, "alice@example.com", "Alice", nil, nil, "sent", "",
		1, nil, openedAt, "token-11", firstOpened, openedAt, 1, openedAt, openedAt,
	)
	mock.ExpectQuery(regexp.QuoteMeta("open_count = open_count + 1")).
		WithArgs("token-11", openedAt).
		WillReturnRows(rows)

	recipient, err := repo.RecordRecipientOpen(context.Background(), "token-11", openedAt)
	require.NoError(t, err)
	require.Equal(t, 1, recipient.OpenCount)
	require.NotNil(t, recipient.FirstOpenedAt)
	require.NotNil(t, recipient.LastOpenedAt)
	require.True(t, recipient.IsOpened())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailRecordRecipientOpenUnknownToken(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectQuery(regexp.QuoteMeta("open_count = open_count + 1")).
		WithArgs("missing", sqlmock.AnyArg()).
		WillReturnRows(massEmailRecipientRows())

	_, err := repo.RecordRecipientOpen(context.Background(), "missing", time.Now())
	require.ErrorIs(t, err, domain.ErrMassEmailRecipientNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailRecordRecipientOpenRejectsEmptyToken(t *testing.T) {
	repo, _ := newMassEmailRepositoryTestRepo(t)
	_, err := repo.RecordRecipientOpen(context.Background(), "   ", time.Now())
	require.ErrorIs(t, err, domain.ErrMassEmailRecipientNotFound)
}

func TestMassEmailRecountCampaign(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(*) FILTER (WHERE open_count > 0)")).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"total", "pending", "sent", "failed", "opened"}).
			AddRow(120, 17, 100, 3, 41))

	stats, err := repo.RecountCampaign(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, domain.MassEmailCampaignStats{Total: 120, Pending: 17, Sent: 100, Failed: 3, Opened: 41}, *stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailRecountCampaignMissing(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(*) FILTER (WHERE open_count > 0)")).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"total", "pending", "sent", "failed", "opened"}))

	_, err := repo.RecountCampaign(context.Background(), 3)
	require.ErrorIs(t, err, domain.ErrMassEmailCampaignNotFound)
}

func TestMassEmailListRecipientsBuildsFilters(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	opened := true
	filters := domain.MassEmailRecipientListFilters{
		Statuses: []string{"sent", "unknown"},
		Opened:   &opened,
		Search:   "Ali",
	}
	params := pagination.PaginationParams{Page: 2, PageSize: 10}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM mass_email_recipient")).
		WithArgs(int64(3), sqlmock.AnyArg(), "%ali%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	rows := massEmailRecipientRows().AddRow(
		11, 3, "alice@example.com", "Alice", nil, nil, "sent", "",
		1, nil, nil, "token-11", nil, nil, 2, time.Now(), time.Now(),
	)
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY id ASC")).
		WithArgs(int64(3), sqlmock.AnyArg(), "%ali%", 10, 10).
		WillReturnRows(rows)

	items, result, err := repo.ListRecipients(context.Background(), 3, params, filters)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(1), result.Total)
	require.Equal(t, 2, result.Page)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailListRecipientsUnopenedFilter(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	unopened := false
	mock.ExpectQuery(regexp.QuoteMeta("open_count = 0")).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta("open_count = 0")).
		WithArgs(int64(3), 20, 0).
		WillReturnRows(massEmailRecipientRows())

	items, result, err := repo.ListRecipients(context.Background(), 3, pagination.PaginationParams{Page: 1, PageSize: 20}, domain.MassEmailRecipientListFilters{Opened: &unopened})
	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, int64(0), result.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailReplaceRecipientsRunsInTransaction(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM mass_email_recipient WHERE campaign_id = $1")).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO mass_email_recipient")).
		WithArgs(
			int64(3), "alice@example.com", "Alice", (*int64)(nil), (*int64)(nil), "pending", "token-a", sqlmock.AnyArg(), sqlmock.AnyArg(),
			int64(3), "bob@example.com", "", (*int64)(nil), (*int64)(nil), "pending", "token-b", sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE mass_email_campaign")).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	inserted, err := repo.ReplaceRecipients(context.Background(), 3, []domain.MassEmailRecipient{
		{Email: "Alice@Example.com", Name: "Alice", TrackingToken: "token-a"},
		{Email: "alice@example.com", Name: "Duplicate", TrackingToken: "token-dup"},
		{Email: "bob@example.com", TrackingToken: "token-b"},
		{Email: "not-an-email", TrackingToken: "token-bad"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailInsertRecipientsIsIdempotentOnConflict(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta("ON CONFLICT DO NOTHING")).
		WithArgs(int64(3), "alice@example.com", "", (*int64)(nil), (*int64)(nil), "pending", "token-a", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	inserted, err := repo.InsertRecipients(context.Background(), 3, []domain.MassEmailRecipient{
		{Email: "ALICE@example.com", TrackingToken: "token-a"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailGetRecipientNotFound(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectQuery(regexp.QuoteMeta("WHERE campaign_id = $1 AND id = $2")).
		WithArgs(int64(3), int64(99)).
		WillReturnRows(massEmailRecipientRows())

	_, err := repo.GetRecipient(context.Background(), 3, 99)
	require.ErrorIs(t, err, domain.ErrMassEmailRecipientNotFound)
}

func TestMassEmailGetRecipientScanNulls(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	rows := massEmailRecipientRows().AddRow(
		11, 3, "alice@example.com", "", nil, int64(4), "failed", "smtp timeout",
		3, nil, nil, "token-11", nil, nil, 0, time.Now(), time.Now(),
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE campaign_id = $1 AND id = $2")).
		WithArgs(int64(3), int64(11)).
		WillReturnRows(rows)

	recipient, err := repo.GetRecipient(context.Background(), 3, 11)
	require.NoError(t, err)
	require.Nil(t, recipient.UserID)
	require.NotNil(t, recipient.EnterpriseID)
	require.Equal(t, int64(4), *recipient.EnterpriseID)
	require.Equal(t, "smtp timeout", recipient.Error)
	require.Equal(t, 3, recipient.Attempts)
}

func TestMassEmailTransitionCampaignStatusCAS(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	now := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta("WHERE id = $1 AND status IN ($4::varchar, $5::varchar)")).
		WithArgs(int64(3), "sending", now, "draft", "scheduled").
		WillReturnResult(sqlmock.NewResult(0, 1))

	ok, err := repo.TransitionCampaignStatus(context.Background(), 3, []string{"draft", "scheduled"}, "sending", now)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailTransitionCampaignStatusLosesRace(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	now := time.Now()
	mock.ExpectExec(regexp.QuoteMeta("WHERE id = $1 AND status IN ($4::varchar)")).
		WithArgs(int64(3), "sending", now, "draft").
		WillReturnResult(sqlmock.NewResult(0, 0))

	ok, err := repo.TransitionCampaignStatus(context.Background(), 3, []string{"draft"}, "sending", now)
	require.NoError(t, err)
	require.False(t, ok, "状态已被其它实例改动时不应重复触发")
}

func TestMassEmailTransitionCampaignStatusRejectsUnknownTarget(t *testing.T) {
	repo, _ := newMassEmailRepositoryTestRepo(t)
	_, err := repo.TransitionCampaignStatus(context.Background(), 3, []string{"draft"}, "bogus", time.Now())
	require.ErrorIs(t, err, domain.ErrMassEmailInvalidFilter)
}

func TestMassEmailRepositoryErrorsAreWrapped(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	boom := errors.New("db down")
	mock.ExpectExec(regexp.QuoteMeta("SET status = 'sent'")).
		WithArgs(int64(3), int64(11), sqlmock.AnyArg()).
		WillReturnError(boom)

	err := repo.MarkRecipientSent(context.Background(), 3, 11, time.Now())
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, sql.ErrNoRows)
}

// --- 纯函数 ---

func TestMassEmailCampaignListOrders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params pagination.PaginationParams
		check  func(t *testing.T, orders []func(*entsql.Selector))
	}{
		{name: "default created_at desc", params: pagination.PaginationParams{}, check: func(t *testing.T, orders []func(*entsql.Selector)) {
			require.Len(t, orders, 2)
		}},
		{name: "scheduled_at asc", params: pagination.PaginationParams{SortBy: "scheduled_at", SortOrder: "asc"}, check: func(t *testing.T, orders []func(*entsql.Selector)) {
			require.Len(t, orders, 2)
		}},
		{name: "id asc single order", params: pagination.PaginationParams{SortBy: "id", SortOrder: "ASC"}, check: func(t *testing.T, orders []func(*entsql.Selector)) {
			require.Len(t, orders, 1)
		}},
		{name: "unknown field falls back", params: pagination.PaginationParams{SortBy: "nope", SortOrder: "asc"}, check: func(t *testing.T, orders []func(*entsql.Selector)) {
			require.Len(t, orders, 2)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, massEmailCampaignListOrders(tt.params))
		})
	}
}

func TestMassEmailValidCampaignStatuses(t *testing.T) {
	t.Parallel()
	require.Nil(t, massEmailValidCampaignStatuses(nil))
	require.Equal(t,
		[]string{domain.MassEmailStatusDraft, domain.MassEmailStatusSending},
		massEmailValidCampaignStatuses([]string{" draft ", "DRAFT", "sending", "bogus", ""}),
	)
}

func TestMassEmailValidRecipientStatuses(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		[]string{domain.MassEmailRecipientStatusFailed, domain.MassEmailRecipientStatusPending, domain.MassEmailRecipientStatusSent},
		massEmailValidRecipientStatuses([]string{"failed", "pending", "sent ", "nope"}),
	)
}

func TestNormalizeMassEmailRecipients(t *testing.T) {
	t.Parallel()

	userID := int64(9)
	out := normalizeMassEmailRecipients([]domain.MassEmailRecipient{
		{Email: " Alice@Example.com ", Name: "  Alice  ", UserID: &userID},
		{Email: "alice@example.com", Name: "dup"},
		{Email: "bad", Name: "bad"},
		{Email: "bob@example.com", Status: "weird"},
	})
	require.Len(t, out, 2)
	require.Equal(t, "alice@example.com", out[0].Email)
	require.Equal(t, "Alice", out[0].Name)
	require.Equal(t, domain.MassEmailRecipientStatusPending, out[0].Status)
	require.Equal(t, int64(9), *out[0].UserID)
	require.Equal(t, "bob@example.com", out[1].Email)
	require.Equal(t, domain.MassEmailRecipientStatusPending, out[1].Status, "非法状态回落到 pending")
}

func TestMassEmailDisplayName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Alice", massEmailDisplayName("Alice", "alice@example.com"))
	require.Equal(t, "alice", massEmailDisplayName("  ", " Alice@Example.com "))
	require.Equal(t, "no-at-sign", massEmailDisplayName("", "no-at-sign"))
}

func TestMassEmailPositiveIDs(t *testing.T) {
	t.Parallel()
	require.Nil(t, massEmailPositiveIDs(nil))
	require.Equal(t, []int64{2, 5}, massEmailPositiveIDs([]int64{5, 0, 2, -3, 5}))
}

func TestTruncateMassEmailError(t *testing.T) {
	t.Parallel()
	require.Equal(t, "ok", truncateMassEmailError("ok"))
	require.Len(t, []rune(truncateMassEmailError(string(make([]rune, 0)))), 0)

	long := make([]rune, 0, 3000)
	for i := 0; i < 3000; i++ {
		long = append(long, '错')
	}
	require.Equal(t, 2000, len([]rune(truncateMassEmailError(string(long)))))
}

func TestMassEmailCreatedByIDs(t *testing.T) {
	t.Parallel()

	a, b := int64(1), int64(1)
	c := int64(3)
	ids := massEmailCreatedByIDs([]domain.MassEmailCampaign{
		{CreatedBy: &a}, {CreatedBy: &b}, {CreatedBy: nil}, {CreatedBy: &c},
	})
	require.Equal(t, []int64{1, 3}, ids)
}

// capturedStatements 返回一个宽松匹配的 sqlmock，把所有实际执行的 SQL 记录下来，
// 便于对 ent 生成语句做断言（ent 的 UPDATE 语句细节会随版本变化，直接匹配原文太脆）。
func capturedStatements(t *testing.T) (*massEmailRepository, sqlmock.Sqlmock, *[]string) {
	t.Helper()
	captured := &[]string{}
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		*captured = append(*captured, actual)
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return &massEmailRepository{client: client, db: db}, mock, captured
}

func hasStatement(captured []string, fragment string) bool {
	for _, statement := range captured {
		if strings.Contains(statement, fragment) {
			return true
		}
	}
	return false
}

func TestMassEmailUpdateCampaignClearsScheduledAtWhenNil(t *testing.T) {
	repo, mock, captured := capturedStatements(t)

	// 前端"存草稿"提交 scheduled_at=null：ent 的 SetNillableScheduledAt(nil) 是 no-op，
	// 仓储必须显式 ClearScheduledAt，否则会出现「没有 scheduled_at 却卡在 scheduled」的任务。
	mock.ExpectBegin()
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("").WillReturnError(errors.New("stop after update"))
	mock.ExpectRollback()

	err := repo.UpdateCampaign(context.Background(), &domain.MassEmailCampaign{
		ID:      3,
		Subject: "重要通知",
		Status:  domain.MassEmailStatusScheduled,
	})
	require.Error(t, err, "后续 SELECT 被故意打断，这里只断言 UPDATE 语句")

	require.True(t, hasStatement(*captured, `"scheduled_at" = NULL`),
		"scheduled_at=null 时必须真正清空定时，实际语句：%v", *captured)
}

func TestMassEmailUpdateCampaignSetsScheduledAtWhenProvided(t *testing.T) {
	repo, mock, captured := capturedStatements(t)

	mock.ExpectBegin()
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("").WillReturnError(errors.New("stop after update"))
	mock.ExpectRollback()

	scheduledAt := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	err := repo.UpdateCampaign(context.Background(), &domain.MassEmailCampaign{
		ID:          3,
		Subject:     "重要通知",
		ScheduledAt: &scheduledAt,
	})
	require.Error(t, err)

	require.True(t, hasStatement(*captured, `"scheduled_at" = $`),
		"提供了 scheduled_at 时应写入参数，实际语句：%v", *captured)
	require.False(t, hasStatement(*captured, `"scheduled_at" = NULL`),
		"提供了 scheduled_at 时不能清空")
}

// jsonArg matches a database/sql argument that must be an exact JSON string.
// #49: domain.MassEmailRecipientFilter 只实现了 MarshalJSON，不是 driver.Valuer，
// 必须由仓储显式序列化成字符串再传参，否则 lib/pq 会报 unsupported type。
type jsonArg struct{ want string }

func (j jsonArg) Match(value driver.Value) bool {
	switch typed := value.(type) {
	case string:
		return typed == j.want
	case []byte:
		return string(typed) == j.want
	default:
		return false
	}
}

func TestMassEmailUpdateCampaignWithRecipientsSerializesFilter(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	scheduledAt := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	campaign := &domain.MassEmailCampaign{
		ID:       3,
		Subject:  "新主题",
		BodyHTML: "<p>new</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{
			IncludeAllUsers: true,
			ManualEmails:    []string{"ops@example.com"},
		},
		ScheduledAt: &scheduledAt,
		Status:      domain.MassEmailStatusScheduled,
	}
	wantFilter := `{"include_all_users":true,"user_filter":{},"enterprise_filter":{},"manual_emails":["ops@example.com"]}`

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`WHERE id = $1 AND status IN ('draft', 'scheduled')`)).
		WithArgs(
			int64(3),
			"新主题",
			"<p>new</p>",
			jsonArg{want: wantFilter},
			scheduledAt,
			string(domain.MassEmailStatusScheduled),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM mass_email_recipient WHERE campaign_id = $1")).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO mass_email_recipient")).
		WithArgs(
			int64(3), "alice@example.com", "", (*int64)(nil), (*int64)(nil), "pending", "token-a", sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("total_count = (SELECT COUNT(*)::int FROM mass_email_recipient")).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ok, err := repo.UpdateCampaignWithRecipients(context.Background(), campaign, []domain.MassEmailRecipient{
		{Email: "alice@example.com", TrackingToken: "token-a"},
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailUpdateCampaignWithRecipientsCASRejectsAdvancedCampaign(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	campaign := &domain.MassEmailCampaign{
		ID:       3,
		Subject:  "新主题",
		BodyHTML: "<p>new</p>",
		Status:   domain.MassEmailStatusDraft,
	}

	mock.ExpectBegin()
	// CAS 命中 0 行：任务已被 worker 推进，整笔事务回滚，什么都不改。
	mock.ExpectExec(regexp.QuoteMeta("WHERE id = $1 AND status IN ('draft', 'scheduled')")).
		WithArgs(int64(3), "新主题", "<p>new</p>", sqlmock.AnyArg(), (*time.Time)(nil), string(domain.MassEmailStatusDraft)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	ok, err := repo.UpdateCampaignWithRecipients(context.Background(), campaign, nil)
	require.NoError(t, err)
	require.False(t, ok, "CAS 失败必须返回 false 让 service 转 409")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailReleaseClaimedRecipientsRollsBackAttempts(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta("attempts = GREATEST(attempts - 1, 0)")).
		WithArgs(int64(3), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))

	released, err := repo.ReleaseClaimedRecipients(context.Background(), 3, []int64{11, 12})
	require.NoError(t, err)
	require.Equal(t, 2, released)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailReleaseClaimedRecipientsEmptyIsNoop(t *testing.T) {
	repo, _ := newMassEmailRepositoryTestRepo(t)
	released, err := repo.ReleaseClaimedRecipients(context.Background(), 3, nil)
	require.NoError(t, err)
	require.Equal(t, 0, released)
}

// TestMassEmailInsertRowMatchesColumnCount 是 #50 的防回归：
// INSERT 目标列数必须与每行的占位符数、参数个数完全一致。
// 线上曾经出现「9 列 8 值」，PG 直接报 "INSERT has more target columns than expressions"。
func TestMassEmailInsertRowMatchesColumnCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	placeholder, args := buildMassEmailRecipientInsertRow(0, 3, domain.MassEmailRecipient{
		Email:         "alice@example.com",
		Name:          "Alice",
		Status:        domain.MassEmailRecipientStatusPending,
		TrackingToken: "token-a",
	}, now)

	require.Len(t, args, len(massEmailRecipientInsertColumns), "参数个数必须等于列数")
	require.Equal(t, len(massEmailRecipientInsertColumns), strings.Count(placeholder, "$"), "占位符个数必须等于列数")

	// created_at / updated_at 都落到时间上，且都非空。
	require.Equal(t, now, args[7])
	require.Equal(t, now, args[8])

	// 多行时占位符编号连续，且不会出现 $0。
	second, secondArgs := buildMassEmailRecipientInsertRow(len(args), 3, domain.MassEmailRecipient{
		Email:  "bob@example.com",
		Status: domain.MassEmailRecipientStatusPending,
	}, now)
	require.Len(t, secondArgs, len(massEmailRecipientInsertColumns))
	require.True(t, strings.HasPrefix(second, "($10,"), "第二行应从 $10 开始，实际：%s", second)
	require.NotContains(t, placeholder+second, "$0")
}

// TestMassEmailInsertSQLColumnValueParity 直接检查拼出来的整条 INSERT 语句。
func TestMassEmailInsertSQLColumnValueParity(t *testing.T) {
	t.Parallel()

	rows := []domain.MassEmailRecipient{
		{Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending, TrackingToken: "t1"},
		{Email: "b@example.com", Status: domain.MassEmailRecipientStatusPending, TrackingToken: "t2"},
	}
	now := time.Now()
	args := make([]any, 0, len(rows)*len(massEmailRecipientInsertColumns))
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		placeholder, rowArgs := buildMassEmailRecipientInsertRow(len(args), 3, row, now)
		args = append(args, rowArgs...)
		values = append(values, placeholder)
	}

	// 与 insertMassEmailRecipients 里拼装的方式保持一致。
	columns := strings.Join(massEmailRecipientInsertColumns, ", ")
	query := "INSERT INTO mass_email_recipient (" + columns + ") VALUES " + strings.Join(values, ", ") + " ON CONFLICT DO NOTHING"

	// 每行占位符数 == 列数，总参数 == 行数 × 列数。
	require.Equal(t, len(rows)*len(massEmailRecipientInsertColumns), len(args))
	for _, row := range values {
		require.Equal(t, len(massEmailRecipientInsertColumns), strings.Count(row, "$"), row)
	}
	require.Contains(t, query, "tracking_token, created_at, updated_at")
	require.Contains(t, query, "$18", "两行共 18 个参数")
	require.NotContains(t, query, "$19")
}

func TestMassEmailDeleteCampaignIfNotSendingDeletesDraft(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM mass_email_campaign WHERE id = $1 AND status <> 'sending'`)).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, err := repo.DeleteCampaignIfNotSending(context.Background(), 3)
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMassEmailDeleteCampaignIfNotSendingRejectsSending(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	// 0 行删除（因为 status='sending'），随后存在性检查发现任务仍在 → 返回 false 让 service 转 409。
	mock.ExpectExec(regexp.QuoteMeta(`AND status <> 'sending'`)).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	rows := sqlmock.NewRows([]string{
		"id", "subject", "body_html", "recipient_filter", "status", "scheduled_at",
		"total_count", "sent_count", "failed_count", "opened_count", "created_by",
		"started_at", "finished_at", "last_error", "created_at", "updated_at",
	}).AddRow(3, "s", "<p>x</p>", []byte(`{}`), "sending", nil, 1, 0, 0, 0, nil, nil, nil, "", time.Now(), time.Now())
	mock.ExpectQuery(regexp.QuoteMeta(`FROM "mass_email_campaign"`)).
		WithArgs(int64(3)).
		WillReturnRows(rows)

	deleted, err := repo.DeleteCampaignIfNotSending(context.Background(), 3)
	require.NoError(t, err)
	require.False(t, deleted, "sending 中的任务不能被删除")
}

func TestMassEmailDeleteCampaignIfNotSendingMissing(t *testing.T) {
	repo, mock := newMassEmailRepositoryTestRepo(t)

	mock.ExpectExec(regexp.QuoteMeta(`AND status <> 'sending'`)).
		WithArgs(int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM "mass_email_campaign"`)).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := repo.DeleteCampaignIfNotSending(context.Background(), 9)
	require.ErrorIs(t, err, domain.ErrMassEmailCampaignNotFound)
}
