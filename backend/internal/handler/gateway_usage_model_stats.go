package handler

import "github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"

// Keep the API-key usage response independent of internal/admin cost fields.
type gatewayUsageModelStat struct {
	Model               string  `json:"model"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Cost                float64 `json:"cost"`
	ActualCost          float64 `json:"actual_cost"`
}

func publicGatewayUsageModelStats(stats []usagestats.ModelStat) []gatewayUsageModelStat {
	result := make([]gatewayUsageModelStat, len(stats))
	for i, stat := range stats {
		result[i] = gatewayUsageModelStat{
			Model: stat.Model, Requests: stat.Requests,
			InputTokens: stat.InputTokens, OutputTokens: stat.OutputTokens,
			CacheCreationTokens: stat.CacheCreationTokens, CacheReadTokens: stat.CacheReadTokens,
			TotalTokens: stat.TotalTokens, Cost: stat.Cost, ActualCost: stat.ActualCost,
		}
	}
	return result
}
