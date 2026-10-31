//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type modelCatalogAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
	err      error
}

func (r *modelCatalogAccountRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	accounts := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account := r.accounts[id]; account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func newModelCatalogTestService(count int) (*AccountModelCatalogService, *modelCatalogAccountRepo) {
	repo := &modelCatalogAccountRepo{accounts: make(map[int64]*Account)}
	for id := 1; id <= count; id++ {
		repo.accounts[int64(id)] = &Account{ID: int64(id), Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	}
	return NewAccountModelCatalogService(repo, nil), repo
}

func TestAccountModelCatalogIntersectionValidation(t *testing.T) {
	tooMany := make([]int64, 101)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	for _, tc := range []struct {
		name    string
		ids     []int64
		mixed   bool
		message string
	}{
		{name: "empty", message: "between 1 and 100"},
		{name: "nonpositive", ids: []int64{0}, message: "positive integers"},
		{name: "negative", ids: []int64{-1}, message: "positive integers"},
		{name: "too many", ids: tooMany, message: "between 1 and 100"},
		{name: "missing", ids: []int64{1, 3}, message: "Account 3 not found"},
		{name: "mixed", ids: []int64{1, 2}, mixed: true, message: "Batch test requires accounts from the same platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModelCatalogTestService(2)
			if tc.mixed {
				repo.accounts[2].Platform = PlatformOpenAI
			}
			result, err := svc.ModelIntersection(context.Background(), tc.ids)
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Contains(t, infraerrors.Message(err), tc.message)
		})
	}
	svc, repo := newModelCatalogTestService(1)
	repo.err = errors.New("database unavailable")
	_, err := svc.ModelIntersection(context.Background(), []int64{1})
	require.ErrorContains(t, err, "load model intersection accounts")
}

func TestAccountModelCatalogIntersectionUsesSharedPlatformCatalog(t *testing.T) {
	svc, repo := newModelCatalogTestService(2)
	for _, account := range repo.accounts {
		account.Credentials = map[string]any{"model_mapping": map[string]any{
			"shared-model": "upstream-model", fmt.Sprintf("private-%d", account.ID): "another-model",
		}}
	}
	result, err := svc.ModelIntersection(context.Background(), []int64{2, 1, 2})
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, result.Platform)
	require.Equal(t, 2, result.AccountCount)
	require.Equal(t, []AccountCatalogModel{{ID: "shared-model", DisplayName: "shared-model"}}, result.Models)
	require.Empty(t, result.FailedAccountIDs)

	for _, account := range repo.accounts {
		account.Platform = PlatformGemini
		account.Type = AccountTypeOAuth
	}
	result, err = svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, result.Models, len(geminicli.DefaultModels))
	for i := 1; i < len(result.Models); i++ {
		require.Less(t, result.Models[i-1].ID, result.Models[i].ID)
	}
}

func TestAccountModelCatalogIntersectionPartialFailuresAndNames(t *testing.T) {
	svc, _ := newModelCatalogTestService(3)
	svc.fetchModels = func(_ context.Context, account *Account) (any, error) {
		if account.ID == 2 {
			return nil, errors.New("upstream unavailable")
		}
		return []AccountCatalogModel{
			{ID: "z-model", DisplayName: fmt.Sprintf("Account %d", account.ID)},
			{ID: "a-model"}, {ID: "z-model", DisplayName: "duplicate ignored"},
			{ID: fmt.Sprintf("private-%d", account.ID)},
		}, nil
	}
	result, err := svc.ModelIntersection(context.Background(), []int64{3, 2, 1, 3})
	require.NoError(t, err)
	require.Equal(t, 3, result.AccountCount)
	require.Equal(t, []int64{2}, result.FailedAccountIDs)
	require.Equal(t, []AccountCatalogModel{{ID: "a-model", DisplayName: "a-model"}, {ID: "z-model", DisplayName: "Account 3"}}, result.Models)
	// The cache key is a set, but display-name precedence follows each request.
	result.Models[1].DisplayName = "caller mutation"
	result, err = svc.ModelIntersection(context.Background(), []int64{1, 2, 3})
	require.NoError(t, err)
	require.Equal(t, "Account 1", result.Models[1].DisplayName)
	require.Equal(t, []int64{2}, result.FailedAccountIDs)
}

func TestAccountModelCatalogIntersectionEmptyAndAllFailed(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{
		{name: "empty intersection"}, {name: "all failed", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newModelCatalogTestService(2)
			svc.fetchModels = func(_ context.Context, account *Account) (any, error) {
				if tc.fail {
					return nil, errors.New("model fetch failed")
				}
				return []AccountCatalogModel{{ID: fmt.Sprintf("private-%d", account.ID)}}, nil
			}
			result, err := svc.ModelIntersection(context.Background(), []int64{1, 2})
			if tc.fail {
				require.Nil(t, result)
				require.Equal(t, 500, infraerrors.Code(err))
				require.Contains(t, infraerrors.Message(err), "all selected accounts")
				require.Empty(t, svc.cache, "failed fetches must not poison the group cache")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result.Models)
			require.Empty(t, result.Models)
			require.NotNil(t, result.FailedAccountIDs)
		})
	}
	// An authoritative empty catalog is successful and empties the intersection.
	svc, _ := newModelCatalogTestService(2)
	svc.fetchModels = func(_ context.Context, account *Account) (any, error) {
		if account.ID == 1 {
			return []AccountCatalogModel{}, nil
		}
		return []AccountCatalogModel{{ID: "model"}}, nil
	}
	result, err := svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Empty(t, result.Models)
	require.Empty(t, result.FailedAccountIDs)
}

func TestAccountModelCatalogIntersectionCacheTTLAndCurrentAccountValidation(t *testing.T) {
	svc, repo := newModelCatalogTestService(2)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	var calls atomic.Int32
	svc.fetchModels = func(_ context.Context, account *Account) (any, error) {
		calls.Add(1)
		return []AccountCatalogModel{{ID: "model"}}, nil
	}
	for _, ids := range [][]int64{{2, 1}, {1, 2, 2}} {
		_, err := svc.ModelIntersection(context.Background(), ids)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, calls.Load())
	now = now.Add(59 * time.Second)
	_, err := svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	now = now.Add(time.Second)
	_, err = svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.EqualValues(t, 4, calls.Load(), "the entry expires at exactly 60 seconds")
	repo.accounts[2].Platform = PlatformGemini
	_, err = svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.ErrorIs(t, err, ErrAccountBatchTestMixedPlatforms)
	delete(repo.accounts, 2)
	_, err = svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.Equal(t, 400, infraerrors.Code(err))
	require.Contains(t, infraerrors.Message(err), "not found")
	require.EqualValues(t, 4, calls.Load(), "cached groups still validate current accounts")
}

func TestAccountModelCatalogIntersectionCacheBounded(t *testing.T) {
	svc, _ := newModelCatalogTestService(1)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	for i := range accountModelIntersectionCacheLimit + 20 {
		svc.storeIntersection(fmt.Sprintf("group-%03d", i), accountModelIntersectionCacheEntry{})
		now = now.Add(time.Millisecond)
	}
	require.Len(t, svc.cache, accountModelIntersectionCacheLimit)
	_, hit := svc.cachedIntersection("group-000")
	require.False(t, hit)
	now = now.Add(accountModelIntersectionTTL)
	svc.storeIntersection("fresh", accountModelIntersectionCacheEntry{})
	require.Len(t, svc.cache, 1)
}

func TestAccountModelCatalogIntersectionConcurrencyAndCancellation(t *testing.T) {
	svc, _ := newModelCatalogTestService(12)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 12)
	var active, peak atomic.Int32
	svc.fetchModels = func(ctx context.Context, account *Account) (any, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.ModelIntersection(ctx, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
		done <- err
	}()
	for range 5 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("model fetch did not start")
		}
	}
	require.EqualValues(t, 5, peak.Load())
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("model workers did not stop on cancellation")
	}
	require.Zero(t, active.Load())
	require.Empty(t, svc.fetchSlots)
	require.Empty(t, svc.cache)
}

func TestAccountModelCatalogIntersectionRecoversOneFailedWorker(t *testing.T) {
	svc, _ := newModelCatalogTestService(2)
	svc.fetchModels = func(_ context.Context, account *Account) (any, error) {
		if account.ID == 1 {
			panic("unexpected catalog failure")
		}
		return []AccountCatalogModel{{ID: "model"}}, nil
	}
	result, err := svc.ModelIntersection(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.FailedAccountIDs)
	require.Equal(t, []AccountCatalogModel{{ID: "model", DisplayName: "model"}}, result.Models)
	require.Empty(t, svc.fetchSlots)
}

func TestAccountModelCatalogOpenAIDiscoveryFallback(t *testing.T) {
	svc := NewAccountModelCatalogService(nil, &AccountTestService{})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	models, err := svc.GetAvailableModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, openai.DefaultModels, models)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.GetAvailableModels(ctx, account)
	require.ErrorIs(t, err, context.Canceled)
}
