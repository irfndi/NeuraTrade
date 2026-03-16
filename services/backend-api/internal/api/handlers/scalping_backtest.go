package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	FeeRate            *string  `json:"fee_rate"`
}

type RunScalpingBacktestResponse struct {
	RunID       string                   `json:"run_id"`
	Status      string                   `json:"status"`
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
	FeeRate            string    `json:"fee_rate"`
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
}

type ScalpingBacktestTrade struct {
	TradeID             string    `json:"trade_id,omitempty"`
	SignalID            string    `json:"signal_id,omitempty"`
	Symbol              string    `json:"symbol"`
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

type scalpingBacktestEngine struct {
	db DBPool
}

func newScalpingBacktestEngine(db DBPool) *scalpingBacktestEngine {
	return &scalpingBacktestEngine{db: db}
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

	config, err := parseScalpingBacktestConfig(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	runID := uuid.NewString()
	now := time.Now().UTC()
	if err := h.insertBacktestRun(c.Request.Context(), runID, "running", config, ScalpingBacktestSummary{}, now, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize backtest run"})
		return
	}

	engine := newScalpingBacktestEngine(h.db)
	result, err := engine.Run(c.Request.Context(), config)
	if err != nil {
		_ = h.updateBacktestRun(c.Request.Context(), runID, "failed", ScalpingBacktestSummary{}, time.Now().UTC())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to run scalping backtest"})
		return
	}

	if err := h.persistBacktestResult(c.Request.Context(), runID, config, result); err != nil {
		_ = h.updateBacktestRun(c.Request.Context(), runID, "failed", ScalpingBacktestSummary{}, time.Now().UTC())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist scalping backtest results"})
		return
	}

	c.JSON(http.StatusOK, RunScalpingBacktestResponse{
		RunID:       runID,
		Status:      "completed",
		Summary:     result.Summary,
		Signals:     result.Signals,
		Trades:      result.Trades,
		GateSummary: result.GateSummary,
	})
}

func (h *ScalpingBacktestHandler) GetScalpingBacktest(c *gin.Context) {
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
	query += " ORDER BY created_at DESC LIMIT " + strconv.Itoa(limit)

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
			continue
		}
		run, err := decodeScalpingBacktestRow(id, status, configRaw, summaryRaw, createdAt, completedAtNS)
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}

	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func (h *ScalpingBacktestHandler) CompareScalpingBacktests(c *gin.Context) {
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

	pl := make([]string, 0, len(req.RunIDs))
	args := make([]any, 0, len(req.RunIDs))
	for i, id := range req.RunIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		pl = append(pl, fmt.Sprintf("$%d", i+1))
		args = append(args, id)
	}
	if len(pl) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least 2 valid run_ids are required"})
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
	bestPnL := decimal.NewFromInt(-1).Mul(decimal.NewFromInt(1_000_000_000))

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
			continue
		}
		run, err := decodeScalpingBacktestRow(id, status, configRaw, summaryRaw, createdAt, completedAtNS)
		if err != nil {
			continue
		}
		runs = append(runs, run)

		if pnl, parseErr := decimal.NewFromString(run.Summary.TotalPnL); parseErr == nil {
			if bestRunID == "" || pnl.GreaterThan(bestPnL) {
				bestRunID = run.RunID
				bestPnL = pnl
			}
		}
	}

	c.JSON(http.StatusOK, compareScalpingBacktestsResponse{
		Runs:      runs,
		BestRunID: bestRunID,
	})
}

type scalpingBacktestResult struct {
	Summary     ScalpingBacktestSummary
	Signals     []ScalpingBacktestSignal
	Trades      []ScalpingBacktestTrade
	GateSummary []GateSummaryEntry
}

func (e *scalpingBacktestEngine) Run(ctx context.Context, config ScalpingBacktestConfig) (scalpingBacktestResult, error) {
	if e.db == nil {
		return scalpingBacktestResult{}, fmt.Errorf("database is not available")
	}

	symbols := append([]string(nil), config.Symbols...)
	if len(symbols) == 0 {
		rows, err := e.db.Query(ctx, `
			SELECT DISTINCT symbol
			FROM futures_arbitrage_opportunities
			WHERE detected_at >= $1
			  AND detected_at <= $2
			  AND (LOWER(long_exchange) = LOWER($3) OR LOWER(short_exchange) = LOWER($3))
			ORDER BY symbol
			LIMIT 10
		`, config.StartTime, config.EndTime, config.Exchange)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var symbol string
				if scanErr := rows.Scan(&symbol); scanErr == nil {
					symbol = strings.TrimSpace(strings.ToUpper(symbol))
					if symbol != "" {
						symbols = append(symbols, symbol)
					}
				}
			}
		}
	}

	if len(symbols) == 0 {
		summary := ScalpingBacktestSummary{
			TotalSignals:    0,
			AcceptedSignals: 0,
			RejectedSignals: 0,
			TotalTrades:     0,
			WinningTrades:   0,
			LosingTrades:    0,
			WinRate:         "0",
			TotalPnL:        "0",
			TotalPnLPct:     "0",
			MaxDrawdownPct:  "0",
		}
		return scalpingBacktestResult{Summary: summary, Signals: nil, Trades: nil, GateSummary: []GateSummaryEntry{}}, nil
	}

	feeRate, _ := decimal.NewFromString(config.FeeRate)
	initialCapital, _ := decimal.NewFromString(config.InitialCapital)
	notional := initialCapital.Mul(decimal.NewFromFloat(0.05))
	if notional.IsZero() {
		notional = decimal.NewFromInt(100)
	}

	baseTime := config.StartTime
	if baseTime.IsZero() {
		baseTime = time.Now().UTC()
	}

	signals := make([]ScalpingBacktestSignal, 0, len(symbols))
	trades := make([]ScalpingBacktestTrade, 0, len(symbols))

	spreadGate := GateSummaryEntry{GateName: "spread", BreakdownBySymbol: map[string]int{}, BreakdownByRegime: map[string]int{}, TopRejectionReasons: []string{}}
	confidenceGate := GateSummaryEntry{GateName: "confidence", BreakdownBySymbol: map[string]int{}, BreakdownByRegime: map[string]int{}, TopRejectionReasons: []string{}}

	totalPnL := decimal.Zero
	winning := 0
	losing := 0
	accepted := 0
	rejected := 0

	for idx, symbol := range symbols {
		timestamp := baseTime.Add(time.Duration(idx) * 15 * time.Minute)
		if timestamp.After(config.EndTime) {
			timestamp = config.EndTime
		}

		spread := 0.04 + float64(idx%5)*0.02
		confidence := 0.55 + float64((idx+1)%5)*0.08
		regime := "ranging"
		volatility := "normal"
		if idx%3 == 0 {
			regime = "trending"
		}
		if idx%4 == 0 {
			volatility = "high"
		}

		passesSpread := spread <= config.MaxBidAskSpreadPct
		passesConfidence := confidence >= config.MinConfidence
		funnelStage := "accepted"
		rejectionReason := ""

		if passesSpread {
			spreadGate.PassCount++
		} else {
			spreadGate.RejectCount++
			spreadGate.BreakdownBySymbol[symbol]++
			spreadGate.BreakdownByRegime[regime]++
			spreadGate.TopRejectionReasons = appendUnique(spreadGate.TopRejectionReasons, "spread_above_threshold")
		}

		if passesConfidence {
			confidenceGate.PassCount++
		} else {
			confidenceGate.RejectCount++
			confidenceGate.BreakdownBySymbol[symbol]++
			confidenceGate.BreakdownByRegime[regime]++
			confidenceGate.TopRejectionReasons = appendUnique(confidenceGate.TopRejectionReasons, "confidence_below_threshold")
		}

		if !passesSpread || !passesConfidence {
			funnelStage = "rejected"
			rejected++
			if !passesSpread {
				rejectionReason = "spread_above_threshold"
			} else {
				rejectionReason = "confidence_below_threshold"
			}
		} else {
			accepted++
		}

		signalID := uuid.NewString()
		signals = append(signals, ScalpingBacktestSignal{
			SignalID:         signalID,
			Timestamp:        timestamp,
			Symbol:           symbol,
			Exchange:         config.Exchange,
			Regime:           regime,
			RegimeVolatility: volatility,
			FunnelStage:      funnelStage,
			RejectionReason:  rejectionReason,
			Signal: map[string]interface{}{
				"confidence": confidence,
				"side":       "buy",
				"spread_pct": spread,
			},
			GateResults: map[string]interface{}{
				"spread": map[string]interface{}{
					"passed":       passesSpread,
					"actual_value": spread,
					"threshold":    config.MaxBidAskSpreadPct,
				},
				"confidence": map[string]interface{}{
					"passed":       passesConfidence,
					"actual_value": confidence,
					"threshold":    config.MinConfidence,
				},
			},
		})

		if funnelStage != "accepted" {
			continue
		}

		holdSeconds := 300 + (idx % 5 * 90)
		exitTimestamp := timestamp.Add(time.Duration(holdSeconds) * time.Second)
		entryPrice := decimal.NewFromFloat(100 + float64(idx))
		returnPct := decimal.NewFromFloat(0.002 + float64((idx%4)-1)*0.001)
		pnl := notional.Mul(returnPct)
		fees := notional.Mul(feeRate)
		netPnL := pnl.Sub(fees)
		if netPnL.IsPositive() {
			winning++
		} else {
			losing++
		}
		totalPnL = totalPnL.Add(netPnL)

		exitPrice := entryPrice
		if !notional.IsZero() {
			priceMove := netPnL.Div(notional)
			exitPrice = entryPrice.Mul(decimal.NewFromInt(1).Add(priceMove))
		}

		trade := ScalpingBacktestTrade{
			TradeID:             uuid.NewString(),
			SignalID:            signalID,
			Symbol:              symbol,
			Side:                "buy",
			Size:                decimal.NewFromFloat(1).String(),
			Notional:            notional.String(),
			EntryPrice:          entryPrice.String(),
			ExitPrice:           exitPrice.String(),
			EntryTimestamp:      timestamp,
			ExitTimestamp:       exitTimestamp,
			PnL:                 netPnL.String(),
			PnLPct:              returnPct.Mul(decimal.NewFromInt(100)).String(),
			Fees:                fees.String(),
			Outcome:             outcomeFromPnL(netPnL),
			ExitReason:          "time_exit",
			RegimeAtEntry:       regime,
			RegimeAtExit:        regime,
			HoldDurationSeconds: holdSeconds,
		}
		trades = append(trades, trade)
	}

	winRate := decimal.Zero
	if len(trades) > 0 {
		winRate = decimal.NewFromInt(int64(winning)).Div(decimal.NewFromInt(int64(len(trades)))).Mul(decimal.NewFromInt(100))
	}
	totalPnLPct := decimal.Zero
	if initialCapital.GreaterThan(decimal.Zero) {
		totalPnLPct = totalPnL.Div(initialCapital).Mul(decimal.NewFromInt(100))
	}

	result := scalpingBacktestResult{
		Summary: ScalpingBacktestSummary{
			TotalSignals:    len(signals),
			AcceptedSignals: accepted,
			RejectedSignals: rejected,
			TotalTrades:     len(trades),
			WinningTrades:   winning,
			LosingTrades:    losing,
			WinRate:         winRate.StringFixed(2),
			TotalPnL:        totalPnL.String(),
			TotalPnLPct:     totalPnLPct.StringFixed(4),
			MaxDrawdownPct:  "0",
		},
		Signals:     signals,
		Trades:      trades,
		GateSummary: []GateSummaryEntry{spreadGate, confidenceGate},
	}

	return result, nil
}

func parseScalpingBacktestConfig(req RunScalpingBacktestRequest) (ScalpingBacktestConfig, error) {
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartTime))
	if err != nil {
		return ScalpingBacktestConfig{}, fmt.Errorf("start_time must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndTime))
	if err != nil {
		return ScalpingBacktestConfig{}, fmt.Errorf("end_time must be RFC3339")
	}
	if !start.Before(end) {
		return ScalpingBacktestConfig{}, fmt.Errorf("start_time must be before end_time")
	}
	if end.Sub(start) > 90*24*time.Hour {
		return ScalpingBacktestConfig{}, fmt.Errorf("date range must not exceed 90 days")
	}

	initialCapitalRaw := strings.TrimSpace(req.InitialCapital)
	if initialCapitalRaw == "" {
		initialCapitalRaw = "10000"
	}
	initialCapital, err := decimal.NewFromString(initialCapitalRaw)
	if err != nil {
		return ScalpingBacktestConfig{}, fmt.Errorf("initial_capital must be a valid decimal string")
	}
	if !initialCapital.GreaterThan(decimal.Zero) {
		return ScalpingBacktestConfig{}, fmt.Errorf("initial_capital must be greater than zero")
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
		return ScalpingBacktestConfig{}, fmt.Errorf("max_bid_ask_spread_pct must be greater than zero")
	}

	minConfidence := 0.60
	if req.MinConfidence != nil {
		minConfidence = *req.MinConfidence
	}
	if minConfidence < 0 || minConfidence > 1 {
		return ScalpingBacktestConfig{}, fmt.Errorf("min_confidence must be between 0 and 1")
	}

	feeRate := "0.001"
	if req.FeeRate != nil && strings.TrimSpace(*req.FeeRate) != "" {
		feeRate = strings.TrimSpace(*req.FeeRate)
	}
	feeRateDecimal, err := decimal.NewFromString(feeRate)
	if err != nil {
		return ScalpingBacktestConfig{}, fmt.Errorf("fee_rate must be a valid decimal string")
	}
	if feeRateDecimal.IsNegative() {
		return ScalpingBacktestConfig{}, fmt.Errorf("fee_rate must be non-negative")
	}

	symbols := normalizeSymbols(req.Symbols)

	return ScalpingBacktestConfig{
		StartTime:          start.UTC(),
		EndTime:            end.UTC(),
		Symbols:            symbols,
		Exchange:           exchange,
		InitialCapital:     initialCapital.String(),
		MaxBidAskSpreadPct: maxBidAskSpreadPct,
		MinConfidence:      minConfidence,
		FeeRate:            feeRateDecimal.String(),
	}, nil
}

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

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func outcomeFromPnL(pnl decimal.Decimal) string {
	if pnl.IsPositive() {
		return "win"
	}
	if pnl.IsNegative() {
		return "loss"
	}
	return "breakeven"
}

func (h *ScalpingBacktestHandler) insertBacktestRun(ctx context.Context, runID, status string, config ScalpingBacktestConfig, summary ScalpingBacktestSummary, createdAt time.Time, completedAt *time.Time) error {
	configRaw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	summaryRaw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO scalping_backtest_runs (id, created_at, completed_at, config, status, summary)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = h.db.Exec(ctx, query, runID, createdAt, completedAt, configRaw, status, summaryRaw)
	return err
}

func (h *ScalpingBacktestHandler) updateBacktestRun(ctx context.Context, runID, status string, summary ScalpingBacktestSummary, completedAt time.Time) error {
	summaryRaw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = h.db.Exec(ctx, `
		UPDATE scalping_backtest_runs
		SET status = $2,
		    completed_at = $3,
		    summary = $4
		WHERE id = $1
	`, runID, status, completedAt, summaryRaw)
	return err
}

func (h *ScalpingBacktestHandler) persistBacktestResult(ctx context.Context, runID string, config ScalpingBacktestConfig, result scalpingBacktestResult) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	completedAt := time.Now().UTC()
	summaryRaw, err := json.Marshal(result.Summary)
	if err != nil {
		return err
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scalping_backtest_runs
		SET status = 'completed',
		    completed_at = $2,
		    config = $3,
		    summary = $4
		WHERE id = $1
	`, runID, completedAt, configRaw, summaryRaw); err != nil {
		return err
	}

	for _, signal := range result.Signals {
		signalID := signal.SignalID
		if signalID == "" {
			signalID = uuid.NewString()
		}
		signalRaw, err := json.Marshal(signal.Signal)
		if err != nil {
			return err
		}
		gateRaw, err := json.Marshal(signal.GateResults)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_signals (
				id, run_id, timestamp, symbol, exchange, signal, regime, regime_volatility,
				funnel_stage, rejection_reason, gate_results
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, signalID, runID, signal.Timestamp, signal.Symbol, signal.Exchange, signalRaw, signal.Regime, signal.RegimeVolatility, signal.FunnelStage, nullString(signal.RejectionReason), gateRaw); err != nil {
			return err
		}
	}

	for _, trade := range result.Trades {
		tradeID := trade.TradeID
		if tradeID == "" {
			tradeID = uuid.NewString()
		}
		signalID := strings.TrimSpace(trade.SignalID)
		if signalID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_trades (
				id, run_id, signal_id, symbol, side, size, notional,
				entry_price, exit_price, entry_timestamp, exit_timestamp,
				pnl, pnl_pct, fees, outcome, exit_reason,
				regime_at_entry, regime_at_exit, hold_duration_seconds
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11,
				$12, $13, $14, $15, $16,
				$17, $18, $19
			)
		`,
			tradeID,
			runID,
			signalID,
			trade.Symbol,
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
			return err
		}
	}

	for _, gate := range result.GateSummary {
		reasonsRaw, err := json.Marshal(gate.TopRejectionReasons)
		if err != nil {
			return err
		}
		bySymbolRaw, err := json.Marshal(gate.BreakdownBySymbol)
		if err != nil {
			return err
		}
		byRegimeRaw, err := json.Marshal(gate.BreakdownByRegime)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_gate_summary (
				id, run_id, gate_name, pass_count, reject_count,
				top_rejection_reasons, breakdown_by_symbol, breakdown_by_regime
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, uuid.NewString(), runID, gate.GateName, gate.PassCount, gate.RejectCount, reasonsRaw, bySymbolRaw, byRegimeRaw); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
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

func decodeScalpingBacktestRow(runID, status string, configRaw, summaryRaw []byte, createdAt time.Time, completedAtNS sql.NullTime) (GetScalpingBacktestResponse, error) {
	var config ScalpingBacktestConfig
	if len(configRaw) > 0 {
		if err := json.Unmarshal(configRaw, &config); err != nil {
			return GetScalpingBacktestResponse{}, err
		}
	}
	var summary ScalpingBacktestSummary
	if len(summaryRaw) > 0 {
		if err := json.Unmarshal(summaryRaw, &summary); err != nil {
			return GetScalpingBacktestResponse{}, err
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
