//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoSubmissionWritesRequirePositiveProofOfNonDelivery(t *testing.T) {
	for _, scenario := range []string{"local-validation", "unknown-transport", "known-unsent", "got-connection", "headers", "partial-write", "complete-write", "response", "earlier-attempt-written"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, writes := WithVideoSubmissionWrites(context.Background())
			require.True(t, writes.DefinitelyNotSent())
			if scenario == "local-validation" {
				return
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://video.example/create", nil)
			require.NoError(t, err)
			req, attempt := trackVideoSubmissionWrite(req)
			if scenario == "unknown-transport" {
				require.False(t, writes.DefinitelyNotSent(), "no callbacks from an unknown transport cannot establish non-delivery")
				return
			}
			ConfirmHTTPUpstreamWriteTrace(req)
			trace := httptrace.ContextClientTrace(req.Context())
			switch scenario {
			case "got-connection":
				trace.GotConn(httptrace.GotConnInfo{})
			case "headers":
				trace.WroteHeaders()
			case "partial-write":
				trace.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("partial write")})
			case "complete-write":
				trace.WroteRequest(httptrace.WroteRequestInfo{})
			case "response":
				attempt.mayHaveWritten.Store(true)
			case "earlier-attempt-written":
				trace.WroteRequest(httptrace.WroteRequestInfo{})
				next, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL.String(), nil)
				require.NoError(t, err)
				next, _ = trackVideoSubmissionWrite(next)
				ConfirmHTTPUpstreamWriteTrace(next)
			}
			require.Equal(t, scenario == "known-unsent", writes.DefinitelyNotSent())
		})
	}
}

func TestVideoSubmissionWritesRequiresCompleteBoundedRejectionBody(t *testing.T) {
	const captureLimit = 64 << 10
	for _, scenario := range []string{"complete", "partial", "invalid-json", "oversized", "2xx"} {
		t.Run(scenario, func(t *testing.T) {
			writes := &VideoSubmissionWrites{}
			writes.responseStatus.Store(503)
			body := `{"error":{"message":"unavailable"},"request_id":"trace"}`
			if scenario == "oversized" {
				body = strings.Repeat("x", captureLimit+1)
			}
			if scenario == "invalid-json" {
				body = "{"
			}
			if scenario == "2xx" {
				writes.responseStatus.Store(200)
			}
			reader := &videoRejectionBody{ReadCloser: io.NopCloser(strings.NewReader(body)), writes: writes, limit: captureLimit}
			if scenario == "partial" {
				_, err := reader.Read(make([]byte, 5))
				require.NoError(t, err)
			} else {
				got, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.Equal(t, body, string(got), "capture must not alter the body consumed by the adapter")
			}
			require.NoError(t, reader.Close())
			got, complete := writes.RejectionBody()
			wantComplete := scenario == "complete" || scenario == "invalid-json"
			require.Equal(t, wantComplete, complete)
			if complete {
				require.Equal(t, body, string(got))
				got[0] = '!'
				next, _ := writes.RejectionBody()
				require.Equal(t, body, string(next), "callers cannot alter captured evidence")
			}
		})
	}
}

func TestVideoSubmissionWritesPreserveExistingTraceAndIgnoreDraftReads(t *testing.T) {
	var callbacks int
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { callbacks++ }})
	ctx, writes := WithVideoSubmissionWrites(ctx)
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://video.example/draft", nil)
	require.NoError(t, err)
	_, attempt := trackVideoSubmissionWrite(get)
	require.Nil(t, attempt)
	require.True(t, writes.DefinitelyNotSent())
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://video.example/create", nil)
	require.NoError(t, err)
	post, _ = trackVideoSubmissionWrite(post)
	httptrace.ContextClientTrace(post.Context()).WroteRequest(httptrace.WroteRequestInfo{})
	require.Equal(t, 1, callbacks)
	require.False(t, writes.DefinitelyNotSent())
}
