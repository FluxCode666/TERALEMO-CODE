//go:build unit

package handler

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// videoSubmissionReservationRepo models the durable repository's state changes
// for handler tests. Monetary admission and SQL concurrency are tested against
// PostgreSQL by the repository tests rather than simulated here.
type videoSubmissionReservationRepo struct {
	mu          sync.Mutex
	rows        map[string]*service.VideoBillingReservation
	settlements map[string]*service.UsageLog
}

func newVideoSubmissionReservationRepo() *videoSubmissionReservationRepo {
	return &videoSubmissionReservationRepo{rows: make(map[string]*service.VideoBillingReservation), settlements: make(map[string]*service.UsageLog)}
}

func cloneVideoSubmissionReservation(v *service.VideoBillingReservation) *service.VideoBillingReservation {
	if v == nil {
		return nil
	}
	c := *v
	c.Contract = append([]byte(nil), v.Contract...)
	c.Snapshot = append([]byte(nil), v.Snapshot...)
	c.EnterpriseDepartmentPathIDs = append([]int64(nil), v.EnterpriseDepartmentPathIDs...)
	return &c
}

func (r *videoSubmissionReservationRepo) Reserve(ctx context.Context, v *service.VideoBillingReservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[v.ID]; ok {
		return service.ErrVideoReservationConflict
	}
	v.State = service.VideoReservationPreparing
	r.rows[v.ID] = cloneVideoSubmissionReservation(v)
	return nil
}

func (r *videoSubmissionReservationRepo) Accept(ctx context.Context, id, taskID string, accountID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.rows[id]
	if v == nil {
		return service.ErrVideoReservationNotFound
	}
	for _, other := range r.rows {
		if other.ID != id && other.Protocol == v.Protocol && other.TaskID == taskID {
			return service.ErrVideoReservationConflict
		}
	}
	v.TaskID, v.AccountID, v.State = taskID, accountID, service.VideoReservationAccepted
	return nil
}

func (r *videoSubmissionReservationRepo) Get(ctx context.Context, id string) (*service.VideoBillingReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.rows[id]; v != nil {
		return cloneVideoSubmissionReservation(v), nil
	}
	return nil, service.ErrVideoReservationNotFound
}

func (r *videoSubmissionReservationRepo) GetByTask(ctx context.Context, protocol, taskID string) (*service.VideoBillingReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.rows {
		if v.Protocol == protocol && v.TaskID == taskID {
			return cloneVideoSubmissionReservation(v), nil
		}
	}
	return nil, service.ErrVideoReservationNotFound
}

func (r *videoSubmissionReservationRepo) BindFile(ctx context.Context, id, fileID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.rows[id]; v != nil {
		v.FileID = fileID
		return nil
	}
	return service.ErrVideoReservationNotFound
}

func (r *videoSubmissionReservationRepo) GetByFile(ctx context.Context, protocol, fileID string) (*service.VideoBillingReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.rows {
		if v.Protocol == protocol && v.FileID == fileID {
			return cloneVideoSubmissionReservation(v), nil
		}
	}
	return nil, service.ErrVideoReservationNotFound
}

func (r *videoSubmissionReservationRepo) ClaimDue(context.Context, string, int, time.Duration) ([]*service.VideoBillingReservation, error) {
	return nil, nil
}
func (r *videoSubmissionReservationRepo) Reschedule(context.Context, string, string, time.Time, string) error {
	return nil
}
func (r *videoSubmissionReservationRepo) MarkManual(ctx context.Context, id, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.rows[id]; v != nil {
		v.State, v.LastError = service.VideoReservationManual, reason
		return nil
	}
	return service.ErrVideoReservationNotFound
}

func (r *videoSubmissionReservationRepo) Settle(ctx context.Context, s *service.VideoReservationSettlement) (*service.VideoReservationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.rows[s.ID]
	if v == nil {
		return nil, service.ErrVideoReservationNotFound
	}
	if v.State == service.VideoReservationSettled || v.State == service.VideoReservationReleased {
		return &service.VideoReservationResult{}, nil
	}
	log := *s.Usage
	capped := log.ActualCost > v.HoldAmount
	if capped {
		log.ActualCost = v.HoldAmount
	}
	v.ActualAmount, v.State = log.ActualCost, service.VideoReservationSettled
	r.settlements[s.ID] = &log
	return &service.VideoReservationResult{Applied: true, Capped: capped, ActualAmount: log.ActualCost}, nil
}

func (r *videoSubmissionReservationRepo) Release(ctx context.Context, id, reason string) (*service.VideoReservationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.rows[id]
	if v == nil {
		return nil, service.ErrVideoReservationNotFound
	}
	if v.State == service.VideoReservationSettled || v.State == service.VideoReservationReleased {
		return &service.VideoReservationResult{}, nil
	}
	v.State, v.LastError = service.VideoReservationReleased, reason
	return &service.VideoReservationResult{Applied: true}, nil
}

var _ service.VideoBillingReservationRepository = (*videoSubmissionReservationRepo)(nil)
