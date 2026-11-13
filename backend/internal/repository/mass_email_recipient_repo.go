package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

	"github.com/lib/pq"
)

// massEmailRecipientColumns 是原生 SQL 读取收件人时统一的列顺序，与 scanMassEmailRecipient 一一对应。
const massEmailRecipientColumns = `id, campaign_id, email, name, user_id, enterprise_id, status, "error", attempts,
	claimed_at, sent_at, tracking_token, first_opened_at, last_opened_at, open_count, created_at, updated_at`

// massEmailRecipientInsertBatchSize 单条 INSERT 的最大行数，避免参数个数超限。
const massEmailRecipientInsertBatchSize = 200

// massEmailMaxErrorLen 单个收件人错误文本上限。
const massEmailMaxErrorLen = 2000

func (r *massEmailRepository) ReplaceRecipients(ctx context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error) {
	if campaignID <= 0 {
		return 0, domain.ErrMassEmailCampaignNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM mass_email_recipient WHERE campaign_id = $1`, campaignID); err != nil {
		return 0, err
	}
	inserted, err := insertMassEmailRecipients(ctx, tx, campaignID, recipients)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mass_email_campaign
		SET total_count = (SELECT COUNT(*)::int FROM mass_email_recipient WHERE campaign_id = $1),
		    updated_at = NOW()
		WHERE id = $1`, campaignID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *massEmailRepository) InsertRecipients(ctx context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error) {
	if campaignID <= 0 {
		return 0, domain.ErrMassEmailCampaignNotFound
	}
	return insertMassEmailRecipients(ctx, r.db, campaignID, recipients)
}

// massEmailRecipientInsertColumns 是批量 INSERT 的目标列顺序。
//
// 行的占位符与参数由 buildMassEmailRecipientInsertRow 按这个切片生成，
// 保证「列数 == 占位符数 == 参数个数」永远一致（曾经出现过 9 列 8 值的线上事故）。
var massEmailRecipientInsertColumns = []string{
	"campaign_id",
	"email",
	"name",
	"user_id",
	"enterprise_id",
	"status",
	"tracking_token",
	"created_at",
	"updated_at",
}

// buildMassEmailRecipientInsertRow 生成一行的 `($n, $n+1, ...)` 片段与该行的参数。
//
// base 是该行第一个参数在整条 INSERT 中的偏移（0 基）。
// 参数个数必须与 massEmailRecipientInsertColumns 完全一致，有单测守着。
func buildMassEmailRecipientInsertRow(
	base int,
	campaignID int64,
	recipient domain.MassEmailRecipient,
	now time.Time,
) (string, []any) {
	values := []any{
		campaignID,
		recipient.Email,
		recipient.Name,
		recipient.UserID,
		recipient.EnterpriseID,
		recipient.Status,
		recipient.TrackingToken,
		now,
		now,
	}
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = "$" + strconv.Itoa(base+i+1)
	}
	return "(" + strings.Join(placeholders, ", ") + ")", values
}

// insertMassEmailRecipients 批量插入收件人。
//
// 去重完全交给数据库的表达式唯一索引 (campaign_id, lower(email))：
// ON CONFLICT DO NOTHING 同时覆盖邮箱重复与 tracking_token 重复两种情况。
func insertMassEmailRecipients(ctx context.Context, exec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, campaignID int64, recipients []domain.MassEmailRecipient) (int, error) {
	if len(recipients) == 0 {
		return 0, nil
	}

	normalized := normalizeMassEmailRecipients(recipients)
	if len(normalized) == 0 {
		return 0, nil
	}

	total := 0
	for start := 0; start < len(normalized); start += massEmailRecipientInsertBatchSize {
		end := start + massEmailRecipientInsertBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		batch := normalized[start:end]

		now := time.Now()
		args := make([]any, 0, len(batch)*len(massEmailRecipientInsertColumns))
		values := make([]string, 0, len(batch))
		for _, recipient := range batch {
			placeholder, rowArgs := buildMassEmailRecipientInsertRow(len(args), campaignID, recipient, now)
			args = append(args, rowArgs...)
			values = append(values, placeholder)
		}

		query := `INSERT INTO mass_email_recipient
			(` + strings.Join(massEmailRecipientInsertColumns, ", ") + `)
			VALUES ` + strings.Join(values, ", ") + `
			ON CONFLICT DO NOTHING`
		result, err := exec.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(affected)
	}
	return total, nil
}

// normalizeMassEmailRecipients 统一邮箱小写、按邮箱去重、补齐默认状态与时间戳。
func normalizeMassEmailRecipients(recipients []domain.MassEmailRecipient) []domain.MassEmailRecipient {
	out := make([]domain.MassEmailRecipient, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		email := domain.NormalizeEmail(recipient.Email)
		if !domain.IsValidMassEmailAddress(email) {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}

		recipient.Email = email
		recipient.Name = truncateMassEmailName(recipient.Name)
		if recipient.Status == "" {
			recipient.Status = domain.MassEmailRecipientStatusPending
		}
		if !domain.IsValidMassEmailRecipientStatus(recipient.Status) {
			recipient.Status = domain.MassEmailRecipientStatusPending
		}
		out = append(out, recipient)
	}
	return out
}

func truncateMassEmailName(name string) string {
	name = strings.TrimSpace(strings.ToValidUTF8(name, "�"))
	if len([]rune(name)) <= domain.MassEmailNameMaxLen {
		return name
	}
	return string([]rune(name)[:domain.MassEmailNameMaxLen])
}

func (r *massEmailRepository) ListRecipients(
	ctx context.Context,
	campaignID int64,
	params pagination.PaginationParams,
	filters domain.MassEmailRecipientListFilters,
) ([]domain.MassEmailRecipient, *pagination.PaginationResult, error) {
	where := []string{"campaign_id = $1"}
	args := []any{campaignID}

	if statuses := massEmailValidRecipientStatuses(filters.Statuses); len(statuses) > 0 {
		args = append(args, pq.Array(statuses))
		where = append(where, fmt.Sprintf("status = ANY($%d)", len(args)))
	}
	if filters.Opened != nil {
		if *filters.Opened {
			where = append(where, "open_count > 0")
		} else {
			where = append(where, "open_count = 0")
		}
	}
	if search := strings.TrimSpace(filters.Search); search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		where = append(where, fmt.Sprintf("(lower(email) LIKE $%d OR lower(name) LIKE $%d)", len(args), len(args)))
	}

	condition := strings.Join(where, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mass_email_recipient WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, nil, err
	}

	listArgs := append(append([]any{}, args...), params.Limit(), params.Offset())
	query := `SELECT ` + massEmailRecipientColumns + `
		FROM mass_email_recipient
		WHERE ` + condition + `
		ORDER BY id ASC
		LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)

	rows, err := r.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.MassEmailRecipient, 0, params.Limit())
	for rows.Next() {
		recipient, err := scanMassEmailRecipient(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, *recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, paginationResultFromTotal(total, params), nil
}

func (r *massEmailRepository) GetRecipient(ctx context.Context, campaignID, recipientID int64) (*domain.MassEmailRecipient, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+massEmailRecipientColumns+`
		 FROM mass_email_recipient WHERE campaign_id = $1 AND id = $2`, campaignID, recipientID)
	recipient, err := scanMassEmailRecipient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrMassEmailRecipientNotFound
		}
		return nil, err
	}
	return recipient, nil
}

// ClaimPendingRecipients 领取待发送收件人。
//
// 关键点：
//   - FOR UPDATE SKIP LOCKED 让多个实例/协程并发领取时互不阻塞、不重复；
//   - 租约（claimed_at + lease）保证进程崩溃后，超时的收件人能被重新领取，不会永久卡死；
//   - 领取即 attempts+1，便于排查"重复尝试"。
func (r *massEmailRepository) ClaimPendingRecipients(
	ctx context.Context,
	campaignID int64,
	limit int,
	lease time.Duration,
	now time.Time,
) ([]domain.MassEmailRecipient, error) {
	if limit <= 0 {
		limit = 20
	}
	if lease <= 0 {
		lease = domain.MassEmailClaimLease
	}
	if now.IsZero() {
		now = time.Now()
	}
	leaseCutoff := now.Add(-lease)

	rows, err := r.db.QueryContext(ctx, `
		UPDATE mass_email_recipient AS r
		SET claimed_at = $2,
		    attempts = r.attempts + 1,
		    updated_at = NOW()
		WHERE r.id IN (
			SELECT id FROM mass_email_recipient
			WHERE campaign_id = $1
			  AND status = 'pending'
			  AND (claimed_at IS NULL OR claimed_at < $3)
			ORDER BY id ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+massEmailRecipientColumns,
		campaignID, now, leaseCutoff, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.MassEmailRecipient, 0, limit)
	for rows.Next() {
		recipient, scanErr := scanMassEmailRecipient(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *massEmailRepository) ReleaseClaimedRecipients(ctx context.Context, campaignID int64, recipientIDs []int64) (int, error) {
	if len(recipientIDs) == 0 {
		return 0, nil
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE mass_email_recipient
		SET claimed_at = NULL,
		    attempts = GREATEST(attempts - 1, 0),
		    updated_at = NOW()
		WHERE campaign_id = $1 AND status = 'pending' AND id = ANY($2)`,
		campaignID, pq.Array(recipientIDs))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func (r *massEmailRepository) MarkRecipientSent(ctx context.Context, campaignID, recipientID int64, sentAt time.Time) error {
	if sentAt.IsZero() {
		sentAt = time.Now()
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE mass_email_recipient
		SET status = 'sent', sent_at = $3, claimed_at = NULL, "error" = '', updated_at = NOW()
		WHERE campaign_id = $1 AND id = $2`, campaignID, recipientID, sentAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrMassEmailRecipientNotFound
	}
	return nil
}

func (r *massEmailRepository) MarkRecipientFailed(ctx context.Context, campaignID, recipientID int64, reason string, failedAt time.Time) error {
	if failedAt.IsZero() {
		failedAt = time.Now()
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE mass_email_recipient
		SET status = 'failed', "error" = $3, claimed_at = NULL, updated_at = $4
		WHERE campaign_id = $1 AND id = $2`, campaignID, recipientID, truncateMassEmailError(reason), failedAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrMassEmailRecipientNotFound
	}
	return nil
}

func (r *massEmailRepository) ResetFailedRecipients(ctx context.Context, campaignID int64, recipientID *int64) (int, error) {
	args := []any{campaignID}
	query := `UPDATE mass_email_recipient
		SET status = 'pending', claimed_at = NULL, "error" = '', updated_at = NOW()
		WHERE campaign_id = $1 AND status = 'failed'`
	if recipientID != nil {
		args = append(args, *recipientID)
		query += ` AND id = $2`
	}
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func (r *massEmailRepository) RecordRecipientOpen(ctx context.Context, trackingToken string, openedAt time.Time) (*domain.MassEmailRecipient, error) {
	token := strings.TrimSpace(trackingToken)
	if token == "" {
		return nil, domain.ErrMassEmailRecipientNotFound
	}
	if openedAt.IsZero() {
		openedAt = time.Now()
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE mass_email_recipient
		SET open_count = open_count + 1,
		    first_opened_at = COALESCE(first_opened_at, $2),
		    last_opened_at = $2,
		    updated_at = NOW()
		WHERE tracking_token = $1
		RETURNING `+massEmailRecipientColumns, token, openedAt)

	recipient, err := scanMassEmailRecipient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrMassEmailRecipientNotFound
		}
		return nil, err
	}
	return recipient, nil
}

// scanMassEmailRecipient 按 massEmailRecipientColumns 的顺序扫描一行。
func scanMassEmailRecipient(scanner interface{ Scan(dest ...any) error }) (*domain.MassEmailRecipient, error) {
	var (
		recipient    domain.MassEmailRecipient
		userID       sql.NullInt64
		enterpriseID sql.NullInt64
		claimedAt    sql.NullTime
		sentAt       sql.NullTime
		firstOpened  sql.NullTime
		lastOpened   sql.NullTime
	)
	if err := scanner.Scan(
		&recipient.ID,
		&recipient.CampaignID,
		&recipient.Email,
		&recipient.Name,
		&userID,
		&enterpriseID,
		&recipient.Status,
		&recipient.Error,
		&recipient.Attempts,
		&claimedAt,
		&sentAt,
		&recipient.TrackingToken,
		&firstOpened,
		&lastOpened,
		&recipient.OpenCount,
		&recipient.CreatedAt,
		&recipient.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if userID.Valid {
		value := userID.Int64
		recipient.UserID = &value
	}
	if enterpriseID.Valid {
		value := enterpriseID.Int64
		recipient.EnterpriseID = &value
	}
	if claimedAt.Valid {
		value := claimedAt.Time
		recipient.ClaimedAt = &value
	}
	if sentAt.Valid {
		value := sentAt.Time
		recipient.SentAt = &value
	}
	if firstOpened.Valid {
		value := firstOpened.Time
		recipient.FirstOpenedAt = &value
	}
	if lastOpened.Valid {
		value := lastOpened.Time
		recipient.LastOpenedAt = &value
	}
	return &recipient, nil
}
