package migrations_test

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/migrate"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usagelog"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoUsageRequestIDPreservesGeminiOperationResource(t *testing.T) {
	// Both variable resource segments are valid at the gateway's 128-byte
	// segment boundary. The resulting billing ID exceeds both old SQL limits.
	operation := "models/" + strings.Repeat("m", 128) + "/operations/" + strings.Repeat("o", 128)
	require.NoError(t, service.ValidateGeminiVideoOperation(operation))
	id := service.StableGrokVideoBillingRequestID("gemini:" + operation)
	require.Greater(t, len(id), 255)
	require.Equal(t, "grok-video:gemini:"+operation, id)
	require.Equal(t, id, service.StableGrokVideoBillingRequestID(id), "retry must keep the existing dedup key")
	require.NoError(t, usagelog.RequestIDValidator(id), "Ent builders must accept the unchanged ID")
	require.Error(t, usagelog.RequestIDValidator(""))

	for _, column := range migrate.UsageLogsColumns {
		if column.Name == "request_id" {
			require.Equal(t, "text", column.SchemaType["postgres"])
			return
		}
	}
	t.Fatal("generated usage_logs schema has no request_id")
}
