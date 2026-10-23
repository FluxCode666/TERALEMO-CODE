package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type videoBillingReservationRepository struct {
	db      *sql.DB
	billing *usageBillingRepository
}

func NewVideoBillingReservationRepository(db *sql.DB, cache ...service.EnterpriseBillingCache) service.VideoBillingReservationRepository {
	billing := &usageBillingRepository{db: db}
	if len(cache) > 0 {
		billing.enterpriseCache = cache[0]
	}
	return &videoBillingReservationRepository{db: db, billing: billing}
}

const videoReservationColumns = `id,user_id,api_key_id,group_id,subscription_id,enterprise_id,
 enterprise_department_id,enterprise_member_id,enterprise_department_path_ids,platform,protocol,
 endpoint,task_id,file_id,account_id,hold_amount,actual_amount,contract,snapshot,state,attempts,last_error,
 next_attempt_at,lease_owner,lease_until,created_at,updated_at`
const videoActiveHolds = `state IN ('preparing','accepted','manual')`

type videoRowScanner interface{ Scan(...any) error }

func scanVideoReservation(row videoRowScanner) (*service.VideoBillingReservation, error) {
	v := &service.VideoBillingReservation{}
	var path []byte
	err := row.Scan(&v.ID, &v.UserID, &v.APIKeyID, &v.GroupID, &v.SubscriptionID, &v.EnterpriseID,
		&v.EnterpriseDepartmentID, &v.EnterpriseMemberID, &path, &v.Platform, &v.Protocol,
		&v.Endpoint, &v.TaskID, &v.FileID, &v.AccountID, &v.HoldAmount, &v.ActualAmount, &v.Contract, &v.Snapshot, &v.State, &v.Attempts, &v.LastError,
		&v.NextAttemptAt, &v.LeaseOwner, &v.LeaseUntil, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(path, &v.EnterpriseDepartmentPathIDs); err != nil {
		return nil, err
	}
	return v, nil
}

func (r *videoBillingReservationRepository) Get(ctx context.Context, id string) (*service.VideoBillingReservation, error) {
	return scanVideoReservation(r.db.QueryRowContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations WHERE id=$1`, id))
}

func (r *videoBillingReservationRepository) GetByTask(ctx context.Context, protocol, taskID string) (*service.VideoBillingReservation, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, service.ErrVideoReservationNotFound
	}
	return scanVideoReservation(r.db.QueryRowContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations WHERE protocol=$1 AND task_id=$2`, protocol, taskID))
}

func (r *videoBillingReservationRepository) GetByFile(ctx context.Context, protocol, fileID string) (*service.VideoBillingReservation, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, service.ErrVideoReservationNotFound
	}
	return scanVideoReservation(r.db.QueryRowContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations WHERE protocol=$1 AND file_id=$2 ORDER BY created_at DESC LIMIT 1`, protocol, fileID))
}

func (r *videoBillingReservationRepository) BindFile(ctx context.Context, id, fileID string) error {
	if strings.TrimSpace(fileID) == "" {
		return service.ErrVideoReservationInvalid
	}
	return videoRequireUpdated(r.db.ExecContext(ctx, `UPDATE video_billing_reservations SET file_id=$2,updated_at=NOW() WHERE id=$1 AND (file_id='' OR file_id=$2)`, id, fileID))
}

func videoRequireUpdated(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrVideoReservationConflict
	}
	return nil
}

func validVideoAmount(n float64) bool { return n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }

func (r *videoBillingReservationRepository) Reserve(ctx context.Context, v *service.VideoBillingReservation) error {
	if v == nil || strings.TrimSpace(v.ID) == "" || len(v.ID) > 64 || v.UserID <= 0 || v.APIKeyID <= 0 ||
		v.AccountID <= 0 || strings.TrimSpace(v.Platform) == "" || strings.TrimSpace(v.Protocol) == "" ||
		!validVideoAmount(v.HoldAmount) || !json.Valid(v.Contract) ||
		(v.EnterpriseID > 0 && v.EnterpriseMemberID <= 0) {
		return service.ErrVideoReservationInvalid
	}
	if err := service.ValidateFrozenEnterpriseDepartmentIDs(v.EnterpriseDepartmentPathIDs, v.EnterpriseDepartmentID); err != nil {
		return err
	}
	v.HoldAmount = service.QuantizeUsageBillingAmount(v.HoldAmount)
	if len(v.Snapshot) == 0 {
		v.Snapshot = json.RawMessage(`{}`)
	}
	if !json.Valid(v.Snapshot) {
		return service.ErrVideoReservationInvalid
	}
	path := service.FrozenEnterpriseDepartmentIDs(v.EnterpriseDepartmentPathIDs, v.EnterpriseDepartmentID)
	if path == nil {
		path = []int64{}
	}
	pathJSON, _ := json.Marshal(path)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inserted string
	err = tx.QueryRowContext(ctx, `INSERT INTO video_billing_reservations
 (id,user_id,api_key_id,group_id,subscription_id,enterprise_id,enterprise_department_id,
 enterprise_member_id,enterprise_department_path_ids,platform,protocol,endpoint,account_id,hold_amount,contract,snapshot)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
 ON CONFLICT(id) DO NOTHING RETURNING id`, v.ID, v.UserID, v.APIKeyID, v.GroupID, v.SubscriptionID, v.EnterpriseID,
		v.EnterpriseDepartmentID, v.EnterpriseMemberID, string(pathJSON), v.Platform, v.Protocol, v.Endpoint, v.AccountID, v.HoldAmount, string(v.Contract), string(v.Snapshot)).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT user_id=$2 AND api_key_id=$3 AND hold_amount=$4 AND contract=$5::jsonb
 AND enterprise_id=$6 AND subscription_id IS NOT DISTINCT FROM $7 AND account_id=$8
 AND group_id=$9 AND protocol=$10 AND platform=$11 AND endpoint=$12 AND snapshot=$13::jsonb
 AND enterprise_department_id=$14 AND enterprise_member_id=$15 AND enterprise_department_path_ids=$16::jsonb
 FROM video_billing_reservations WHERE id=$1`, v.ID, v.UserID, v.APIKeyID, v.HoldAmount, string(v.Contract), v.EnterpriseID, v.SubscriptionID, v.AccountID,
			v.GroupID, v.Protocol, v.Platform, v.Endpoint, string(v.Snapshot), v.EnterpriseDepartmentID, v.EnterpriseMemberID, string(pathJSON)).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return service.ErrVideoReservationConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	// Wallet locks serialize all holds for one payer across instances. The debit
	// removes held funds from the balance observed by ordinary gateway requests.
	if v.SubscriptionID == nil {
		if _, err = reserveUsageBillingBatchImageBalance(ctx, tx, videoBatchHold(v, 0)); err != nil {
			if errors.Is(err, service.ErrBatchImageInsufficientBalance) {
				return service.ErrInsufficientBalance
			}
			return err
		}
		// Free tasks also acquire the payer lock: quotas may have an explicit zero.
		if v.HoldAmount == 0 && v.EnterpriseID == 0 {
			var id int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, v.UserID).Scan(&id); err != nil {
				return err
			}
		}
	} else {
		if v.EnterpriseID > 0 {
			if err = r.checkSubscriptionEnterpriseAccess(ctx, tx, v); err != nil {
				return err
			}
		}
		if err = r.checkSubscription(ctx, tx, v); err != nil {
			return err
		}
	}
	if v.EnterpriseID > 0 && v.SubscriptionID == nil {
		if err = r.checkEnterpriseQuotas(ctx, tx, v); err != nil {
			return err
		}
	}
	if err = r.checkKeyQuotas(ctx, tx, v); err != nil {
		return err
	}
	if v.SubscriptionID == nil {
		if err = r.checkPlatformQuotas(ctx, tx, v); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	v.State = service.VideoReservationPreparing
	r.invalidateEnterprise(ctx, v, false)
	return nil
}

func videoBatchHold(v *service.VideoBillingReservation, actual float64) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{UserID: v.UserID, APIKeyID: v.APIKeyID, EnterpriseID: v.EnterpriseID,
		EnterpriseDepartmentID: v.EnterpriseDepartmentID, EnterpriseMemberID: v.EnterpriseMemberID,
		EnterpriseDepartmentPathIDs: v.EnterpriseDepartmentPathIDs, HoldAmount: v.HoldAmount, ActualAmount: actual}
}

func videoWindowUsage(used float64, start sql.NullTime, boundary time.Time) float64 {
	if !start.Valid || start.Time.Before(boundary) {
		return 0
	}
	return used
}

func videoExceeds(limit sql.NullFloat64, used, held, amount float64, zeroDisables bool) bool {
	if !limit.Valid {
		return false
	}
	if limit.Float64 == 0 {
		return zeroDisables
	}
	return used+held+amount-limit.Float64 > 0.00000000001
}

// Holds are included in every active window even when accepted in a previous
// one: their eventual completion can fall in the new window. They never expire.
func (r *videoBillingReservationRepository) held(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation, condition string, args ...any) (float64, error) {
	params := []any{v.ID}
	params = append(params, args...)
	var n float64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(hold_amount),0) FROM video_billing_reservations WHERE id<>$1 AND `+videoActiveHolds+` AND `+condition, params...).Scan(&n)
	return n, err
}

func (r *videoBillingReservationRepository) checkKeyQuotas(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation) error {
	var quota, used, limit5, limit1, limit7, used5, used1, used7 float64
	var start5, start1, start7 sql.NullTime
	var userID int64
	var status string
	var expires sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT user_id,status,expires_at,quota,quota_used,rate_limit_5h,rate_limit_1d,rate_limit_7d,
 usage_5h,usage_1d,usage_7d,window_5h_start,window_1d_start,window_7d_start FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, v.APIKeyID).
		Scan(&userID, &status, &expires, &quota, &used, &limit5, &limit1, &limit7, &used5, &used1, &used7, &start5, &start1, &start7)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAPIKeyNotFound
	}
	if err != nil {
		return err
	}
	if userID != v.UserID || (status != service.StatusAPIKeyActive && status != service.StatusAPIKeyQuotaExhausted) || (expires.Valid && !expires.Time.After(time.Now())) {
		return service.ErrVideoReservationInvalid
	}
	held, err := r.held(ctx, tx, v, `api_key_id=$2`, v.APIKeyID)
	if err != nil {
		return err
	}
	if quota > 0 && used+held+v.HoldAmount > quota+0.00000000001 {
		return service.ErrVideoReservationQuotaExceeded
	}
	now := time.Now()
	for _, w := range []struct {
		limit, used float64
		start       sql.NullTime
		duration    time.Duration
		err         error
	}{
		{limit5, used5, start5, 5 * time.Hour, service.ErrAPIKeyRateLimit5hExceeded},
		{limit1, used1, start1, 24 * time.Hour, service.ErrAPIKeyRateLimit1dExceeded},
		{limit7, used7, start7, 7 * 24 * time.Hour, service.ErrAPIKeyRateLimit7dExceeded},
	} {
		used := w.used
		if !w.start.Valid || !w.start.Time.Add(w.duration).After(now) {
			used = 0
		}
		if w.limit > 0 && used+held+v.HoldAmount > w.limit+0.00000000001 {
			return w.err
		}
	}
	return nil
}

func (r *videoBillingReservationRepository) checkPlatformQuotas(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation) error {
	var limits [3]sql.NullFloat64
	var used [3]float64
	var starts [3]sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT daily_limit_usd,weekly_limit_usd,monthly_limit_usd,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,
 daily_window_start,weekly_window_start,monthly_window_start FROM user_platform_quotas WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL FOR UPDATE`, v.UserID, v.Platform).
		Scan(&limits[0], &limits[1], &limits[2], &used[0], &used[1], &used[2], &starts[0], &starts[1], &starts[2])
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	held, err := r.held(ctx, tx, v, `user_id=$2 AND platform=$3 AND subscription_id IS NULL`, v.UserID, v.Platform)
	if err != nil {
		return err
	}
	now := timezone.Now()
	bounds := []time.Time{timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now)}
	if floor := v.QuotaUsageFloor; floor != nil {
		floorUsage := []float64{floor.DailyUsageUSD, floor.WeeklyUsageUSD, floor.MonthlyUsageUSD}
		floorStarts := []*time.Time{floor.DailyWindowStart, floor.WeeklyWindowStart, floor.MonthlyWindowStart}
		for i := range used {
			used[i] = videoWindowUsage(used[i], starts[i], bounds[i])
			if start := floorStarts[i]; start != nil && !start.Before(bounds[i]) && validVideoAmount(floorUsage[i]) {
				used[i] = math.Max(used[i], floorUsage[i])
			}
			starts[i] = sql.NullTime{Time: bounds[i], Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_platform_quotas SET daily_usage_usd=$3,weekly_usage_usd=$4,monthly_usage_usd=$5,
 daily_window_start=$6,weekly_window_start=$7,monthly_window_start=$8,updated_at=NOW()
 WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL`, v.UserID, v.Platform, used[0], used[1], used[2], bounds[0], bounds[1], bounds[2]); err != nil {
			return err
		}
	}
	for i := range limits {
		if videoExceeds(limits[i], videoWindowUsage(used[i], starts[i], bounds[i]), held, v.HoldAmount, true) {
			return service.ErrVideoReservationQuotaExceeded
		}
	}
	return nil
}

func (r *videoBillingReservationRepository) checkSubscription(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation) error {
	sub, err := normalizeVideoSubscription(ctx, tx, *v.SubscriptionID)
	if err != nil {
		return err
	}
	var limits [3]sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT g.daily_limit_usd,g.weekly_limit_usd,g.monthly_limit_usd
 FROM user_subscriptions us JOIN groups g ON g.id=us.group_id WHERE us.id=$1 AND us.deleted_at IS NULL AND g.deleted_at IS NULL`, *v.SubscriptionID).
		Scan(&limits[0], &limits[1], &limits[2])
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSubscriptionNotFound
	}
	if err != nil {
		return err
	}
	if sub.UserID != v.UserID || sub.GroupID != v.GroupID || sub.Status != "active" || !sub.ExpiresAt.After(time.Now()) || sub.StartsAt.After(time.Now()) {
		return service.ErrVideoReservationInvalid
	}
	held, err := r.held(ctx, tx, v, `subscription_id=$2`, *v.SubscriptionID)
	if err != nil {
		return err
	}
	for i, u := range []float64{sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD} {
		if videoExceeds(limits[i], u, held, v.HoldAmount, false) {
			return service.ErrVideoReservationQuotaExceeded
		}
	}
	return nil
}

func normalizeVideoSubscription(ctx context.Context, tx *sql.Tx, id int64) (*service.UserSubscription, error) {
	sub := &service.UserSubscription{ID: id}
	err := tx.QueryRowContext(ctx, `SELECT user_id,group_id,status,starts_at,expires_at,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,
 daily_window_start,weekly_window_start,monthly_window_start FROM user_subscriptions WHERE id=$1 FOR UPDATE`, id).
		Scan(&sub.UserID, &sub.GroupID, &sub.Status, &sub.StartsAt, &sub.ExpiresAt, &sub.DailyUsageUSD, &sub.WeeklyUsageUSD, &sub.MonthlyUsageUSD,
			&sub.DailyWindowStart, &sub.WeeklyWindowStart, &sub.MonthlyWindowStart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	service.NormalizeVideoSubscriptionWindows(sub, timezone.Now())
	_, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=$2,weekly_usage_usd=$3,monthly_usage_usd=$4,
 daily_window_start=$5,weekly_window_start=$6,monthly_window_start=$7,updated_at=NOW() WHERE id=$1`, id,
		sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD, sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart)
	return sub, err
}

func (r *videoBillingReservationRepository) checkEnterpriseQuotas(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation) error {
	path := service.FrozenEnterpriseDepartmentIDs(v.EnterpriseDepartmentPathIDs, v.EnterpriseDepartmentID)
	rows, err := tx.QueryContext(ctx, `SELECT scope_type,scope_id,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,total_limit_usd,
 daily_usage_usd,weekly_usage_usd,monthly_usage_usd,total_usage_usd,daily_window_start,weekly_window_start,monthly_window_start
 FROM enterprise_quotas WHERE enterprise_id=$1 AND deleted_at IS NULL
 AND ((scope_type='enterprise' AND scope_id=$1) OR (scope_type='department' AND scope_id=ANY($2::bigint[])) OR (scope_type='member' AND scope_id=$3))
 ORDER BY id FOR UPDATE`, v.EnterpriseID, pq.Array(path), v.EnterpriseMemberID)
	if err != nil {
		return err
	}
	type quota struct {
		scope  string
		id     int64
		limits [4]sql.NullFloat64
		used   [4]float64
		starts [3]sql.NullTime
	}
	var quotas []quota
	for rows.Next() {
		var q quota
		if err = rows.Scan(&q.scope, &q.id, &q.limits[0], &q.limits[1], &q.limits[2], &q.limits[3], &q.used[0], &q.used[1], &q.used[2], &q.used[3], &q.starts[0], &q.starts[1], &q.starts[2]); err != nil {
			rows.Close()
			return err
		}
		quotas = append(quotas, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(quotas) != 2+len(path) {
		return service.ErrBillingServiceUnavailable
	}
	now := timezone.Now()
	bounds := []time.Time{timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now)}
	for _, q := range quotas {
		condition := `enterprise_id=$2 AND subscription_id IS NULL`
		args := []any{v.EnterpriseID}
		failure := error(service.ErrEnterpriseBillingQuotaExceeded)
		if q.scope == "member" {
			condition += ` AND enterprise_member_id=$3`
			args = append(args, q.id)
			failure = service.ErrEnterpriseBillingMemberQuotaExceeded
		}
		if q.scope == "department" {
			condition += ` AND (enterprise_department_id=$3 OR enterprise_department_path_ids @> jsonb_build_array($3::bigint))`
			args = append(args, q.id)
			failure = service.ErrEnterpriseBillingDepartmentQuotaExceeded
		}
		h, err := r.held(ctx, tx, v, condition, args...)
		if err != nil {
			return err
		}
		for i := range q.limits {
			u := q.used[i]
			if i < 3 {
				u = videoWindowUsage(u, q.starts[i], bounds[i])
			}
			if videoExceeds(q.limits[i], u, h, v.HoldAmount, true) {
				return failure
			}
		}
	}
	return nil
}

func (r *videoBillingReservationRepository) Accept(ctx context.Context, id, taskID string, accountID int64) error {
	if strings.TrimSpace(taskID) == "" || accountID <= 0 {
		return service.ErrVideoReservationInvalid
	}
	return videoRequireUpdated(r.db.ExecContext(ctx, `UPDATE video_billing_reservations SET task_id=$2,account_id=$3,
 state=CASE WHEN state='preparing' THEN 'accepted' ELSE state END,next_attempt_at=NOW(),updated_at=NOW()
 WHERE id=$1 AND (task_id='' OR task_id=$2) AND account_id=$3 AND state IN ('preparing','accepted','manual')`, id, taskID, accountID))
}

func (r *videoBillingReservationRepository) ClaimDue(ctx context.Context, owner string, limit int, lease time.Duration) ([]*service.VideoBillingReservation, error) {
	if owner == "" || len(owner) > 128 || limit <= 0 || lease <= 0 {
		return nil, service.ErrVideoReservationInvalid
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `WITH due AS (SELECT id FROM video_billing_reservations
 WHERE state IN ('preparing','accepted') AND next_attempt_at<=NOW() AND (lease_until IS NULL OR lease_until<NOW())
 AND (state <> 'preparing' OR created_at <= NOW() - INTERVAL '3 minutes')
 ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $1)
 UPDATE video_billing_reservations v SET lease_owner=$2,lease_until=NOW()+$3::interval,attempts=attempts+1,updated_at=NOW()
 FROM due WHERE v.id=due.id RETURNING `+prefixedVideoColumns("v"), limit, owner, fmt.Sprintf("%f seconds", lease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*service.VideoBillingReservation
	for rows.Next() {
		v, err := scanVideoReservation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func prefixedVideoColumns(prefix string) string {
	fields := strings.Split(videoReservationColumns, ",")
	for i := range fields {
		fields[i] = prefix + "." + strings.TrimSpace(fields[i])
	}
	return strings.Join(fields, ",")
}

func (r *videoBillingReservationRepository) Reschedule(ctx context.Context, id, owner string, next time.Time, lastError string) error {
	return videoRequireUpdated(r.db.ExecContext(ctx, `UPDATE video_billing_reservations SET next_attempt_at=$3,last_error=$4,
 lease_owner='',lease_until=NULL,updated_at=NOW() WHERE id=$1 AND lease_owner=$2 AND state IN ('preparing','accepted')`, id, owner, next, lastError))
}

func (r *videoBillingReservationRepository) MarkManual(ctx context.Context, id, reason string) error {
	return videoRequireUpdated(r.db.ExecContext(ctx, `UPDATE video_billing_reservations SET state='manual',last_error=$2,
 lease_owner='',lease_until=NULL,updated_at=NOW() WHERE id=$1 AND state IN ('preparing','accepted','manual')`, id, reason))
}

func (r *videoBillingReservationRepository) invalidateEnterprise(ctx context.Context, v *service.VideoBillingReservation, capture bool) {
	if v.EnterpriseID > 0 {
		r.billing.invalidateBatchImageEnterpriseCache(ctx, videoBatchHold(v, 0), capture)
	}
}

func (r *videoBillingReservationRepository) Settle(ctx context.Context, s *service.VideoReservationSettlement) (*service.VideoReservationResult, error) {
	if s == nil || s.Usage == nil || !validVideoAmount(s.Usage.ActualCost) || strings.TrimSpace(s.Usage.RequestID) == "" {
		return nil, service.ErrVideoReservationInvalid
	}
	if s.ManualActorID < 0 || (s.ManualActorID > 0 && strings.TrimSpace(s.ManualReason) == "") {
		return nil, service.ErrVideoReservationInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	v, err := scanVideoReservation(tx.QueryRowContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations WHERE id=$1 FOR UPDATE`, s.ID))
	if err != nil {
		return nil, err
	}
	result := &service.VideoReservationResult{ActualAmount: v.ActualAmount}
	if v.State == service.VideoReservationSettled || v.State == service.VideoReservationReleased {
		r.invalidateEnterprise(ctx, v, true)
		return result, nil
	}
	if s.ManualActorID > 0 && v.State != service.VideoReservationManual {
		return nil, service.ErrVideoManualOnly
	}
	actual := service.QuantizeUsageBillingAmount(s.Usage.ActualCost)
	if actual > v.HoldAmount {
		actual = v.HoldAmount
		result.Capped = true
	}
	result.ActualAmount = actual
	log := *s.Usage
	log.UserID, log.APIKeyID, log.AccountID = v.UserID, v.APIKeyID, v.AccountID
	log.GroupID = &v.GroupID
	log.SubscriptionID = v.SubscriptionID
	log.ActualCost = actual
	service.ApplyUsageLogPayerIdentity(&log, v.EnterpriseID, v.EnterpriseDepartmentID)
	cmd := service.UsageBillingCommand{}
	if s.Billing != nil {
		cmd = *s.Billing
	}
	cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.AccountID = log.RequestID, v.APIKeyID, v.UserID, v.AccountID
	cmd.BalanceCost, cmd.EnterpriseBalanceCost, cmd.SubscriptionCost = 0, 0, 0
	cmd.EnterpriseID, cmd.EnterpriseDepartmentID, cmd.EnterpriseMemberID = v.EnterpriseID, v.EnterpriseDepartmentID, v.EnterpriseMemberID
	cmd.EnterpriseDepartmentPathIDs = v.EnterpriseDepartmentPathIDs
	cmd.SubscriptionID = v.SubscriptionID
	// Even an unlimited key accrues usage; quota settings can change while a
	// task is running. A removed key is skipped by the shared counter helpers.
	cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost = actual, actual
	cmd.RequestFingerprint = ""
	cmd.Normalize()
	claimed, err := r.billing.claimUsageBillingKey(ctx, tx, &cmd)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, service.ErrVideoReservationConflict
	}
	if err = r.captureWallet(ctx, tx, v, actual, result); err != nil {
		return nil, err
	}
	if err = r.billing.applyUsageBillingEffects(ctx, tx, &cmd, &service.UsageBillingApplyResult{}); err != nil {
		return nil, err
	}
	if v.SubscriptionID == nil {
		if err = incrementVideoPlatformQuota(ctx, tx, v.UserID, v.Platform, actual); err != nil {
			return nil, err
		}
	}
	// A zero placeholder from a legacy failed attempt must not silently win.
	// This reservation owns the successful dedup claim, so replacing that one
	// placeholder in this transaction is safe; a genuinely settled row is not.
	if _, err = tx.ExecContext(ctx, `DELETE FROM usage_logs WHERE request_id=$1 AND api_key_id=$2 AND actual_cost=0`, log.RequestID, log.APIKeyID); err != nil {
		return nil, err
	}
	inserted, err := (&usageLogRepository{}).createSingle(ctx, tx, &log)
	if err != nil {
		return nil, err
	}
	if !inserted {
		return nil, service.ErrVideoReservationConflict
	}
	resolutionNote := ""
	if s.ManualActorID > 0 {
		resolutionNote = fmt.Sprintf("manual settlement by administrator %d: %s", s.ManualActorID, strings.TrimSpace(s.ManualReason))
	}
	if _, err = tx.ExecContext(ctx, `UPDATE video_billing_reservations SET state='settled',actual_amount=$2,lease_owner='',lease_until=NULL,last_error=$3,updated_at=NOW() WHERE id=$1`, v.ID, actual, resolutionNote); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	*s.Usage = log
	result.Applied = true
	r.invalidateEnterprise(ctx, v, true)
	return result, nil
}

func (r *videoBillingReservationRepository) captureWallet(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation, actual float64, result *service.VideoReservationResult) error {
	if v.SubscriptionID != nil {
		// Billing follows the accepted subscription even after membership/group
		// changes or archival. The row is retained as a payment identity snapshot.
		if _, err := normalizeVideoSubscription(ctx, tx, *v.SubscriptionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=daily_usage_usd+$2,
 weekly_usage_usd=weekly_usage_usd+$2,monthly_usage_usd=monthly_usage_usd+$2,updated_at=NOW() WHERE id=$1`, *v.SubscriptionID, actual)
		return videoRequireUpdated(res, err)
	}
	table, id := "users", v.UserID
	if v.EnterpriseID > 0 {
		table, id = "enterprises", v.EnterpriseID
	}
	var balance float64
	err := tx.QueryRowContext(ctx, `UPDATE `+table+` SET balance=balance+$2-$3,frozen_balance=frozen_balance-$2,updated_at=NOW()
 WHERE id=$1 AND frozen_balance >= $2 RETURNING balance`, id, v.HoldAmount, actual).Scan(&balance)
	if err != nil {
		return err
	}
	if v.EnterpriseID > 0 {
		result.NewEnterpriseBalance = &balance
		_, err = incrementUsageBillingEnterpriseQuotas(ctx, tx, &service.UsageBillingCommand{RequestID: v.ID, APIKeyID: v.APIKeyID,
			EnterpriseID: v.EnterpriseID, EnterpriseDepartmentID: v.EnterpriseDepartmentID, EnterpriseMemberID: v.EnterpriseMemberID,
			EnterpriseDepartmentPathIDs: v.EnterpriseDepartmentPathIDs, EnterpriseBalanceCost: actual})
	} else {
		result.NewBalance = &balance
	}
	return err
}

func incrementVideoPlatformQuota(ctx context.Context, tx *sql.Tx, userID int64, platform string, amount float64) error {
	now := timezone.Now()
	d, w, m := timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now)
	_, err := tx.ExecContext(ctx, `UPDATE user_platform_quotas SET
 daily_usage_usd=CASE WHEN daily_window_start IS NULL OR daily_window_start<$4 THEN $3 ELSE daily_usage_usd+$3 END,
 weekly_usage_usd=CASE WHEN weekly_window_start IS NULL OR weekly_window_start<$5 THEN $3 ELSE weekly_usage_usd+$3 END,
 monthly_usage_usd=CASE WHEN monthly_window_start IS NULL OR monthly_window_start<$6 THEN $3 ELSE monthly_usage_usd+$3 END,
 daily_window_start=CASE WHEN daily_window_start IS NULL OR daily_window_start<$4 THEN $4 ELSE daily_window_start END,
 weekly_window_start=CASE WHEN weekly_window_start IS NULL OR weekly_window_start<$5 THEN $5 ELSE weekly_window_start END,
 monthly_window_start=CASE WHEN monthly_window_start IS NULL OR monthly_window_start<$6 THEN $6 ELSE monthly_window_start END,
 updated_at=NOW() WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL`, userID, platform, amount, d, w, m)
	return err
}

func (r *videoBillingReservationRepository) Release(ctx context.Context, id, reason string) (*service.VideoReservationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	v, err := scanVideoReservation(tx.QueryRowContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	result := &service.VideoReservationResult{ActualAmount: v.ActualAmount}
	if v.State == service.VideoReservationSettled || v.State == service.VideoReservationReleased {
		r.invalidateEnterprise(ctx, v, false)
		return result, nil
	}
	if v.SubscriptionID == nil {
		table, payerID := "users", v.UserID
		if v.EnterpriseID > 0 {
			table, payerID = "enterprises", v.EnterpriseID
		}
		var balance float64
		if err = tx.QueryRowContext(ctx, `UPDATE `+table+` SET balance=balance+$2,frozen_balance=frozen_balance-$2,updated_at=NOW()
 WHERE id=$1 AND frozen_balance >= $2 RETURNING balance`, payerID, v.HoldAmount).Scan(&balance); err != nil {
			return nil, err
		}
		if v.EnterpriseID > 0 {
			result.NewEnterpriseBalance = &balance
		} else {
			result.NewBalance = &balance
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE video_billing_reservations SET state='released',last_error=$2,lease_owner='',lease_until=NULL,updated_at=NOW() WHERE id=$1`, id, reason); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	result.Applied = true
	r.invalidateEnterprise(ctx, v, false)
	return result, nil
}
