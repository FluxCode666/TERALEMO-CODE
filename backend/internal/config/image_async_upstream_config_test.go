//go:build unit

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadGatewayImageAsyncUpstreamPollTimeout(t *testing.T) {
	tests := []struct {
		name        string
		configValue string
		envValue    string
		want        int
	}{
		{name: "default", want: 1800},
		{name: "config file", configValue: "900", want: 900},
		{name: "config zero preserved", configValue: "0", want: 0},
		{name: "config negative preserved", configValue: "-1", want: -1},
		{name: "environment", envValue: "600", want: 600},
		{name: "environment zero preserved", envValue: "0", want: 0},
		{name: "environment negative preserved", envValue: "-1", want: -1},
		{name: "environment overrides config", configValue: "900", envValue: "1200", want: 1200},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("GATEWAY_IMAGE_ASYNC_UPSTREAM_POLL_TIMEOUT", testCase.envValue)
			if testCase.configValue != "" {
				configFile := filepath.Join(t.TempDir(), "config.yaml")
				content := "gateway:\n  image_async_upstream_poll_timeout: " + testCase.configValue + "\n"
				require.NoError(t, os.WriteFile(configFile, []byte(content), 0o600))
				t.Setenv("CONFIG_FILE", configFile)
			}

			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, testCase.want, cfg.Gateway.ImageAsyncUpstreamPollTimeout)
		})
	}
}
