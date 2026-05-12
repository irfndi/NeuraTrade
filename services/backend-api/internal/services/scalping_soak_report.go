package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type ScalpingSoakReportFilter struct {
	ChatID   string
	Exchange string
	Since    time.Time
	Until    time.Time
	Baseline *ScalpingSoakBaseline
}

type ScalpingSoakBaseline struct {
	Name           string             `json:"name"`
	BalanceUSDT    decimal.Decimal    `json:"balance_usdt"`
	TotalTrades    int                `json:"total_trades"`
	ClosedTrades   int                `json:"closed_trades"`
	WinRate        decimal.Decimal    `json:"win_rate"`
	NetPnL         decimal.Decimal    `json:"net_pnl"`
	Fees           decimal.Decimal    `json:"fees"`
	AvgPnLPerTrade decimal.Decimal    `json:"avg_pnl_per_trade"`
	TotalCycles    int                `json:"total_cycles"`
	ActionSplit    map[string]float64 `json:"action_split"`
	RegimeSplit    map[string]float64 `json:"regime_split"`
}

type ScalpingSoakReport struct {
	ChatID                 string                          `json:"chat_id,omitempty"`
	Exchange               string                          `json:"exchange,omitempty"`
	WindowStart            time.Time                       `json:"window_start"`
	WindowEnd              time.Time                       `json:"window_end"`
	TotalCycles            int                             `json:"total_cycles"`
	ActionBreakdown        map[string]int                  `json:"action_breakdown"`
	ActionSplit            map[string]decimal.Decimal      `json:"action_split"`
	RegimeBreakdown        map[string]int                  `json:"regime_breakdown"`
	RegimeSplit            map[string]decimal.Decimal      `json:"regime_split"`
	RejectionByReason      map[string]int                  `json:"rejection_by_reason"`
	GateBlockByCode        map[string]int                  `json:"gate_block_by_code"`
	SignalQuality          ScalpingSignalQualitySoakStats  `json:"signal_quality"`
	TradeSummary           ScalpingSoakTradeSummary        `json:"trade_summary"`
	AIProviderDegradation  ScalpingAIDegradationSoakStats  `json:"ai_provider_degradation"`
	BaselineComparison     *ScalpingSoakBaselineComparison `json:"baseline_comparison,omitempty"`
	InsufficientTradeProof bool                            `json:"insufficient_trade_proof"`

	cumulativeNetPnL  decimal.Decimal
	peakNetPnL        decimal.Decimal
	grossWinningPnL   decimal.Decimal
	grossLosingPnL    decimal.Decimal
	spreadCount       int
	imbalanceCount    int
	rangeCount        int
	priceChangeCount  int
	holdDurationCount int
}

type ScalpingSignalQualitySoakStats struct {
	KnownCycles                int             `json:"known_cycles"`
	Coverage                   decimal.Decimal `json:"coverage"`
	AvgBidAskSpreadPct         decimal.Decimal `json:"avg_bid_ask_spread_pct"`
	AvgAbsOrderBookImbalance   decimal.Decimal `json:"avg_abs_order_book_imbalance"`
	AvgRangePosition24h        decimal.Decimal `json:"avg_range_position_24h"`
	AvgPriceChange24hPct       decimal.Decimal `json:"avg_price_change_24h_pct"`
	MissingSignalQualityCycles int             `json:"missing_signal_quality_cycles"`
}

type ScalpingSoakTradeSummary struct {
	ClosedTrades       int             `json:"closed_trades"`
	Wins               int             `json:"wins"`
	Losses             int             `json:"losses"`
	Breakeven          int             `json:"breakeven"`
	WinRate            decimal.Decimal `json:"win_rate"`
	GrossPnL           decimal.Decimal `json:"gross_pnl"`
	NetPnL             decimal.Decimal `json:"net_pnl"`
	Fees               decimal.Decimal `json:"fees"`
	AvgNetPnLPerTrade  decimal.Decimal `json:"avg_net_pnl_per_trade"`
	BestTradeNetPnL    decimal.Decimal `json:"best_trade_net_pnl"`
	WorstTradeNetPnL   decimal.Decimal `json:"worst_trade_net_pnl"`
	MaxDrawdown        decimal.Decimal `json:"max_drawdown"`
	MaxDrawdownPct     decimal.Decimal `json:"max_drawdown_pct"`
	ProfitFactor       decimal.Decimal `json:"profit_factor"`
	AvgHoldDurationSec decimal.Decimal `json:"avg_hold_duration_sec"`
}

type ScalpingAIDegradationSoakStats struct {
	DegradedCycles int            `json:"degraded_cycles"`
	ByReason       map[string]int `json:"by_reason"`
}

type ScalpingSoakBaselineComparison struct {
	BaselineName        string          `json:"baseline_name"`
	DeltaClosedTrades   int             `json:"delta_closed_trades"`
	DeltaWinRate        decimal.Decimal `json:"delta_win_rate"`
	DeltaNetPnL         decimal.Decimal `json:"delta_net_pnl"`
	DeltaFees           decimal.Decimal `json:"delta_fees"`
	DeltaAvgPnLPerTrade decimal.Decimal `json:"delta_avg_pnl_per_trade"`
	DeltaCycles         int             `json:"delta_cycles"`
}

type scalpingSoakCycleRow struct {
	action              string
	regime              string
	gateBlockCode       string
	rejectionCountsJSON string
	bidAskSpreadPct     sql.NullFloat64
	orderBookImbalance  sql.NullFloat64
	rangePosition24h    sql.NullFloat64
	priceChange24hPct   sql.NullFloat64
	outcome             string
	grossPnL            decimal.Decimal
	fees                decimal.Decimal
	netPnL              decimal.Decimal
	holdDurationSeconds sql.NullInt64
}

func BrokenScalpingBaseline() ScalpingSoakBaseline {
	return ScalpingSoakBaseline{
		Name:           "broken-live-baseline-2026-05-11",
		BalanceUSDT:    decimal.NewFromInt(48),
		TotalTrades:    68,
		ClosedTrades:   57,
		WinRate:        mustSoakDecimal("0.123"),
		NetPnL:         mustSoakDecimal("-0.18"),
		Fees:           mustSoakDecimal("-0.57"),
		AvgPnLPerTrade: mustSoakDecimal("-0.003"),
		TotalCycles:    5406,
		ActionSplit: map[string]float64{
			"hold": 0.745,
			"buy":  0.184,
			"sell": 0.071,
		},
		RegimeSplit: map[string]float64{
			"neutral": 0.755,
			"trend":   0.191,
		},
	}
}

func BuildScalpingSoakReport(ctx context.Context, db DBPool, filter ScalpingSoakReportFilter) (ScalpingSoakReport, error) {
	if isNilDBPool(db) {
		return ScalpingSoakReport{}, fmt.Errorf("scalping soak report requires database")
	}
	if filter.Since.IsZero() {
		filter.Since = time.Unix(0, 0).UTC()
	}
	if filter.Until.IsZero() {
		filter.Until = time.Now().UTC()
	}
	if filter.Until.Before(filter.Since) {
		return ScalpingSoakReport{}, fmt.Errorf("soak report until must be after since")
	}

	report := ScalpingSoakReport{
		ChatID:            strings.TrimSpace(filter.ChatID),
		Exchange:          strings.TrimSpace(filter.Exchange),
		WindowStart:       filter.Since.UTC(),
		WindowEnd:         filter.Until.UTC(),
		ActionBreakdown:   make(map[string]int),
		ActionSplit:       make(map[string]decimal.Decimal),
		RegimeBreakdown:   make(map[string]int),
		RegimeSplit:       make(map[string]decimal.Decimal),
		RejectionByReason: make(map[string]int),
		GateBlockByCode:   make(map[string]int),
		AIProviderDegradation: ScalpingAIDegradationSoakStats{
			ByReason: make(map[string]int),
		},
	}

	rows, err := queryScalpingSoakRows(ctx, db, filter)
	if err != nil {
		return ScalpingSoakReport{}, err
	}
	for _, row := range rows {
		report.addCycle(row)
	}

	report.finalize(filter.Baseline)
	return report, nil
}

func queryScalpingSoakRows(ctx context.Context, db DBPool, filter ScalpingSoakReportFilter) ([]scalpingSoakCycleRow, error) {
	store := NewScalpingTelemetryStore(db, nil)
	query := `
		SELECT
			COALESCE(c.action, ''),
			COALESCE(c.regime, ''),
			COALESCE(c.gate_block_code, ''),
			COALESCE(c.rejection_counts, ''),
			c.bid_ask_spread_pct,
			c.order_book_imbalance,
			c.range_position_24h,
			c.price_change_24h_pct,
			COALESCE(c.outcome, ''),
			CAST(COALESCE(j.realized_pnl, c.pnl, 0) AS TEXT),
			CAST(COALESCE(j.fees, 0) AS TEXT),
			CAST(COALESCE(j.realized_pnl + CASE
				WHEN COALESCE(j.fees, 0) > 0 THEN -COALESCE(j.fees, 0)
				ELSE COALESCE(j.fees, 0)
			END, c.pnl, 0) AS TEXT),
			c.hold_duration_seconds
		FROM scalping_cycle_telemetry c
		LEFT JOIN realized_pnl_journal j ON j.order_id = c.order_id
		WHERE c.cycle_at >= ? AND c.cycle_at <= ?
	`
	args := []any{filter.Since.UTC(), filter.Until.UTC()}
	if strings.TrimSpace(filter.ChatID) != "" {
		query += " AND COALESCE(c.chat_id, '') = ?"
		args = append(args, strings.TrimSpace(filter.ChatID))
	}
	if strings.TrimSpace(filter.Exchange) != "" {
		query += " AND COALESCE(c.exchange, '') = ?"
		args = append(args, strings.TrimSpace(filter.Exchange))
	}
	query += " ORDER BY c.cycle_at ASC, c.id ASC"

	resultRows, err := db.Query(ctx, store.bindQuery(query), args...)
	if err != nil {
		return nil, fmt.Errorf("query scalping soak report rows: %w", err)
	}
	defer resultRows.Close()

	rows := make([]scalpingSoakCycleRow, 0)
	for resultRows.Next() {
		var row scalpingSoakCycleRow
		var grossRaw string
		var feesRaw string
		var netRaw string
		if scanErr := resultRows.Scan(
			&row.action,
			&row.regime,
			&row.gateBlockCode,
			&row.rejectionCountsJSON,
			&row.bidAskSpreadPct,
			&row.orderBookImbalance,
			&row.rangePosition24h,
			&row.priceChange24hPct,
			&row.outcome,
			&grossRaw,
			&feesRaw,
			&netRaw,
			&row.holdDurationSeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("scan scalping soak report row: %w", scanErr)
		}
		row.grossPnL = decimalFromStringOrZero(grossRaw)
		row.fees = decimalFromStringOrZero(feesRaw)
		row.netPnL = decimalFromStringOrZero(netRaw)
		rows = append(rows, row)
	}
	if err := resultRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scalping soak report rows: %w", err)
	}
	return rows, nil
}

func (r *ScalpingSoakReport) addCycle(row scalpingSoakCycleRow) {
	r.TotalCycles++
	action := normalizeSoakBucket(row.action, "unknown")
	regime := normalizeSoakBucket(row.regime, "unknown")
	r.ActionBreakdown[action]++
	r.RegimeBreakdown[regime]++
	if strings.TrimSpace(row.gateBlockCode) != "" {
		r.GateBlockByCode[strings.TrimSpace(row.gateBlockCode)]++
	}
	r.addRejections(row.rejectionCountsJSON)
	r.addSignalQuality(row)
	r.addAIDegradation(row)
	r.addTrade(row)
}

func (r *ScalpingSoakReport) addRejections(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	counts := make(map[string]int)
	if err := json.Unmarshal([]byte(raw), &counts); err != nil {
		return
	}
	for reason, count := range counts {
		reason = strings.TrimSpace(reason)
		if reason == "" || count == 0 {
			continue
		}
		r.RejectionByReason[reason] += count
	}
}

func (r *ScalpingSoakReport) addSignalQuality(row scalpingSoakCycleRow) {
	if !row.bidAskSpreadPct.Valid &&
		!row.orderBookImbalance.Valid &&
		!row.rangePosition24h.Valid &&
		!row.priceChange24hPct.Valid {
		return
	}
	r.SignalQuality.KnownCycles++
	if row.bidAskSpreadPct.Valid {
		r.SignalQuality.AvgBidAskSpreadPct = r.SignalQuality.AvgBidAskSpreadPct.Add(decimal.NewFromFloat(row.bidAskSpreadPct.Float64))
		r.spreadCount++
	}
	if row.orderBookImbalance.Valid {
		value := decimal.NewFromFloat(row.orderBookImbalance.Float64)
		if value.LessThan(decimal.Zero) {
			value = value.Abs()
		}
		r.SignalQuality.AvgAbsOrderBookImbalance = r.SignalQuality.AvgAbsOrderBookImbalance.Add(value)
		r.imbalanceCount++
	}
	if row.rangePosition24h.Valid {
		r.SignalQuality.AvgRangePosition24h = r.SignalQuality.AvgRangePosition24h.Add(decimal.NewFromFloat(row.rangePosition24h.Float64))
		r.rangeCount++
	}
	if row.priceChange24hPct.Valid {
		r.SignalQuality.AvgPriceChange24hPct = r.SignalQuality.AvgPriceChange24hPct.Add(decimal.NewFromFloat(row.priceChange24hPct.Float64))
		r.priceChangeCount++
	}
}

func (r *ScalpingSoakReport) addTrade(row scalpingSoakCycleRow) {
	outcome := strings.ToLower(strings.TrimSpace(row.outcome))
	if outcome == "" {
		return
	}
	r.TradeSummary.ClosedTrades++
	switch {
	case row.netPnL.GreaterThan(decimal.Zero):
		r.TradeSummary.Wins++
	case row.netPnL.LessThan(decimal.Zero):
		r.TradeSummary.Losses++
	default:
		r.TradeSummary.Breakeven++
	}
	if row.netPnL.GreaterThan(decimal.Zero) {
		r.grossWinningPnL = r.grossWinningPnL.Add(row.netPnL)
	} else if row.netPnL.LessThan(decimal.Zero) {
		r.grossLosingPnL = r.grossLosingPnL.Add(row.netPnL.Abs())
	}
	if r.TradeSummary.ClosedTrades == 1 || row.netPnL.GreaterThan(r.TradeSummary.BestTradeNetPnL) {
		r.TradeSummary.BestTradeNetPnL = row.netPnL
	}
	if r.TradeSummary.ClosedTrades == 1 || row.netPnL.LessThan(r.TradeSummary.WorstTradeNetPnL) {
		r.TradeSummary.WorstTradeNetPnL = row.netPnL
	}
	r.TradeSummary.GrossPnL = r.TradeSummary.GrossPnL.Add(row.grossPnL)
	r.TradeSummary.NetPnL = r.TradeSummary.NetPnL.Add(row.netPnL)
	r.TradeSummary.Fees = r.TradeSummary.Fees.Add(row.fees)
	if row.holdDurationSeconds.Valid {
		r.TradeSummary.AvgHoldDurationSec = r.TradeSummary.AvgHoldDurationSec.Add(decimal.NewFromInt(row.holdDurationSeconds.Int64))
		r.holdDurationCount++
	}
	r.cumulativeNetPnL = r.cumulativeNetPnL.Add(row.netPnL)
	if r.TradeSummary.ClosedTrades == 1 || r.cumulativeNetPnL.GreaterThan(r.peakNetPnL) {
		r.peakNetPnL = r.cumulativeNetPnL
	}
	drawdown := r.peakNetPnL.Sub(r.cumulativeNetPnL)
	if drawdown.GreaterThan(r.TradeSummary.MaxDrawdown) {
		r.TradeSummary.MaxDrawdown = drawdown
	}
}

func (r *ScalpingSoakReport) addAIDegradation(row scalpingSoakCycleRow) {
	key, degraded := classifySoakAIDegradation(row)
	if !degraded {
		return
	}
	r.AIProviderDegradation.ByReason[key]++
	r.AIProviderDegradation.DegradedCycles++
}

func classifySoakAIDegradation(row scalpingSoakCycleRow) (string, bool) {
	combined := strings.ToLower(row.gateBlockCode + " " + row.rejectionCountsJSON)
	switch {
	case strings.Contains(combined, "ai_unavailable"):
		return "ai_unavailable", true
	case strings.Contains(combined, "llm"):
		return normalizeSoakBucket(row.gateBlockCode, "llm_degraded"), true
	case strings.Contains(combined, "provider"):
		return normalizeSoakBucket(row.gateBlockCode, "provider_degraded"), true
	case strings.Contains(combined, "runtime"):
		return normalizeSoakBucket(row.gateBlockCode, "runtime_degraded"), true
	default:
		return "", false
	}
}

func (r *ScalpingSoakReport) finalize(baseline *ScalpingSoakBaseline) {
	r.ActionSplit = ratioMap(r.ActionBreakdown, r.TotalCycles)
	r.RegimeSplit = ratioMap(r.RegimeBreakdown, r.TotalCycles)
	if r.TotalCycles > 0 {
		r.SignalQuality.Coverage = decimal.NewFromInt(int64(r.SignalQuality.KnownCycles)).
			Div(decimal.NewFromInt(int64(r.TotalCycles)))
		r.SignalQuality.MissingSignalQualityCycles = r.TotalCycles - r.SignalQuality.KnownCycles
	}
	if r.SignalQuality.KnownCycles > 0 {
		r.SignalQuality.AvgBidAskSpreadPct = divideByCount(r.SignalQuality.AvgBidAskSpreadPct, r.spreadCount)
		r.SignalQuality.AvgAbsOrderBookImbalance = divideByCount(r.SignalQuality.AvgAbsOrderBookImbalance, r.imbalanceCount)
		r.SignalQuality.AvgRangePosition24h = divideByCount(r.SignalQuality.AvgRangePosition24h, r.rangeCount)
		r.SignalQuality.AvgPriceChange24hPct = divideByCount(r.SignalQuality.AvgPriceChange24hPct, r.priceChangeCount)
	}
	if r.TradeSummary.ClosedTrades > 0 {
		denom := decimal.NewFromInt(int64(r.TradeSummary.ClosedTrades))
		r.TradeSummary.WinRate = decimal.NewFromInt(int64(r.TradeSummary.Wins)).Div(denom)
		r.TradeSummary.AvgNetPnLPerTrade = r.TradeSummary.NetPnL.Div(denom)
		r.TradeSummary.AvgHoldDurationSec = divideByCount(r.TradeSummary.AvgHoldDurationSec, r.holdDurationCount)
	}
	r.computeDrawdownPct(baseline)
	r.computeProfitFactor()
	r.InsufficientTradeProof = r.TradeSummary.ClosedTrades == 0
	if baseline != nil {
		r.BaselineComparison = compareScalpingSoakBaseline(*baseline, *r)
	}
}

func (r *ScalpingSoakReport) computeDrawdownPct(baseline *ScalpingSoakBaseline) {
	if baseline == nil || !baseline.BalanceUSDT.GreaterThan(decimal.Zero) {
		return
	}
	r.TradeSummary.MaxDrawdownPct = r.TradeSummary.MaxDrawdown.Div(baseline.BalanceUSDT)
}

func (r *ScalpingSoakReport) computeProfitFactor() {
	if r.TradeSummary.ClosedTrades == 0 || !r.grossWinningPnL.GreaterThan(decimal.Zero) {
		return
	}
	if r.grossLosingPnL.GreaterThan(decimal.Zero) {
		r.TradeSummary.ProfitFactor = r.grossWinningPnL.Div(r.grossLosingPnL)
		return
	}
	r.TradeSummary.ProfitFactor = r.grossWinningPnL
}

func compareScalpingSoakBaseline(baseline ScalpingSoakBaseline, report ScalpingSoakReport) *ScalpingSoakBaselineComparison {
	comparison := &ScalpingSoakBaselineComparison{
		BaselineName:        strings.TrimSpace(baseline.Name),
		DeltaClosedTrades:   report.TradeSummary.ClosedTrades - baseline.ClosedTrades,
		DeltaWinRate:        report.TradeSummary.WinRate.Sub(baseline.WinRate),
		DeltaNetPnL:         report.TradeSummary.NetPnL.Sub(baseline.NetPnL),
		DeltaFees:           report.TradeSummary.Fees.Sub(baseline.Fees),
		DeltaAvgPnLPerTrade: report.TradeSummary.AvgNetPnLPerTrade.Sub(baseline.AvgPnLPerTrade),
		DeltaCycles:         report.TotalCycles - baseline.TotalCycles,
	}
	if comparison.BaselineName == "" {
		comparison.BaselineName = "baseline"
	}
	return comparison
}

func ratioMap(counts map[string]int, total int) map[string]decimal.Decimal {
	ratios := make(map[string]decimal.Decimal, len(counts))
	if total <= 0 {
		return ratios
	}
	denom := decimal.NewFromInt(int64(total))
	for key, count := range counts {
		ratios[key] = decimal.NewFromInt(int64(count)).Div(denom)
	}
	return ratios
}

func divideByCount(value decimal.Decimal, count int) decimal.Decimal {
	if count <= 0 {
		return decimal.Zero
	}
	return value.Div(decimal.NewFromInt(int64(count)))
}

func normalizeSoakBucket(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func decimalFromStringOrZero(raw string) decimal.Decimal {
	value, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return decimal.Zero
	}
	return value
}

func mustSoakDecimal(raw string) decimal.Decimal {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		panic(err)
	}
	return value
}
