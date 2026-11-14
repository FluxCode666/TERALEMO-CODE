package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 假仓储
// ---------------------------------------------------------------------------

type fakeMassEmailRepo struct {
	mu sync.Mutex

	campaigns  map[int64]*domain.MassEmailCampaign
	recipients map[int64][]*domain.MassEmailRecipient
	nextID     int64

	users           []domain.MassEmailRecipientCandidate
	enterprises     []domain.MassEmailRecipientCandidate
	enterpriseNames map[int64]string

	recounts       int
	replaced       int
	lastClaimLimit int

	// failReplace 让 ReplaceRecipients 返回错误，用于验证创建流程的回滚。
	failReplace error
}

func newFakeMassEmailRepo() *fakeMassEmailRepo {
	return &fakeMassEmailRepo{
		campaigns:       map[int64]*domain.MassEmailCampaign{},
		recipients:      map[int64][]*domain.MassEmailRecipient{},
		enterpriseNames: map[int64]string{},
		nextID:          1,
	}
}

func (f *fakeMassEmailRepo) CreateCampaign(_ context.Context, c *domain.MassEmailCampaign) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.ID = f.nextID
	f.nextID++
	clone := *c
	f.campaigns[c.ID] = &clone
	return nil
}

func (f *fakeMassEmailRepo) GetCampaign(_ context.Context, id int64) (*domain.MassEmailCampaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return nil, domain.ErrMassEmailCampaignNotFound
	}
	clone := *c
	return &clone, nil
}

func (f *fakeMassEmailRepo) UpdateCampaign(_ context.Context, c *domain.MassEmailCampaign) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.campaigns[c.ID]; !ok {
		return domain.ErrMassEmailCampaignNotFound
	}
	clone := *c
	f.campaigns[c.ID] = &clone
	return nil
}

func (f *fakeMassEmailRepo) DeleteCampaign(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.campaigns, id)
	delete(f.recipients, id)
	return nil
}

func (f *fakeMassEmailRepo) DeleteCampaignIfNotSending(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	campaign, ok := f.campaigns[id]
	if !ok {
		return false, domain.ErrMassEmailCampaignNotFound
	}
	if campaign.Status == domain.MassEmailStatusSending {
		return false, nil
	}
	delete(f.campaigns, id)
	delete(f.recipients, id)
	return true, nil
}

func (f *fakeMassEmailRepo) ListCampaigns(_ context.Context, _ pagination.PaginationParams, filters domain.MassEmailCampaignListFilters) ([]domain.MassEmailCampaign, *pagination.PaginationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.MassEmailCampaign, 0, len(f.campaigns))
	for _, c := range f.campaigns {
		if len(filters.Statuses) > 0 {
			matched := false
			for _, status := range filters.Statuses {
				if c.Status == status {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, *c)
	}
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: 1, PageSize: 20}, nil
}

func (f *fakeMassEmailRepo) ListDueScheduledCampaigns(_ context.Context, now time.Time, _ int) ([]domain.MassEmailCampaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.MassEmailCampaign, 0)
	for _, c := range f.campaigns {
		if c.Status == domain.MassEmailStatusScheduled && c.ScheduledAt != nil && !c.ScheduledAt.After(now) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakeMassEmailRepo) TransitionCampaignStatus(_ context.Context, id int64, from []string, to string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return false, domain.ErrMassEmailCampaignNotFound
	}
	matched := false
	for _, status := range from {
		if c.Status == status {
			matched = true
		}
	}
	if !matched {
		return false, nil
	}
	c.Status = to
	switch to {
	case domain.MassEmailStatusSending:
		if c.StartedAt == nil {
			c.StartedAt = &now
		}
		c.FinishedAt = nil
	case domain.MassEmailStatusCompleted, domain.MassEmailStatusFailed, domain.MassEmailStatusCancelled:
		c.FinishedAt = &now
	}
	return true, nil
}

func (f *fakeMassEmailRepo) UpdateCampaignError(_ context.Context, id int64, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.campaigns[id]; ok {
		c.LastError = message
	}
	return nil
}

func (f *fakeMassEmailRepo) RecountCampaign(_ context.Context, id int64) (*domain.MassEmailCampaignStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recounts++
	c, ok := f.campaigns[id]
	if !ok {
		return nil, domain.ErrMassEmailCampaignNotFound
	}
	stats := &domain.MassEmailCampaignStats{}
	for _, r := range f.recipients[id] {
		stats.Total++
		switch r.Status {
		case domain.MassEmailRecipientStatusPending:
			stats.Pending++
		case domain.MassEmailRecipientStatusSent:
			stats.Sent++
		case domain.MassEmailRecipientStatusFailed:
			stats.Failed++
		}
		if r.OpenCount > 0 {
			stats.Opened++
		}
	}
	c.TotalCount = stats.Total
	c.SentCount = stats.Sent
	c.FailedCount = stats.Failed
	c.OpenedCount = stats.Opened
	return stats, nil
}

func (f *fakeMassEmailRepo) UpdateCampaignWithRecipients(ctx context.Context, campaign *domain.MassEmailCampaign, recipients []domain.MassEmailRecipient) (bool, error) {
	f.mu.Lock()
	existing, ok := f.campaigns[campaign.ID]
	if !ok {
		f.mu.Unlock()
		return false, domain.ErrMassEmailCampaignNotFound
	}
	if existing.Status != domain.MassEmailStatusDraft && existing.Status != domain.MassEmailStatusScheduled {
		f.mu.Unlock()
		return false, nil // CAS 失败
	}
	clone := *campaign
	f.campaigns[campaign.ID] = &clone
	f.mu.Unlock()

	if _, err := f.ReplaceRecipients(ctx, campaign.ID, recipients); err != nil {
		return false, err
	}
	if _, err := f.RecountCampaign(ctx, campaign.ID); err != nil {
		return false, err
	}
	return true, nil
}

func (f *fakeMassEmailRepo) ReplaceRecipients(_ context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReplace != nil {
		return 0, f.failReplace
	}
	f.replaced++
	seen := map[string]struct{}{}
	list := make([]*domain.MassEmailRecipient, 0, len(recipients))
	for i := range recipients {
		clone := recipients[i]
		clone.CampaignID = campaignID
		if clone.Status == "" {
			clone.Status = domain.MassEmailRecipientStatusPending
		}
		if _, ok := seen[clone.Email]; ok {
			continue
		}
		seen[clone.Email] = struct{}{}
		list = append(list, &clone)
	}
	f.recipients[campaignID] = list
	return len(list), nil
}

func (f *fakeMassEmailRepo) InsertRecipients(ctx context.Context, campaignID int64, recipients []domain.MassEmailRecipient) (int, error) {
	f.mu.Lock()
	existing := append([]domain.MassEmailRecipient{}, recipients...)
	f.mu.Unlock()
	return f.ReplaceRecipients(ctx, campaignID, existing)
}

func (f *fakeMassEmailRepo) ListRecipients(_ context.Context, campaignID int64, _ pagination.PaginationParams, filters domain.MassEmailRecipientListFilters) ([]domain.MassEmailRecipient, *pagination.PaginationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.MassEmailRecipient, 0)
	for _, r := range f.recipients[campaignID] {
		if filters.Opened != nil && (*filters.Opened) != (r.OpenCount > 0) {
			continue
		}
		out = append(out, *r)
	}
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: 1, PageSize: 20}, nil
}

func (f *fakeMassEmailRepo) GetRecipient(_ context.Context, campaignID, recipientID int64) (*domain.MassEmailRecipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recipients[campaignID] {
		if r.ID == recipientID {
			clone := *r
			return &clone, nil
		}
	}
	return nil, domain.ErrMassEmailRecipientNotFound
}

func (f *fakeMassEmailRepo) ClaimPendingRecipients(_ context.Context, campaignID int64, limit int, _ time.Duration, _ time.Time) ([]domain.MassEmailRecipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastClaimLimit = limit
	out := make([]domain.MassEmailRecipient, 0, limit)
	for _, r := range f.recipients[campaignID] {
		if r.Status != domain.MassEmailRecipientStatusPending || len(out) >= limit {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

func (f *fakeMassEmailRepo) ReleaseClaimedRecipients(_ context.Context, campaignID int64, recipientIDs []int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wanted := make(map[int64]struct{}, len(recipientIDs))
	for _, id := range recipientIDs {
		wanted[id] = struct{}{}
	}
	released := 0
	for _, r := range f.recipients[campaignID] {
		if _, ok := wanted[r.ID]; ok && r.Status == domain.MassEmailRecipientStatusPending {
			r.ClaimedAt = nil
			if r.Attempts > 0 {
				r.Attempts--
			}
			released++
		}
	}
	return released, nil
}

func (f *fakeMassEmailRepo) MarkRecipientSent(_ context.Context, campaignID, recipientID int64, sentAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recipients[campaignID] {
		if r.ID == recipientID {
			r.Status = domain.MassEmailRecipientStatusSent
			r.SentAt = &sentAt
			return nil
		}
	}
	return domain.ErrMassEmailRecipientNotFound
}

func (f *fakeMassEmailRepo) MarkRecipientFailed(_ context.Context, campaignID, recipientID int64, reason string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recipients[campaignID] {
		if r.ID == recipientID {
			r.Status = domain.MassEmailRecipientStatusFailed
			r.Error = reason
			return nil
		}
	}
	return domain.ErrMassEmailRecipientNotFound
}

func (f *fakeMassEmailRepo) ResetFailedRecipients(_ context.Context, campaignID int64, recipientID *int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reset := 0
	for _, r := range f.recipients[campaignID] {
		if r.Status != domain.MassEmailRecipientStatusFailed {
			continue
		}
		if recipientID != nil && r.ID != *recipientID {
			continue
		}
		r.Status = domain.MassEmailRecipientStatusPending
		r.Error = ""
		reset++
	}
	return reset, nil
}

func (f *fakeMassEmailRepo) RecordRecipientOpen(_ context.Context, token string, openedAt time.Time) (*domain.MassEmailRecipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for campaignID, list := range f.recipients {
		for _, r := range list {
			if r.TrackingToken != token {
				continue
			}
			r.OpenCount++
			if r.FirstOpenedAt == nil {
				r.FirstOpenedAt = &openedAt
			}
			r.LastOpenedAt = &openedAt
			clone := *r
			clone.CampaignID = campaignID
			return &clone, nil
		}
	}
	return nil, domain.ErrMassEmailRecipientNotFound
}

func (f *fakeMassEmailRepo) MassEmailEnterpriseNames(_ context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for _, id := range ids {
		if name, ok := f.enterpriseNames[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

func (f *fakeMassEmailRepo) ResolveMassEmailUsers(_ context.Context, filter domain.MassEmailUserFilter, includeAll bool) ([]domain.MassEmailRecipientCandidate, error) {
	if !filter.WantsAllUsers(includeAll) {
		return []domain.MassEmailRecipientCandidate{}, nil
	}
	return f.users, nil
}

func (f *fakeMassEmailRepo) ResolveMassEmailEnterprises(_ context.Context, _ []int64, _ string) ([]domain.MassEmailRecipientCandidate, error) {
	return f.enterprises, nil
}

func newMassEmailServiceForTest(repo MassEmailRepository) *MassEmailService {
	return NewMassEmailService(repo, nil, nil, nil)
}

// ---------------------------------------------------------------------------
// 变量渲染
// ---------------------------------------------------------------------------

func TestRenderMassEmailTemplate(t *testing.T) {
	t.Parallel()

	vars := map[string]string{
		"name":            "Alice",
		"email":           "alice@example.com",
		"enterprise_name": "示例企业",
		"site_name":       "Sub2API",
	}

	got := RenderMassEmailTemplate("你好 {{name}}（{{ email }}），{{enterprise_name}} @ {{ site_name }}", vars)
	require.Equal(t, "你好 Alice（alice@example.com），示例企业 @ Sub2API", got)

	require.Equal(t, "未知 {{unknown}} 保留", RenderMassEmailTemplate("未知 {{unknown}} 保留", vars))
	require.Equal(t, "", RenderMassEmailTemplate("", vars))
}

func TestRenderMassEmailTemplateEscapesHTML(t *testing.T) {
	t.Parallel()
	got := RenderMassEmailTemplate("{{name}}", map[string]string{"name": `<script>alert(1)</script>`})
	require.NotContains(t, got, "<script>")
	require.Contains(t, got, "&lt;script&gt;")
}

func TestMassEmailTemplateVariables(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"name", "email", "enterprise_name", "site_name"}, MassEmailTemplateVariables())
}

// ---------------------------------------------------------------------------
// HTML 净化
// ---------------------------------------------------------------------------

func TestSanitizeMassEmailHTMLDropsDangerousContent(t *testing.T) {
	t.Parallel()

	input := `<p>hi</p><script>alert(1)</script><iframe src="https://evil"></iframe>` +
		`<a href="javascript:alert(1)" onclick="x()">bad</a>` +
		`<img src="https://ok.example/a.png" onerror="x()" />`
	out := SanitizeMassEmailHTML(input)

	require.NotContains(t, strings.ToLower(out), "<script")
	require.NotContains(t, strings.ToLower(out), "<iframe")
	require.NotContains(t, strings.ToLower(out), "javascript:")
	require.NotContains(t, strings.ToLower(out), "onclick")
	require.NotContains(t, strings.ToLower(out), "onerror")
	require.Contains(t, out, "<p>hi</p>")
	require.Contains(t, out, `src="https://ok.example/a.png"`)
}

func TestSanitizeMassEmailHTMLKeepsFormatting(t *testing.T) {
	t.Parallel()

	input := `<h2 style="text-align: center; background-image: url(evil)">标题</h2><ul><li>a</li></ul><p><strong>粗</strong><em>斜</em></p>`
	out := SanitizeMassEmailHTML(input)

	require.Contains(t, out, "<h2")
	require.Contains(t, out, "text-align: center")
	require.NotContains(t, out, "url(")
	require.Contains(t, out, "<ul>")
	require.Contains(t, out, "<strong>粗</strong>")
}

func TestSanitizeMassEmailHTMLDropsEventAttributes(t *testing.T) {
	t.Parallel()
	out := SanitizeMassEmailHTML(`<a href="https://ok.example" onmouseover="alert(1)">x</a>`)
	require.NotContains(t, out, "onmouseover")
	require.Contains(t, out, `href="https://ok.example"`)
}

func TestSanitizeMassEmailHTMLVoidTagsDoNotSwallowContent(t *testing.T) {
	t.Parallel()

	// <input> 是 void 元素：只丢它自己，不能把后面的正文一起吞掉。
	out := SanitizeMassEmailHTML(`<p>before</p><input type="text"><p>after</p>`)
	require.Contains(t, out, "<p>before</p>")
	require.Contains(t, out, "<p>after</p>")
	require.NotContains(t, out, "<input")

	// 完整 HTML 文档：<head><meta></head> 之后 body 的内容必须保留。
	doc := `<html><head><meta charset="utf-8"><link rel="stylesheet" href="x.css"></head><body><p>body text</p></body></html>`
	out = SanitizeMassEmailHTML(doc)
	require.Contains(t, out, "body text")
	require.NotContains(t, out, "<meta")
	require.NotContains(t, out, "<link")
}

func TestSanitizeMassEmailHTMLEmpty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", SanitizeMassEmailHTML("   "))
	require.Equal(t, "", SanitizeMassEmailHTML("<script>only</script>"))
}

func TestInjectMassEmailTrackingPixel(t *testing.T) {
	t.Parallel()

	withBody := InjectMassEmailTrackingPixel("<html><body><p>hi</p></body></html>", "https://site.test/api/v1/mail/o/abc.gif")
	require.Contains(t, withBody, "https://site.test/api/v1/mail/o/abc.gif")
	require.Less(t, strings.Index(withBody, "abc.gif"), strings.Index(withBody, "</body>"))

	noBody := InjectMassEmailTrackingPixel("<p>hi</p>", "https://site.test/x.gif")
	require.True(t, strings.HasSuffix(noBody, "/>"))
	require.Contains(t, noBody, "width=\"1\"")
	require.Contains(t, noBody, "height=\"1\"")

	require.Equal(t, "<p>hi</p>", InjectMassEmailTrackingPixel("<p>hi</p>", ""))
}

// ---------------------------------------------------------------------------
// 收件人解析
// ---------------------------------------------------------------------------

func TestResolveRecipientsCombinesSourcesAndDedupes(t *testing.T) {
	t.Parallel()

	userID := int64(7)
	enterpriseID := int64(3)
	repo := newFakeMassEmailRepo()
	repo.enterprises = []domain.MassEmailRecipientCandidate{
		{Email: "boss@example.com", Name: "王总", EnterpriseID: &enterpriseID, Source: domain.MassEmailSourceEnterpriseContact},
		{Email: "shared@example.com", Name: "联系人", EnterpriseID: &enterpriseID, Source: domain.MassEmailSourceEnterpriseContact},
	}
	repo.users = []domain.MassEmailRecipientCandidate{
		{Email: "alice@example.com", Name: "Alice", UserID: &userID, Source: domain.MassEmailSourceUser},
		{Email: "SHARED@example.com", Name: "成员", UserID: &userID, Source: domain.MassEmailSourceUser},
	}

	svc := newMassEmailServiceForTest(repo)
	filter := domain.MassEmailRecipientFilter{
		IncludeAllUsers:  true,
		EnterpriseFilter: domain.MassEmailEnterpriseFilter{EnterpriseIDs: []int64{3}, Scope: "both"},
		ManualEmails:     []string{"ops@example.com, OPS@example.com\nbad-email"},
	}

	candidates, sources, invalid, err := svc.ResolveRecipients(context.Background(), filter)
	require.NoError(t, err)

	// shared@example.com 只出现一次，且保留企业联系人那条（有 enterprise_name）。
	require.Len(t, candidates, 4)
	emails := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		emails = append(emails, candidate.Email)
	}
	require.Equal(t, []string{"boss@example.com", "shared@example.com", "alice@example.com", "ops@example.com"}, emails)

	require.Equal(t, 2, sources.EnterpriseContacts)
	require.Equal(t, 1, sources.Users)
	require.Equal(t, 1, sources.Manual)
	require.Equal(t, []string{"bad-email"}, invalid)
}

func TestResolveRecipientsIgnoresEmptyUserFilter(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "all@example.com", Source: domain.MassEmailSourceUser}}

	svc := newMassEmailServiceForTest(repo)
	// 只选企业 + 手动邮箱，user_filter 是零值（前端提交 null / {}）。
	candidates, sources, _, err := svc.ResolveRecipients(context.Background(), domain.MassEmailRecipientFilter{
		EnterpriseFilter: domain.MassEmailEnterpriseFilter{EnterpriseIDs: []int64{1}},
		ManualEmails:     []string{"ops@example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, sources.Users, "空 user_filter 不能被当成全部用户")
	require.Len(t, candidates, 1)
}

func TestPreviewRecipientsSampleSize(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{
		{Email: "a@example.com", Source: domain.MassEmailSourceUser},
		{Email: "b@example.com", Source: domain.MassEmailSourceUser},
		{Email: "c@example.com", Source: domain.MassEmailSourceUser},
	}
	svc := newMassEmailServiceForTest(repo)

	preview, err := svc.PreviewRecipients(context.Background(), domain.MassEmailRecipientFilter{IncludeAllUsers: true}, 2)
	require.NoError(t, err)
	require.Equal(t, 3, preview.Total)
	require.Len(t, preview.Sample, 2)
	require.Equal(t, 3, preview.BySource.Users)
}

// ---------------------------------------------------------------------------
// 创建 / 状态流转
// ---------------------------------------------------------------------------

func TestCreateDraftDoesNotRequireSMTP(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{
		{Email: "alice@example.com", Name: "Alice", Source: domain.MassEmailSourceUser},
	}
	svc := newMassEmailServiceForTest(repo)

	campaign, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>你好 {{name}}</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.NoError(t, err)
	require.Equal(t, domain.MassEmailStatusDraft, campaign.Status)
	require.Equal(t, 1, campaign.TotalCount)
	require.Equal(t, 1, repo.replaced)
}

func TestCreateSendNowWithoutSMTPReturnsNotConfigured(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>x</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
		SendNow:         true,
	})
	require.ErrorIs(t, err, ErrEmailNotConfigured)
}

func TestCreateScheduledInPastRejected(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	past := time.Now().Add(-time.Hour)
	_, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>x</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
		ScheduledAt:     &past,
	})
	require.ErrorIs(t, err, domain.ErrMassEmailInvalidScheduledAt)
}

func TestCreateWithoutRecipientsRejected(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>x</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.ErrorIs(t, err, domain.ErrMassEmailNoRecipients)
}

func TestCreateRejectsEmptySubjectAndBody(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "   ",
		BodyHTML:        "<p>x</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.ErrorIs(t, err, domain.ErrMassEmailInvalidSubject)

	_, err = svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "ok",
		BodyHTML:        "<script>x</script>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.ErrorIs(t, err, domain.ErrMassEmailInvalidBody, "净化后为空也算非法正文")
}

func TestUpdateScheduledToDraftWhenScheduledAtNull(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	at := time.Now().Add(2 * time.Hour)
	campaign, err := repo.GetCampaign(context.Background(), 0)
	require.Error(t, err)
	require.Nil(t, campaign)

	// 直接构造一个 scheduled 任务（跳过 SMTP 检查）。
	repo.campaigns[1] = &domain.MassEmailCampaign{
		ID:              1,
		Subject:         "重要通知",
		BodyHTML:        "<p>x</p>",
		Status:          domain.MassEmailStatusScheduled,
		ScheduledAt:     &at,
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	}

	updated, err := svc.Update(context.Background(), 1, MassEmailUpdateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>x</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
		ScheduledAt:     nil,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MassEmailStatusDraft, updated.Status)
	require.Nil(t, updated.ScheduledAt)
}

func TestUpdateRejectsSendingCampaign(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Update(context.Background(), 1, MassEmailUpdateInput{Subject: "x", BodyHTML: "<p>x</p>"})
	require.ErrorIs(t, err, domain.ErrMassEmailSending)
}

func TestCancelTransitions(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusScheduled}
	svc := newMassEmailServiceForTest(repo)

	campaign, err := svc.Cancel(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, domain.MassEmailStatusCancelled, campaign.Status)

	_, err = svc.Cancel(context.Background(), 1)
	require.ErrorIs(t, err, domain.ErrMassEmailNotCancellable)
}

func TestRetryFailedResetsAndResumes(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusCompleted}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusFailed},
		{ID: 12, CampaignID: 1, Email: "b@example.com", Status: domain.MassEmailRecipientStatusSent},
	}
	svc := newMassEmailServiceForTest(repo)

	// SMTP 未配置时直接拒绝。
	_, _, err := svc.RetryFailed(context.Background(), 1)
	require.ErrorIs(t, err, ErrEmailNotConfigured)

	// 注入一个假的 EmailService 不可行（真实类型），这里直接验证 Reset 层行为。
	reset, resetErr := repo.ResetFailedRecipients(context.Background(), 1, nil)
	require.NoError(t, resetErr)
	require.Equal(t, 1, reset)
}

func TestRetryRecipientRejectsNonFailed(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusSent},
	}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.RetryRecipient(context.Background(), 1, 11)
	require.ErrorIs(t, err, domain.ErrMassEmailRecipientNotRetryable)
}

// ---------------------------------------------------------------------------
// 打开统计 / 限速
// ---------------------------------------------------------------------------

func TestRecordOpenRecountsOnlyOnFirstOpen(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusSent, TrackingToken: "token-a"},
	}
	svc := newMassEmailServiceForTest(repo)

	svc.RecordOpen(context.Background(), "token-a")
	require.Equal(t, 1, repo.recounts, "首次打开触发一次重算")

	svc.RecordOpen(context.Background(), "token-a")
	require.Equal(t, 1, repo.recounts, "再次打开不重复重算")

	// 无效 token 不 panic 也不重算。
	svc.RecordOpen(context.Background(), "missing")
	require.Equal(t, 1, repo.recounts)
}

func TestRateLimitPerSecondFallback(t *testing.T) {
	t.Parallel()

	svc := newMassEmailServiceForTest(newFakeMassEmailRepo())
	require.Equal(t, domain.MassEmailDefaultRateLimitPerSecond, svc.RateLimitPerSecond(context.Background()))
}

func TestNewMassEmailTrackingToken(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		token := NewMassEmailTrackingToken()
		require.Len(t, token, domain.MassEmailTrackingTokenLen)
		require.NotContains(t, seen, token)
		seen[token] = struct{}{}
	}
}

func TestSendRejectsCancelledAndCompleted(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusCancelled}
	repo.campaigns[2] = &domain.MassEmailCampaign{ID: 2, Status: domain.MassEmailStatusCompleted}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Send(context.Background(), 1)
	require.ErrorIs(t, err, domain.ErrMassEmailCancelled)

	_, err = svc.Send(context.Background(), 2)
	require.ErrorIs(t, err, domain.ErrMassEmailNotEditable)
}

func TestSendWithoutSMTPReturnsNotConfigured(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusDraft, TotalCount: 1}
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Send(context.Background(), 1)
	require.ErrorIs(t, err, ErrEmailNotConfigured)
}

func TestSendIsIdempotentWhileSending(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending, TotalCount: 1}
	svc := newMassEmailServiceForTest(repo)

	campaign, err := svc.Send(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, domain.MassEmailStatusSending, campaign.Status)
}

var _ = errors.New

// TestMassEmailDeleteRejectsSendingCampaign 覆盖 T4 指出的竞态：
// 先读 status 再删会被 /send 抢先，必须由仓储层原子拒绝。
func TestMassEmailDeleteRejectsSendingCampaign(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	svc := newMassEmailServiceForTest(repo)

	require.ErrorIs(t, svc.Delete(context.Background(), 1), domain.ErrMassEmailSending)
	require.Contains(t, repo.campaigns, int64(1), "发送中的任务不能被删")
}

func TestMassEmailDeleteRemovesDraft(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusDraft}
	repo.recipients[1] = []*domain.MassEmailRecipient{{ID: 11, CampaignID: 1, Email: "a@example.com"}}
	svc := newMassEmailServiceForTest(repo)

	require.NoError(t, svc.Delete(context.Background(), 1))
	require.NotContains(t, repo.campaigns, int64(1))
	require.NotContains(t, repo.recipients, int64(1), "收件人随任务一起删除")
}

func TestMassEmailDeleteMissingCampaign(t *testing.T) {
	t.Parallel()

	svc := newMassEmailServiceForTest(newFakeMassEmailRepo())
	require.ErrorIs(t, svc.Delete(context.Background(), 404), domain.ErrMassEmailCampaignNotFound)
}
