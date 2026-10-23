//go:build unit

package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const metasoH3InsufficientBalanceBody = `{"error":{"http_code":"402","message":"H3 积分余额不足 (1008)","type":"insufficient_balance_error"},"request_id":"3bae0f38-8f1d-4bf9-9482-174b366ddf65","type":"error"}`

func TestVideoExplicitRejectionProtocolIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		status               int
		retain, partial      bool
	}{
		{"seedance-trace-id", service.VideoProtocolSeedance, metasoH3InsufficientBalanceBody, 402, false, false},
		{"gemini-trace-id", service.VideoProtocolGemini, `{"error":{"code":503,"message":"unavailable","request_id":"nested-trace"},"request_id":"trace","trace_id":"other-trace"}`, 503, false, false},
		{"minimax-v1-trace-id", "minimax-v1", metasoH3InsufficientBalanceBody, 402, false, false},
		{"minimax-v2-metaso-402", "minimax-v2", metasoH3InsufficientBalanceBody, 402, false, false},
		{"minimax-business-error", "minimax-v1", `{"base_resp":{"status_code":1008,"status_msg":"insufficient credits"},"request_id":"trace"}`, 503, false, false},
		{"grok-nested-trace", "grok", `{"error":{"message":"unavailable","request_id":"nested-trace"},"trace_id":"trace"}`, 503, false, false},
		{"grok-request-id-is-task", "grok", metasoH3InsufficientBalanceBody, 402, true, false},
		{"grok-id-is-task", "grok", `{"error":{"message":"unavailable"},"id":"task-1"}`, 503, true, false},
		{"seedance-id-is-task", service.VideoProtocolSeedance, `{"error":{"message":"unavailable"},"id":"task-1","request_id":"trace"}`, 503, true, false},
		{"gemini-name-is-task", service.VideoProtocolGemini, `{"error":{"code":503},"name":"models/veo/operations/task-1","request_id":"trace"}`, 503, true, false},
		{"minimax-task-id", "minimax-v1", `{"error":{"message":"unavailable"},"task_id":"task-1","request_id":"trace"}`, 503, true, false},
		{"minimax-nested-task-id", "minimax-v2", `{"error":{"message":"unavailable"},"task":{"id":"task-1"},"request_id":"trace"}`, 503, true, false},
		{"no-explicit-error", "minimax-v2", `{"message":"unavailable","request_id":"trace"}`, 503, true, false},
		{"malformed-error-rewritten-by-adapter", "minimax-v2", `<html>error</html>`, 503, true, false},
		{"partial-error-body", "minimax-v2", metasoH3InsufficientBalanceBody, 402, true, true},
		{"large-complete-error-body", "minimax-v2", `{"error":{"message":"` + strings.Repeat("x", 70<<10) + `"}}`, 503, false, false},
		{"2xx-error-is-uncertain", "minimax-v2", metasoH3InsufficientBalanceBody, 200, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "17")
				if tc.partial {
					w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)+100))
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			h, slots, repo := transportVideoHandler(t, tc.protocol, transportVideoBase(tc.protocol, srv.URL), nil)
			w := submitTransportVideo(t, h, tc.protocol)
			if tc.protocol == "grok" {
				// Grok retains its existing gateway error mapping policy.
				require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
			} else if tc.status >= 400 {
				require.Equal(t, tc.status, w.Code, w.Body.String())
			} else {
				require.Equal(t, 502, w.Code, w.Body.String())
			}
			if tc.retain {
				requireTransportHold(t, repo, service.VideoReservationManual, .2)
			} else {
				v := requireTransportHold(t, repo, service.VideoReservationReleased, 0)
				require.Equal(t, "upstream explicitly rejected creation", v.LastError)
			}
			slots.assertReleased(t)
		})
	}
}

func TestVideoExplicitRejectionRequiresErrorObject(t *testing.T) {
	for _, body := range []string{`{}`, `{"error":null}`, `{"error":"message"}`, `[]`, `{`, `{"base_resp":{"status_code":0}}`} {
		require.False(t, videoExplicitUpstreamRejection(service.MiniMaxVideoEndpointCreate, []byte(body)), body)
	}
	require.True(t, videoExplicitUpstreamRejection(service.MiniMaxVideoEndpointCreate, []byte(`{"error":{},"id":"tracking","name":"tracking","request_id":"tracking"}`)))
}
