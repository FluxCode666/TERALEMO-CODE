package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type geminiDownloadRoundTripFunc func(*http.Request) (*http.Response, error)

func (f geminiDownloadRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newGeminiDownloadTestClient(t *testing.T, downloadURL string, transport http.RoundTripper) *GeminiBatchHTTPClient {
	t.Helper()
	apiClient := &http.Client{Transport: geminiDownloadRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1beta/files/results", req.URL.Path)
		require.Equal(t, "secret-key", req.Header.Get("x-goog-api-key"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"downloadUri":%q,"mimeType":"application/jsonl"}`, downloadURL))),
			Request:    req,
		}, nil
	})}
	client := NewGeminiBatchHTTPClient("https://generativelanguage.googleapis.com", apiClient)
	require.NotSame(t, apiClient, client.downloadClient, "an injected API client must not bypass the safe download client")
	client.downloadClient.Transport = transport
	return client
}

func TestBatchImageGeminiDownloadRejectsUnsafeRedirects(t *testing.T) {
	for _, target := range []string{
		"https://169.254.169.254/latest/meta-data/",
		"https://127.0.0.1/private",
		"https://[::1]/private",
		"https://attacker.example.test/collect",
		"https://storage.googleapis.com/collect",
		"https://generativelanguage.googleapis.com:8443/collect",
		"http://generativelanguage.googleapis.com/collect",
	} {
		t.Run(target, func(t *testing.T) {
			var requests int
			client := newGeminiDownloadTestClient(t, "https://generativelanguage.googleapis.com/result", geminiDownloadRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "generativelanguage.googleapis.com", req.URL.Host)
				require.Equal(t, "secret-key", req.Header.Get("x-goog-api-key"))
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{target}},
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			}))
			body, _, err := client.DownloadFile(context.Background(), "secret-key", "files/results")
			require.Error(t, err)
			require.Nil(t, body)
			require.Equal(t, 1, requests, "the redirect target must not receive a request or the API key")
		})
	}
}

func TestBatchImageGeminiDownloadAllowsSameOriginRedirect(t *testing.T) {
	var paths []string
	client := newGeminiDownloadTestClient(t, "https://generativelanguage.googleapis.com/result", geminiDownloadRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		require.Equal(t, "secret-key", req.Header.Get("x-goog-api-key"))
		response := &http.Response{Header: make(http.Header), Request: req}
		if req.URL.Path == "/result" {
			response.StatusCode = http.StatusTemporaryRedirect
			response.Header.Set("Location", "/result-final")
			response.Body = io.NopCloser(strings.NewReader(""))
		} else {
			response.StatusCode = http.StatusOK
			response.Body = io.NopCloser(strings.NewReader("result-jsonl"))
		}
		return response, nil
	}))
	body, contentType, err := client.DownloadFile(context.Background(), "secret-key", "files/results")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "result-jsonl", string(data))
	require.Equal(t, "application/jsonl", contentType)
	require.Equal(t, []string{"/result", "/result-final"}, paths)
}

func TestBatchImageGeminiDownloadBoundsRedirects(t *testing.T) {
	var requests int
	client := newGeminiDownloadTestClient(t, "https://generativelanguage.googleapis.com/result", geminiDownloadRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"/result"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}))
	_, _, err := client.DownloadFile(context.Background(), "secret-key", "files/results")
	require.ErrorContains(t, err, "stopped after 10 Gemini download redirects")
	require.Equal(t, 10, requests)
}

func TestBatchImageGeminiDownloadRejectsPrivateInitialHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "100.100.100.200", "[::1]", "metadata.google.internal", "localhost."} {
		t.Run(host, func(t *testing.T) {
			rawURL := "https://" + host + "/result"
			require.Error(t, validateGeminiDownloadHost(rawURL, rawURL))
		})
	}
}

func TestBatchImageGeminiDownloadDialRejectsPrivateResolvedAddresses(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("internal"))
	}))
	defer internal.Close()

	client := NewGeminiBatchHTTPClient("", nil)
	transport, ok := client.downloadClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy, "a proxy must not resolve the target outside the safe dialer")
	_, port, err := net.SplitHostPort(internal.Listener.Addr().String())
	require.NoError(t, err)
	// localhost. 绕过裸 localhost 名称黑名单，覆盖 DNS 解析后回环 IP 的校验。
	for _, host := range []string{"127.0.0.1", "localhost", "localhost."} {
		conn, err := transport.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
		if conn != nil {
			_ = conn.Close()
		}
		require.ErrorContains(t, err, "blocked by SSRF policy", host)
	}
	require.Zero(t, hits.Load())
}
