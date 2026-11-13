package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// MassEmailSettingKeyRateLimitPerSecond 邮件群发限速设置键（封/秒）。
// 未配置时回落到 domain.MassEmailDefaultRateLimitPerSecond（Resend 默认 2/s）。
//
// 该键同时出现在管理后台系统设置（GET/PUT /admin/settings）的
// `mass_email_rate_limit_per_second` 字段里，默认 2，范围 1~50。
const MassEmailSettingKeyRateLimitPerSecond = "mass_email_rate_limit_per_second"

// 邮件群发速率的默认值与上限（管理后台可调）。
const (
	DefaultMassEmailRateLimitPerSecond = domain.MassEmailDefaultRateLimitPerSecond
	MaxMassEmailRateLimitPerSecond     = 50
)

// clampMassEmailRateLimit 把速率夹到 [1, 50]，非法值回落到默认 2。
func clampMassEmailRateLimit(value int) int {
	if value <= 0 {
		return DefaultMassEmailRateLimitPerSecond
	}
	if value > MaxMassEmailRateLimitPerSecond {
		return MaxMassEmailRateLimitPerSecond
	}
	return value
}

// normalizeMassEmailRateLimit 把设置里的字符串解析成合法速率。
func normalizeMassEmailRateLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return DefaultMassEmailRateLimitPerSecond
	}
	return clampMassEmailRateLimit(value)
}

// MassEmailCreateInput 创建任务入参。
type MassEmailCreateInput struct {
	Subject         string
	BodyHTML        string
	RecipientFilter domain.MassEmailRecipientFilter
	SendNow         bool
	ScheduledAt     *time.Time
}

// MassEmailUpdateInput 编辑任务入参（仅 draft/scheduled 可用）。
type MassEmailUpdateInput struct {
	Subject         string
	BodyHTML        string
	RecipientFilter domain.MassEmailRecipientFilter
	ScheduledAt     *time.Time
}

// MassEmailTestInput 测试发送入参。
type MassEmailTestInput struct {
	Subject  string
	BodyHTML string
	To       string
}

// MassEmailPreviewSources 各来源去重后的收件人数。
type MassEmailPreviewSources struct {
	Users              int `json:"users"`
	EnterpriseContacts int `json:"enterprise_contacts"`
	EnterpriseMembers  int `json:"enterprise_members"`
	Manual             int `json:"manual"`
}

// MassEmailPreview 收件人预览结果。
type MassEmailPreview struct {
	Total         int
	Sample        []domain.MassEmailRecipientCandidate
	BySource      MassEmailPreviewSources
	InvalidEmails []string
}

// MassEmailDetail 详情 = 任务 + 统计。
type MassEmailDetail struct {
	Campaign *domain.MassEmailCampaign
	Stats    *domain.MassEmailCampaignStats
}

// MassEmailService 邮件群发业务逻辑：收件人解析、变量渲染、HTML 净化、
// 创建/编辑/发送/取消/重试/测试发送、打开统计。
//
// 发送本身由 MassEmailRunner 异步驱动，这里只负责状态与数据。
type MassEmailService struct {
	repo       MassEmailRepository
	email      *EmailService
	settings   *SettingService
	settingRep SettingRepository
}

// NewMassEmailService 创建邮件群发服务。
func NewMassEmailService(
	repo MassEmailRepository,
	email *EmailService,
	settings *SettingService,
	settingRepo SettingRepository,
) *MassEmailService {
	return &MassEmailService{repo: repo, email: email, settings: settings, settingRep: settingRepo}
}

// ---------------------------------------------------------------------------
// 收件人解析
// ---------------------------------------------------------------------------

// ResolveRecipients 解析并去重收件人，返回候选列表与来源统计。
//
// 四种来源可任意组合，最终按邮箱小写去重（企业联系人/成员优先于用户，
// 用户优先于手动邮箱，保证 {{enterprise_name}} 尽量有值）。
func (s *MassEmailService) ResolveRecipients(
	ctx context.Context,
	filter domain.MassEmailRecipientFilter,
) ([]domain.MassEmailRecipientCandidate, MassEmailPreviewSources, []string, error) {
	var sources MassEmailPreviewSources

	filter.Normalize()
	if err := filter.ValidateSyntax(); err != nil {
		return nil, sources, nil, err
	}

	combined := make([]domain.MassEmailRecipientCandidate, 0, 64)

	// C. 企业：联系人优先于成员。
	if len(filter.EnterpriseFilter.EnterpriseIDs) > 0 {
		candidates, err := s.repo.ResolveMassEmailEnterprises(
			ctx,
			filter.EnterpriseFilter.EnterpriseIDs,
			filter.EnterpriseFilter.NormalizedScope(),
		)
		if err != nil {
			return nil, sources, nil, err
		}
		combined = append(combined, candidates...)
	}

	// A/B. 用户：只有 include_all_users=true 或有真实条件时才解析。
	if filter.UserFilter.WantsAllUsers(filter.IncludeAllUsers) {
		candidates, err := s.repo.ResolveMassEmailUsers(ctx, filter.UserFilter, filter.IncludeAllUsers)
		if err != nil {
			return nil, sources, nil, err
		}
		combined = append(combined, candidates...)
	}

	// D. 手动邮箱：每个元素都可能包含分隔符。
	manualValid := make([]string, 0, len(filter.ManualEmails))
	invalid := make([]string, 0)
	for _, raw := range filter.ManualEmails {
		valid, bad := domain.ParseMassEmailManualInput(raw)
		manualValid = append(manualValid, valid...)
		invalid = append(invalid, bad...)
	}
	for _, email := range manualValid {
		combined = append(combined, domain.MassEmailRecipientCandidate{
			Email:  email,
			Source: domain.MassEmailSourceManual,
		})
	}

	deduped := domain.DedupeMassEmailCandidates(combined)
	for i := range deduped {
		switch deduped[i].Source {
		case domain.MassEmailSourceUser:
			sources.Users++
		case domain.MassEmailSourceEnterpriseContact:
			sources.EnterpriseContacts++
		case domain.MassEmailSourceEnterpriseMember:
			sources.EnterpriseMembers++
		case domain.MassEmailSourceManual:
			sources.Manual++
		}
	}
	return deduped, sources, invalid, nil
}

// PreviewRecipients 预览收件人数量与样例。
func (s *MassEmailService) PreviewRecipients(
	ctx context.Context,
	filter domain.MassEmailRecipientFilter,
	sampleSize int,
) (*MassEmailPreview, error) {
	if sampleSize <= 0 {
		sampleSize = domain.MassEmailPreviewSampleDefault
	}
	if sampleSize > domain.MassEmailPreviewSampleMax {
		sampleSize = domain.MassEmailPreviewSampleMax
	}

	candidates, sources, invalid, err := s.ResolveRecipients(ctx, filter)
	if err != nil {
		return nil, err
	}

	sample := candidates
	if len(sample) > sampleSize {
		sample = sample[:sampleSize]
	}
	if sample == nil {
		sample = []domain.MassEmailRecipientCandidate{}
	}
	if invalid == nil {
		invalid = []string{}
	}

	return &MassEmailPreview{
		Total:         len(candidates),
		Sample:        sample,
		BySource:      sources,
		InvalidEmails: invalid,
	}, nil
}

// ---------------------------------------------------------------------------
// 任务 CRUD
// ---------------------------------------------------------------------------

// Create 创建任务（send_now / scheduled_at / 存草稿三种模式）。
func (s *MassEmailService) Create(
	ctx context.Context,
	adminID int64,
	input MassEmailCreateInput,
) (*domain.MassEmailCampaign, error) {
	subject, bodyHTML, err := normalizeMassEmailContent(input.Subject, input.BodyHTML)
	if err != nil {
		return nil, err
	}

	filter := input.RecipientFilter
	filter.Normalize()
	if err := filter.ValidateSyntax(); err != nil {
		return nil, err
	}

	status := domain.MassEmailStatusDraft
	var scheduledAt *time.Time
	switch {
	case input.SendNow:
		status = domain.MassEmailStatusSending
	case input.ScheduledAt != nil:
		at := input.ScheduledAt.UTC()
		if !at.After(time.Now()) {
			return nil, domain.ErrMassEmailInvalidScheduledAt
		}
		scheduledAt = &at
		status = domain.MassEmailStatusScheduled
	}

	// 立即发送 / 定时发送必须先确认 SMTP 可用；存草稿不校验。
	if status != domain.MassEmailStatusDraft {
		if err := s.ensureSMTPConfigured(ctx); err != nil {
			return nil, err
		}
	}

	candidates, _, _, err := s.ResolveRecipients(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, domain.ErrMassEmailNoRecipients
	}

	now := time.Now()
	// 关键顺序（lead #42 C）：先以 draft 落库，物化收件人 + 重算计数成功后再 CAS 到目标状态。
	// 这样 worker 永远不会看到「sending 但还没有收件人」的幽灵任务。
	campaign := &domain.MassEmailCampaign{
		Subject:         subject,
		BodyHTML:        bodyHTML,
		RecipientFilter: filter,
		Status:          domain.MassEmailStatusDraft,
		ScheduledAt:     scheduledAt,
		TotalCount:      len(candidates),
		CreatedBy:       &adminID,
	}
	if err := s.repo.CreateCampaign(ctx, campaign); err != nil {
		return nil, err
	}

	recipients := massEmailRecipientsFromCandidates(campaign.ID, candidates)
	if _, err := s.repo.ReplaceRecipients(ctx, campaign.ID, recipients); err != nil {
		s.rollbackCreate(ctx, campaign.ID)
		return nil, err
	}
	if _, err := s.repo.RecountCampaign(ctx, campaign.ID); err != nil {
		s.rollbackCreate(ctx, campaign.ID)
		return nil, err
	}

	if status != domain.MassEmailStatusDraft {
		ok, transitionErr := s.repo.TransitionCampaignStatus(
			ctx, campaign.ID, []string{domain.MassEmailStatusDraft}, status, now,
		)
		if transitionErr != nil || !ok {
			s.rollbackCreate(ctx, campaign.ID)
			if transitionErr != nil {
				return nil, transitionErr
			}
			return nil, domain.ErrMassEmailNotEditable
		}
	}

	created, err := s.repo.GetCampaign(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}
	return created, nil
}

// rollbackCreate 创建流程失败时清掉半成品任务，避免留下幽灵草稿。
func (s *MassEmailService) rollbackCreate(ctx context.Context, campaignID int64) {
	if err := s.repo.DeleteCampaign(ctx, campaignID); err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] rollback create failed id=%d: %v", campaignID, err)
	}
}

// Update 编辑任务（仅 draft/scheduled）。
//
// scheduled_at 传 null 表示取消定时并回到 draft（前端"存草稿"语义）。
func (s *MassEmailService) Update(
	ctx context.Context,
	id int64,
	input MassEmailUpdateInput,
) (*domain.MassEmailCampaign, error) {
	existing, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status == domain.MassEmailStatusSending {
		return nil, domain.ErrMassEmailSending
	}
	if existing.Status != domain.MassEmailStatusDraft && existing.Status != domain.MassEmailStatusScheduled {
		return nil, domain.ErrMassEmailNotEditable
	}

	subject, bodyHTML, err := normalizeMassEmailContent(input.Subject, input.BodyHTML)
	if err != nil {
		return nil, err
	}

	filter := input.RecipientFilter
	filter.Normalize()
	if err := filter.ValidateSyntax(); err != nil {
		return nil, err
	}

	targetStatus := domain.MassEmailStatusDraft
	var scheduledAt *time.Time
	if input.ScheduledAt != nil {
		at := input.ScheduledAt.UTC()
		if !at.After(time.Now()) {
			return nil, domain.ErrMassEmailInvalidScheduledAt
		}
		scheduledAt = &at
		targetStatus = domain.MassEmailStatusScheduled
		if err := s.ensureSMTPConfigured(ctx); err != nil {
			return nil, err
		}
	}

	// 先解析+校验（含 NO_RECIPIENTS），任何失败都在写库之前返回，不留部分更新。
	candidates, _, _, err := s.ResolveRecipients(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, domain.ErrMassEmailNoRecipients
	}

	existing.Subject = subject
	existing.BodyHTML = bodyHTML
	existing.RecipientFilter = filter
	existing.ScheduledAt = scheduledAt
	existing.Status = targetStatus

	// 内容 + 收件人 + 计数 + 状态在同一个事务里，并用 status IN (draft,scheduled) 做 CAS。
	recipients := massEmailRecipientsFromCandidates(id, candidates)
	ok, err := s.repo.UpdateCampaignWithRecipients(ctx, existing, recipients)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 期间被 worker 推进到 sending（或已结束）→ 409。
		return nil, domain.ErrMassEmailSending
	}
	return s.repo.GetCampaign(ctx, id)
}

// Get 读取任务详情与统计。
func (s *MassEmailService) Get(ctx context.Context, id int64) (*MassEmailDetail, error) {
	// 先重算再读取：否则返回的 campaign 计数会比 stats 滞后一拍
	//（首次打开 recount 失败、或 worker 刚发完一批时最明显）。
	stats, err := s.repo.RecountCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	campaign, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	return &MassEmailDetail{Campaign: campaign, Stats: stats}, nil
}

// List 分页查询任务列表。
func (s *MassEmailService) List(
	ctx context.Context,
	params pagination.PaginationParams,
	filters domain.MassEmailCampaignListFilters,
) ([]domain.MassEmailCampaign, *pagination.PaginationResult, error) {
	return s.repo.ListCampaigns(ctx, params, filters)
}

// Delete 删除任务（发送中不允许）。
func (s *MassEmailService) Delete(ctx context.Context, id int64) error {
	// 原子删除：先读后删会被 /send 抢先，导致删掉正在发送的任务。
	deleted, err := s.repo.DeleteCampaignIfNotSending(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return domain.ErrMassEmailSending
	}
	return nil
}

// ---------------------------------------------------------------------------
// 发送控制
// ---------------------------------------------------------------------------

// Send 立即开始发送（draft/scheduled/failed → sending）。
func (s *MassEmailService) Send(ctx context.Context, id int64) (*domain.MassEmailCampaign, error) {
	campaign, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	switch campaign.Status {
	case domain.MassEmailStatusSending:
		return campaign, nil
	case domain.MassEmailStatusCancelled:
		return nil, domain.ErrMassEmailCancelled
	case domain.MassEmailStatusCompleted:
		return nil, domain.ErrMassEmailNotEditable
	}

	if err := s.ensureSMTPConfigured(ctx); err != nil {
		return nil, err
	}
	if campaign.TotalCount == 0 {
		return nil, domain.ErrMassEmailNoRecipients
	}

	if _, err := s.repo.TransitionCampaignStatus(
		ctx, id, []string{domain.MassEmailStatusDraft, domain.MassEmailStatusScheduled, domain.MassEmailStatusFailed},
		domain.MassEmailStatusSending, time.Now(),
	); err != nil {
		return nil, err
	}
	return s.repo.GetCampaign(ctx, id)
}

// Cancel 取消任务（draft/scheduled/sending 可取消）。
func (s *MassEmailService) Cancel(ctx context.Context, id int64) (*domain.MassEmailCampaign, error) {
	campaign, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	if domain.IsMassEmailTerminalStatus(campaign.Status) {
		return nil, domain.ErrMassEmailNotCancellable
	}
	if _, err := s.repo.TransitionCampaignStatus(
		ctx, id,
		[]string{domain.MassEmailStatusDraft, domain.MassEmailStatusScheduled, domain.MassEmailStatusSending, domain.MassEmailStatusFailed},
		domain.MassEmailStatusCancelled, time.Now(),
	); err != nil {
		return nil, err
	}
	return s.repo.GetCampaign(ctx, id)
}

// RetryFailed 一键重发所有失败收件人。
func (s *MassEmailService) RetryFailed(ctx context.Context, id int64) (int, *domain.MassEmailCampaign, error) {
	campaign, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	if err := s.ensureSMTPConfigured(ctx); err != nil {
		return 0, nil, err
	}

	reset, err := s.repo.ResetFailedRecipients(ctx, id, nil)
	if err != nil {
		return 0, nil, err
	}
	if reset > 0 && campaign.Status != domain.MassEmailStatusSending {
		if _, err := s.repo.TransitionCampaignStatus(
			ctx, id,
			[]string{domain.MassEmailStatusCompleted, domain.MassEmailStatusFailed, domain.MassEmailStatusCancelled, domain.MassEmailStatusDraft, domain.MassEmailStatusScheduled},
			domain.MassEmailStatusSending, time.Now(),
		); err != nil {
			return 0, nil, err
		}
	}
	updated, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	return reset, updated, nil
}

// RetryRecipient 单个收件人重发。
func (s *MassEmailService) RetryRecipient(
	ctx context.Context,
	campaignID, recipientID int64,
) (*domain.MassEmailRecipient, error) {
	recipient, err := s.repo.GetRecipient(ctx, campaignID, recipientID)
	if err != nil {
		return nil, err
	}
	if recipient.Status != domain.MassEmailRecipientStatusFailed {
		return nil, domain.ErrMassEmailRecipientNotRetryable
	}
	if err := s.ensureSMTPConfigured(ctx); err != nil {
		return nil, err
	}

	reset, err := s.repo.ResetFailedRecipients(ctx, campaignID, &recipientID)
	if err != nil {
		return nil, err
	}
	if reset == 0 {
		return nil, domain.ErrMassEmailRecipientNotRetryable
	}

	campaign, err := s.repo.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if campaign.Status != domain.MassEmailStatusSending {
		if _, err := s.repo.TransitionCampaignStatus(
			ctx, campaignID,
			[]string{domain.MassEmailStatusCompleted, domain.MassEmailStatusFailed, domain.MassEmailStatusCancelled, domain.MassEmailStatusDraft, domain.MassEmailStatusScheduled},
			domain.MassEmailStatusSending, time.Now(),
		); err != nil {
			return nil, err
		}
	}
	return s.repo.GetRecipient(ctx, campaignID, recipientID)
}

// ListRecipients 分页查询收件人明细。
func (s *MassEmailService) ListRecipients(
	ctx context.Context,
	campaignID int64,
	params pagination.PaginationParams,
	filters domain.MassEmailRecipientListFilters,
) ([]domain.MassEmailRecipient, *pagination.PaginationResult, error) {
	return s.repo.ListRecipients(ctx, campaignID, params, filters)
}

// ---------------------------------------------------------------------------
// 测试发送 / 打开统计
// ---------------------------------------------------------------------------

// TestSend 发送测试邮件（默认发给当前管理员）。
func (s *MassEmailService) TestSend(ctx context.Context, fallbackTo string, input MassEmailTestInput) (string, error) {
	to := strings.TrimSpace(input.To)
	if to == "" {
		to = strings.TrimSpace(fallbackTo)
	}
	if !domain.IsValidMassEmailAddress(to) {
		return "", domain.ErrMassEmailInvalidEmail
	}

	subject, bodyHTML, err := normalizeMassEmailContent(input.Subject, input.BodyHTML)
	if err != nil {
		return "", err
	}

	if s.email == nil {
		return "", ErrEmailNotConfigured
	}
	config, err := s.email.GetSMTPConfig(ctx)
	if err != nil {
		if errors.Is(err, ErrEmailNotConfigured) {
			return "", ErrEmailNotConfigured
		}
		return "", err
	}

	siteName := s.siteName(ctx)
	vars := map[string]string{
		"name":            "测试用户",
		"email":           to,
		"enterprise_name": "示例企业",
		"site_name":       siteName,
	}
	rendered := RenderMassEmailTemplate(bodyHTML, vars)
	// 主题是纯文本头部，不能做 HTML 转义（否则姓名里的 & 会显示成 &amp;）。
	renderedSubject := RenderMassEmailTemplatePlain(subject, vars)

	sendCtx, cancel := context.WithTimeout(ctx, massEmailSendTimeout)
	defer cancel()
	if err := s.email.sendEmailWithConfigContext(sendCtx, config, to, renderedSubject, rendered); err != nil {
		return "", domain.ErrMassEmailSendFailed.WithCause(err)
	}
	return to, nil
}

// RecordOpen 记录一次打开；首次打开时顺带刷新任务计数。
func (s *MassEmailService) RecordOpen(ctx context.Context, token string) {
	if s == nil || s.repo == nil {
		return
	}
	recipient, err := s.repo.RecordRecipientOpen(ctx, token, time.Now())
	if err != nil || recipient == nil {
		return
	}
	// 只在该收件人第一次打开时重算，避免机器人预取把 count(*) 放大成热点。
	if recipient.OpenCount == 1 {
		if _, err := s.repo.RecountCampaign(ctx, recipient.CampaignID); err != nil {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] recount after open failed campaign=%d: %v", recipient.CampaignID, err)
		}
	}
}

// TrackingPixelURL 生成追踪像素地址（站点根地址取前端 URL 设置）。
func (s *MassEmailService) TrackingPixelURL(ctx context.Context, token string) string {
	base := s.frontendURL(ctx)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/api/v1/mail/o/" + url.PathEscape(token) + ".gif"
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

func (s *MassEmailService) ensureSMTPConfigured(ctx context.Context) error {
	if s.email == nil {
		return ErrEmailNotConfigured
	}
	if _, err := s.email.GetSMTPConfig(ctx); err != nil {
		if errors.Is(err, ErrEmailNotConfigured) {
			return ErrEmailNotConfigured
		}
		return err
	}
	return nil
}

func (s *MassEmailService) siteName(ctx context.Context) string {
	if s.settings != nil {
		if name := strings.TrimSpace(s.settings.GetSiteName(ctx)); name != "" {
			return name
		}
	}
	return "Sub2API"
}

func (s *MassEmailService) frontendURL(ctx context.Context) string {
	if s.settings == nil {
		return ""
	}
	return strings.TrimSpace(s.settings.GetFrontendURL(ctx))
}

// RateLimitPerSecond 读取限速设置，缺省 2 封/秒（Resend 默认限制）。
func (s *MassEmailService) RateLimitPerSecond(ctx context.Context) int {
	const fallback = domain.MassEmailDefaultRateLimitPerSecond
	if s.settingRep == nil {
		return fallback
	}
	raw, err := s.settingRep.GetValue(ctx, MassEmailSettingKeyRateLimitPerSecond)
	if err != nil {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return clampMassEmailRateLimit(value)
}

// normalizeMassEmailContent 校验并净化主题/正文。
func normalizeMassEmailContent(subject, bodyHTML string) (string, string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || len([]rune(subject)) > domain.MassEmailSubjectMaxLen {
		return "", "", domain.ErrMassEmailInvalidSubject
	}
	if len(bodyHTML) > domain.MassEmailBodyMaxBytes {
		return "", "", domain.ErrMassEmailInvalidBody
	}
	sanitized := SanitizeMassEmailHTML(bodyHTML)
	if strings.TrimSpace(sanitized) == "" {
		return "", "", domain.ErrMassEmailInvalidBody
	}
	return subject, sanitized, nil
}

// massEmailRecipientsFromCandidates 把候选收件人转成待落库的收件人（生成追踪 token）。
func massEmailRecipientsFromCandidates(campaignID int64, candidates []domain.MassEmailRecipientCandidate) []domain.MassEmailRecipient {
	out := make([]domain.MassEmailRecipient, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, domain.MassEmailRecipient{
			CampaignID:    campaignID,
			Email:         candidate.Email,
			Name:          candidate.Name,
			UserID:        candidate.UserID,
			EnterpriseID:  candidate.EnterpriseID,
			Status:        domain.MassEmailRecipientStatusPending,
			TrackingToken: NewMassEmailTrackingToken(),
		})
	}
	return out
}

// NewMassEmailTrackingToken 生成 32 字符追踪 token（正好等于列宽 VARCHAR(32)）。
func NewMassEmailTrackingToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败极罕见，退回时间戳保证不 panic。
		return fmt.Sprintf("%032x", time.Now().UnixNano())[:domain.MassEmailTrackingTokenLen]
	}
	return hex.EncodeToString(buf)
}
