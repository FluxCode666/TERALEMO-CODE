//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type securitySettingsRepo struct {
	SettingRepository
	values map[string]string
}

func (repo *securitySettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		if value, exists := repo.values[key]; exists {
			values[key] = value
		}
	}
	return values, nil
}

func (repo *securitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := repo.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (repo *securitySettingsRepo) Set(_ context.Context, key, value string) error {
	repo.values[key] = value
	return nil
}

func (repo *securitySettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		repo.values[key] = value
	}
	return nil
}

func newSecurityTestSettings() *SettingService {
	return NewSettingService(&securitySettingsRepo{values: map[string]string{}}, &config.Config{})
}

func TestModelAllowlistRulesAndDNSCache(t *testing.T) {
	settings := newSecurityTestSettings()
	var calls atomic.Int32
	settings.modelAllowlist.lookup = func(_ context.Context, domain string) ([]net.IPAddr, error) {
		calls.Add(1)
		if domain == "broken.example" {
			return nil, errors.New("dns failed")
		}
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.9")}, {IP: net.ParseIP("2001:db8::9")}}, nil
	}
	rules := ModelAllowlistSettings{Enabled: true, Entries: []string{"192.0.2.1", "198.51.100.0/24", "2001:db8:1::/48", "customer.example", "broken.example"}}
	require.NoError(t, ValidateModelAllowlist(&rules))
	for _, address := range []string{"192.0.2.1", "198.51.100.9", "2001:db8:1::1", "203.0.113.9", "2001:db8::9"} {
		require.True(t, settings.TestModelAllowlist(context.Background(), rules, address), address)
	}
	require.False(t, settings.TestModelAllowlist(context.Background(), rules, "203.0.113.10"))
	require.Equal(t, int32(2), calls.Load())
	settings.modelAllowlist.dns["customer.example"] = modelDNSResult{expires: time.Now().Add(-time.Second)}
	require.True(t, settings.TestModelAllowlist(context.Background(), rules, "203.0.113.9"))
	require.Equal(t, int32(3), calls.Load())
	rules.Enabled = false
	require.True(t, settings.TestModelAllowlist(context.Background(), rules, "8.8.8.8"))
	require.True(t, settings.TestModelAllowlist(context.Background(), rules, ""))
}

func TestModelAllowlistSlowDomainDoesNotBlockOtherDomains(t *testing.T) {
	settings := newSecurityTestSettings()
	started, release := make(chan struct{}), make(chan struct{})
	settings.modelAllowlist.lookup = func(ctx context.Context, domain string) ([]net.IPAddr, error) {
		if domain == "slow.example" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.9")}}, nil
	}
	done := make(chan modelDNSResult, 1)
	go func() { done <- settings.resolveModelDomain(context.Background(), "slow.example") }()
	<-started
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fast := settings.resolveModelDomain(ctx, "fast.example")
	require.Empty(t, fast.err)
	require.Equal(t, []string{"203.0.113.9"}, fast.addresses)
}

func TestModelAllowlistValidationAndInvalidation(t *testing.T) {
	for _, entry := range []string{"https://customer.example", "*.example.com", "1.2.3.4:443", "bad_domain.example", "2001:db8::1%eth0", "1.2.3.0/99"} {
		require.Error(t, ValidateModelAllowlist(&ModelAllowlistSettings{Entries: []string{entry}}), entry)
	}
	normalized := ModelAllowlistSettings{Entries: []string{"192.0.2.9/24", "2001:DB8::0001", "Customer.Example."}, Notes: map[string]string{
		"192.0.2.9/24": "CIDR customer", "2001:DB8::0001": "IPv6 customer", "Customer.Example.": "Domain customer",
	}}
	require.NoError(t, ValidateModelAllowlist(&normalized))
	require.Equal(t, map[string]string{"192.0.2.0/24": "CIDR customer", "2001:db8::1": "IPv6 customer", "customer.example": "Domain customer"}, normalized.Notes)
	settings := newSecurityTestSettings()
	published := ""
	settings.modelAllowlistPublisher = func(_ context.Context, key string) error { published = key; return nil }
	require.NoError(t, settings.SaveModelAllowlistEntries(context.Background(), ModelAllowlistSettings{Enabled: true, Entries: []string{"192.0.2.1"}, Notes: map[string]string{"192.0.2.1": "Customer A"}}))
	loaded, err := settings.GetModelAllowlist(context.Background())
	require.NoError(t, err)
	require.False(t, loaded.Enabled)
	require.Equal(t, "Customer A", loaded.Notes["192.0.2.1"])
	require.Equal(t, modelAllowlistInvalidationKey, published)
	keyService := &APIKeyService{modelAllowlistSettings: settings}
	keyService.invalidateLocalAuthCache(modelAllowlistInvalidationKey)
	require.True(t, settings.modelAllowlist.expires.IsZero())
	require.NoError(t, settings.SetModelAllowlistEnabled(context.Background(), true))
	require.NoError(t, settings.SaveModelAllowlistEntries(context.Background(), ModelAllowlistSettings{Enabled: false, Entries: []string{"198.51.100.1"}}))
	loaded, err = settings.GetModelAllowlist(context.Background())
	require.NoError(t, err)
	require.True(t, loaded.Enabled)
	require.Equal(t, []string{"198.51.100.1"}, loaded.Entries)
	require.NoError(t, settings.SetModelAllowlistEnabled(context.Background(), false))
	loaded, err = settings.GetModelAllowlist(context.Background())
	require.NoError(t, err)
	require.False(t, loaded.Enabled)
	require.Equal(t, []string{"198.51.100.1"}, loaded.Entries)
}

type modelObservationStub struct{ observations []ModelAllowlistObservation }

func (store *modelObservationStub) RecordModelAllowlistObservation(_ context.Context, value ModelAllowlistObservation) error {
	store.observations = append(store.observations, value)
	return nil
}
func (store *modelObservationStub) ListModelAllowlistObservations(context.Context) ([]ModelAllowlistObservation, error) {
	return store.observations, nil
}

func TestModelAllowlistDisabledObservesWithoutBlocking(t *testing.T) {
	settings := newSecurityTestSettings()
	store := &modelObservationStub{}
	settings.modelAllowlistObservations = store
	require.NoError(t, settings.SaveModelAllowlistEntries(context.Background(), ModelAllowlistSettings{Entries: []string{"192.0.2.1"}}))
	keyService := &APIKeyService{modelAllowlistSettings: settings}
	key := &APIKey{ID: 3, Name: "customer", User: &User{ID: 4, Email: "customer@example.com"}}
	allowed, err := keyService.CheckModelRequestIP(context.Background(), "203.0.113.1", key)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Len(t, store.observations, 1)
	require.Equal(t, "203.0.113.1", store.observations[0].IP)
	require.Equal(t, int64(4), store.observations[0].UserID)
}
