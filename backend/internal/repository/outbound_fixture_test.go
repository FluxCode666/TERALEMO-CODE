//go:build !integration

package repository

import (
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
)

// Local HTTP fixtures require an explicit exception just like private deployments.
// Destination-policy tests install their own policy and restore this fixture policy.
func TestMain(m *testing.M) {
	if err := outbound.Configure([]string{"127.0.0.0/8", "::1/128"}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
