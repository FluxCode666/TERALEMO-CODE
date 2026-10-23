//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newSettledVideoUsageTestRepo(t *testing.T) (*usageLogRepository, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	columns := []string{"id INTEGER PRIMARY KEY AUTOINCREMENT"}
	require.Len(t, settledVideoUsageColumns, len(usageLogInsertArgTypes))
	for i, name := range settledVideoUsageColumns {
		kind := usageLogInsertArgTypes[i]
		if kind == "timestamptz" {
			kind = "TIMESTAMP"
		}
		columns = append(columns, name+" "+kind)
	}
	columns = append(columns, "UNIQUE (request_id, api_key_id)")
	_, err = db.Exec("CREATE TABLE usage_logs (" + strings.Join(columns, ",") + ")")
	require.NoError(t, err)
	return newUsageLogRepositoryWithSQL(nil, db), db
}

func settledVideoUsageTestLog() *service.UsageLog {
	resolution, mode, duration := "2k", "video", 6
	return &service.UsageLog{
		RequestID: "grok-video:gemini:models/veo-3.1-generate-preview/operations/" + strings.Repeat("o", 128),
		UserID:    1, APIKeyID: 2, AccountID: 3, Model: "veo-3.1", RateMultiplier: 3,
		TotalCost: 0.2, ActualCost: 0.6, VideoCount: 1, VideoResolution: &resolution,
		VideoDurationSeconds: &duration, BillingMode: &mode, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestUpsertSettledVideoUsageRepairsPlaceholderAndConfirmsPersistence(t *testing.T) {
	for _, placeholder := range []bool{false, true} {
		t.Run(fmt.Sprintf("placeholder=%v", placeholder), func(t *testing.T) {
			repo, db := newSettledVideoUsageTestRepo(t)
			log := settledVideoUsageTestLog()
			if placeholder {
				_, err := db.Exec("INSERT INTO usage_logs (request_id, api_key_id, actual_cost, model, created_at) VALUES (?, ?, 0, 'failed-placeholder', ?)", log.RequestID, log.APIKeyID, log.CreatedAt)
				require.NoError(t, err)
			}
			require.NoError(t, repo.UpsertSettledVideoUsage(context.Background(), log))
			require.NotZero(t, log.ID)
			var id int64
			var actual, multiplier float64
			var model, resolution, mode, requestID string
			var count, duration int
			require.NoError(t, db.QueryRow("SELECT id, request_id, actual_cost, rate_multiplier, model, video_resolution, video_count, video_duration_seconds, billing_mode FROM usage_logs WHERE request_id = ? AND api_key_id = ?", log.RequestID, log.APIKeyID).Scan(&id, &requestID, &actual, &multiplier, &model, &resolution, &count, &duration, &mode))
			require.Equal(t, log.ID, id)
			require.Equal(t, log.RequestID, requestID)
			require.Equal(t, 0.6, actual)
			require.Equal(t, 3.0, multiplier)
			require.Equal(t, "veo-3.1", model)
			require.Equal(t, "2k", resolution)
			require.Equal(t, 1, count)
			require.Equal(t, 6, duration)
			require.Equal(t, "video", mode)
			// A stale retry cannot change an already settled positive-cost row.
			log.ActualCost, log.Model = 9, "wrong-retry-model"
			require.NoError(t, repo.UpsertSettledVideoUsage(context.Background(), log))
			require.NoError(t, db.QueryRow("SELECT actual_cost, model FROM usage_logs WHERE id = ?", id).Scan(&actual, &model))
			require.Equal(t, 0.6, actual)
			require.Equal(t, "veo-3.1", model)
		})
	}
}

func TestUpsertSettledVideoUsageConcurrentRetriesAndExplicitFree(t *testing.T) {
	for _, cost := range []float64{0, 0.6} {
		t.Run(fmt.Sprintf("cost=%v", cost), func(t *testing.T) {
			repo, db := newSettledVideoUsageTestRepo(t)
			errs := make([]error, 20)
			var wg sync.WaitGroup
			for i := range errs {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					log := settledVideoUsageTestLog()
					log.ActualCost = cost
					errs[i] = repo.UpsertSettledVideoUsage(context.Background(), log)
				}(i)
			}
			wg.Wait()
			for _, err := range errs {
				require.NoError(t, err)
			}
			var count int
			var actual float64
			require.NoError(t, db.QueryRow("SELECT COUNT(*), SUM(actual_cost) FROM usage_logs").Scan(&count, &actual))
			require.Equal(t, 1, count)
			require.Equal(t, cost, actual)
		})
	}
}

func TestUpsertSettledVideoUsageReturnsPersistenceFailure(t *testing.T) {
	repo, db := newSettledVideoUsageTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, repo.UpsertSettledVideoUsage(ctx, settledVideoUsageTestLog()), context.Canceled)
	_, err := db.Exec("DROP TABLE usage_logs")
	require.NoError(t, err)
	require.Error(t, repo.UpsertSettledVideoUsage(context.Background(), settledVideoUsageTestLog()))
}

func TestUpsertSettledVideoUsageKeepsAPIKeyIdentitySeparate(t *testing.T) {
	repo, db := newSettledVideoUsageTestRepo(t)
	first := settledVideoUsageTestLog()
	require.NoError(t, repo.UpsertSettledVideoUsage(context.Background(), first))
	other := settledVideoUsageTestLog()
	other.APIKeyID++
	other.ActualCost = 0.3
	require.NoError(t, repo.UpsertSettledVideoUsage(context.Background(), other))
	require.NotEqual(t, first.ID, other.ID)
	var count int
	var cost float64
	require.NoError(t, db.QueryRow("SELECT COUNT(*), SUM(actual_cost) FROM usage_logs").Scan(&count, &cost))
	require.Equal(t, 2, count)
	require.InDelta(t, 0.9, cost, 1e-10)
}
