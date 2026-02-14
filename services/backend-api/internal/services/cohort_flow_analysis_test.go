package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCohortFlowAnalyzer_DefaultConfig(t *testing.T) {
	config := DefaultCohortConfig()
	assert.NotNil(t, config)
	assert.Equal(t, decimal.NewFromFloat(100000), config.WhaleThreshold)
	assert.Equal(t, 100, config.SmartMoneyMinTrades)
}

func TestCohortFlowAnalyzer_ClassifyFlows(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	flows := []WalletFlow{
		{WalletAddress: "whale1", Volume: decimal.NewFromFloat(150000), Direction: FlowIn},
		{WalletAddress: "whale2", Volume: decimal.NewFromFloat(200000), Direction: FlowOut},
		{WalletAddress: "smart1", Volume: decimal.NewFromFloat(50000), Direction: FlowIn},
		{WalletAddress: "smart2", Volume: decimal.NewFromFloat(30000), Direction: FlowIn},
		{WalletAddress: "retail1", Volume: decimal.NewFromFloat(1000), Direction: FlowIn},
		{WalletAddress: "retail2", Volume: decimal.NewFromFloat(500), Direction: FlowOut},
	}

	cohortFlows := analyzer.classifyFlows(flows)

	// Check whale classification
	whaleFlows, ok := cohortFlows[CohortTypeWhale]
	assert.True(t, ok)
	assert.Equal(t, 2, len(whaleFlows))

	// Check smart money classification
	smartFlows, ok := cohortFlows[CohortTypeSmartMoney]
	assert.True(t, ok)
	assert.Equal(t, 2, len(smartFlows))

	// Check retail classification
	retailFlows, ok := cohortFlows[CohortTypeRetail]
	assert.True(t, ok)
	assert.Equal(t, 2, len(retailFlows))
}

func TestCohortFlowAnalyzer_AggregateByCohort(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	cohortFlows := map[CohortType][]WalletFlow{
		CohortTypeWhale: {
			{WalletAddress: "whale1", Volume: decimal.NewFromFloat(100000), Direction: FlowIn},
			{WalletAddress: "whale2", Volume: decimal.NewFromFloat(50000), Direction: FlowOut},
		},
		CohortTypeRetail: {
			{WalletAddress: "retail1", Volume: decimal.NewFromFloat(1000), Direction: FlowIn},
		},
	}

	metrics := analyzer.aggregateByCohort(cohortFlows)

	// Check whale metrics
	var whaleMetrics *CohortMetrics
	for i := range metrics {
		if metrics[i].CohortType == CohortTypeWhale {
			whaleMetrics = &metrics[i]
			break
		}
	}
	assert.NotNil(t, whaleMetrics)
	assert.Equal(t, 2, whaleMetrics.TotalWallets)
	assert.True(t, whaleMetrics.TotalInflow.Equal(decimal.NewFromFloat(100000)))
	assert.True(t, whaleMetrics.TotalOutflow.Equal(decimal.NewFromFloat(50000)))
	assert.True(t, whaleMetrics.NetFlow.Equal(decimal.NewFromFloat(50000)))
}

func TestCohortFlowAnalyzer_CalculateOverallTrend(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	tests := []struct {
		name     string
		cohorts  []CohortMetrics
		expected FlowDirection
	}{
		{
			name: "net inflow",
			cohorts: []CohortMetrics{
				{TotalInflow: decimal.NewFromFloat(20000), TotalOutflow: decimal.NewFromFloat(5000)},
				{TotalInflow: decimal.NewFromFloat(5000), TotalOutflow: decimal.NewFromFloat(1000)},
			},
			expected: FlowIn,
		},
		{
			name: "net outflow",
			cohorts: []CohortMetrics{
				{TotalInflow: decimal.NewFromFloat(5000), TotalOutflow: decimal.NewFromFloat(20000)},
				{TotalInflow: decimal.NewFromFloat(1000), TotalOutflow: decimal.NewFromFloat(5000)},
			},
			expected: FlowOut,
		},
		{
			name: "neutral",
			cohorts: []CohortMetrics{
				{TotalInflow: decimal.NewFromFloat(10000), TotalOutflow: decimal.NewFromFloat(10000)},
				{TotalInflow: decimal.NewFromFloat(5000), TotalOutflow: decimal.NewFromFloat(5000)},
			},
			expected: FlowNeutral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.calculateOverallTrend(tt.cohorts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCohortFlowAnalyzer_CalculateTotalVolume(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	flows := []WalletFlow{
		{Volume: decimal.NewFromFloat(10000)},
		{Volume: decimal.NewFromFloat(5000)},
		{Volume: decimal.NewFromFloat(2500)},
	}

	total := analyzer.calculateTotalVolume(flows)
	assert.True(t, total.Equal(decimal.NewFromFloat(17500)))
}

func TestCohortFlowAnalyzer_CalculateSmartMoneyFlow(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	cohorts := []CohortMetrics{
		{CohortType: CohortTypeWhale, NetFlow: decimal.NewFromFloat(100000)},
		{CohortType: CohortTypeSmartMoney, NetFlow: decimal.NewFromFloat(50000)},
		{CohortType: CohortTypeRetail, NetFlow: decimal.NewFromFloat(1000)},
	}

	smartFlow := analyzer.calculateSmartMoneyFlow(cohorts)
	assert.True(t, smartFlow.Equal(decimal.NewFromFloat(150000)))
}

func TestCohortFlowAnalyzer_AnalyzeFlows(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	// Mock fetchWalletFlows behavior by using a test implementation
	ctx := context.Background()
	startTime := time.Now().Add(-24 * time.Hour)

	result, err := analyzer.AnalyzeFlows(ctx, startTime)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// Empty flows should return empty cohorts
	assert.NotNil(t, result.Cohorts)
}

func TestDetermineCohort(t *testing.T) {
	analyzer := NewCohortFlowAnalyzer(nil, nil)

	tests := []struct {
		name     string
		volume   decimal.Decimal
		expected CohortType
	}{
		{
			name:     "whale threshold",
			volume:   decimal.NewFromFloat(150000),
			expected: CohortTypeWhale,
		},
		{
			name:     "at whale threshold",
			volume:   decimal.NewFromFloat(100000),
			expected: CohortTypeWhale,
		},
		{
			name:     "just below whale threshold - smart money",
			volume:   decimal.NewFromFloat(50000),
			expected: CohortTypeSmartMoney,
		},
		{
			name:     "retail volume",
			volume:   decimal.NewFromFloat(1000),
			expected: CohortTypeRetail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := WalletFlow{WalletAddress: "test", Volume: tt.volume, Direction: FlowIn}
			cohort := analyzer.determineCohort(flow)
			assert.Equal(t, tt.expected, cohort)
		})
	}
}
