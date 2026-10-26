package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestAntigravityModelRateLimitInCache_SharedSnapshotCopyOnWrite(t *testing.T) {
	const workers = 8
	oldLimit := map[string]any{"rate_limit_reset_at": "2026-10-26T10:00:00Z"}
	oldLimits := map[string]any{"existing-model": oldLimit}
	oldExtra := map[string]any{modelRateLimitsKey: oldLimits, "keep": true}
	snapshot := Account{ID: 1, Extra: oldExtra}
	svc := &AntigravityGatewayService{schedulerSnapshot: &SchedulerSnapshotService{}}
	resetAt := time.Now().Add(time.Minute)
	results := make(chan Account, workers)

	// Each request owns its account value but shares the maps from the snapshot.
	for i := range workers {
		go func() {
			account := snapshot
			svc.updateAccountModelRateLimitInCache(context.Background(), &account, fmt.Sprintf("model-%d", i), resetAt)
			results <- account
		}()
	}
	for range workers {
		account := <-results
		limits := account.Extra[modelRateLimitsKey].(map[string]any)
		if len(limits) != 2 || !reflect.DeepEqual(limits["existing-model"], oldLimit) {
			t.Fatalf("expected the existing limit and this request's limit, got %#v", limits)
		}
		if account.Extra["keep"] != true {
			t.Fatal("unrelated Extra value was lost")
		}
		for key, value := range limits {
			if key == "existing-model" {
				continue
			}
			limit := value.(map[string]any)
			if limit["rate_limit_reset_at"] != resetAt.UTC().Format(time.RFC3339) {
				t.Fatalf("unexpected reset time: %#v", limit)
			}
		}
		account.Extra["keep"] = false
	}
	if len(oldLimits) != 1 || !reflect.DeepEqual(oldLimits["existing-model"], oldLimit) {
		t.Fatalf("shared model limits were modified: %#v", oldLimits)
	}
	if oldExtra["keep"] != true || len(oldExtra) != 2 {
		t.Fatalf("shared Extra map was modified: %#v", oldExtra)
	}
}

func TestAntigravityModelRateLimitInCache_InitializesLimitsCopyOnWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "nil Extra"},
		{name: "missing limits", extra: map[string]any{"keep": true}},
		{name: "nil limits", extra: map[string]any{modelRateLimitsKey: map[string]any(nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 1, Extra: tc.extra}
			oldExtraLen := len(tc.extra)
			svc := &AntigravityGatewayService{schedulerSnapshot: &SchedulerSnapshotService{}}
			svc.updateAccountModelRateLimitInCache(context.Background(), account, "new-model", time.Now().Add(time.Minute))
			limits, ok := account.Extra[modelRateLimitsKey].(map[string]any)
			if !ok || limits["new-model"] == nil {
				t.Fatalf("expected the new model limit, got %#v", account.Extra)
			}
			if len(tc.extra) != oldExtraLen {
				t.Fatalf("original Extra was modified: %#v", tc.extra)
			}
			if oldLimits, _ := tc.extra[modelRateLimitsKey].(map[string]any); oldLimits != nil {
				t.Fatalf("original model limits were initialized in place: %#v", oldLimits)
			}
		})
	}
}

type antigravityCreditsCopyOnWriteRepo struct {
	AccountRepository
	accountID int64
	updates   map[string]any
}

func (r *antigravityCreditsCopyOnWriteRepo) UpdateExtra(_ context.Context, accountID int64, updates map[string]any) error {
	r.accountID = accountID
	r.updates = updates
	return nil
}

func TestAntigravityClearCreditsExhausted_SharedSnapshotCopyOnWrite(t *testing.T) {
	for _, keepModelLimit := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep model limit %t", keepModelLimit), func(t *testing.T) {
			creditLimit := map[string]any{"rate_limit_reset_at": "2026-10-26T10:00:00Z"}
			oldLimits := map[string]any{creditsExhaustedKey: creditLimit}
			if keepModelLimit {
				oldLimits["existing-model"] = "keep-model-limit"
			}
			oldExtra := map[string]any{modelRateLimitsKey: oldLimits, "allow_overages": true}
			account := &Account{ID: 123, Extra: oldExtra}
			repo := &antigravityCreditsCopyOnWriteRepo{}
			svc := &AntigravityGatewayService{accountRepo: repo}

			svc.clearCreditsExhausted(context.Background(), account)

			limits := account.Extra[modelRateLimitsKey].(map[string]any)
			if _, exists := limits[creditsExhaustedKey]; exists {
				t.Fatal("credits exhaustion was not cleared")
			}
			if keepModelLimit && limits["existing-model"] != "keep-model-limit" {
				t.Fatal("existing model limit was lost")
			}
			if !reflect.DeepEqual(oldLimits[creditsExhaustedKey], creditLimit) {
				t.Fatal("credits exhaustion was deleted from the shared snapshot")
			}
			if repo.accountID != account.ID || len(repo.updates) != 1 || !reflect.DeepEqual(repo.updates[modelRateLimitsKey], limits) {
				t.Fatalf("repository did not receive the new model limits: %#v", repo.updates)
			}

			// The account must own both modified map layers; the repository receives
			// its new limits map while the shared snapshot remains unchanged.
			account.Extra["allow_overages"] = false
			limits["new-only"] = true
			if oldExtra["allow_overages"] != true || oldLimits["new-only"] != nil {
				t.Fatal("updated account still aliases the shared map layers")
			}
			if repo.updates[modelRateLimitsKey].(map[string]any)["new-only"] != true {
				t.Fatal("repository received a different limits map")
			}
		})
	}
}
