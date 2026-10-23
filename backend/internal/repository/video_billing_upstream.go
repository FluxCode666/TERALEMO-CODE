package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *videoBillingReservationRepository) BindUpstreamOrigin(ctx context.Context, id, origin string) (string, error) {
	normalized, err := service.NormalizeVideoUpstreamOrigin(origin)
	if err != nil || normalized != origin {
		return "", service.ErrVideoReservationInvalid
	}
	var bound string
	err = r.db.QueryRowContext(ctx, `UPDATE video_billing_reservations SET snapshot=CASE
 WHEN COALESCE(snapshot->>'upstream_origin','')='' THEN jsonb_set(snapshot,'{upstream_origin}',to_jsonb($2::text))
 ELSE snapshot END,updated_at=NOW() WHERE id=$1 RETURNING snapshot->>'upstream_origin'`, id, origin).Scan(&bound)
	return bound, err
}

func (r *accountRepository) CountActiveVideoBillingReservations(ctx context.Context, accountID int64) (int64, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT COUNT(*) FROM video_billing_reservations WHERE account_id=$1 AND `+videoActiveHolds, accountID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, service.ErrBillingServiceUnavailable
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, rows.Err()
}

var _ service.VideoBillingUpstreamOriginRepository = (*videoBillingReservationRepository)(nil)
var _ service.VideoAccountReservationInspector = (*accountRepository)(nil)

// Subscription bills retain enterprise attribution without charging its wallet.
func (r *videoBillingReservationRepository) checkSubscriptionEnterpriseAccess(ctx context.Context, tx *sql.Tx, v *service.VideoBillingReservation) error {
	var enterpriseStatus, memberStatus string
	err := tx.QueryRowContext(ctx, `SELECT e.status,m.status FROM enterprises e JOIN enterprise_members m ON m.enterprise_id=e.id
 WHERE e.id=$1 AND m.id=$2 AND m.user_id=$3 AND e.deleted_at IS NULL AND m.deleted_at IS NULL FOR SHARE OF e,m`, v.EnterpriseID, v.EnterpriseMemberID, v.UserID).Scan(&enterpriseStatus, &memberStatus)
	if err != nil {
		return err
	}
	if enterpriseStatus != service.EnterpriseStatusActive {
		return service.ErrEnterpriseGatewayDisabled
	}
	if memberStatus != service.EnterpriseMemberStatusActive {
		return service.ErrEnterpriseGatewayMemberDisabled
	}
	return nil
}
