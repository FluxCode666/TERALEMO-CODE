//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type storageConfigEncryptor struct{}

func (storageConfigEncryptor) Encrypt(secret string) (string, error) {
	return "encrypted:" + secret, nil
}

func (storageConfigEncryptor) Decrypt(secret string) (string, error) {
	return strings.TrimPrefix(secret, "encrypted:"), nil
}

func TestUpdateSharedS3CredentialsRefreshesImageStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newTestSettingRepo()
	shared := service.NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}, storageConfigEncryptor{}, nil)
	_, err := shared.UpdateS3Config(context.Background(), service.BackupS3Config{
		Bucket: "shared-bucket", AccessKeyID: "old-key", SecretAccessKey: "old-secret",
	})
	require.NoError(t, err)
	var clients []config.ImageStorageConfig
	images := service.NewImageStorageSettingService(repo, storageConfigEncryptor{}, shared,
		func(_ context.Context, cfg *config.ImageStorageConfig) (service.ImageStorage, error) {
			clients = append(clients, *cfg)
			return nil, nil
		}, config.ImageStorageConfig{})
	_, err = images.Update(context.Background(), service.ImageStorageSettings{
		Enabled: true, ReuseBackupS3: true, Prefix: "images/",
	})
	require.NoError(t, err)
	resolver := images.Resolver()
	_, enabled := resolver()
	require.True(t, enabled)
	require.Len(t, clients, 1)
	require.Equal(t, "old-secret", clients[0].SecretAccessKey)
	router := gin.New()
	router.PUT("/s3-config", NewBackupHandler(shared, images).UpdateS3Config)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/s3-config", strings.NewReader(
		`{"bucket":"shared-bucket","access_key_id":"rotated-key","secret_access_key":"rotated-secret"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "rotated-secret")
	_, enabled = resolver()
	require.True(t, enabled)
	require.Len(t, clients, 2)
	require.Equal(t, "rotated-key", clients[1].AccessKeyID)
	require.Equal(t, "rotated-secret", clients[1].SecretAccessKey)
	require.Equal(t, "images/", clients[1].Prefix)
	var stored service.BackupS3Config
	require.NoError(t, json.Unmarshal([]byte(repo.values["backup_s3_config"]), &stored))
	require.Equal(t, "encrypted:rotated-secret", stored.SecretAccessKey)
}
