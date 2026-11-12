package antigravity

import (
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/testutil/outboundtest"
)

// Local HTTP fixtures require an explicit exception just like private deployments.
// Destination-policy tests install their own policy and restore this fixture policy.
func TestMain(m *testing.M) {
	outboundtest.AllowLoopback()
	os.Exit(m.Run())
}
