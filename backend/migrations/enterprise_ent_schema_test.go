package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// Handler tests build databases from the generated Ent schema. Exercise the
// constraints in SQLite as well as the authoritative PostgreSQL migrations.
func TestEnterpriseGeneratedSchemaSQLiteParity(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:enterprise_generated_schema?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })

	company, err := client.Enterprise.Create().SetName("SQLite Enterprise").Save(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultEnterpriseSettings(), company.Settings)
	_, err = client.Enterprise.Create().SetName(company.Name).Save(ctx)
	require.True(t, dbent.IsConstraintError(err))
	other, err := client.Enterprise.Create().SetName("Other Enterprise").Save(ctx)
	require.NoError(t, err)
	department, err := client.EnterpriseDepartment.Create().SetEnterpriseID(company.ID).SetName("Engineering").Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseDepartment.Create().SetEnterpriseID(company.ID).SetName(department.Name).Save(ctx)
	require.True(t, dbent.IsConstraintError(err))
	_, err = client.EnterpriseDepartment.Create().SetEnterpriseID(other.ID).SetName(department.Name).Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().SetEmail("enterprise-schema@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	member, err := client.EnterpriseMember.Create().SetEnterpriseID(company.ID).SetUserID(user.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseMember.Create().SetEnterpriseID(other.ID).SetUserID(user.ID).Save(ctx)
	require.True(t, dbent.IsConstraintError(err))
	require.NoError(t, client.EnterpriseMember.DeleteOne(member).Exec(ctx))
	_, err = client.EnterpriseMember.Create().SetEnterpriseID(other.ID).SetUserID(user.ID).Save(ctx)
	require.NoError(t, err)

	group, err := client.Group.Create().SetName("Enterprise Schema Group").Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseAllowedGroup.Create().SetEnterpriseID(company.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseAllowedGroup.Create().SetEnterpriseID(company.ID).SetGroupID(group.ID).Save(ctx)
	require.True(t, dbent.IsConstraintError(err))
	_, err = client.EnterpriseGroupWhitelist.Create().SetEnterpriseID(company.ID).SetScopeType("enterprise").SetScopeID(company.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Group.DeleteOne(group).Exec(mixins.SkipSoftDelete(ctx)))
	count, err := client.EnterpriseAllowedGroup.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = client.EnterpriseGroupWhitelist.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	var columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pragma_table_info('enterprise_allowed_groups') WHERE pk > 0`).Scan(&columns))
	require.Equal(t, 2, columns)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pragma_table_info('usage_logs') WHERE name IN ('enterprise_id', 'enterprise_department_id') AND "notnull" = 0`).Scan(&columns))
	require.Equal(t, 2, columns)
}
