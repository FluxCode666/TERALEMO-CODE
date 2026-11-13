package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/massemailcampaign"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

// massEmailRepository 邮件群发数据层实现。
//
// 任务元数据走 ent（便于复用谓词与分页），收件人表因为需要
// FOR UPDATE SKIP LOCKED / 表达式唯一索引冲突处理 / 批量插入，走原生 SQL。
type massEmailRepository struct {
	client *dbent.Client
	db     *sql.DB
}

// NewMassEmailRepository 创建邮件群发仓储。
func NewMassEmailRepository(client *dbent.Client, sqlDB *sql.DB) service.MassEmailRepository {
	return &massEmailRepository{client: client, db: sqlDB}
}

func (r *massEmailRepository) CreateCampaign(ctx context.Context, campaign *domain.MassEmailCampaign) error {
	if campaign == nil {
		return domain.ErrMassEmailInvalidFilter
	}
	client := clientFromContext(ctx, r.client)
	builder := client.MassEmailCampaign.Create().
		SetSubject(campaign.Subject).
		SetBodyHTML(campaign.BodyHTML).
		SetRecipientFilter(campaign.RecipientFilter).
		SetStatus(campaign.Status).
		SetTotalCount(campaign.TotalCount).
		SetSentCount(campaign.SentCount).
		SetFailedCount(campaign.FailedCount).
		SetOpenedCount(campaign.OpenedCount).
		SetLastError(campaign.LastError).
		SetNillableScheduledAt(campaign.ScheduledAt).
		SetNillableCreatedBy(campaign.CreatedBy).
		SetNillableStartedAt(campaign.StartedAt).
		SetNillableFinishedAt(campaign.FinishedAt)

	created, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	campaign.ID = created.ID
	campaign.CreatedAt = created.CreatedAt
	campaign.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *massEmailRepository) GetCampaign(ctx context.Context, id int64) (*domain.MassEmailCampaign, error) {
	client := clientFromContext(ctx, r.client)
	entity, err := client.MassEmailCampaign.Query().
		Where(massemailcampaign.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, domain.ErrMassEmailCampaignNotFound, nil)
	}
	out := massEmailCampaignFromEntity(entity)
	names, err := massEmailUserDisplayNames(ctx, client, massEmailCreatedByIDs([]domain.MassEmailCampaign{*out}))
	if err != nil {
		return nil, err
	}
	if out.CreatedBy != nil {
		out.CreatedByUsername = names[*out.CreatedBy]
	}
	return out, nil
}

func (r *massEmailRepository) UpdateCampaign(ctx context.Context, campaign *domain.MassEmailCampaign) error {
	if campaign == nil {
		return domain.ErrMassEmailCampaignNotFound
	}
	client := clientFromContext(ctx, r.client)
	builder := client.MassEmailCampaign.UpdateOneID(campaign.ID).
		SetSubject(campaign.Subject).
		SetBodyHTML(campaign.BodyHTML).
		SetRecipientFilter(campaign.RecipientFilter)
	// 注意：SetNillableScheduledAt(nil) 在 ent 里是 no-op，不会清空列。
	// 前端"存草稿"会显式提交 scheduled_at=null，这里必须真正清空。
	if campaign.ScheduledAt != nil {
		builder = builder.SetScheduledAt(*campaign.ScheduledAt)
	} else {
		builder = builder.ClearScheduledAt()
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, domain.ErrMassEmailCampaignNotFound, nil)
	}
	campaign.UpdatedAt = updated.UpdatedAt
	return nil
}

func (r *massEmailRepository) UpdateCampaignWithRecipients(
	ctx context.Context,
	campaign *domain.MassEmailCampaign,
	recipients []domain.MassEmailRecipient,
) (bool, error) {
	if campaign == nil {
		return false, domain.ErrMassEmailCampaignNotFound
	}
	// domain.MassEmailRecipientFilter 只实现了 MarshalJSON，不是 driver.Valuer，
	// 直接当 SQL 参数会报 "unsupported type"；这里显式序列化成 JSON 文本。
	filterJSON, err := json.Marshal(campaign.RecipientFilter)
	if err != nil {
		return false, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1) 内容 + 状态 CAS：只有 draft/scheduled 才能被编辑。
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE mass_email_campaign
		SET subject = $2,
		    body_html = $3,
		    recipient_filter = $4,
		    scheduled_at = $5,
		    status = $6,
		    finished_at = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND status IN ('draft', 'scheduled')`,
		campaign.ID, campaign.Subject, campaign.BodyHTML, string(filterJSON),
		campaign.ScheduledAt, campaign.Status)
	if err != nil {
		return false, err
	}
	affected, err := updateResult.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		// 任务已被推进（sending/completed/...），不改任何东西。
		return false, nil
	}

	// 2) 收件人重建。
	if _, err := tx.ExecContext(ctx, `DELETE FROM mass_email_recipient WHERE campaign_id = $1`, campaign.ID); err != nil {
		return false, err
	}
	if _, err := insertMassEmailRecipients(ctx, tx, campaign.ID, recipients); err != nil {
		return false, err
	}

	// 3) 计数刷新。
	if _, err := tx.ExecContext(ctx, `
		UPDATE mass_email_campaign
		SET total_count = (SELECT COUNT(*)::int FROM mass_email_recipient WHERE campaign_id = $1),
		    updated_at = NOW()
		WHERE id = $1`, campaign.ID); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *massEmailRepository) DeleteCampaign(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.MassEmailCampaign.Delete().
		Where(massemailcampaign.IDEQ(id)).
		Exec(ctx)
	return err
}

func (r *massEmailRepository) DeleteCampaignIfNotSending(ctx context.Context, id int64) (bool, error) {
	// 单条 SQL 做「存在 + 非 sending」的原子判断，避免先读后删被 /send 抢先。
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM mass_email_campaign WHERE id = $1 AND status <> 'sending'`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		return true, nil
	}
	// 0 行：要么不存在（404），要么正在发送（409）。
	if _, err := r.GetCampaign(ctx, id); err != nil {
		return false, err
	}
	return false, nil
}

func (r *massEmailRepository) ListCampaigns(
	ctx context.Context,
	params pagination.PaginationParams,
	filters domain.MassEmailCampaignListFilters,
) ([]domain.MassEmailCampaign, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	query := client.MassEmailCampaign.Query()

	if statuses := massEmailValidCampaignStatuses(filters.Statuses); len(statuses) > 0 {
		query = query.Where(massemailcampaign.StatusIn(statuses...))
	}
	if search := strings.TrimSpace(filters.Search); search != "" {
		query = query.Where(massemailcampaign.SubjectContainsFold(search))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	itemsQuery := query.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range massEmailCampaignListOrders(params) {
		itemsQuery = itemsQuery.Order(order)
	}

	entities, err := itemsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := massEmailCampaignsFromEntities(entities)
	if err := r.fillCreatedByUsernames(ctx, client, out); err != nil {
		return nil, nil, err
	}
	return out, paginationResultFromTotal(int64(total), params), nil
}

func (r *massEmailRepository) ListDueScheduledCampaigns(ctx context.Context, now time.Time, limit int) ([]domain.MassEmailCampaign, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	client := clientFromContext(ctx, r.client)
	entities, err := client.MassEmailCampaign.Query().
		Where(
			massemailcampaign.StatusEQ(domain.MassEmailStatusScheduled),
			massemailcampaign.ScheduledAtNotNil(),
			massemailcampaign.ScheduledAtLTE(now),
		).
		Order(dbent.Asc(massemailcampaign.FieldScheduledAt), dbent.Asc(massemailcampaign.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return massEmailCampaignsFromEntities(entities), nil
}

func (r *massEmailRepository) TransitionCampaignStatus(
	ctx context.Context,
	id int64,
	fromStatuses []string,
	toStatus string,
	now time.Time,
) (bool, error) {
	fromStatuses = massEmailValidCampaignStatuses(fromStatuses)
	if len(fromStatuses) == 0 || !domain.IsValidMassEmailStatus(toStatus) {
		return false, domain.ErrMassEmailInvalidFilter
	}
	if now.IsZero() {
		now = time.Now()
	}

	args := make([]any, 0, 3+len(fromStatuses))
	args = append(args, id, toStatus, now)
	placeholders := make([]string, 0, len(fromStatuses))
	for _, status := range fromStatuses {
		args = append(args, status)
		// 显式 cast：同一参数会同时出现在 varchar 赋值和文本比较里，
		// 不 cast 时 PG 会报 "inconsistent types deduced for parameter $2"。
		placeholders = append(placeholders, "$"+strconv.Itoa(len(args))+"::varchar")
	}

	query := `
		UPDATE mass_email_campaign
		SET status = $2::varchar,
		    started_at = CASE WHEN $2::text = 'sending' THEN COALESCE(started_at, $3::timestamptz) ELSE started_at END,
		    finished_at = CASE WHEN $2::text IN ('completed', 'cancelled', 'failed') THEN $3::timestamptz ELSE NULL END,
		    updated_at = NOW()
		WHERE id = $1 AND status IN (` + strings.Join(placeholders, ", ") + `)`

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *massEmailRepository) UpdateCampaignError(ctx context.Context, id int64, message string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.MassEmailCampaign.UpdateOneID(id).
		SetLastError(truncateMassEmailError(message)).
		Save(ctx)
	return translatePersistenceError(err, domain.ErrMassEmailCampaignNotFound, nil)
}

func (r *massEmailRepository) RecountCampaign(ctx context.Context, id int64) (*domain.MassEmailCampaignStats, error) {
	stats := &domain.MassEmailCampaignStats{}
	row := r.db.QueryRowContext(ctx, `
		WITH counts AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE status = 'pending')::int AS pending,
				COUNT(*) FILTER (WHERE status = 'sent')::int AS sent,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed,
				COUNT(*) FILTER (WHERE open_count > 0)::int AS opened
			FROM mass_email_recipient
			WHERE campaign_id = $1
		), updated AS (
			UPDATE mass_email_campaign
			SET total_count = counts.total,
			    sent_count = counts.sent,
			    failed_count = counts.failed,
			    opened_count = counts.opened,
			    updated_at = NOW()
			FROM counts
			WHERE mass_email_campaign.id = $1
			RETURNING counts.total, counts.pending, counts.sent, counts.failed, counts.opened
		)
		SELECT total, pending, sent, failed, opened FROM updated`, id)
	if err := row.Scan(&stats.Total, &stats.Pending, &stats.Sent, &stats.Failed, &stats.Opened); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrMassEmailCampaignNotFound
		}
		return nil, err
	}
	return stats, nil
}

// fillCreatedByUsernames 批量回填创建人显示名（username 优先，空则用邮箱）。
func (r *massEmailRepository) fillCreatedByUsernames(ctx context.Context, client *dbent.Client, campaigns []domain.MassEmailCampaign) error {
	ids := massEmailCreatedByIDs(campaigns)
	if len(ids) == 0 {
		return nil
	}

	names, err := massEmailUserDisplayNames(ctx, client, ids)
	if err != nil {
		return err
	}
	for i := range campaigns {
		if campaigns[i].CreatedBy == nil {
			continue
		}
		campaigns[i].CreatedByUsername = names[*campaigns[i].CreatedBy]
	}
	return nil
}

// massEmailCreatedByIDs 收集任务列表里出现过的创建人 ID（去重）。
func massEmailCreatedByIDs(campaigns []domain.MassEmailCampaign) []int64 {
	ids := make([]int64, 0, len(campaigns))
	seen := make(map[int64]struct{}, len(campaigns))
	for i := range campaigns {
		if campaigns[i].CreatedBy == nil {
			continue
		}
		if _, ok := seen[*campaigns[i].CreatedBy]; ok {
			continue
		}
		seen[*campaigns[i].CreatedBy] = struct{}{}
		ids = append(ids, *campaigns[i].CreatedBy)
	}
	return ids
}

func massEmailCampaignFromEntity(entity *dbent.MassEmailCampaign) *domain.MassEmailCampaign {
	if entity == nil {
		return nil
	}
	return &domain.MassEmailCampaign{
		ID:              entity.ID,
		Subject:         entity.Subject,
		BodyHTML:        entity.BodyHTML,
		RecipientFilter: entity.RecipientFilter,
		Status:          entity.Status,
		ScheduledAt:     entity.ScheduledAt,
		TotalCount:      entity.TotalCount,
		SentCount:       entity.SentCount,
		FailedCount:     entity.FailedCount,
		OpenedCount:     entity.OpenedCount,
		CreatedBy:       entity.CreatedBy,
		StartedAt:       entity.StartedAt,
		FinishedAt:      entity.FinishedAt,
		LastError:       entity.LastError,
		CreatedAt:       entity.CreatedAt,
		UpdatedAt:       entity.UpdatedAt,
	}
}

func massEmailCampaignsFromEntities(entities []*dbent.MassEmailCampaign) []domain.MassEmailCampaign {
	out := make([]domain.MassEmailCampaign, 0, len(entities))
	for _, entity := range entities {
		if campaign := massEmailCampaignFromEntity(entity); campaign != nil {
			out = append(out, *campaign)
		}
	}
	return out
}

// massEmailCampaignListOrders 任务列表排序（默认 created_at desc）。
func massEmailCampaignListOrders(params pagination.PaginationParams) []func(*entsql.Selector) {
	field := strings.ToLower(strings.TrimSpace(params.SortBy))
	order := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var column string
	switch field {
	case "scheduled_at":
		column = massemailcampaign.FieldScheduledAt
	case "status":
		column = massemailcampaign.FieldStatus
	case "id":
		column = massemailcampaign.FieldID
	case "", "created_at":
		column = massemailcampaign.FieldCreatedAt
	default:
		column = massemailcampaign.FieldCreatedAt
		order = pagination.SortOrderDesc
	}

	if order == pagination.SortOrderAsc {
		if column == massemailcampaign.FieldID {
			return []func(*entsql.Selector){dbent.Asc(column)}
		}
		return []func(*entsql.Selector){dbent.Asc(column), dbent.Asc(massemailcampaign.FieldID)}
	}
	if column == massemailcampaign.FieldID {
		return []func(*entsql.Selector){dbent.Desc(column)}
	}
	return []func(*entsql.Selector){dbent.Desc(column), dbent.Desc(massemailcampaign.FieldID)}
}

// massEmailValidCampaignStatuses 过滤掉非法状态值（避免把任意字符串塞进 SQL）。
func massEmailValidCampaignStatuses(statuses []string) []string {
	if len(statuses) == 0 {
		return nil
	}
	out := make([]string, 0, len(statuses))
	seen := make(map[string]struct{}, len(statuses))
	for _, raw := range statuses {
		status := strings.ToLower(strings.TrimSpace(raw))
		if !domain.IsValidMassEmailStatus(status) {
			continue
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out
}

// massEmailValidRecipientStatuses 过滤掉非法收件人状态值。
func massEmailValidRecipientStatuses(statuses []string) []string {
	if len(statuses) == 0 {
		return nil
	}
	out := make([]string, 0, len(statuses))
	seen := make(map[string]struct{}, len(statuses))
	for _, raw := range statuses {
		status := strings.ToLower(strings.TrimSpace(raw))
		if !domain.IsValidMassEmailRecipientStatus(status) {
			continue
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out
}

// truncateMassEmailError 截断错误文本，避免把超长上游响应写进库。
func truncateMassEmailError(message string) string {
	message = strings.ToValidUTF8(message, "�")
	const maxLen = 2000
	if len(message) <= maxLen {
		return message
	}
	// 按 rune 截断，避免切碎多字节字符。
	runes := []rune(message)
	if len(runes) <= maxLen {
		return message
	}
	return string(runes[:maxLen])
}
