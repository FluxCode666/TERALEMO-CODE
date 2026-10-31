package service

import (
	"context"
	"time"
)

type ScheduledQuestionTestPlan struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	AccountIDs      []int64    `json:"account_ids"`
	QuestionIDs     []int64    `json:"question_ids"`
	ModelID         string     `json:"model_id"`
	CronExpression  string     `json:"cron_expression"`
	Enabled         bool       `json:"enabled"`
	MinPassRatio    int        `json:"min_pass_ratio"`
	CooldownMinutes int        `json:"cooldown_minutes"`
	NotifyEnabled   bool       `json:"notify_enabled"`
	LastRunAt       *time.Time `json:"last_run_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastStatus      string     `json:"last_status"`
	PassedAccounts  int        `json:"passed_accounts"`
	TotalAccounts   int        `json:"total_accounts"`
	AlertedAccounts int        `json:"alerted_accounts"`
	Running         bool       `json:"running"`
}

type ScheduledQuestionTestAccepted struct {
	Accepted bool  `json:"accepted"`
	PlanID   int64 `json:"plan_id"`
}

type ScheduledQuestionTestAccountSummary struct {
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	Platform    string `json:"platform"`
	Passed      int    `json:"passed"`
	Total       int    `json:"total"`
	Judged      int    `json:"judged"`
	Ratio       *int   `json:"ratio"`
	Alerting    bool   `json:"alerting"`
	StateAfter  string `json:"state_after"`
	Error       string `json:"error"`
}

type ScheduledQuestionTestRun struct {
	CronExpression          string                                `json:"-"`
	PlanUpdatedAt           time.Time                             `json:"-"`
	ID                      int64                                 `json:"id"`
	PlanID                  int64                                 `json:"plan_id"`
	Status                  string                                `json:"status"`
	StartedAt               time.Time                             `json:"started_at"`
	FinishedAt              time.Time                             `json:"finished_at"`
	TotalAccounts           int                                   `json:"total_accounts"`
	PassedAccounts          int                                   `json:"passed_accounts"`
	AlertedAccounts         int                                   `json:"alerted_accounts"`
	ErrorMessage            string                                `json:"error_message"`
	NotificationsSuppressed bool                                  `json:"notifications_suppressed"`
	Summary                 []ScheduledQuestionTestAccountSummary `json:"summary"`
	CreatedAt               time.Time                             `json:"created_at"`
}

type ScheduledQuestionTestAccountState struct {
	ID          int64      `json:"id"`
	PlanID      int64      `json:"plan_id"`
	AccountID   int64      `json:"account_id"`
	State       string     `json:"state"`
	LastAlertAt *time.Time `json:"last_alert_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ScheduledQuestionTestPlanInput distinguishes omitted fields from explicit zero values.
type ScheduledQuestionTestPlanInput struct {
	Name            *string
	AccountIDs      *[]int64
	QuestionIDs     *[]int64
	ModelID         *string
	CronExpression  *string
	Enabled         *bool
	MinPassRatio    *int
	CooldownMinutes *int
	NotifyEnabled   *bool
}

type ScheduledQuestionTestPlanUpdateMask struct {
	Name, AccountIDs, QuestionIDs, ModelID, CronExpression bool
	Enabled, MinPassRatio, CooldownMinutes, NotifyEnabled  bool
	NextRunAt                                              bool
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
	FinishRun(ctx context.Context, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, nextRunAt time.Time) (*ScheduledQuestionTestRun, error)
}
