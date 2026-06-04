package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type DBPool = services.DBPool

type ScalpingBacktestHandler struct {
	db DBPool
}

func NewScalpingBacktestHandler(db any) *ScalpingBacktestHandler {
	return &ScalpingBacktestHandler{db: normalizeDBPool(db)}
}

type RunScalpingBacktestRequest struct {
	StartTime          string   `json:"start_time"`
	EndTime            string   `json:"end_time"`
	Symbols            []string `json:"symbols"`
	Exchange           string   `json:"exchange"`
	InitialCapital     string   `json:"initial_capital"`
	MaxBidAskSpreadPct *float64 `json:"max_bid_ask_spread_pct"`
	MinConfidence      *float64 `json:"min_confidence"`
	SpreadMultiplier   *float64 `json:"spread_multiplier"`
	FeeRate            *string  `json:"fee_rate"`
	NoisePct           *float64 `json:"noise_pct"`
	// Mode selects the decision pipeline: "deterministic" (default) or
	// "ai". See services.ScalpingBacktestConfig.Mode for semantics.
	Mode *string `json:"mode"`
}

type RunScalpingBacktestResponse struct {
	RunID       string                   `json:"run_id"`
	Status      string                   `json:"status"`
	Mode        string                   `json:"mode"`
	Summary     ScalpingBacktestSummary  `json:"summary"`
	Signals     []ScalpingBacktestSignal `json:"signals,omitempty"`
	Trades      []ScalpingBacktestTrade  `json:"trades,omitempty"`
	GateSummary []GateSummaryEntry       `json:"gate_summary"`
}

type GetScalpingBacktestResponse struct {
	RunID       string                  `json:"run_id"`
	Status      string                  `json:"status"`
	Config      ScalpingBacktestConfig  `json:"config"`
	Summary     ScalpingBacktestSummary `json:"summary"`
	CreatedAt   time.Time               `json:"created_at"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
}

type ScalpingBacktestConfig struct {
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	Symbols            []string  `json:"symbols,omitempty"`
	Exchange           string    `json:"exchange"`
	InitialCapital     string    `json:"initial_capital"`
	MaxBidAskSpreadPct float64   `json:"max_bid_ask_spread_pct"`
	MinConfidence      float64   `json:"min_confidence"`
	SpreadMultiplier   float64   `json:"spread_multiplier"`
	FeeRate            string    `json:"fee_rate"`
	Mode               string    `json:"mode"`
}

type ScalpingBacktestSummary struct {
	TotalSignals    int    `json:"total_signals"`
	AcceptedSignals int    `json:"accepted_signals"`
	RejectedSignals int    `json:"rejected_signals"`
	TotalTrades     int    `json:"total_trades"`
	WinningTrades   int    `json:"winning_trades"`
	LosingTrades    int    `json:"losing_trades"`
	WinRate         string `json:"win_rate"`
	TotalPnL        string `json:"total_pnl"`
	TotalPnLPct     string `json:"total_pnl_pct"`
	MaxDrawdownPct  string `json:"max_drawdown_pct"`
}

type ScalpingBacktestSignal struct {
	SignalID         string                 `json:"signal_id,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
	Symbol           string                 `json:"symbol"`
	Exchange         string                 `json:"exchange"`
	Regime           string                 `json:"regime"`
	RegimeVolatility string                 `json:"regime_volatility"`
	FunnelStage      string                 `json:"funnel_stage"`
	RejectionReason  string                 `json:"rejection_reason,omitempty"`
	Signal           map[string]interface{} `json:"signal,omitempty"`
	GateResults      map[string]interface{} `json:"gate_results,omitempty"`
	Hints            *services.SignalHints  `json:"hints,omitempty"`
}

type ScalpingBacktestTrade struct {
	TradeID             string    `json:"trade_id,omitempty"`
	SignalID            string    `json:"signal_id,omitempty"`
	Symbol              string    `json:"symbol"`
	Exchange            string    `json:"exchange"`
	Side                string    `json:"side"`
	Size                string    `json:"size"`
	Notional            string    `json:"notional"`
	EntryPrice          string    `json:"entry_price"`
	ExitPrice           string    `json:"exit_price"`
	EntryTimestamp      time.Time `json:"entry_timestamp"`
	ExitTimestamp       time.Time `json:"exit_timestamp"`
	PnL                 string    `json:"pnl"`
	PnLPct              string    `json:"pnl_pct"`
	Fees                string    `json:"fees"`
	Outcome             string    `json:"outcome"`
	ExitReason          string    `json:"exit_reason"`
	RegimeAtEntry       string    `json:"regime_at_entry"`
	RegimeAtExit        string    `json:"regime_at_exit"`
	HoldDurationSeconds int       `json:"hold_duration_seconds"`
}

type GateSummaryEntry struct {
	GateName            string         `json:"gate_name"`
	PassCount           int            `json:"pass_count"`
	RejectCount         int            `json:"reject_count"`
	TopRejectionReasons []string       `json:"top_rejection_reasons"`
	BreakdownBySymbol   map[string]int `json:"breakdown_by_symbol,omitempty"`
	BreakdownByRegime   map[string]int `json:"breakdown_by_regime,omitempty"`
}

type compareScalpingBacktestsRequest struct {
	RunIDs []string `json:"run_ids"`
}

type compareScalpingBacktestsResponse struct {
	Runs      []GetScalpingBacktestResponse `json:"runs"`
	BestRunID string                        `json:"best_run_id,omitempty"`
}

func (h *ScalpingBacktestHandler) RunScalpingBacktest(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database is not available"})
		return
	}

	var req RunScalpingBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	svcConfig, err := parseToServiceConfig(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	runID := uuid.NewString()
	now := time.Now().UTC()
	apiConfig := serviceConfigToAPI(svcConfig)
	emptySummary := ScalpingBacktestSummary{}
	if err := h.insertBacktestRun(c.Request.Context(), runID, "running", apiConfig, emptySummary, now, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize backtest run"})
		return
	}

	engine := services.NewScalpingBacktestEngine(h.db, svcConfig)
	result, err := engine.Run(c.Request.Context())
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.updateBacktestRun(cleanupCtx, runID, "failed", emptySummary, time.Now().UTC())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to run scalping backtest"})
		return
	}

	apiResult := serviceResultToAPI(result)
	if err := h.persistBacktestResult(c.Request.Context(), runID, apiConfig, apiResult); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.updateBacktestRun(cleanupCtx, runID, "failed", emptySummary, time.Now().UTC())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist scalping backtest results"})
		return
	}

	c.JSON(http.StatusOK, RunScalpingBacktestResponse{
		RunID:       runID,
		Status:      "completed",
		Mode:        apiResult.Mode,
		Summary:     apiResult.Summary,
		Signals:     apiResult.Signals,
		Trades:      apiResult.Trades,
		GateSummary: apiResult.GateSummary,
	})
}

func (h *ScalpingBacktestHandler) GetScalpingBacktest(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database is not available"})
		return
	}
	runID := strings.TrimSpace(c.Param("run_id"))
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run_id is required"})
		return
	}

	run, err := h.fetchBacktestRun(c.Request.Context(), runID)
	if err != nil {
		if isNoRowsBacktestError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "backtest run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load backtest run"})
		return
	}

	c.JSON(http.StatusOK, run)
}

func (h *ScalpingBacktestHandler) ListScalpingBacktests(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database is not available"})
		return
	}
	limit := 20
	if raw := strings.TrimSpace(c.DefaultQuery("limit", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	statusFilter := strings.TrimSpace(strings.ToLower(c.Query("status")))
	query := `
		SELECT id, status, config, summary, created_at, completed_at
		FROM scalping_backtest_runs
	`
	args := make([]any, 0, 1)
	if statusFilter != "" {
		query += " WHERE status = $1"
		args = append(args, statusFilter)
	}
	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	rows, err := h.db.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list backtest runs"})
		return
	}
	defer rows.Close()

	runs := make([]GetScalpingBacktestResponse, 0, limit)
	for rows.Next() {
		var (
			id            string
			status        string
			configRaw     []byte
			summaryRaw    []byte
			createdAt     time.Time
			completedAtNS sql.NullTime
		)
		if err := rows.Scan(&id, &status, &configRaw, &summaryRaw, &createdAt, &completedAtNS); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan backtest run"})
			return
		}
		run, err := decodeScalpingBacktestRow(id, status, configRaw, summaryRaw, createdAt, completedAtNS)
		if err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode backtest run"})
			return
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to iterate backtest runs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func (h *ScalpingBacktestHandler) CompareScalpingBacktests(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database is not available"})
		return
	}
	var req compareScalpingBacktestsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if len(req.RunIDs) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least 2 run_ids are required"})
		return
	}
	if len(req.RunIDs) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "maximum 20 run_ids are allowed"})
		return
	}

	seen := make(map[string]bool, len(req.RunIDs))
	pl := make([]string, 0, len(req.RunIDs))
	args := make([]any, 0, len(req.RunIDs))
	for _, id := range req.RunIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "run_ids must not be empty"})
			return
		}
		if seen[id] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "duplicate run_id: " + id})
			return
		}
		seen[id] = true
		pl = append(pl, fmt.Sprintf("$%d", len(args)+1))
		args = append(args, id)
	}
	if len(pl) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least 2 distinct run_ids are required"})
		return
	}

	query := `
		SELECT id, status, config, summary, created_at, completed_at
		FROM scalping_backtest_runs
		WHERE id IN (` + strings.Join(pl, ",") + `)
	`
	rows, err := h.db.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compare backtest runs"})
		return
	}
	defer rows.Close()

	runs := make([]GetScalpingBacktestResponse, 0, len(req.RunIDs))
	bestRunID := ""
	bestPnL := decimal.Decimal{}
	firstBestSet := false

	for rows.Next() {
		var (
			id            string
			status        string
			configRaw     []byte
			summaryRaw    []byte
			createdAt     time.Time
			completedAtNS sql.NullTime
		)
		if err := rows.Scan(&id, &status, &configRaw, &summaryRaw, &createdAt, &completedAtNS); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan backtest run for comparison"})
			return
		}
		run, err := decodeScalpingBacktestRow(id, status, configRaw, summaryRaw, createdAt, completedAtNS)
		if err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode backtest run for comparison"})
			return
		}
		runs = append(runs, run)
		if pnl, parseErr := decimal.NewFromString(run.Summary.TotalPnL); parseErr == nil {
			if !firstBestSet || pnl.GreaterThan(bestPnL) {
				bestPnL = pnl
				bestRunID = id
				firstBestSet = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to iterate backtest runs for comparison"})
		return
	}

	c.JSON(http.StatusOK, compareScalpingBacktestsResponse{Runs: runs, BestRunID: bestRunID})
}

func parseToServiceConfig(req RunScalpingBacktestRequest) (services.ScalpingBacktestConfig, error) {
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartTime))
	if err != nil {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("start_time must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndTime))
	if err != nil {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("end_time must be RFC3339")
	}
	if !start.Before(end) {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("start_time must be before end_time")
	}
	if start.AddDate(5, 0, 0).Before(end) {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("date range must not exceed 5 years")
	}

	initialCapitalRaw := strings.TrimSpace(req.InitialCapital)
	if initialCapitalRaw == "" {
		initialCapitalRaw = "10000"
	}
	initialCapital, err := decimal.NewFromString(initialCapitalRaw)
	if err != nil {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("initial_capital must be a valid decimal string")
	}
	if !initialCapital.GreaterThan(decimal.Zero) {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("initial_capital must be greater than zero")
	}

	exchange := strings.TrimSpace(strings.ToLower(req.Exchange))
	if exchange == "" {
		exchange = "bitget"
	}

	maxBidAskSpreadPct := 0.08
	if req.MaxBidAskSpreadPct != nil {
		maxBidAskSpreadPct = *req.MaxBidAskSpreadPct
	}
	if maxBidAskSpreadPct <= 0 {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("max_bid_ask_spread_pct must be greater than zero")
	}

	minConfidence := 0.60
	if req.MinConfidence != nil {
		minConfidence = *req.MinConfidence
	}
	if minConfidence < 0 || minConfidence > 1 {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("min_confidence must be between 0 and 1")
	}

	spreadMultiplier := float64(services.DefaultScalpingBacktestSpreadMultiplier)
	if req.SpreadMultiplier != nil {
		spreadMultiplier = *req.SpreadMultiplier
	}
	if spreadMultiplier <= 0 {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("spread_multiplier must be greater than zero")
	}

	feeRate := "0.001"
	if req.FeeRate != nil && strings.TrimSpace(*req.FeeRate) != "" {
		feeRate = strings.TrimSpace(*req.FeeRate)
	}
	feeRateDecimal, err := decimal.NewFromString(feeRate)
	if err != nil {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("fee_rate must be a valid decimal string")
	}
	if feeRateDecimal.IsNegative() {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("fee_rate must be non-negative")
	}

	// resolveSymbols picks the effective backtest universe using precedence:
	//   1. Request-provided symbols (normalized, de-duped, upper-cased).
	//   2. NEURATRADE_BACKTEST_SYMBOLS env var (comma-separated).
	//   3. Service default (when both are absent or empty after normalization).
	//
	// Normalization runs first on each candidate so a whitespace-only request
	// (e.g. []string{"", "  "}) correctly falls through to the env var instead
	// of silently short-circuiting the lookup.
	resolveSymbols := func(candidates []string) []string {
		return normalizeSymbols(candidates)
	}

	var symbols []string
	switch {
	case len(resolveSymbols(req.Symbols)) > 0:
		symbols = resolveSymbols(req.Symbols)
	default:
		if envSymbols := os.Getenv("NEURATRADE_BACKTEST_SYMBOLS"); envSymbols != "" {
			rawSymbols := strings.Split(envSymbols, ",")
			symbols = resolveSymbols(rawSymbols)
		}
	}

	noisePct := services.DefaultScalpingBacktestNoise
	if req.NoisePct != nil {
		noisePct = *req.NoisePct
	}
	if noisePct < 0 || noisePct > 1.0 {
		return services.ScalpingBacktestConfig{}, fmt.Errorf("noise_pct must be between 0 and 1.0")
	}

	mode := "deterministic"
	if req.Mode != nil {
		candidate := strings.ToLower(strings.TrimSpace(*req.Mode))
		if candidate == "" {
			mode = "deterministic"
		} else {
			mode = candidate
		}
	}

	return services.ScalpingBacktestConfig{
		StartTime:          start.UTC(),
		EndTime:            end.UTC(),
		Symbols:            symbols,
		Exchange:           exchange,
		InitialCapital:     initialCapital,
		MaxBidAskSpreadPct: maxBidAskSpreadPct,
		MinConfidence:      minConfidence,
		SpreadMultiplier:   spreadMultiplier,
		FeeRate:            feeRateDecimal,
		SlippagePct:        decimal.NewFromFloat(services.DefaultScalpingBacktestSlippage),
		NoisePct:           noisePct,
		MaxCapitalPct:      services.DefaultScalpingBacktestMaxCapitalPct,
		DefaultHoldPeriod:  services.DefaultScalpingBacktestHoldPeriod,
		Mode:               mode,
	}, nil
}

func serviceConfigToAPI(cfg services.ScalpingBacktestConfig) ScalpingBacktestConfig {
	return ScalpingBacktestConfig{
		StartTime:          cfg.StartTime,
		EndTime:            cfg.EndTime,
		Symbols:            cfg.Symbols,
		Exchange:           cfg.Exchange,
		InitialCapital:     cfg.InitialCapital.String(),
		MaxBidAskSpreadPct: cfg.MaxBidAskSpreadPct,
		MinConfidence:      cfg.MinConfidence,
		SpreadMultiplier:   cfg.SpreadMultiplier,
		FeeRate:            cfg.FeeRate.String(),
		Mode:               cfg.Mode,
	}
}

type apiBacktestResult struct {
	Mode        string
	Summary     ScalpingBacktestSummary
	Signals     []ScalpingBacktestSignal
	Trades      []ScalpingBacktestTrade
	GateSummary []GateSummaryEntry
}

func serviceResultToAPI(result *services.ScalpingBacktestResult) apiBacktestResult {
	summary := serviceSummaryToAPI(result.Summary)

	type signalLookupKey struct {
		symbol   string
		exchange string
		time     time.Time
	}
	tradeLookup := make(map[signalLookupKey]string)
	signals := make([]ScalpingBacktestSignal, 0, len(result.Signals))
	for _, s := range result.Signals {
		id := uuid.NewString()
		lookupKey := signalLookupKey{s.Symbol, s.Exchange, s.Timestamp}
		if _, exists := tradeLookup[lookupKey]; !exists {
			tradeLookup[lookupKey] = id
		}
		signals = append(signals, ScalpingBacktestSignal{
			SignalID:         id,
			Timestamp:        s.Timestamp,
			Symbol:           s.Symbol,
			Exchange:         s.Exchange,
			Regime:           s.Regime,
			RegimeVolatility: s.RegimeVolatility,
			FunnelStage:      s.FunnelStage,
			RejectionReason:  s.RejectionReason,
			GateResults:      boolMapToInterfaceMap(s.GateResults),
			Hints:            s.Hints,
			Signal: map[string]interface{}{
				"symbol":    s.Symbol,
				"timestamp": s.Timestamp.Format(time.RFC3339),
			},
		})
	}

	trades := make([]ScalpingBacktestTrade, 0, len(result.Trades))
	for _, t := range result.Trades {
		holdDuration := int(t.ExitTime.Sub(t.EntryTime).Seconds())
		trade := ScalpingBacktestTrade{
			SignalID:            tradeLookup[signalLookupKey{t.Symbol, t.Exchange, t.EntryTime}],
			Symbol:              t.Symbol,
			Exchange:            t.Exchange,
			Side:                t.Side,
			Size:                t.Size.String(),
			Notional:            t.Notional.String(),
			EntryPrice:          t.EntryPrice.String(),
			ExitPrice:           t.ExitPrice.String(),
			EntryTimestamp:      t.EntryTime,
			ExitTimestamp:       t.ExitTime,
			PnL:                 t.PnL.String(),
			PnLPct:              t.PnLPct.String(),
			Fees:                t.Fees.String(),
			Outcome:             t.Outcome,
			ExitReason:          t.ExitReason,
			RegimeAtEntry:       t.RegimeAtEntry,
			RegimeAtExit:        t.RegimeAtExit,
			HoldDurationSeconds: holdDuration,
		}
		trades = append(trades, trade)
	}

	gateSummary := make([]GateSummaryEntry, 0, len(result.GateSummary))
	for _, g := range result.GateSummary {
		reasons := make([]string, 0, len(g.TopRejectionReasons))
		for _, r := range g.TopRejectionReasons {
			reasons = append(reasons, r.Reason)
		}
		gateSummary = append(gateSummary, GateSummaryEntry{
			GateName:            g.GateName,
			PassCount:           g.PassCount,
			RejectCount:         g.RejectCount,
			TopRejectionReasons: reasons,
			BreakdownBySymbol:   g.BreakdownBySymbol,
			BreakdownByRegime:   g.BreakdownByRegime,
		})
	}

	return apiBacktestResult{
		Mode:        result.Mode,
		Summary:     summary,
		Signals:     signals,
		Trades:      trades,
		GateSummary: gateSummary,
	}
}

// serviceSummaryToAPI converts a services.ScalpingBacktestSummary into an API-ready ScalpingBacktestSummary.
//
// Numeric and decimal metrics are formatted as strings: win rate with two decimal places, total PnL as a plain decimal
// string, total PnL percentage and max drawdown percentage with four decimal places. Zero-valued decimals are represented
// by sensible string defaults ("0.00", "0", or "0.0000") while integer counters are copied directly.
func serviceSummaryToAPI(s services.ScalpingBacktestSummary) ScalpingBacktestSummary {
	winRate := "0.00"
	if !s.WinRate.IsZero() {
		winRate = s.WinRate.StringFixed(2)
	}
	totalPnL := "0"
	if !s.TotalPnL.IsZero() {
		totalPnL = s.TotalPnL.String()
	}
	totalPnLPct := "0.0000"
	if !s.TotalReturnPct.IsZero() {
		totalPnLPct = s.TotalReturnPct.StringFixed(4)
	}
	maxDD := "0.0000"
	if !s.MaxDrawdownPct.IsZero() {
		maxDD = s.MaxDrawdownPct.StringFixed(4)
	}

	return ScalpingBacktestSummary{
		TotalSignals:    s.TotalSignals,
		AcceptedSignals: s.EligibleSignals,
		RejectedSignals: s.RejectedSignals,
		TotalTrades:     s.TotalTrades,
		WinningTrades:   s.WinningTrades,
		LosingTrades:    s.LosingTrades,
		WinRate:         winRate,
		TotalPnL:        totalPnL,
		TotalPnLPct:     totalPnLPct,
		MaxDrawdownPct:  maxDD,
	}
}

func boolMapToInterfaceMap(m map[string]bool) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = map[string]interface{}{"passed": v}
	}
	return result
}

func (h *ScalpingBacktestHandler) insertBacktestRun(ctx context.Context, runID, status string, config ScalpingBacktestConfig, summary ScalpingBacktestSummary, createdAt time.Time, completedAt *time.Time) error {
	configRaw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal backtest config for insert: %w", err)
	}
	summaryRaw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal backtest summary for insert: %w", err)
	}
	query := `
		INSERT INTO scalping_backtest_runs (id, created_at, completed_at, config, status, summary)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = h.db.Exec(ctx, query, runID, createdAt, completedAt, configRaw, status, summaryRaw)
	if err != nil {
		return fmt.Errorf("insert backtest run %s: %w", runID, err)
	}
	return nil
}

func (h *ScalpingBacktestHandler) updateBacktestRun(ctx context.Context, runID, status string, summary ScalpingBacktestSummary, completedAt time.Time) error {
	summaryRaw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal backtest summary for update: %w", err)
	}
	_, err = h.db.Exec(ctx, `
		UPDATE scalping_backtest_runs
		SET status = $2,
		    completed_at = $3,
		    summary = $4
		WHERE id = $1
	`, runID, status, completedAt, summaryRaw)
	if err != nil {
		return fmt.Errorf("update backtest run %s: %w", runID, err)
	}
	return nil
}

func (h *ScalpingBacktestHandler) persistBacktestResult(ctx context.Context, runID string, config ScalpingBacktestConfig, result apiBacktestResult) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction for run %s: %w", runID, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	completedAt := time.Now().UTC()
	summaryRaw, err := json.Marshal(result.Summary)
	if err != nil {
		return fmt.Errorf("marshal backtest summary for persist: %w", err)
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal backtest config for persist: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scalping_backtest_runs
		SET status = 'completed',
		    completed_at = $2,
		    config = $3,
		    summary = $4
		WHERE id = $1
	`, runID, completedAt, configRaw, summaryRaw); err != nil {
		return fmt.Errorf("update run %s status in transaction: %w", runID, err)
	}

	for _, signal := range result.Signals {
		signalID := signal.SignalID
		if signalID == "" {
			signalID = uuid.NewString()
		}
		signalRaw, err := json.Marshal(signal.Signal)
		if err != nil {
			return fmt.Errorf("marshal signal %s: %w", signalID, err)
		}
		gateRaw, err := json.Marshal(signal.GateResults)
		if err != nil {
			return fmt.Errorf("marshal gate results for signal %s: %w", signalID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_signals (
				id, run_id, timestamp, symbol, exchange, signal, regime, regime_volatility,
				funnel_stage, rejection_reason, gate_results
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, signalID, runID, signal.Timestamp, signal.Symbol, signal.Exchange, signalRaw, signal.Regime, signal.RegimeVolatility, signal.FunnelStage, nullString(signal.RejectionReason), gateRaw); err != nil {
			return fmt.Errorf("insert signal %s for run %s: %w", signalID, runID, err)
		}
	}

	for _, trade := range result.Trades {
		tradeID := trade.TradeID
		if tradeID == "" {
			tradeID = uuid.NewString()
		}
		signalID := strings.TrimSpace(trade.SignalID)
		if signalID == "" {
			return fmt.Errorf("trade %s (symbol=%s, entry=%s) has no linked signal_id; run cannot be persisted with partial data",
				tradeID, trade.Symbol, trade.EntryTimestamp.Format(time.RFC3339))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_trades (
				id, run_id, signal_id, symbol, exchange, side, size, notional,
				entry_price, exit_price, entry_timestamp, exit_timestamp,
				pnl, pnl_pct, fees, outcome, exit_reason,
				regime_at_entry, regime_at_exit, hold_duration_seconds
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12,
				$13, $14, $15, $16, $17,
				$18, $19, $20
			)
		`,
			tradeID,
			runID,
			signalID,
			trade.Symbol,
			trade.Exchange,
			trade.Side,
			trade.Size,
			trade.Notional,
			trade.EntryPrice,
			trade.ExitPrice,
			trade.EntryTimestamp,
			trade.ExitTimestamp,
			trade.PnL,
			trade.PnLPct,
			trade.Fees,
			trade.Outcome,
			trade.ExitReason,
			trade.RegimeAtEntry,
			trade.RegimeAtExit,
			trade.HoldDurationSeconds,
		); err != nil {
			return fmt.Errorf("insert trade %s for run %s: %w", tradeID, runID, err)
		}
	}

	for _, gate := range result.GateSummary {
		reasonsRaw, err := json.Marshal(gate.TopRejectionReasons)
		if err != nil {
			return fmt.Errorf("marshal top rejection reasons for gate %s: %w", gate.GateName, err)
		}
		bySymbolRaw, err := json.Marshal(gate.BreakdownBySymbol)
		if err != nil {
			return fmt.Errorf("marshal symbol breakdown for gate %s: %w", gate.GateName, err)
		}
		byRegimeRaw, err := json.Marshal(gate.BreakdownByRegime)
		if err != nil {
			return fmt.Errorf("marshal regime breakdown for gate %s: %w", gate.GateName, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_gate_summary (
				id, run_id, gate_name, pass_count, reject_count,
				top_rejection_reasons, breakdown_by_symbol, breakdown_by_regime
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, uuid.NewString(), runID, gate.GateName, gate.PassCount, gate.RejectCount, reasonsRaw, bySymbolRaw, byRegimeRaw); err != nil {
			return fmt.Errorf("insert gate summary %s for run %s: %w", gate.GateName, runID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction for run %s: %w", runID, err)
	}
	return nil
}

func (h *ScalpingBacktestHandler) fetchBacktestRun(ctx context.Context, runID string) (GetScalpingBacktestResponse, error) {
	var (
		status        string
		configRaw     []byte
		summaryRaw    []byte
		createdAt     time.Time
		completedAtNS sql.NullTime
	)
	err := h.db.QueryRow(ctx, `
		SELECT status, config, summary, created_at, completed_at
		FROM scalping_backtest_runs
		WHERE id = $1
	`, runID).Scan(&status, &configRaw, &summaryRaw, &createdAt, &completedAtNS)
	if err != nil {
		return GetScalpingBacktestResponse{}, err
	}
	return decodeScalpingBacktestRow(runID, status, configRaw, summaryRaw, createdAt, completedAtNS)
}

// decodeScalpingBacktestRow decodes raw JSON config and summary bytes from a scalping backtest row and builds a GetScalpingBacktestResponse.
//
// If configRaw or summaryRaw contain valid JSON they are unmarshaled into the corresponding API types; invalid JSON yields an error that wraps the run ID.
// The `completedAtNS` sql.NullTime is converted to a *time.Time when valid, otherwise `CompletedAt` is nil. The returned response contains the provided run ID, status, created timestamp, and the decoded config and summary.
func decodeScalpingBacktestRow(runID, status string, configRaw, summaryRaw []byte, createdAt time.Time, completedAtNS sql.NullTime) (GetScalpingBacktestResponse, error) {
	var config ScalpingBacktestConfig
	if len(configRaw) > 0 {
		if err := json.Unmarshal(configRaw, &config); err != nil {
			return GetScalpingBacktestResponse{}, fmt.Errorf("unmarshal backtest config for run %s: %w", runID, err)
		}
		config = normalizeDecodedScalpingBacktestConfig(config)
	}
	var summary ScalpingBacktestSummary
	if len(summaryRaw) > 0 {
		if err := json.Unmarshal(summaryRaw, &summary); err != nil {
			return GetScalpingBacktestResponse{}, fmt.Errorf("unmarshal backtest summary for run %s: %w", runID, err)
		}
	}

	var completedAt *time.Time
	if completedAtNS.Valid {
		completedAt = &completedAtNS.Time
	}
	return GetScalpingBacktestResponse{
		RunID:       runID,
		Status:      status,
		Config:      config,
		Summary:     summary,
		CreatedAt:   createdAt,
		CompletedAt: completedAt,
	}, nil
}

func normalizeDecodedScalpingBacktestConfig(config ScalpingBacktestConfig) ScalpingBacktestConfig {
	if config.SpreadMultiplier <= 0 {
		config.SpreadMultiplier = float64(services.DefaultScalpingBacktestSpreadMultiplier)
	}
	return config
}

func isNoRowsBacktestError(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows)
}

func nullString(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

// normalizeSymbols trims, upper-cases, and de-duplicates a symbol list,
// discarding empty entries. The returned slice is in first-seen order so
// downstream logs and backtest runs are deterministic.
func normalizeSymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(symbols))
	normalized := make([]string, 0, len(symbols))
	for _, s := range symbols {
		symbol := strings.TrimSpace(strings.ToUpper(s))
		if symbol == "" {
			continue
		}
		if _, exists := seen[symbol]; exists {
			continue
		}
		seen[symbol] = struct{}{}
		normalized = append(normalized, symbol)
	}
	return normalized
}
