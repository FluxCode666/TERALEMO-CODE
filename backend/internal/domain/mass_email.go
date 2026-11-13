package domain

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// MassEmailCampaignStatus 邮件群发任务状态。
const (
	MassEmailStatusDraft     = "draft"
	MassEmailStatusScheduled = "scheduled"
	MassEmailStatusSending   = "sending"
	MassEmailStatusCompleted = "completed"
	MassEmailStatusCancelled = "cancelled"
	MassEmailStatusFailed    = "failed"
)

// MassEmailRecipientStatus 单个收件人的发送状态。
const (
	MassEmailRecipientStatusPending = "pending"
	MassEmailRecipientStatusSent    = "sent"
	MassEmailRecipientStatusFailed  = "failed"
)

// MassEmailEnterpriseScope 指定企业时的取件范围。
const (
	MassEmailScopeContacts = "contacts" // 仅企业联系人邮箱
	MassEmailScopeMembers  = "members"  // 仅企业成员（关联用户）邮箱
	MassEmailScopeBoth     = "both"     // 两者都取
)

// MassEmailRecipientSource 收件人来源标记（仅用于预览/排查，不落库）。
const (
	MassEmailSourceUser              = "user"
	MassEmailSourceEnterpriseContact = "enterprise_contact"
	MassEmailSourceEnterpriseMember  = "enterprise_member"
	MassEmailSourceManual            = "manual"
)

// 邮件群发相关限制。
const (
	MassEmailSubjectMaxLen        = 200
	MassEmailBodyMaxBytes         = 1 << 20 // 1MB
	MassEmailNameMaxLen           = 100
	MassEmailEmailMaxLen          = 255
	MassEmailTrackingTokenLen     = 32
	MassEmailPreviewSampleMax     = 50
	MassEmailPreviewSampleDefault = 5
)

// MassEmailClaimLease 收件人领取租约：超过该时长仍未落定，允许其它实例/重启后的进程重新领取，
// 避免进程崩溃后收件人永久卡在 pending 之外的状态。
const MassEmailClaimLease = 5 * time.Minute

// MassEmailDefaultRateLimitPerSecond Resend 默认发送限速（封/秒）。
const MassEmailDefaultRateLimitPerSecond = 2

// 邮件群发错误码。错误对象本身使用 infraerrors，便于 handler 直接 response.ErrorFrom。
const (
	MassEmailErrorCodeNotFound              = "MASS_EMAIL_NOT_FOUND"
	MassEmailErrorCodeRecipientNotFound     = "RECIPIENT_NOT_FOUND"
	MassEmailErrorCodeInvalidFilter         = "INVALID_RECIPIENT_FILTER"
	MassEmailErrorCodeInvalidSubject        = "INVALID_SUBJECT"
	MassEmailErrorCodeInvalidBody           = "INVALID_BODY"
	MassEmailErrorCodeInvalidEmail          = "INVALID_EMAIL"
	MassEmailErrorCodeInvalidScheduledAt    = "INVALID_SCHEDULED_AT"
	MassEmailErrorCodeNoRecipients          = "NO_RECIPIENTS"
	MassEmailErrorCodeNotEditable           = "CAMPAIGN_NOT_EDITABLE"
	MassEmailErrorCodeNotCancellable        = "CAMPAIGN_NOT_CANCELLABLE"
	MassEmailErrorCodeSending               = "CAMPAIGN_SENDING"
	MassEmailErrorCodeCancelled             = "CAMPAIGN_CANCELLED"
	MassEmailErrorCodeRecipientNotRetryable = "RECIPIENT_NOT_RETRYABLE"
	MassEmailErrorCodeSendFailed            = "EMAIL_SEND_FAILED"
)

var (
	// ErrMassEmailCampaignNotFound 任务不存在。
	ErrMassEmailCampaignNotFound = infraerrors.New(404, MassEmailErrorCodeNotFound, "mass email campaign not found")
	// ErrMassEmailRecipientNotFound 收件人不存在（或不属于该任务）。
	ErrMassEmailRecipientNotFound = infraerrors.New(404, MassEmailErrorCodeRecipientNotFound, "mass email recipient not found")
	// ErrMassEmailInvalidFilter 收件人条件非法。
	ErrMassEmailInvalidFilter = infraerrors.New(400, MassEmailErrorCodeInvalidFilter, "invalid recipient filter")
	// ErrMassEmailInvalidSubject 主题非法。
	ErrMassEmailInvalidSubject = infraerrors.New(400, MassEmailErrorCodeInvalidSubject, "invalid subject")
	// ErrMassEmailInvalidBody 正文非法。
	ErrMassEmailInvalidBody = infraerrors.New(400, MassEmailErrorCodeInvalidBody, "invalid body")
	// ErrMassEmailInvalidEmail 邮箱格式非法。
	ErrMassEmailInvalidEmail = infraerrors.New(400, MassEmailErrorCodeInvalidEmail, "invalid email address")
	// ErrMassEmailInvalidScheduledAt 定时时间非法（必须是未来时间）。
	ErrMassEmailInvalidScheduledAt = infraerrors.New(400, MassEmailErrorCodeInvalidScheduledAt, "scheduled_at must be in the future")
	// ErrMassEmailNoRecipients 解析后没有有效收件人。
	ErrMassEmailNoRecipients = infraerrors.New(400, MassEmailErrorCodeNoRecipients, "no valid recipients")
	// ErrMassEmailNotEditable 仅草稿/定时任务可编辑。
	ErrMassEmailNotEditable = infraerrors.New(400, MassEmailErrorCodeNotEditable, "only draft or scheduled campaigns can be edited")
	// ErrMassEmailNotCancellable 已完成/已取消的任务不可取消。
	ErrMassEmailNotCancellable = infraerrors.New(409, MassEmailErrorCodeNotCancellable, "campaign cannot be cancelled")
	// ErrMassEmailSending 发送中的任务不可修改/删除。
	ErrMassEmailSending = infraerrors.New(409, MassEmailErrorCodeSending, "campaign is sending")
	// ErrMassEmailCancelled 已取消的任务不可直接发送。
	ErrMassEmailCancelled = infraerrors.New(409, MassEmailErrorCodeCancelled, "campaign is cancelled")
	// ErrMassEmailRecipientNotRetryable 仅 failed 的收件人可重发。
	ErrMassEmailRecipientNotRetryable = infraerrors.New(400, MassEmailErrorCodeRecipientNotRetryable, "recipient is not retryable")
	// ErrMassEmailSendFailed 测试邮件发送失败。
	ErrMassEmailSendFailed = infraerrors.New(500, MassEmailErrorCodeSendFailed, "send email failed")
)

// MassEmailUserFilter 用户维度筛选条件。
type MassEmailUserFilter struct {
	Statuses    []string   `json:"statuses,omitempty"`
	Roles       []string   `json:"roles,omitempty"`
	GroupIDs    []int64    `json:"group_ids,omitempty"`
	Search      string     `json:"search,omitempty"`
	CreatedFrom *time.Time `json:"created_from,omitempty"`
	CreatedTo   *time.Time `json:"created_to,omitempty"`
}

// HasConditions 判断用户筛选条件里是否至少有一个非空条件。
//
// 重要语义（前后端一致）：user_filter 为 {}、null 或未传时，都表示"不选用户"。
// 只有 include_all_users=true 或 HasConditions()=true 时才把用户作为收件人来源，
// 绝不能因为 UserFilter 是值类型（反序列化 null 后变成零值）就去解析全部用户。
func (f MassEmailUserFilter) HasConditions() bool {
	return len(f.Statuses) > 0 || len(f.Roles) > 0 || len(f.GroupIDs) > 0 ||
		strings.TrimSpace(f.Search) != "" || f.CreatedFrom != nil || f.CreatedTo != nil
}

// IsEmpty 判断该用户筛选条件是否为空（没有任何非空条件）。
func (f MassEmailUserFilter) IsEmpty() bool {
	return !f.HasConditions()
}

// WantsAllUsers 判断是否应当解析"全部用户"来源。
func (f MassEmailUserFilter) WantsAllUsers(includeAllUsers bool) bool {
	return includeAllUsers || f.HasConditions()
}

// MassEmailEnterpriseFilter 企业维度筛选条件。
type MassEmailEnterpriseFilter struct {
	EnterpriseIDs []int64 `json:"enterprise_ids,omitempty"`
	Scope         string  `json:"scope,omitempty"`
}

// NormalizedScope 返回规范化后的取件范围，缺省为 both；非法取值返回空串。
func (f MassEmailEnterpriseFilter) NormalizedScope() string {
	switch strings.ToLower(strings.TrimSpace(f.Scope)) {
	case MassEmailScopeContacts:
		return MassEmailScopeContacts
	case MassEmailScopeMembers:
		return MassEmailScopeMembers
	case MassEmailScopeBoth, "":
		return MassEmailScopeBoth
	default:
		return ""
	}
}

// HasValidScope 判断原始 scope 取值是否合法（空串表示未指定）。
func (f MassEmailEnterpriseFilter) HasValidScope() bool {
	return f.NormalizedScope() != ""
}

// WantsContacts 是否包含企业联系人邮箱。
func (f MassEmailEnterpriseFilter) WantsContacts() bool {
	scope := f.NormalizedScope()
	return scope == MassEmailScopeContacts || scope == MassEmailScopeBoth
}

// WantsMembers 是否包含企业成员邮箱。
func (f MassEmailEnterpriseFilter) WantsMembers() bool {
	scope := f.NormalizedScope()
	return scope == MassEmailScopeMembers || scope == MassEmailScopeBoth
}

// MassEmailRecipientFilter 收件人条件（以 JSONB 形式落库到 mass_email_campaign.recipient_filter）。
//
// 四段可任意组合，最终按邮箱小写去重。
type MassEmailRecipientFilter struct {
	IncludeAllUsers  bool                      `json:"include_all_users"`
	UserFilter       MassEmailUserFilter       `json:"user_filter"`
	EnterpriseFilter MassEmailEnterpriseFilter `json:"enterprise_filter"`
	ManualEmails     []string                  `json:"manual_emails"`
}

// Normalize 就地规范化：去空白、小写、去重、状态/角色去空白。
func (f *MassEmailRecipientFilter) Normalize() {
	if f == nil {
		return
	}
	f.UserFilter.Statuses = normalizeStringSet(f.UserFilter.Statuses)
	f.UserFilter.Roles = normalizeStringSet(f.UserFilter.Roles)
	f.UserFilter.Search = strings.TrimSpace(f.UserFilter.Search)
	f.UserFilter.GroupIDs = normalizeInt64Set(f.UserFilter.GroupIDs)
	f.EnterpriseFilter.EnterpriseIDs = normalizeInt64Set(f.EnterpriseFilter.EnterpriseIDs)
	// 只做大小写/空白规范化：非法取值保留原样，交给 Validate 报错，避免被静默改写。
	f.EnterpriseFilter.Scope = strings.ToLower(strings.TrimSpace(f.EnterpriseFilter.Scope))
	f.ManualEmails = normalizeMassEmailManualEntries(f.ManualEmails)
}

// IsEmpty 判断是否没有配置任何收件人来源。
func (f MassEmailRecipientFilter) IsEmpty() bool {
	return !f.IncludeAllUsers &&
		f.UserFilter.IsEmpty() &&
		len(f.EnterpriseFilter.EnterpriseIDs) == 0 &&
		len(f.ManualEmails) == 0
}

// ValidateSyntax 校验收件人条件结构本身是否合法。调用前应先 Normalize。
//
// 注意：方法名故意不叫 Validate —— ent 会把 field.JSON 类型上的 Validate() error
// 当成 ORM 层校验器，那样连"先存草稿、之后再补收件人"都会被拒绝。
// "解析后必须至少有 1 个收件人" 属于业务规则，由 service 在解析后判断（ErrMassEmailNoRecipients）。
func (f MassEmailRecipientFilter) ValidateSyntax() error {
	if !f.EnterpriseFilter.HasValidScope() {
		return ErrMassEmailInvalidFilter
	}
	if len(f.EnterpriseFilter.EnterpriseIDs) == 0 && f.EnterpriseFilter.NormalizedScope() != MassEmailScopeBoth {
		return ErrMassEmailInvalidFilter
	}
	// 手动邮箱不在这里校验：每个元素可能是"换行/逗号分隔的一段文本"，
	// 非法项由解析阶段过滤并通过 invalid_emails 回显给前端，不阻断保存。
	return nil
}

// MarshalJSON 保证落库的 JSON 结构稳定（空切片序列化为 [] 而不是 null）。
func (f MassEmailRecipientFilter) MarshalJSON() ([]byte, error) {
	type alias MassEmailRecipientFilter
	out := alias(f)
	if out.ManualEmails == nil {
		out.ManualEmails = []string{}
	}
	if out.EnterpriseFilter.EnterpriseIDs == nil {
		out.EnterpriseFilter.EnterpriseIDs = []int64{}
	}
	return json.Marshal(out)
}

// MassEmailCampaign 邮件群发任务。
type MassEmailCampaign struct {
	ID              int64
	Subject         string
	BodyHTML        string
	RecipientFilter MassEmailRecipientFilter
	Status          string
	ScheduledAt     *time.Time

	TotalCount  int
	SentCount   int
	FailedCount int
	OpenedCount int

	CreatedBy         *int64
	CreatedByUsername string // 派生字段，不落库

	StartedAt  *time.Time
	FinishedAt *time.Time
	LastError  string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// OpenRate 打开率（0~1）。total=0 时返回 0。
func (c *MassEmailCampaign) OpenRate() float64 {
	if c == nil || c.TotalCount <= 0 {
		return 0
	}
	return float64(c.OpenedCount) / float64(c.TotalCount)
}

// Progress 已处理比例（0~1），用于前端进度条。
func (c *MassEmailCampaign) Progress() float64 {
	if c == nil || c.TotalCount <= 0 {
		return 0
	}
	done := c.SentCount + c.FailedCount
	if done < 0 {
		done = 0
	}
	if done > c.TotalCount {
		done = c.TotalCount
	}
	return float64(done) / float64(c.TotalCount)
}

// MassEmailRecipient 单个收件人及其发送结果。
type MassEmailRecipient struct {
	ID           int64
	CampaignID   int64
	Email        string
	Name         string
	UserID       *int64
	EnterpriseID *int64
	Status       string
	Error        string
	Attempts     int
	ClaimedAt    *time.Time
	SentAt       *time.Time

	TrackingToken string
	FirstOpenedAt *time.Time
	LastOpenedAt  *time.Time
	OpenCount     int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsOpened 是否至少打开过一次。
func (r *MassEmailRecipient) IsOpened() bool {
	return r != nil && r.OpenCount > 0
}

// MassEmailRecipientCandidate 解析出来的候选收件人（尚未落库）。
type MassEmailRecipientCandidate struct {
	Email        string
	Name         string
	UserID       *int64
	EnterpriseID *int64
	Source       string
}

// MassEmailCampaignStats 详情页统计。
type MassEmailCampaignStats struct {
	Total   int
	Pending int
	Sent    int
	Failed  int
	Opened  int
}

// MassEmailCampaignListFilters 任务列表筛选。
type MassEmailCampaignListFilters struct {
	Statuses []string
	Search   string
}

// MassEmailRecipientListFilters 收件人列表筛选。
type MassEmailRecipientListFilters struct {
	Statuses []string
	// Opened 为 nil 表示不按打开状态过滤；true 表示 open_count>0；false 表示 open_count=0。
	Opened *bool
	Search string
}

// CanTransitionMassEmailStatus 校验任务状态流转是否合法。
func CanTransitionMassEmailStatus(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case MassEmailStatusDraft:
		return to == MassEmailStatusScheduled || to == MassEmailStatusSending ||
			to == MassEmailStatusCancelled || to == MassEmailStatusCompleted || to == MassEmailStatusFailed
	case MassEmailStatusScheduled:
		return to == MassEmailStatusDraft || to == MassEmailStatusSending ||
			to == MassEmailStatusCancelled || to == MassEmailStatusFailed
	case MassEmailStatusSending:
		return to == MassEmailStatusCompleted || to == MassEmailStatusCancelled || to == MassEmailStatusFailed
	case MassEmailStatusFailed:
		return to == MassEmailStatusSending || to == MassEmailStatusCancelled
	case MassEmailStatusCompleted, MassEmailStatusCancelled:
		return false
	default:
		return false
	}
}

// IsMassEmailTerminalStatus 是否为终态（不可再编辑/取消）。
func IsMassEmailTerminalStatus(status string) bool {
	return status == MassEmailStatusCompleted || status == MassEmailStatusCancelled
}

// IsValidMassEmailStatus 校验任务状态取值。
func IsValidMassEmailStatus(status string) bool {
	switch status {
	case MassEmailStatusDraft, MassEmailStatusScheduled, MassEmailStatusSending,
		MassEmailStatusCompleted, MassEmailStatusCancelled, MassEmailStatusFailed:
		return true
	default:
		return false
	}
}

// IsValidMassEmailRecipientStatus 校验收件人状态取值。
func IsValidMassEmailRecipientStatus(status string) bool {
	switch status {
	case MassEmailRecipientStatusPending, MassEmailRecipientStatusSent, MassEmailRecipientStatusFailed:
		return true
	default:
		return false
	}
}

var massEmailAddressPattern = regexp.MustCompile(`^[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$`)

// IsValidMassEmailAddress 校验邮箱格式（要求至少一个点号域名，避免误收 "a@b"）。
func IsValidMassEmailAddress(email string) bool {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || len(trimmed) > MassEmailEmailMaxLen {
		return false
	}
	return massEmailAddressPattern.MatchString(trimmed)
}

// NormalizeEmail 统一邮箱表示：去空白 + 小写。落库与去重都以该形式为准。
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ParseMassEmailManualInput 解析手动粘贴的邮箱列表（换行/逗号/分号/空格分隔），
// 返回按小写去重后的有效邮箱与非法项。
func ParseMassEmailManualInput(raw string) (valid []string, invalid []string) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case '\n', '\r', ',', ';', '\t', ' ', '，', '；', '、':
			return true
		default:
			return false
		}
	})
	return NormalizeMassEmailList(fields)
}

// NormalizeMassEmailList 对邮箱列表做规范化 + 去重，返回有效项与非法项（均保持首次出现顺序）。
func NormalizeMassEmailList(emails []string) (valid []string, invalid []string) {
	if len(emails) == 0 {
		return []string{}, []string{}
	}
	seen := make(map[string]struct{}, len(emails))
	valid = make([]string, 0, len(emails))
	invalid = make([]string, 0)
	for _, raw := range emails {
		email := NormalizeEmail(raw)
		if email == "" {
			continue
		}
		if !IsValidMassEmailAddress(email) {
			invalid = append(invalid, strings.TrimSpace(raw))
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		valid = append(valid, email)
	}
	return valid, invalid
}

// DedupeMassEmailCandidates 按邮箱小写去重候选收件人，保留首次出现的那条（后出现的字段不覆盖）。
func DedupeMassEmailCandidates(candidates []MassEmailRecipientCandidate) []MassEmailRecipientCandidate {
	if len(candidates) == 0 {
		return []MassEmailRecipientCandidate{}
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]MassEmailRecipientCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		email := NormalizeEmail(candidate.Email)
		if !IsValidMassEmailAddress(email) {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		candidate.Email = email
		candidate.Name = strings.TrimSpace(candidate.Name)
		if len(candidate.Name) > MassEmailNameMaxLen {
			candidate.Name = candidate.Name[:MassEmailNameMaxLen]
		}
		out = append(out, candidate)
	}
	return out
}

// normalizeMassEmailManualEntries 规范化手动邮箱输入：trim + 小写 + 去重，
// 但**不**拆分隔符、**不**丢弃非法项 —— 单个元素可能是一整段粘贴文本，
// 拆分与校验由 ParseMassEmailManualInput 在解析阶段完成。
func normalizeMassEmailManualEntries(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeStringSet(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeInt64Set(values []int64) []int64 {
	if len(values) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
