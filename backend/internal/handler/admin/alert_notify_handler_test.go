//go:build unit

package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type alertNotifyHandlerRepo struct {
	service.SettingRepository
	value string
}

func (repo *alertNotifyHandlerRepo) GetValue(_ context.Context, key string) (string, error) {
	switch key {
	case service.SettingKeyAlertNotifyChannels:
		return repo.value, nil
	case service.SettingKeySiteName:
		return "Test Site", nil
	default:
		return "", service.ErrSettingNotFound
	}
}

func (repo *alertNotifyHandlerRepo) Set(_ context.Context, key, value string) error {
	if key != service.SettingKeyAlertNotifyChannels {
		return errors.New("unexpected setting key")
	}
	repo.value = value
	return nil
}

type alertNotifyHandlerTransport func(*http.Request) (*http.Response, error)

func (send alertNotifyHandlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return send(req)
}

func TestAlertNotifyHandlerGetPutAndTest(t *testing.T) {
	const endpoint = "https://receiver.example/webhooks/private-path-1234?token=query-token"
	const secret = "sample-signing-key"
	var outboundCalls int
	var signature string
	var status = http.StatusNoContent
	client := &http.Client{Transport: alertNotifyHandlerTransport(func(req *http.Request) (*http.Response, error) {
		outboundCalls++
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, endpoint, req.URL.String())
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		require.Equal(t, "AlertNotify/1.0", req.UserAgent())
		require.Equal(t, "test", req.Header.Get("X-Webhook-Event"))
		timestamp := req.Header.Get("X-Webhook-Timestamp")
		require.Regexp(t, `^\d+$`, timestamp)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var payload map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, `"1"`, string(payload["version"]))
		require.Equal(t, `"test"`, string(payload["event"]))
		require.Equal(t, `"firing"`, string(payload["status"]))
		require.Equal(t, `"info"`, string(payload["severity"]))
		require.Equal(t, "true", string(payload["test"]))
		require.Equal(t, `"Test Site"`, string(payload["site_name"]))
		signature = req.Header.Get("X-Webhook-Signature")
		if signature != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			_, err = mac.Write(append([]byte(timestamp+"."), body...))
			require.NoError(t, err)
			require.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), signature)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(endpoint + secret))}, nil
	})}
	var cfg config.Config
	cfg.Totp.EncryptionKey = strings.Repeat("ab", 32)
	encryptor, err := repository.NewAESEncryptor(&cfg)
	require.NoError(t, err)
	repo := &alertNotifyHandlerRepo{}
	handler := NewAlertNotifyHandler(service.NewAlertNotifyService(repo, encryptor, client))
	router := gin.New()
	const path = "/api/v1/admin/alert-notify/channels"
	router.GET(path, handler.GetChannels)
	router.PUT(path, handler.PutChannels)
	router.POST(path+"/test", handler.TestChannel)
	request := func(method, path, body string, wantStatus int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, wantStatus, rec.Code, rec.Body.String())
		for _, sensitive := range []string{endpoint, secret, "private-path", "query-token", "url_enc", "secret_enc"} {
			require.NotContains(t, rec.Body.String(), sensitive)
		}
		return rec
	}
	initial := request(http.MethodGet, path, "", http.StatusOK)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"channels":[],"supported_events":["ops.alert","account.quota_alert"]}}`, initial.Body.String())
	body := `{"channels":[{"name":"test channel","type":"webhook","url":"` + endpoint + `","secret":"` + secret + `","events":["ops.alert"],"min_severity":"critical"}]}`
	updated := request(http.MethodPut, path, body, http.StatusOK)
	require.NotContains(t, repo.value, endpoint)
	require.NotContains(t, repo.value, "query-token")
	require.NotContains(t, repo.value, secret)
	var stored []struct {
		URLEnc    string `json:"url_enc"`
		SecretEnc string `json:"secret_enc"`
	}
	require.NoError(t, json.Unmarshal([]byte(repo.value), &stored))
	require.Len(t, stored, 1)
	storedURL, err := encryptor.Decrypt(stored[0].URLEnc)
	require.NoError(t, err)
	require.Equal(t, endpoint, storedURL)
	storedSecret, err := encryptor.Decrypt(stored[0].SecretEnc)
	require.NoError(t, err)
	require.Equal(t, secret, storedSecret)
	var envelope struct {
		Data service.AlertNotifyChannelsView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Channels, 1)
	channel := envelope.Data.Channels[0]
	require.Equal(t, "https://receiver.example/webhooks/****1234", channel.URLMasked)
	require.True(t, channel.SecretConfigured)
	require.False(t, channel.Enabled)
	loaded := request(http.MethodGet, path, "", http.StatusOK)
	require.JSONEq(t, updated.Body.String(), loaded.Body.String())
	request(http.MethodPut, path, `{"channels":[{"id":"missing"}]}`, http.StatusBadRequest)
	request(http.MethodPut, path, `{}`, http.StatusBadRequest)
	request(http.MethodPost, path+"/test", `{"id":"missing","type":"webhook"}`, http.StatusBadRequest)
	input := `{"id":"` + channel.ID + `","type":"webhook"}`
	tested := request(http.MethodPost, path+"/test", input, http.StatusOK)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"success":true}}`, tested.Body.String())
	require.NotEmpty(t, signature)
	request(http.MethodPost, path+"/test", `{"id":"`+channel.ID+`","type":"webhook","clear_secret":true}`, http.StatusOK)
	require.Empty(t, signature)
	status = http.StatusInternalServerError
	failed := request(http.MethodPost, path+"/test", input, http.StatusBadRequest)
	require.Contains(t, failed.Body.String(), "webhook returned HTTP 500")
	require.Equal(t, 3, outboundCalls)
}
