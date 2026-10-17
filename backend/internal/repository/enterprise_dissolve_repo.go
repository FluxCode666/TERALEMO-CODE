package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.EnterpriseDissolveRepository = (*enterpriseRepository)(nil)

func (r *enterpriseRepository) Dissolve(ctx context.Context, enterpriseID int64) error {
	return enterpriseWithTx(ctx, r.client, func(ctx context.Context, client *dbent.Client) error {
		if err := enterpriseRequireEnterprise(ctx, client, enterpriseID, true); err != nil {
			return err
		}
		queries := []string{
			`UPDATE enterprise_members SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND deleted_at IS NULL`,
			`UPDATE enterprise_quotas SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND deleted_at IS NULL`,
			// Match group deletion's association-table lock order.
			`DELETE FROM enterprise_allowed_groups WHERE enterprise_id = $1`,
			`DELETE FROM enterprise_group_whitelists WHERE enterprise_id = $1`,
			`DELETE FROM enterprise_group_whitelist_scopes WHERE enterprise_id = $1`,
			`UPDATE enterprise_departments SET deleted_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND deleted_at IS NULL`,
			`UPDATE enterprises SET deleted_at = NOW(), updated_at = NOW(), group_pool_configured = FALSE WHERE id = $1 AND deleted_at IS NULL`,
		}
		for _, query := range queries {
			if _, err := client.ExecContext(ctx, query, enterpriseID); err != nil {
				return translatePersistenceError(err, nil, nil)
			}
		}
		return nil
	})
}
