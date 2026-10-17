//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestIsExpectedGrokRealtimeClose(t *testing.T) {
	for _, status := range []coderws.StatusCode{
		coderws.StatusNormalClosure,
		coderws.StatusGoingAway,
		coderws.StatusNoStatusRcvd,
		coderws.StatusAbnormalClosure,
	} {
		if !isExpectedGrokRealtimeClose(coderws.CloseError{Code: status}) {
			t.Fatalf("status %v should be treated as an expected session close", status)
		}
	}
	if isExpectedGrokRealtimeClose(coderws.CloseError{Code: coderws.StatusPolicyViolation}) {
		t.Fatal("policy violations must be reported as failed session closes")
	}
}

func TestGrokRealtimeAudioSettlesBeforeEveryRelayErrorWithFrozenPayer(t *testing.T) {
	for _, relayErr := range []error{
		errors.New("invalid realtime event: unexpected end of JSON input"),
		coderws.CloseError{Code: coderws.StatusPolicyViolation, Reason: "invalid frame"},
		coderws.CloseError{Code: coderws.StatusInternalError, Reason: "upstream error"},
		io.ErrUnexpectedEOF,
	} {
		t.Run(relayErr.Error(), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
			billing := historicalVideoBillingRepo{commands: make(chan *service.UsageBillingCommand, 1)}
			gateway := service.NewOpenAIGatewayService(nil, logs, billing, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, service.NewDeferredService(nil, nil, time.Second), nil, nil, nil, nil, nil, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: gateway, cfg: cfg}
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.RateMultiplier = 1
			department := int64(8)
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, DepartmentID: &department}
			price := 1.0
			key.Group.AudioRealtimePricePerMin = &price
			account := &service.Account{ID: 3, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey}
			err := h.proxyAndBillGrokRealtime(c, key, account, nil, "grok-voice-latest", func() (bool, error) {
				// Audio was consumed under the initial payer. A later refresh must
				// neither move the charge nor let an invalid frame erase it.
				key.User.Enterprise.EnterpriseID = 70
				key.User.Enterprise.MemberID = 90
				*key.User.Enterprise.DepartmentID = 80
				time.Sleep(time.Millisecond)
				return true, relayErr
			})
			require.ErrorIs(t, err, relayErr)
			// Mandatory submission completes before the transport error is returned.
			select {
			case cmd := <-billing.commands:
				require.Positive(t, cmd.EnterpriseBalanceCost)
				require.Zero(t, cmd.BalanceCost)
				require.EqualValues(t, 7, cmd.EnterpriseID)
				require.EqualValues(t, 8, cmd.EnterpriseDepartmentID)
				require.EqualValues(t, 9, cmd.EnterpriseMemberID)
			default:
				t.Fatal("observed audio did not settle before handling the relay error")
			}
			select {
			case log := <-logs.created:
				require.Positive(t, log.ActualCost)
				require.EqualValues(t, 7, *log.EnterpriseID)
				require.EqualValues(t, 8, *log.EnterpriseDepartmentID)
			default:
				t.Fatal("observed audio usage log missing")
			}
		})
	}
}

func TestGrokRealtimeBillingResultRequiresObservedAudio(t *testing.T) {
	if grokRealtimeBillingResult("grok-voice-latest", time.Second, false) != nil {
		t.Fatal("a session without observed audio must not be billed")
	}
	if grokRealtimeBillingResult("grok-voice-latest", 0, true) != nil {
		t.Fatal("zero-duration sessions must not be billed")
	}
}

func TestGrokRealtimeBillingResultUsesForcedUniqueID(t *testing.T) {
	first := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	second := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	if first == nil || second == nil {
		t.Fatal("observed audio sessions should be billable")
	}
	if first.RequestID == "" {
		t.Fatalf("unexpected billing request ID %q", first.RequestID)
	}
	if first.RequestID == second.RequestID {
		t.Fatal("independent realtime connections must not share a billing request ID")
	}
	if first.AudioUsage == nil || first.AudioUsage.Mode != "realtime" || first.AudioUsage.DurationOrUnits != 1.5 {
		t.Fatalf("unexpected audio usage: %#v", first.AudioUsage)
	}
}
