package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestUserGroupRateEffectiveQueryPriority(t *testing.T) {
	tests := []struct {
		name              string
		memberStatus      string
		enterpriseStatus  string
		memberDeleted     bool
		enterpriseDeleted bool
		enterpriseRate    *float64
		personalRate      *float64
		want              *float64
	}{
		{name: "enterprise overrides personal", enterpriseRate: ratePointer(0.8), personalRate: ratePointer(0.6), want: ratePointer(0.8)},
		{name: "enterprise zero overrides personal", enterpriseRate: ratePointer(0), personalRate: ratePointer(0.6), want: ratePointer(0)},
		{name: "enterprise null falls back to personal", personalRate: ratePointer(0.6), want: ratePointer(0.6)},
		{name: "enterprise only", enterpriseRate: ratePointer(0.8), want: ratePointer(0.8)},
		{name: "no override keeps default"},
		{name: "disabled member", memberStatus: service.EnterpriseMemberStatusDisabled, enterpriseRate: ratePointer(0.8), personalRate: ratePointer(0.6), want: ratePointer(0.6)},
		{name: "disabled enterprise", enterpriseStatus: service.EnterpriseStatusDisabled, enterpriseRate: ratePointer(0.8), personalRate: ratePointer(0.6), want: ratePointer(0.6)},
		{name: "removed member", memberDeleted: true, enterpriseRate: ratePointer(0.8), personalRate: ratePointer(0.6), want: ratePointer(0.6)},
		{name: "deleted enterprise", enterpriseDeleted: true, enterpriseRate: ratePointer(0.8), personalRate: ratePointer(0.6), want: ratePointer(0.6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			for _, query := range []string{
				`CREATE TABLE enterprises (id INTEGER, status TEXT, deleted_at DATETIME)`,
				`CREATE TABLE enterprise_members (enterprise_id INTEGER, user_id INTEGER, status TEXT, deleted_at DATETIME)`,
				`CREATE TABLE enterprise_allowed_groups (enterprise_id INTEGER, group_id INTEGER, rate_multiplier NUMERIC)`,
				`CREATE TABLE user_group_rate_multipliers (user_id INTEGER, group_id INTEGER, rate_multiplier NUMERIC)`,
			} {
				_, err := db.Exec(query)
				require.NoError(t, err)
			}
			if tt.memberStatus == "" {
				tt.memberStatus = service.EnterpriseMemberStatusActive
			}
			if tt.enterpriseStatus == "" {
				tt.enterpriseStatus = service.EnterpriseStatusActive
			}
			_, err = db.Exec(`INSERT INTO enterprises VALUES (1, $1, CASE WHEN $2 THEN CURRENT_TIMESTAMP ELSE NULL END)`, tt.enterpriseStatus, tt.enterpriseDeleted)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO enterprise_members VALUES (1, 1, $1, CASE WHEN $2 THEN CURRENT_TIMESTAMP ELSE NULL END)`, tt.memberStatus, tt.memberDeleted)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO enterprise_allowed_groups VALUES (1, 10, $1)`, tt.enterpriseRate)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO user_group_rate_multipliers VALUES (1, 10, $1), (1, 11, 0.4), (1, 12, NULL), (2, 10, 0.3)`, tt.personalRate)
			require.NoError(t, err)
			repo := &userGroupRateRepository{sql: db}
			ctx := context.Background()
			rate, err := repo.GetEffectiveByUserAndGroup(ctx, 1, 10)
			require.NoError(t, err)
			require.Equal(t, tt.want, rate)
			rates, err := repo.GetEffectiveByUserID(ctx, 1)
			require.NoError(t, err)
			wantRates := map[int64]float64{11: 0.4}
			if tt.want != nil {
				wantRates[10] = *tt.want
			}
			require.Equal(t, wantRates, rates)
			personal, err := repo.GetByUserAndGroup(ctx, 1, 10)
			require.NoError(t, err)
			require.Equal(t, tt.personalRate, personal, "admin personal-rate queries must retain their original meaning")
			rate, err = repo.GetEffectiveByUserAndGroup(ctx, 1, 99)
			require.NoError(t, err)
			require.Nil(t, rate)
			rate, err = repo.GetEffectiveByUserAndGroup(ctx, 2, 10)
			require.NoError(t, err)
			require.Equal(t, ratePointer(0.3), rate, "non-members keep personal overrides")
			rates, err = repo.GetEffectiveByUserID(ctx, 3)
			require.NoError(t, err)
			require.NotNil(t, rates)
			require.Empty(t, rates)
		})
	}
}

func ratePointer(value float64) *float64 { return &value }

var _ service.EffectiveUserGroupRateRepository = (*userGroupRateRepository)(nil)
var _ service.EnterpriseGroupRateRepository = (*enterpriseGroupRepository)(nil)
