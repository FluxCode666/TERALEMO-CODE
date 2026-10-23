//go:build unit

package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoReservationCapStillWarnsWhenUpstreamExceedsCount(t *testing.T) {
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	price := .02
	input := videoContractTestInput(nil)
	input.Model, input.Resolution = "veo-2.0-generate-001", "720p"
	input.APIKey.Group.VideoPrice720P = &price
	input.APIKey.Group.VideoRateIndependent, input.APIKey.Group.VideoRateMultiplier = true, 2
	input.MaxDurationSeconds, input.RequestedDurationSeconds, input.MaxVideoCount = 8, 8, 1
	contract, err := (&OpenAIGatewayService{}).BuildVideoPricingContract(context.Background(), input)
	require.NoError(t, err)
	require.InDelta(t, .32, contract.ReservedAmount, 1e-8)
	cost, err := contract.CalculateActualCost(VideoActualUsage{VideoCount: 2, DurationSeconds: 8})
	require.NoError(t, err)
	require.InDelta(t, .32, cost.ActualCost, 1e-8, "the customer charge must remain capped")
	require.Contains(t, logs.String(), "video_billing_actual_cost_exceeds_reservation")
	require.Contains(t, logs.String(), "actual_cost=0.64")
	require.Contains(t, logs.String(), "reserved_amount=0.32")
}
