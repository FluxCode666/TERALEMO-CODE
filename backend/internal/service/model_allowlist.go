package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const SettingKeyModelAllowlist = "model_request_allowlist"
const SettingKeyModelAllowlistEnabled = "model_request_allowlist_enabled"
const modelAllowlistInvalidationKey = "settings:model_request_allowlist"
const modelAllowlistCacheTTL = 5 * time.Second
const modelAllowlistDNSTTL = 5 * time.Minute

type ModelAllowlistSettings struct {
	Enabled    bool              `json:"enabled"`
	Entries    []string          `json:"entries"`
	Notes      map[string]string `json:"notes"`
	Cloudflare bool              `json:"cloudflare"`
	TrustedCDN []string          `json:"trusted_cdn_proxies"`
}

type ModelAllowlistPreview struct {
	Entry     string    `json:"entry"`
	Addresses []string  `json:"addresses"`
	Error     string    `json:"error,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type modelDNSResult struct {
	addresses []string
	err       string
	expires   time.Time
}

type modelAllowlistRuntime struct {
	mu       sync.Mutex
	dnsMu    sync.Mutex
	dnsGroup singleflight.Group
	settings ModelAllowlistSettings
	expires  time.Time
	dns      map[string]modelDNSResult
	lookup   func(context.Context, string) ([]net.IPAddr, error)
}

func defaultModelAllowlist() ModelAllowlistSettings {
	return ModelAllowlistSettings{Entries: []string{}, Notes: map[string]string{}, Cloudflare: true, TrustedCDN: []string{}}
}

func validAllowlistDomain(entry string) bool {
	if len(entry) > 253 || !strings.Contains(entry, ".") {
		return false
	}
	for _, label := range strings.Split(entry, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func ValidateModelAllowlist(settings *ModelAllowlistSettings) error {
	if len(settings.Entries) > 256 || len(settings.TrustedCDN) > 128 {
		return infraerrors.BadRequest("MODEL_ALLOWLIST_INVALID", "allowlist supports at most 256 entries and 128 CDN ranges")
	}
	seen := map[string]bool{}
	entries := make([]string, 0, len(settings.Entries))
	notes := make(map[string]string, len(settings.Entries))
	for _, raw := range settings.Entries {
		entry := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if address, err := netip.ParseAddr(entry); err == nil && address.Zone() == "" {
			entry = address.Unmap().String()
		} else if prefix, err := netip.ParsePrefix(entry); err == nil {
			entry = prefix.Masked().String()
		} else if !validAllowlistDomain(entry) || net.ParseIP(entry) != nil {
			return infraerrors.BadRequest("MODEL_ALLOWLIST_INVALID", "invalid IP, CIDR or domain: "+raw)
		}
		if !seen[entry] {
			entries = append(entries, entry)
			seen[entry] = true
		}
		note, exists := settings.Notes[raw]
		if !exists {
			note = settings.Notes[entry]
		}
		if len(note) > 256 {
			return infraerrors.BadRequest("MODEL_ALLOWLIST_INVALID", "entry note exceeds 256 bytes: "+raw)
		}
		if note != "" {
			notes[entry] = strings.TrimSpace(note)
		}
	}
	if _, err := ip.ParseProxyRanges(settings.TrustedCDN); err != nil {
		return infraerrors.BadRequest("MODEL_ALLOWLIST_INVALID", err.Error())
	}
	settings.Entries = entries
	settings.Notes = notes
	if settings.TrustedCDN == nil {
		settings.TrustedCDN = []string{}
	}
	return nil
}

func (s *SettingService) GetModelAllowlist(ctx context.Context) (ModelAllowlistSettings, error) {
	runtime := &s.modelAllowlist
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if time.Now().Before(runtime.expires) {
		return cloneModelAllowlist(runtime.settings), nil
	}
	settings := defaultModelAllowlist()
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(readCtx, []string{SettingKeyModelAllowlist, SettingKeyModelAllowlistEnabled})
	if err != nil {
		return settings, fmt.Errorf("read model allowlist: %w", err)
	}
	if raw := values[SettingKeyModelAllowlist]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return settings, fmt.Errorf("decode model allowlist: %w", err)
		}
	}
	settings.Enabled = false
	if enabled, exists := values[SettingKeyModelAllowlistEnabled]; exists {
		if enabled != "true" && enabled != "false" {
			return settings, fmt.Errorf("invalid model allowlist enforcement switch")
		}
		settings.Enabled = enabled == "true"
	}
	if err := ValidateModelAllowlist(&settings); err != nil {
		return settings, err
	}
	runtime.settings = cloneModelAllowlist(settings)
	runtime.expires = time.Now().Add(modelAllowlistCacheTTL)
	return settings, nil
}

func cloneModelAllowlist(settings ModelAllowlistSettings) ModelAllowlistSettings {
	settings.Entries = append([]string{}, settings.Entries...)
	settings.TrustedCDN = append([]string{}, settings.TrustedCDN...)
	notes := make(map[string]string, len(settings.Notes))
	for entry, note := range settings.Notes {
		notes[entry] = note
	}
	settings.Notes = notes
	return settings
}

func (s *SettingService) InvalidateModelAllowlist() {
	s.modelAllowlist.mu.Lock()
	s.modelAllowlist.expires = time.Time{}
	s.modelAllowlist.mu.Unlock()
}

func (s *SettingService) SaveModelAllowlistEntries(ctx context.Context, settings ModelAllowlistSettings) error {
	if err := ValidateModelAllowlist(&settings); err != nil {
		return err
	}
	settings.Enabled = false
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyModelAllowlist, string(encoded)); err != nil {
		return err
	}
	return s.notifyModelAllowlistUpdate(ctx)
}

func (s *SettingService) SetModelAllowlistEnabled(ctx context.Context, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	if err := s.settingRepo.Set(ctx, SettingKeyModelAllowlistEnabled, value); err != nil {
		return err
	}
	return s.notifyModelAllowlistUpdate(ctx)
}

func (s *SettingService) notifyModelAllowlistUpdate(ctx context.Context) error {
	s.InvalidateModelAllowlist()
	if s.modelAllowlistPublisher != nil {
		return s.modelAllowlistPublisher(ctx, modelAllowlistInvalidationKey)
	}
	return nil
}

func (s *SettingService) ModelProxyPolicy(ctx context.Context) (ip.ProxyPolicy, error) {
	policy := ip.ProxyPolicy{}
	if s.cfg != nil {
		var err error
		policy.Local, err = ip.ParseProxyRanges(s.cfg.Server.TrustedProxies)
		if err != nil {
			return policy, err
		}
	}
	settings, err := s.GetModelAllowlist(ctx)
	if err != nil {
		return policy, err
	}
	ranges := append([]string{}, settings.TrustedCDN...)
	if settings.Cloudflare {
		ranges = append(ranges, ip.CloudflareProxyRanges...)
	}
	policy.CDN, err = ip.ParseProxyRanges(ranges)
	return policy, err
}

func (s *SettingService) resolveModelDomain(ctx context.Context, domain string) modelDNSResult {
	runtime := &s.modelAllowlist
	runtime.dnsMu.Lock()
	if cached, ok := runtime.dns[domain]; ok && time.Now().Before(cached.expires) {
		runtime.dnsMu.Unlock()
		return cached
	}
	runtime.dnsMu.Unlock()
	if ctx.Err() != nil {
		return modelDNSResult{addresses: []string{}, err: "DNS resolution timed out"}
	}
	pending := runtime.dnsGroup.DoChan(domain, func() (any, error) {
		runtime.dnsMu.Lock()
		if cached, ok := runtime.dns[domain]; ok && time.Now().Before(cached.expires) {
			runtime.dnsMu.Unlock()
			return cached, nil
		}
		lookup := runtime.lookup
		runtime.dnsMu.Unlock()
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIPAddr
		}
		dnsCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		addresses, err := lookup(dnsCtx, domain)
		result := modelDNSResult{addresses: []string{}, expires: time.Now().Add(modelAllowlistDNSTTL)}
		if err != nil {
			result.err = "DNS resolution failed"
		}
		for _, address := range addresses {
			result.addresses = append(result.addresses, address.IP.String())
		}
		if err == nil && len(result.addresses) == 0 {
			result.err = "no A or AAAA records"
		}
		runtime.dnsMu.Lock()
		defer runtime.dnsMu.Unlock()
		if runtime.dns == nil {
			runtime.dns = map[string]modelDNSResult{}
		}
		if len(runtime.dns) >= 256 {
			for cachedDomain, cached := range runtime.dns {
				if time.Now().After(cached.expires) {
					delete(runtime.dns, cachedDomain)
				}
			}
		}
		if len(runtime.dns) < 256 {
			runtime.dns[domain] = result
		}
		return result, nil
	})
	select {
	case <-ctx.Done():
		return modelDNSResult{addresses: []string{}, err: "DNS resolution timed out"}
	case resolved := <-pending:
		result, ok := resolved.Val.(modelDNSResult)
		if !ok {
			return modelDNSResult{addresses: []string{}, err: "DNS resolution failed"}
		}
		return result
	}
}

func (s *SettingService) PreviewModelAllowlist(ctx context.Context, settings ModelAllowlistSettings) []ModelAllowlistPreview {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	results := make([]ModelAllowlistPreview, 0, len(settings.Entries))
	for _, entry := range settings.Entries {
		if validAllowlistDomain(entry) && net.ParseIP(entry) == nil {
			results = append(results, ModelAllowlistPreview{Entry: entry})
		}
	}
	var group errgroup.Group
	for index := range results {
		s.modelAllowlist.dnsMu.Lock()
		cached, exists := s.modelAllowlist.dns[results[index].Entry]
		s.modelAllowlist.dnsMu.Unlock()
		if exists && time.Now().Before(cached.expires) {
			results[index].Addresses, results[index].Error, results[index].ExpiresAt = cached.addresses, cached.err, cached.expires
			continue
		}
		group.Go(func() error {
			result := s.resolveModelDomain(ctx, results[index].Entry)
			results[index].Addresses, results[index].Error, results[index].ExpiresAt = result.addresses, result.err, result.expires
			return nil
		})
	}
	_ = group.Wait()
	return results
}

func (s *SettingService) CheckModelAllowlist(ctx context.Context, clientIP string) (bool, error) {
	settings, err := s.GetModelAllowlist(ctx)
	if err != nil {
		return false, err
	}
	return s.TestModelAllowlist(ctx, settings, clientIP), nil
}

func (s *SettingService) TestModelAllowlist(ctx context.Context, settings ModelAllowlistSettings, clientIP string) bool {
	if !settings.Enabled {
		return true
	}
	return s.ModelAllowlistMatches(ctx, settings, clientIP)
}

func (s *SettingService) ModelAllowlistMatches(ctx context.Context, settings ModelAllowlistSettings, clientIP string) bool {
	if net.ParseIP(clientIP) == nil {
		return false
	}
	for _, entry := range settings.Entries {
		if ip.MatchesPattern(clientIP, entry) {
			return true
		}
	}
	for _, result := range s.PreviewModelAllowlist(ctx, settings) {
		if result.Error != "" {
			continue
		}
		for _, address := range result.Addresses {
			if ip.MatchesPattern(clientIP, address) {
				return true
			}
		}
	}
	return false
}
