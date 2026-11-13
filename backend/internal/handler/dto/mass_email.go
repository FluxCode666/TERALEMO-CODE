package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// MassEmailCampaign 邮件群发任务。
type MassEmailCampaign struct {
	ID                int64                           `json:"id"`
	Subject           string                          `json:"subject"`
	BodyHTML          string                          `json:"body_html"`
	RecipientFilter   domain.MassEmailRecipientFilter `json:"recipient_filter"`
	Status            string                          `json:"status"`
	ScheduledAt       *time.Time                      `json:"scheduled_at"`
	TotalCount        int                             `json:"total_count"`
	SentCount         int                             `json:"sent_count"`
	FailedCount       int                             `json:"failed_count"`
	OpenedCount       int                             `json:"opened_count"`
	OpenRate          float64                         `json:"open_rate"`
	CreatedBy         *int64                          `json:"created_by"`
	CreatedByUsername string                          `json:"created_by_username"`
	StartedAt         *time.Time                      `json:"started_at"`
	FinishedAt        *time.Time                      `json:"finished_at"`
	LastError         string                          `json:"last_error"`
	CreatedAt         time.Time                       `json:"created_at"`
	UpdatedAt         time.Time                       `json:"updated_at"`
}

// MassEmailCampaignFromService 转换任务对象（open_rate 由后端算好）。
func MassEmailCampaignFromService(c *domain.MassEmailCampaign) *MassEmailCampaign {
	if c == nil {
		return nil
	}
	return &MassEmailCampaign{
		ID:                c.ID,
		Subject:           c.Subject,
		BodyHTML:          c.BodyHTML,
		RecipientFilter:   c.RecipientFilter,
		Status:            c.Status,
		ScheduledAt:       c.ScheduledAt,
		TotalCount:        c.TotalCount,
		SentCount:         c.SentCount,
		FailedCount:       c.FailedCount,
		OpenedCount:       c.OpenedCount,
		OpenRate:          c.OpenRate(),
		CreatedBy:         c.CreatedBy,
		CreatedByUsername: c.CreatedByUsername,
		StartedAt:         c.StartedAt,
		FinishedAt:        c.FinishedAt,
		LastError:         c.LastError,
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}
}

// MassEmailCampaignsFromService 批量转换。
func MassEmailCampaignsFromService(items []domain.MassEmailCampaign) []MassEmailCampaign {
	out := make([]MassEmailCampaign, 0, len(items))
	for i := range items {
		if item := MassEmailCampaignFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

// MassEmailRecipient 收件人明细（不暴露 tracking_token）。
type MassEmailRecipient struct {
	ID            int64      `json:"id"`
	CampaignID    int64      `json:"campaign_id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	UserID        *int64     `json:"user_id"`
	EnterpriseID  *int64     `json:"enterprise_id"`
	Status        string     `json:"status"`
	Error         string     `json:"error"`
	Attempts      int        `json:"attempts"`
	SentAt        *time.Time `json:"sent_at"`
	FirstOpenedAt *time.Time `json:"first_opened_at"`
	LastOpenedAt  *time.Time `json:"last_opened_at"`
	OpenCount     int        `json:"open_count"`
	CreatedAt     time.Time  `json:"created_at"`
}

// MassEmailRecipientFromService 转换收件人。
func MassEmailRecipientFromService(r *domain.MassEmailRecipient) *MassEmailRecipient {
	if r == nil {
		return nil
	}
	return &MassEmailRecipient{
		ID:            r.ID,
		CampaignID:    r.CampaignID,
		Email:         r.Email,
		Name:          r.Name,
		UserID:        r.UserID,
		EnterpriseID:  r.EnterpriseID,
		Status:        r.Status,
		Error:         r.Error,
		Attempts:      r.Attempts,
		SentAt:        r.SentAt,
		FirstOpenedAt: r.FirstOpenedAt,
		LastOpenedAt:  r.LastOpenedAt,
		OpenCount:     r.OpenCount,
		CreatedAt:     r.CreatedAt,
	}
}

// MassEmailRecipientsFromService 批量转换。
func MassEmailRecipientsFromService(items []domain.MassEmailRecipient) []MassEmailRecipient {
	out := make([]MassEmailRecipient, 0, len(items))
	for i := range items {
		if item := MassEmailRecipientFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

// MassEmailCampaignStats 详情页统计。
type MassEmailCampaignStats struct {
	Total    int     `json:"total"`
	Pending  int     `json:"pending"`
	Sent     int     `json:"sent"`
	Failed   int     `json:"failed"`
	Opened   int     `json:"opened"`
	OpenRate float64 `json:"open_rate"`
}

// MassEmailCampaignStatsFromService 转换统计（open_rate = opened / total）。
func MassEmailCampaignStatsFromService(s *domain.MassEmailCampaignStats) *MassEmailCampaignStats {
	if s == nil {
		return nil
	}
	rate := 0.0
	if s.Total > 0 {
		rate = float64(s.Opened) / float64(s.Total)
	}
	return &MassEmailCampaignStats{
		Total:    s.Total,
		Pending:  s.Pending,
		Sent:     s.Sent,
		Failed:   s.Failed,
		Opened:   s.Opened,
		OpenRate: rate,
	}
}

// MassEmailCampaignDetail 详情响应。
type MassEmailCampaignDetail struct {
	Campaign *MassEmailCampaign      `json:"campaign"`
	Stats    *MassEmailCampaignStats `json:"stats"`
}

// MassEmailPreviewRecipient 预览样例收件人。
type MassEmailPreviewRecipient struct {
	Email        string `json:"email"`
	Name         string `json:"name"`
	UserID       *int64 `json:"user_id"`
	EnterpriseID *int64 `json:"enterprise_id"`
	Source       string `json:"source"`
}

// MassEmailPreviewSources 各来源数量。
type MassEmailPreviewSources struct {
	Users              int `json:"users"`
	EnterpriseContacts int `json:"enterprise_contacts"`
	EnterpriseMembers  int `json:"enterprise_members"`
	Manual             int `json:"manual"`
}

// MassEmailPreview 预览响应。
type MassEmailPreview struct {
	Total         int                         `json:"total"`
	Sample        []MassEmailPreviewRecipient `json:"sample"`
	BySource      MassEmailPreviewSources     `json:"by_source"`
	InvalidEmails []string                    `json:"invalid_emails"`
}

// MassEmailPreviewFromService 转换预览结果。
func MassEmailPreviewFromService(p *service.MassEmailPreview) *MassEmailPreview {
	if p == nil {
		return nil
	}
	sample := make([]MassEmailPreviewRecipient, 0, len(p.Sample))
	for _, candidate := range p.Sample {
		sample = append(sample, MassEmailPreviewRecipient{
			Email:        candidate.Email,
			Name:         candidate.Name,
			UserID:       candidate.UserID,
			EnterpriseID: candidate.EnterpriseID,
			Source:       candidate.Source,
		})
	}
	invalid := p.InvalidEmails
	if invalid == nil {
		invalid = []string{}
	}
	return &MassEmailPreview{
		Total:         p.Total,
		Sample:        sample,
		BySource:      MassEmailPreviewSources(p.BySource),
		InvalidEmails: invalid,
	}
}
