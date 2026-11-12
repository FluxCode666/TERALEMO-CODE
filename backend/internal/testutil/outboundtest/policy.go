// Package outboundtest configures explicit exceptions for local HTTP fixtures.
package outboundtest

import "github.com/Wei-Shaw/sub2api/internal/pkg/outbound"

// AllowLoopback is called explicitly by test entrypoints, never during init.
// Policy tests must install their own policy and restore this fixture policy.
func AllowLoopback() {
	if err := outbound.Configure([]string{"127.0.0.0/8", "::1/128"}); err != nil {
		panic(err)
	}
}
