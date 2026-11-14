//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsModelAllowlistRepo struct {
	service.SettingRepository
	values map[string]string
}

func (repo *wsModelAllowlistRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		if value, exists := repo.values[key]; exists {
			values[key] = value
		}
	}
	return values, nil
}

func (repo *wsModelAllowlistRepo) Set(_ context.Context, key, value string) error {
	repo.values[key] = value
	return nil
}

func TestOpenAIWSModelAllowlistRechecksPolicyBetweenTurns(t *testing.T) {
	ctx := context.Background()
	settings := service.NewSettingService(&wsModelAllowlistRepo{values: map[string]string{}}, &config.Config{})
	require.NoError(t, settings.SaveModelAllowlistEntries(ctx, service.ModelAllowlistSettings{Entries: []string{"192.0.2.1"}}))
	keys := &service.APIKeyService{}
	keys.SetModelAllowlistSettings(settings)
	handler := &OpenAIGatewayHandler{apiKeyService: keys}
	key := &service.APIKey{ID: 3, User: &service.User{ID: 4}}
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key))
	require.NoError(t, settings.SetModelAllowlistEnabled(ctx, true))
	require.ErrorContains(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key), "203.0.113.9")
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "192.0.2.1", key))
	require.NoError(t, settings.SetModelAllowlistEnabled(ctx, false))
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key))
}

func TestOpenAIWSTurnChecksKeyIPRules(t *testing.T) {
	handler := &OpenAIGatewayHandler{}
	for _, test := range []struct {
		name                 string
		whitelist, blacklist []string
		allowed              bool
	}{
		{"matching whitelist", []string{"192.0.2.10"}, nil, true},
		{"different whitelist", []string{"192.0.2.20"}, nil, false},
		{"matching blacklist", nil, []string{"192.0.2.10"}, false},
		{"different blacklist", nil, []string{"192.0.2.20"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := &service.APIKey{IPWhitelist: test.whitelist, IPBlacklist: test.blacklist}
			err := handler.checkOpenAIWSModelRequestIP(t.Context(), "192.0.2.10", key)
			if test.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "192.0.2.10")
			}
		})
	}
}

func TestOpenAIWSFollowupPolicyUsesConnectionSecurityIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		client  openAIWSClientIPFixture
		allowed bool
	}{
		{"untrusted forwarding metadata", openAIWSClientIPFixture{forwardedIP: "192.0.2.10", allowlist: []string{"192.0.2.10"}, metadataIP: "192.0.2.10", securityIP: "127.0.0.1"}, false},
		{"connection peer", openAIWSClientIPFixture{allowlist: []string{"127.0.0.1"}, metadataIP: "127.0.0.1", securityIP: "127.0.0.1"}, true},
		{"trusted forwarding chain", openAIWSClientIPFixture{trustedProxies: []string{"127.0.0.1"}, forwardedIP: "192.0.2.10", allowlist: []string{"192.0.2.10"}, metadataIP: "192.0.2.10", securityIP: "192.0.2.10"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var received atomic.Int64
			upstreamDone := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(upstreamDone)
				conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
				if err != nil {
					t.Errorf("accept upstream websocket: %v", err)
					return
				}
				defer func() { _ = conn.CloseNow() }()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				for {
					if _, _, err := conn.Read(ctx); err != nil {
						return
					}
					received.Add(1)
					if err := conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_policy_test","model":"gpt-5.1","usage":{"input_tokens":2,"output_tokens":1}}}`)); err != nil {
						return
					}
				}
			}))
			defer upstream.Close()
			harness := newOpenAIWSPassthroughHandlerHarnessWithClientIP(t, upstream.URL, &test.client)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"test"}`)
			require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
			_, first, err := harness.clientConn.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "response.completed", gjson.GetBytes(first, "type").String())
			select {
			case usage := <-harness.usageRepo.created:
				require.NotNil(t, usage.IPAddress)
				require.Equal(t, test.client.metadataIP, *usage.IPAddress)
			case <-ctx.Done():
				t.Fatal("first turn usage was not recorded")
			}
			require.NoError(t, harness.modelSettings.SetModelAllowlistEnabled(t.Context(), true))
			require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
			_, next, err := harness.clientConn.Read(ctx)
			require.NoError(t, err)
			if test.allowed {
				require.Equal(t, "response.completed", gjson.GetBytes(next, "type").String())
			} else {
				require.Equal(t, "error", gjson.GetBytes(next, "type").String())
				require.Equal(t, int64(http.StatusForbidden), gjson.GetBytes(next, "error.status").Int())
				require.Contains(t, gjson.GetBytes(next, "error.message").String(), test.client.securityIP)
				_, _, err = harness.clientConn.Read(ctx)
				require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
			}
			_ = harness.clientConn.CloseNow()
			select {
			case <-harness.handlerDone:
			case <-ctx.Done():
				t.Fatal("websocket handler did not exit")
			}
			select {
			case <-upstreamDone:
			case <-ctx.Done():
				t.Fatal("upstream websocket did not exit")
			}
			want := int64(1)
			if test.allowed {
				want = 2
			}
			require.Equal(t, want, received.Load())
		})
	}
}
