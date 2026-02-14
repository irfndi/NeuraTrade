package services

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// CohortType represents the classification of a wallet/cohort.
type CohortType string

const (
	// CohortTypeWhale represents large volume traders.
	CohortTypeWhale CohortType = "whale"
	// CohortTypeSmartMoney represents sophisticated traders with consistent profits.
	CohortTypeSmartMoney CohortType = "smart_money"
	// CohortTypeRetail represents typical retail traders.
	CohortTypeRetail CohortType = "retail"
	// CohortTypeUnknown represents unclassified traders.
	CohortTypeUnknown CohortType = "unknown"
)

// FlowDirection represents the direction of money flow.
type FlowDirection string

const (
	FlowIn      FlowDirection = "in"
	FlowOut     FlowDirection = "out"
	FlowNeutral FlowDirection = "neutral"
)

// WalletFlow represents the flow of funds for a wallet.
type WalletFlow struct {
	WalletAddress string
	Cohort        CohortType
	Direction     FlowDirection
	Volume        decimal.Decimal
	Timestamp     time.Time
}

// CohortMetrics holds aggregated metrics for a cohort.
type CohortMetrics struct {
	CohortType    CohortType
	TotalWallets  int
	TotalInflow   decimal.Decimal
	TotalOutflow  decimal.Decimal
	NetFlow       decimal.Decimal
	AverageVolume decimal.Decimal
	UpdatedAt     time.Time
}

// CohortFlowResult contains the analysis results.
type CohortFlowResult struct {
	Cohorts        []CohortMetrics
	OverallTrend   FlowDirection
	TotalVolume    decimal.Decimal
	SmartMoneyFlow decimal.Decimal
	AnalyzedAt     time.Time
}

// CohortFlowAnalyzer analyzes wallet flows and classifies them into cohorts.
type CohortFlowAnalyzer struct {
	db     DBPool
	config *CohortConfig
}

// CohortConfig holds configuration for cohort analysis.
type CohortConfig struct {
	WhaleThreshold      decimal.Decimal // Minimum volume to be considered a whale
	SmartMoneyMinTrades int             // Minimum trades for smart money classification
	SmartMoneyWinRate   decimal.Decimal // Minimum win rate for smart money
	AnalysisWindow      time.Duration   // Time window for analysis
}

// DefaultCohortConfig returns a standard configuration.
func DefaultCohortConfig() *CohortConfig {
	return &CohortConfig{
		WhaleThreshold:      decimal.NewFromFloat(100000), // $100k
		SmartMoneyMinTrades: 100,
		SmartMoneyWinRate:   decimal.NewFromFloat(0.55), // 55% win rate
		AnalysisWindow:      24 * time.Hour,
	}
}

// NewCohortFlowAnalyzer creates a new cohort flow analyzer.
func NewCohortFlowAnalyzer(db DBPool, config *CohortConfig) *CohortFlowAnalyzer {
	if config == nil {
		config = DefaultCohortConfig()
	}
	return &CohortFlowAnalyzer{
		db:     db,
		config: config,
	}
}

// AnalyzeFlows analyzes wallet flows and returns cohort-level metrics.
func (c *CohortFlowAnalyzer) AnalyzeFlows(ctx context.Context, startTime time.Time) (*CohortFlowResult, error) {
	// Get recent wallet flows from database
	flows, err := c.fetchWalletFlows(ctx, startTime)
	if err != nil {
		return nil, err
	}

	// Classify wallets into cohorts
	cohortFlows := c.classifyFlows(flows)

	// Aggregate metrics by cohort
	cohorts := c.aggregateByCohort(cohortFlows)

	// Calculate overall trend
	overallTrend := c.calculateOverallTrend(cohorts)

	result := &CohortFlowResult{
		Cohorts:        cohorts,
		OverallTrend:   overallTrend,
		TotalVolume:    c.calculateTotalVolume(flows),
		SmartMoneyFlow: c.calculateSmartMoneyFlow(cohorts),
		AnalyzedAt:     time.Now(),
	}

	return result, nil
}

// classifyFlows classifies wallet flows into cohorts based on their characteristics.
func (c *CohortFlowAnalyzer) classifyFlows(flows []WalletFlow) map[CohortType][]WalletFlow {
	cohortFlows := make(map[CohortType][]WalletFlow)

	for _, flow := range flows {
		cohortType := c.determineCohort(flow)
		cohortFlows[cohortType] = append(cohortFlows[cohortType], flow)
	}

	return cohortFlows
}

// determineCohort determines the cohort type for a wallet based on volume and behavior.
func (c *CohortFlowAnalyzer) determineCohort(flow WalletFlow) CohortType {
	// Check if whale based on volume
	if flow.Volume.GreaterThanOrEqual(c.config.WhaleThreshold) {
		return CohortTypeWhale
	}

	// Smart money detection would require historical trade data
	// For now, classify larger volumes as potential smart money
	if flow.Volume.GreaterThan(c.config.WhaleThreshold.Div(decimal.NewFromInt(10))) {
		return CohortTypeSmartMoney
	}

	return CohortTypeRetail
}

// aggregateByCohort aggregates flows by cohort type.
func (c *CohortFlowAnalyzer) aggregateByCohort(cohortFlows map[CohortType][]WalletFlow) []CohortMetrics {
	metrics := make([]CohortMetrics, 0, len(cohortFlows))

	for cohortType, flows := range cohortFlows {
		m := CohortMetrics{
			CohortType:   cohortType,
			TotalWallets: len(flows),
			UpdatedAt:    time.Now(),
		}

		for _, flow := range flows {
			switch flow.Direction {
			case FlowIn:
				m.TotalInflow = m.TotalInflow.Add(flow.Volume)
			case FlowOut:
				m.TotalOutflow = m.TotalOutflow.Add(flow.Volume)
			}
		}

		m.NetFlow = m.TotalInflow.Sub(m.TotalOutflow)
		if len(flows) > 0 {
			m.AverageVolume = m.NetFlow.Div(decimal.NewFromInt(int64(len(flows))))
		}

		metrics = append(metrics, m)
	}

	return metrics
}

// calculateOverallTrend determines the overall market trend based on cohort flows.
func (c *CohortFlowAnalyzer) calculateOverallTrend(cohorts []CohortMetrics) FlowDirection {
	var totalIn, totalOut decimal.Decimal

	for _, m := range cohorts {
		totalIn = totalIn.Add(m.TotalInflow)
		totalOut = totalOut.Add(m.TotalOutflow)
	}

	if totalIn.GreaterThan(totalOut) {
		return FlowIn
	}
	if totalOut.GreaterThan(totalIn) {
		return FlowOut
	}

	return FlowNeutral
}

// calculateTotalVolume calculates the total volume across all flows.
func (c *CohortFlowAnalyzer) calculateTotalVolume(flows []WalletFlow) decimal.Decimal {
	var total decimal.Decimal
	for _, f := range flows {
		total = total.Add(f.Volume)
	}
	return total
}

// calculateSmartMoneyFlow calculates the net flow from smart money cohorts.
func (c *CohortFlowAnalyzer) calculateSmartMoneyFlow(cohorts []CohortMetrics) decimal.Decimal {
	var smartFlow decimal.Decimal
	for _, m := range cohorts {
		if m.CohortType == CohortTypeSmartMoney || m.CohortType == CohortTypeWhale {
			smartFlow = smartFlow.Add(m.NetFlow)
		}
	}
	return smartFlow
}

// fetchWalletFlows retrieves wallet flows from the database.
// This is a placeholder - actual implementation would query the database.
func (c *CohortFlowAnalyzer) fetchWalletFlows(ctx context.Context, startTime time.Time) ([]WalletFlow, error) {
	// TODO: Implement database query to fetch actual wallet flows
	// This would query the positions/trades tables to determine wallet behavior
	return []WalletFlow{}, nil
}

// GetCohortForWallet determines the cohort classification for a specific wallet.
func (c *CohortFlowAnalyzer) GetCohortForWallet(ctx context.Context, walletAddress string) (CohortType, error) {
	// TODO: Implement wallet-specific cohort analysis
	// This would analyze historical trades, win rate, position sizing, etc.
	return CohortTypeUnknown, nil
}
