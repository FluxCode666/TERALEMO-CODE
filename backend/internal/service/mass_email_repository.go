package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// MassEmailRepository 是邮件群发功能的数据层契约。
//
// 该接口由 repository 包实现（repository.NewMassEmailRepository），
// 只承载持久化职责：不包含变量渲染、收件人合并去重、限速、调度等业务逻辑。
//
// 数据模型定义见 internal/domain/mass_email.go；
// 表结构以 SQL 迁移 migrations/266_mass_email_campaigns.sql 为权威。
type MassEmailRepository interface {
	// CreateCampaign 创建任务并回填 ID/时间戳。
	CreateCampaign(ctx context.Context, campaign *domain.MassEmailCampaign) error
	// GetCampaign 按 ID 读取任务（含创建人显示名派生字段）。
	GetCampaign(ctx context.Context, id int64) (*domain.MassEmailCampaign, error)
	// UpdateCampaign 全量更新任务的编辑字段（主题/正文/条件/定时时间）。
	// 状态与计数不通过该方法修改。
	UpdateCampaign(ctx context.Context, campaign *domain.MassEmailCampaign) error
	// DeleteCampaign 硬删除任务（收件人随外键级联删除）。仅供创建回滚等内部路径使用。
	DeleteCampaign(ctx context.Context, id int64) error
	// DeleteCampaignIfNotSending 原子删除：仅当任务当前不是 sending 时删除。
	// 返回 false 表示任务正在发送（调用方应返回 409）；任务不存在时返回 ErrMassEmailCampaignNotFound。
	DeleteCampaignIfNotSending(ctx context.Context, id int64) (bool, error)
	// ListCampaigns 分页查询任务列表。
	ListCampaigns(
		ctx context.Context,
		params pagination.PaginationParams,
		filters domain.MassEmailCampaignListFilters,
	) ([]domain.MassEmailCampaign, *pagination.PaginationResult, error)
	// ListDueScheduledCampaigns 返回 status=scheduled 且 scheduled_at <= now 的任务，供调度器触发。
	ListDueScheduledCampaigns(ctx context.Context, now time.Time, limit int) ([]domain.MassEmailCampaign, error)
	// TransitionCampaignStatus 以 CAS 方式更新任务状态：仅当当前状态在 fromStatuses 内才更新。
	// 返回 true 表示本次调用真正完成了状态流转（多实例并发时只有一个能成功）。
	// 进入 sending 时 started_at 取 COALESCE(started_at, now) 并清空 finished_at；
	// 进入终态（completed/cancelled/failed）时写入 finished_at。
	TransitionCampaignStatus(ctx context.Context, id int64, fromStatuses []string, toStatus string, now time.Time) (bool, error)
	// UpdateCampaignError 记录任务级错误信息。
	UpdateCampaignError(ctx context.Context, id int64, message string) error
	// RecountCampaign 依据收件人表重算并写回任务计数（total/sent/failed/opened），返回最新统计。
	// opened 的口径是 open_count > 0 的收件人数。
	RecountCampaign(ctx context.Context, id int64) (*domain.MassEmailCampaignStats, error)

	// UpdateCampaignWithRecipients 在**单个事务**里完成「内容更新 + 收件人重建 + 计数刷新 + 状态更新」。
	// 只有当前状态属于 draft/scheduled 时才会生效（行级 CAS），返回 false 表示任务已被别人推进
	// （例如 worker 已经开始发送），调用方应返回 409。
	UpdateCampaignWithRecipients(ctx context.Context, campaign *domain.MassEmailCampaign, recipients []domain.MassEmailRecipient) (bool, error)
	// ReplaceRecipients 重建任务的收件人集合（先删后插），用于创建与编辑。
	// 入参按邮箱小写去重（重复项被忽略），返回实际插入条数。
	ReplaceRecipients(ctx context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error)
	// InsertRecipients 追加收件人（按 (campaign_id, lower(email)) 幂等），返回实际插入条数。
	InsertRecipients(ctx context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error)
	// ListRecipients 分页查询收件人明细，支持 status/opened/search 筛选。
	ListRecipients(
		ctx context.Context,
		campaignID int64,
		params pagination.PaginationParams,
		filters domain.MassEmailRecipientListFilters,
	) ([]domain.MassEmailRecipient, *pagination.PaginationResult, error)
	// GetRecipient 读取单个收件人（recipientID 必须属于 campaignID）。
	GetRecipient(ctx context.Context, campaignID, recipientID int64) (*domain.MassEmailRecipient, error)
	// ClaimPendingRecipients 领取待发送收件人：行级锁（FOR UPDATE SKIP LOCKED）+ 租约。
	// 只领取 status=pending 且 claimed_at 为空或早于 now-lease 的行，领取时写入 claimed_at 并 attempts+1。
	// 租约保证进程崩溃后收件人可被重新领取，不会永久卡住。
	ClaimPendingRecipients(ctx context.Context, campaignID int64, limit int, lease time.Duration, now time.Time) ([]domain.MassEmailRecipient, error)
	// ReleaseClaimedRecipients 把"已领取但没发出去"的收件人放回队列：
	// 清空 claimed_at 并把 attempts 减回 1（领取时 +1），避免被 5 分钟租约卡住、也不会虚增尝试次数。
	ReleaseClaimedRecipients(ctx context.Context, campaignID int64, recipientIDs []int64) (int, error)
	// MarkRecipientSent 标记发送成功（清空 claimed_at，写 sent_at）。
	MarkRecipientSent(ctx context.Context, campaignID, recipientID int64, sentAt time.Time) error
	// MarkRecipientFailed 标记发送失败（清空 claimed_at，写 error）。
	MarkRecipientFailed(ctx context.Context, campaignID, recipientID int64, reason string, failedAt time.Time) error
	// ResetFailedRecipients 把 failed 收件人重置为 pending 以便重发。
	// recipientID 为 nil 时重置该任务全部失败收件人；返回重置条数。
	ResetFailedRecipients(ctx context.Context, campaignID int64, recipientID *int64) (int, error)
	// RecordRecipientOpen 记录一次打开（open_count+1，写首次/最近打开时间），返回命中的收件人。
	// token 不存在时返回 domain.ErrMassEmailRecipientNotFound。
	RecordRecipientOpen(ctx context.Context, trackingToken string, openedAt time.Time) (*domain.MassEmailRecipient, error)

	// ResolveMassEmailUsers 解析用户来源的候选收件人。
	// includeAll=true 时忽略 filter；否则按 statuses/roles/group_ids/search/created 区间筛选。
	ResolveMassEmailUsers(ctx context.Context, filter domain.MassEmailUserFilter, includeAll bool) ([]domain.MassEmailRecipientCandidate, error)
	// MassEmailEnterpriseNames 批量取企业名，用于渲染 {{enterprise_name}}。
	MassEmailEnterpriseNames(ctx context.Context, enterpriseIDs []int64) (map[int64]string, error)
	// ResolveMassEmailEnterprises 解析企业来源的候选收件人。
	// scope=contacts 取企业 contact_email；scope=members 取企业成员（关联用户）邮箱；scope=both 两者。
	ResolveMassEmailEnterprises(ctx context.Context, enterpriseIDs []int64, scope string) ([]domain.MassEmailRecipientCandidate, error)
}
