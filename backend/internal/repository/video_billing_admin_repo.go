package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *videoBillingReservationRepository) ListVideoBillingReservations(ctx context.Context, state string, page, pageSize int) ([]*service.VideoBillingReservation, int, error) {
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return nil, 0, service.ErrVideoAdminInput
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_billing_reservations WHERE ($1='' OR state=$1)`, state).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+videoReservationColumns+` FROM video_billing_reservations
 WHERE ($1='' OR state=$1) ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, state, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*service.VideoBillingReservation, 0, pageSize)
	for rows.Next() {
		item, err := scanVideoReservation(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
