package service

import (
	"context"
	"net/url"
	"strings"
	"time"
)

type ScheduledQuestionTestPlan struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	AccountIDs              []int64    `json:"account_ids"`
	QuestionIDs             []int64    `json:"question_ids"`
	ModelID                 string     `json:"model_id"`
	CronExpression          string     `json:"cron_expression"`
	Enabled                 bool       `json:"enabled"`
	MinPassRatio            int        `json:"min_pass_ratio"`
	CooldownMinutes         int        `json:"cooldown_minutes"`
	NotifyEnabled           bool       `json:"notify_enabled"`
	ReportEnabled           bool       `json:"report_enabled"`
	EmailEnabled            bool       `json:"email_enabled"`
	FeishuEnabled           bool       `json:"feishu_enabled"`
	FeishuWebhookURL        string     `json:"-"`
	FeishuSecret            string     `json:"-"`
	FeishuWebhookConfigured bool       `json:"feishu_webhook_configured"`
	FeishuWebhookHint       string     `json:"feishu_webhook_hint"`
	FeishuSecretConfigured  bool       `json:"feishu_secret_configured"`
	AutoPauseEnabled        bool       `json:"auto_pause_enabled"`
	AutoResumeEnabled       bool       `json:"auto_resume_enabled"`
	LastRunAt               *time.Time `json:"last_run_at"`
	NextRunAt               *time.Time `json:"next_run_at"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	LastStatus              string     `json:"last_status"`
	PassedAccounts          int        `json:"passed_accounts"`
	TotalAccounts           int        `json:"total_accounts"`
	AlertedAccounts         int        `json:"alerted_accounts"`
	Running                 bool       `json:"running"`
}

type ScheduledQuestionTestAccepted struct {
	Accepted bool  `json:"accepted"`
	PlanID   int64 `json:"plan_id"`
}

type ScheduledQuestionTestAccountSummary struct {
	AccountID      int64  `json:"account_id"`
	AccountName    string `json:"account_name"`
	Platform       string `json:"platform"`
	Passed         int    `json:"passed"`
	Total          int    `json:"total"`
	Judged         int    `json:"judged"`
	Ratio          *int   `json:"ratio"`
	Alerting       bool   `json:"alerting"`
	StateAfter     string `json:"state_after"`
	Error          string `json:"error"`
	Incomplete     bool   `json:"incomplete,omitempty"`
	ScheduleAction string `json:"schedule_action,omitempty"`
	AutoPaused     bool   `json:"auto_paused"`
}

type ScheduledQuestionTestNotificationResult struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

type ScheduledQuestionTestRun struct {
	CronExpression          string                                    `json:"-"`
	PlanUpdatedAt           time.Time                                 `json:"-"`
	ID                      int64                                     `json:"id"`
	PlanID                  int64                                     `json:"plan_id"`
	Status                  string                                    `json:"status"`
	StartedAt               time.Time                                 `json:"started_at"`
	FinishedAt              time.Time                                 `json:"finished_at"`
	TotalAccounts           int                                       `json:"total_accounts"`
	PassedAccounts          int                                       `json:"passed_accounts"`
	AlertedAccounts         int                                       `json:"alerted_accounts"`
	ErrorMessage            string                                    `json:"error_message"`
	NotificationsSuppressed bool                                      `json:"notifications_suppressed"`
	PausedAccounts          int                                       `json:"paused_accounts"`
	ResumedAccounts         int                                       `json:"resumed_accounts"`
	AutoPauseSkipped        bool                                      `json:"auto_pause_skipped"`
	NotificationResults     []ScheduledQuestionTestNotificationResult `json:"notification_results"`
	Summary                 []ScheduledQuestionTestAccountSummary     `json:"summary"`
	CreatedAt               time.Time                                 `json:"created_at"`
}

type ScheduledQuestionTestAccountState struct {
	ID           int64      `json:"id"`
	PlanID       int64      `json:"plan_id"`
	AccountID    int64      `json:"account_id"`
	State        string     `json:"state"`
	LastAlertAt  *time.Time `json:"last_alert_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	AutoPaused   bool       `json:"auto_paused"`
	AutoPausedAt *time.Time `json:"auto_paused_at"`
}

// ScheduledQuestionTestPlanInput distinguishes omitted fields from explicit zero values.
type ScheduledQuestionTestPlanInput struct {
	Name              *string
	AccountIDs        *[]int64
	QuestionIDs       *[]int64
	ModelID           *string
	CronExpression    *string
	Enabled           *bool
	MinPassRatio      *int
	CooldownMinutes   *int
	NotifyEnabled     *bool
	ReportEnabled     *bool
	EmailEnabled      *bool
	FeishuEnabled     *bool
	FeishuWebhookURL  *string
	FeishuSecret      *string
	AutoPauseEnabled  *bool
	AutoResumeEnabled *bool
}

type ScheduledQuestionTestPlanUpdateMask struct {
	Name, AccountIDs, QuestionIDs, ModelID, CronExpression                     bool
	Enabled, MinPassRatio, CooldownMinutes, NotifyEnabled                      bool
	NextRunAt                                                                  bool
	ReportEnabled, EmailEnabled, FeishuEnabled, FeishuWebhookURL, FeishuSecret bool
	AutoPauseEnabled, AutoResumeEnabled                                        bool
}

type ScheduledQuestionTestFeishuTestInput struct {
	PlanID     int64   `json:"plan_id"`
	WebhookURL string  `json:"webhook_url"`
	Secret     *string `json:"secret"`
}

// PopulateScheduledQuestionTestPlanNotificationConfig exposes only masked configuration.
func PopulateScheduledQuestionTestPlanNotificationConfig(plan *ScheduledQuestionTestPlan) {
	plan.FeishuWebhookConfigured = strings.TrimSpace(plan.FeishuWebhookURL) != ""
	plan.FeishuSecretConfigured = strings.TrimSpace(plan.FeishuSecret) != ""
	plan.FeishuWebhookHint = ""
	if !plan.FeishuWebhookConfigured || validateScheduledQuestionFeishuWebhook(plan.FeishuWebhookURL) != nil {
		return
	}
	u, err := url.Parse(plan.FeishuWebhookURL)
	if err != nil {
		return
	}
	token := strings.TrimPrefix(u.Path, "/open-apis/bot/v2/hook/")
	mask := "****"
	if len([]rune(token)) >= 8 {
		runes := []rune(token)
		mask += string(runes[len(runes)-4:])
	}
	plan.FeishuWebhookHint = u.Host + "/…/hook/" + mask
}

// ScheduledQuestionTestRepository persists a round, its states and schedule atomically.
type ScheduledQuestionTestRepository interface {
	Create(ctx context.Context, plan *ScheduledQuestionTestPlan) (*ScheduledQuestionTestPlan, error)
	GetByID(ctx context.Context, id int64) (*ScheduledQuestionTestPlan, error)
	List(ctx context.Context) ([]*ScheduledQuestionTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]*ScheduledQuestionTestPlan, error)
	Update(ctx context.Context, plan *ScheduledQuestionTestPlan, fields ScheduledQuestionTestPlanUpdateMask) (*ScheduledQuestionTestPlan, error)
	Delete(ctx context.Context, id int64) error
	ListRuns(ctx context.Context, planID int64, limit int) ([]*ScheduledQuestionTestRun, error)
	ListStates(ctx context.Context, planID int64) ([]*ScheduledQuestionTestAccountState, error)
	ReleaseAutoPause(ctx context.Context, planID, accountID int64) (bool, error)
	SetAutoPaused(ctx context.Context, planID, accountID int64, paused bool, at *time.Time) error
	ClaimAutoPauseIfOtherHeld(ctx context.Context, planID, accountID int64, at *time.Time) (bool, error)
	FinishRun(ctx context.Context, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, nextRunAt time.Time) (*ScheduledQuestionTestRun, error)
}
