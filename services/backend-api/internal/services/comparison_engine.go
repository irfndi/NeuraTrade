package services

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type LiveShadowMetrics struct {
	PnL            decimal.Decimal
	WinRate        decimal.Decimal
	TradeCount     int64
	RejectionCount int64
}

type ShadowVariantMetrics struct {
	VariantID       string
	VariantName     string
	PnL             decimal.Decimal
	WinRate         decimal.Decimal
	TradeCount      int64
	RejectionCount  int64
	EntryTimingBps  decimal.Decimal
	ExitTimingBps   decimal.Decimal
	OpportunityCost decimal.Decimal
	GateRejections  map[string]int64
}

type LiveShadowVariantComparison struct {
	VariantID             string           `json:"variant_id"`
	VariantName           string           `json:"variant_name"`
	LivePnL               decimal.Decimal  `json:"live_pnl"`
	ShadowPnL             decimal.Decimal  `json:"shadow_pnl"`
	PnLDivergence         decimal.Decimal  `json:"pnl_divergence"`
	LiveWinRate           decimal.Decimal  `json:"live_win_rate"`
	ShadowWinRate         decimal.Decimal  `json:"shadow_win_rate"`
	LiveTradeCount        int64            `json:"live_trade_count"`
	ShadowTradeCount      int64            `json:"shadow_trade_count"`
	LiveRejectionCount    int64            `json:"live_rejection_count"`
	ShadowRejectionCount  int64            `json:"shadow_rejection_count"`
	EntryTimingDiffBps    decimal.Decimal  `json:"entry_timing_diff_bps"`
	ExitTimingDiffBps     decimal.Decimal  `json:"exit_timing_diff_bps"`
	OpportunityCost       decimal.Decimal  `json:"opportunity_cost"`
	GateRejectionDelta    map[string]int64 `json:"gate_rejection_delta"`
	OperatorSummary       string           `json:"operator_summary"`
	OutperformingBaseline bool             `json:"outperforming_baseline"`
}

type LiveShadowComparisonReport struct {
	GeneratedAt time.Time                     `json:"generated_at"`
	WindowStart time.Time                     `json:"window_start"`
	WindowEnd   time.Time                     `json:"window_end"`
	Comparisons []LiveShadowVariantComparison `json:"comparisons"`
}

type LiveShadowComparisonEngine struct {
	logger *zap.Logger
}

func NewLiveShadowComparisonEngine(logger *zap.Logger) *LiveShadowComparisonEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LiveShadowComparisonEngine{logger: logger}
}

func (e *LiveShadowComparisonEngine) BuildReport(
	windowStart time.Time,
	windowEnd time.Time,
	live LiveShadowMetrics,
	shadow []ShadowVariantMetrics,
) LiveShadowComparisonReport {
	if windowEnd.IsZero() {
		windowEnd = time.Now().UTC()
	}
	if windowStart.IsZero() {
		windowStart = windowEnd.Add(-24 * time.Hour)
	}
	report := LiveShadowComparisonReport{
		GeneratedAt: time.Now().UTC(),
		WindowStart: windowStart.UTC(),
		WindowEnd:   windowEnd.UTC(),
		Comparisons: make([]LiveShadowVariantComparison, 0, len(shadow)),
	}
	for _, metric := range shadow {
		comparison := LiveShadowVariantComparison{
			VariantID:            metric.VariantID,
			VariantName:          metric.VariantName,
			LivePnL:              live.PnL,
			ShadowPnL:            metric.PnL,
			PnLDivergence:        metric.PnL.Sub(live.PnL),
			LiveWinRate:          live.WinRate,
			ShadowWinRate:        metric.WinRate,
			LiveTradeCount:       live.TradeCount,
			ShadowTradeCount:     metric.TradeCount,
			LiveRejectionCount:   live.RejectionCount,
			ShadowRejectionCount: metric.RejectionCount,
			// EntryTimingDiffBps and ExitTimingDiffBps require per-trade
			// timestamps for both live and shadow which are not available in
			// the aggregated metrics. Leaving as zero until timestamp tracking
			// is added to the live pipeline.
			OpportunityCost:    metric.OpportunityCost,
			GateRejectionDelta: gateRejectionDelta(metric.GateRejections, live.RejectionCount, metric.RejectionCount),
		}
		comparison.OutperformingBaseline = comparison.PnLDivergence.GreaterThan(decimal.Zero)
		comparison.OperatorSummary = buildOperatorSummary(comparison)
		report.Comparisons = append(report.Comparisons, comparison)
	}
	sort.Slice(report.Comparisons, func(i, j int) bool {
		if report.Comparisons[i].PnLDivergence.Equal(report.Comparisons[j].PnLDivergence) {
			return report.Comparisons[i].VariantID < report.Comparisons[j].VariantID
		}
		return report.Comparisons[i].PnLDivergence.GreaterThan(report.Comparisons[j].PnLDivergence)
	})
	return report
}

func gateRejectionDelta(shadowGates map[string]int64, liveCount int64, shadowCount int64) map[string]int64 {
	result := make(map[string]int64, len(shadowGates)+1)
	// Per-gate values are shadow-only since live metrics only expose an
	// aggregate RejectionCount. The "total_delta" field provides the
	// overall shadow-vs-live difference.
	for code, count := range shadowGates {
		result[code] = count
	}
	result["total_delta"] = shadowCount - liveCount
	return result
}

func buildOperatorSummary(c LiveShadowVariantComparison) string {
	divergencePct := decimal.Zero
	if !c.LivePnL.IsZero() {
		divergencePct = c.PnLDivergence.Div(c.LivePnL.Abs()).Mul(decimal.NewFromInt(100))
	}
	return fmt.Sprintf(
		"Shadow variant %s %s live by %s%% over %d shadow trades (live win-rate %s%% vs shadow %s%%)",
		c.VariantName,
		compareVerb(c.PnLDivergence),
		divergencePct.Round(2).String(),
		c.ShadowTradeCount,
		c.LiveWinRate.Round(2).String(),
		c.ShadowWinRate.Round(2).String(),
	)
}

func compareVerb(divergence decimal.Decimal) string {
	if divergence.GreaterThan(decimal.Zero) {
		return "outperformed"
	}
	if divergence.LessThan(decimal.Zero) {
		return "underperformed"
	}
	return "matched"
}
