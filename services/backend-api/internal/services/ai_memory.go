package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type TradeMemory struct {
	db *sql.DB
}

type AITradeRecord struct {
	ID             string          `json:"id"`
	Timestamp      time.Time       `json:"timestamp"`
	Exchange       string          `json:"exchange"`
	Symbol         string          `json:"symbol"`
	Action         string          `json:"action"`
	SizePercent    float64         `json:"size_percent"`
	Confidence     float64         `json:"confidence"`
	Reasoning      string          `json:"reasoning"`
	MarketContext  string          `json:"market_context"`
	Outcome        string          `json:"outcome"`
	PnL            decimal.Decimal `json:"pnl"`
	PnLPercent     float64         `json:"pnl_percent"`
	LessonsLearned string          `json:"lessons_learned"`
	EntryPrice     float64         `json:"entry_price"`
	ExitPrice      float64         `json:"exit_price"`
	HoldDuration   time.Duration   `json:"hold_duration"`
}

type SimilarTrade struct {
	AITradeRecord
	SimilarityScore float64 `json:"similarity_score"`
}

type TradePerformanceWindowStats struct {
	LookbackHours      int
	WindowFrom         time.Time
	WindowTo           time.Time
	TotalTrades        int
	Wins               int
	Losses             int
	Breakeven          int
	Pending            int
	DecisiveTrades     int
	DecisiveWinRatePct float64
	TotalPnL           decimal.Decimal
	AvgConfidence      float64
}

func NewTradeMemory(db *sql.DB) (*TradeMemory, error) {
	tm := &TradeMemory{db: db}
	if err := tm.initTables(); err != nil {
		return nil, fmt.Errorf("failed to init trade memory tables: %w", err)
	}
	return tm, nil
}

func (tm *TradeMemory) initTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS ai_trade_memory (
		id TEXT PRIMARY KEY,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		action TEXT NOT NULL,
		size_percent REAL,
		confidence REAL,
		reasoning TEXT,
		market_context TEXT,
		outcome TEXT DEFAULT 'pending',
		pnl REAL DEFAULT 0,
		pnl_percent REAL DEFAULT 0,
		lessons_learned TEXT,
		entry_price REAL,
		exit_price REAL,
		hold_duration_seconds INTEGER
	)`
	_, err := tm.db.Exec(schema)
	if err != nil {
		return err
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_trade_memory_symbol ON ai_trade_memory(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_memory_outcome ON ai_trade_memory(outcome)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_memory_timestamp ON ai_trade_memory(timestamp)`,
	}
	for _, idx := range indexes {
		_, _ = tm.db.Exec(idx)
	}

	lessonsTable := `CREATE TABLE IF NOT EXISTS ai_lessons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		category TEXT NOT NULL,
		pattern TEXT NOT NULL,
		lesson TEXT NOT NULL,
		example_trade_id TEXT,
		weight REAL DEFAULT 1.0
	)`
	_, _ = tm.db.Exec(lessonsTable)
	return nil
}

func (tm *TradeMemory) RecordDecision(ctx context.Context, record AITradeRecord) error {
	if record.ID == "" {
		record.ID = fmt.Sprintf("trade_%d", time.Now().UnixNano())
	}

	query := `
		INSERT INTO ai_trade_memory
		(id, timestamp, exchange, symbol, action, size_percent, confidence, reasoning, market_context, entry_price)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := tm.db.ExecContext(ctx, query,
		record.ID,
		record.Timestamp,
		record.Exchange,
		record.Symbol,
		record.Action,
		record.SizePercent,
		record.Confidence,
		record.Reasoning,
		record.MarketContext,
		record.EntryPrice,
	)
	if err != nil {
		return fmt.Errorf("failed to record trade decision: %w", err)
	}

	log.Printf("[AI-MEMORY] Recorded decision: %s %s on %s (confidence: %.2f)",
		record.Action, record.Symbol, record.Exchange, record.Confidence)
	return nil
}

func (tm *TradeMemory) UpdateOutcome(ctx context.Context, tradeID string, outcome string, exitPrice float64, pnl decimal.Decimal) error {
	query := `
		UPDATE ai_trade_memory
		SET outcome = ?, exit_price = ?, pnl = ?, pnl_percent = ?
		WHERE id = ?
	`
	pnlPercent := 0.0
	if exitPrice > 0 {
		pnlFloat, _ := pnl.Float64()
		pnlPercent = pnlFloat
	}

	_, err := tm.db.ExecContext(ctx, query, outcome, exitPrice, pnl, pnlPercent, tradeID)
	if err != nil {
		return fmt.Errorf("failed to update trade outcome: %w", err)
	}

	log.Printf("[AI-MEMORY] Updated trade %s: outcome=%s, pnl=%s", tradeID, outcome, pnl.String())
	return nil
}

func (tm *TradeMemory) GetRecentTrades(ctx context.Context, limit int) ([]AITradeRecord, error) {
	query := `
		SELECT id, timestamp, exchange, symbol, action, size_percent, confidence, reasoning,
			   market_context, outcome, pnl, pnl_percent, lessons_learned, entry_price, exit_price
		FROM ai_trade_memory
		ORDER BY timestamp DESC
		LIMIT ?
	`
	rows, err := tm.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var trades []AITradeRecord
	for rows.Next() {
		var t AITradeRecord
		var reasoning, contextStr, lessons sql.NullString
		var pnlFloat float64
		var entryPrice, exitPrice sql.NullFloat64
		var confidence, sizePercent sql.NullFloat64

		err := rows.Scan(
			&t.ID, &t.Timestamp, &t.Exchange, &t.Symbol, &t.Action, &sizePercent,
			&confidence, &reasoning, &contextStr, &t.Outcome, &pnlFloat, &t.PnLPercent,
			&lessons, &entryPrice, &exitPrice,
		)
		if err != nil {
			log.Printf("[AI-MEMORY] Scan error in GetRecentTrades: %v", err)
			continue
		}

		t.Reasoning = reasoning.String
		t.MarketContext = contextStr.String
		t.LessonsLearned = lessons.String
		t.PnL = decimal.NewFromFloat(pnlFloat)
		if sizePercent.Valid {
			t.SizePercent = sizePercent.Float64
		}
		if confidence.Valid {
			t.Confidence = confidence.Float64
		}
		if entryPrice.Valid {
			t.EntryPrice = entryPrice.Float64
		}
		if exitPrice.Valid {
			t.ExitPrice = exitPrice.Float64
		}
		trades = append(trades, t)
	}
	return trades, nil
}

func (tm *TradeMemory) FindSimilarPatterns(ctx context.Context, symbol string, currentContext string) ([]SimilarTrade, error) {
	query := `
		SELECT id, timestamp, exchange, symbol, action, size_percent, confidence, reasoning,
			   market_context, outcome, pnl, pnl_percent, lessons_learned, entry_price, exit_price
		FROM ai_trade_memory
		WHERE symbol = ? AND outcome != 'pending'
		ORDER BY timestamp DESC
		LIMIT 20
	`
	rows, err := tm.db.QueryContext(ctx, query, symbol)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var similar []SimilarTrade
	keywords := extractKeywords(currentContext)

	for rows.Next() {
		var t AITradeRecord
		var reasoning, contextStr, lessons sql.NullString
		var pnlFloat float64
		var entryPrice, exitPrice sql.NullFloat64
		var confidence, sizePercent sql.NullFloat64

		err := rows.Scan(
			&t.ID, &t.Timestamp, &t.Exchange, &t.Symbol, &t.Action, &sizePercent,
			&confidence, &reasoning, &contextStr, &t.Outcome, &pnlFloat, &t.PnLPercent,
			&lessons, &entryPrice, &exitPrice,
		)
		if err != nil {
			log.Printf("[AI-MEMORY] Scan error in FindSimilarPatterns: %v", err)
			continue
		}

		t.Reasoning = reasoning.String
		t.MarketContext = contextStr.String
		t.LessonsLearned = lessons.String
		t.PnL = decimal.NewFromFloat(pnlFloat)
		if sizePercent.Valid {
			t.SizePercent = sizePercent.Float64
		}
		if confidence.Valid {
			t.Confidence = confidence.Float64
		}
		if entryPrice.Valid {
			t.EntryPrice = entryPrice.Float64
		}
		if exitPrice.Valid {
			t.ExitPrice = exitPrice.Float64
		}

		similarity := calculateSimilarity(keywords, t.MarketContext+" "+t.Reasoning)
		if similarity > 0.3 {
			similar = append(similar, SimilarTrade{
				AITradeRecord:   t,
				SimilarityScore: similarity,
			})
		}
	}

	return similar, nil
}

func (tm *TradeMemory) GetLessonsLearned(ctx context.Context) (string, error) {
	query := `SELECT pattern, lesson, weight FROM ai_lessons WHERE weight > 0.5 ORDER BY weight DESC LIMIT 10`
	rows, err := tm.db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	var lessons []string
	for rows.Next() {
		var pattern, lesson string
		var weight float64
		if err := rows.Scan(&pattern, &lesson, &weight); err != nil {
			continue
		}
		lessons = append(lessons, fmt.Sprintf("- Pattern: %s → Lesson: %s", pattern, lesson))
	}

	if len(lessons) == 0 {
		lessons = tm.extractLessonsFromTrades(ctx)
	}

	return strings.Join(lessons, "\n"), nil
}

func (tm *TradeMemory) extractLessonsFromTrades(ctx context.Context) []string {
	query := `
		SELECT symbol, action, reasoning, outcome, pnl_percent
		FROM ai_trade_memory
		WHERE outcome IN ('win', 'loss') AND pnl_percent != 0
		ORDER BY ABS(pnl_percent) DESC
		LIMIT 10
	`
	rows, err := tm.db.QueryContext(ctx, query)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var lessons []string
	for rows.Next() {
		var symbol, action, reasoning, outcome string
		var pnlPercent float64
		if err := rows.Scan(&symbol, &action, &reasoning, &outcome, &pnlPercent); err != nil {
			continue
		}

		if outcome == "loss" && pnlPercent < -2 {
			lessons = append(lessons, fmt.Sprintf("AVOID: %s action on %s led to %.1f%% loss. Reason: %s",
				action, symbol, pnlPercent, truncate(reasoning, 50)))
		} else if outcome == "win" && pnlPercent > 2 {
			lessons = append(lessons, fmt.Sprintf("SUCCESS: %s on %s gave %.1f%% gain. Reason: %s",
				action, symbol, pnlPercent, truncate(reasoning, 50)))
		}
	}
	return lessons
}

func (tm *TradeMemory) GetPerformanceStats(ctx context.Context) (map[string]interface{}, error) {
	windowStats, err := tm.GetPerformanceStatsWindow(ctx, 0)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total_trades":         windowStats.TotalTrades,
		"wins":                 windowStats.Wins,
		"losses":               windowStats.Losses,
		"breakeven":            windowStats.Breakeven,
		"pending":              windowStats.Pending,
		"decisive_trades":      windowStats.DecisiveTrades,
		"win_rate":             windowStats.DecisiveWinRatePct,
		"decisive_win_rate":    windowStats.DecisiveWinRatePct,
		"avg_confidence":       windowStats.AvgConfidence,
		"total_pnl":            windowStats.TotalPnL,
		"lookback_hours":       windowStats.LookbackHours,
		"window_from":          windowStats.WindowFrom.Format(time.RFC3339),
		"window_to":            windowStats.WindowTo.Format(time.RFC3339),
		"decisive_sample_size": windowStats.DecisiveTrades,
	}, nil
}

func (tm *TradeMemory) GetPerformanceStatsWindow(ctx context.Context, lookbackHours int) (*TradePerformanceWindowStats, error) {
	if lookbackHours <= 0 {
		lookbackHours = 24 * 30
	}
	windowTo := time.Now().UTC()
	windowFrom := windowTo.Add(-time.Duration(lookbackHours) * time.Hour)

	stats := &TradePerformanceWindowStats{
		LookbackHours: lookbackHours,
		WindowFrom:    windowFrom,
		WindowTo:      windowTo,
		TotalPnL:      decimal.Zero,
	}

	rows, err := tm.db.QueryContext(ctx, `
		SELECT outcome, COUNT(*), COALESCE(SUM(pnl), 0), COALESCE(AVG(confidence), 0)
		FROM ai_trade_memory
		WHERE timestamp >= $1
		GROUP BY outcome
	`, windowFrom)
	if err != nil {
		return nil, fmt.Errorf("query performance stats window: %w", err)
	}
	defer func() { _ = rows.Close() }()

	totalConfidence := 0.0
	for rows.Next() {
		var outcome string
		var count int
		var pnl float64
		var avgConfidence float64
		if err := rows.Scan(&outcome, &count, &pnl, &avgConfidence); err != nil {
			return nil, fmt.Errorf("scan performance stats window: %w", err)
		}
		outcome = strings.ToLower(strings.TrimSpace(outcome))
		stats.TotalTrades += count
		stats.TotalPnL = stats.TotalPnL.Add(decimal.NewFromFloat(pnl))
		totalConfidence += avgConfidence * float64(count)

		switch outcome {
		case "win":
			stats.Wins += count
		case "loss":
			stats.Losses += count
		case "breakeven", "break_even":
			stats.Breakeven += count
		default:
			stats.Pending += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate performance stats window: %w", err)
	}

	if stats.TotalTrades > 0 {
		stats.AvgConfidence = totalConfidence / float64(stats.TotalTrades)
	}

	stats.DecisiveTrades = stats.Wins + stats.Losses
	if stats.DecisiveTrades > 0 {
		stats.DecisiveWinRatePct = (float64(stats.Wins) / float64(stats.DecisiveTrades)) * 100
	}
	return stats, nil
}

func (tm *TradeMemory) GetLastDecisionTimestamp(ctx context.Context) (time.Time, error) {
	var raw sql.NullString
	if err := tm.db.QueryRowContext(ctx, `
		SELECT MAX(timestamp) FROM ai_trade_memory
	`).Scan(&raw); err != nil {
		return time.Time{}, fmt.Errorf("query last decision timestamp: %w", err)
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return time.Time{}, nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw.String); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, nil
}

func (tm *TradeMemory) BuildMemoryContext(ctx context.Context, symbol string, currentContext string) (string, error) {
	var contextBuilder strings.Builder

	contextBuilder.WriteString("## Past Trading History\n\n")

	stats, err := tm.GetPerformanceStatsWindow(ctx, 24*30)
	if err == nil {
		contextBuilder.WriteString("### Performance Stats\n")
		fmt.Fprintf(&contextBuilder, "- Lookback: %dh\n", stats.LookbackHours)
		fmt.Fprintf(&contextBuilder, "- Total Trades: %d (pending: %d)\n", stats.TotalTrades, stats.Pending)
		fmt.Fprintf(&contextBuilder, "- Decisive Sample: %d (wins: %d, losses: %d, breakeven: %d)\n", stats.DecisiveTrades, stats.Wins, stats.Losses, stats.Breakeven)
		fmt.Fprintf(&contextBuilder, "- Decisive Win Rate: %.1f%%\n", stats.DecisiveWinRatePct)
		fmt.Fprintf(&contextBuilder, "- Total PnL: %s\n", stats.TotalPnL.StringFixed(4))
		contextBuilder.WriteString("\n")
	}

	similar, err := tm.FindSimilarPatterns(ctx, symbol, currentContext)
	if err == nil && len(similar) > 0 {
		contextBuilder.WriteString("### Similar Past Trades\n")
		for i, s := range similar {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&contextBuilder, "- %s: %s %s (confidence: %.2f) → %s (PnL: %.2f%%)\n",
				s.Timestamp.Format("2006-01-02 15:04"),
				s.Action, s.Symbol, s.Confidence, s.Outcome, s.PnLPercent)
			if s.LessonsLearned != "" {
				fmt.Fprintf(&contextBuilder, "  Lesson: %s\n", s.LessonsLearned)
			}
		}
		contextBuilder.WriteString("\n")
	}

	lessons, err := tm.GetLessonsLearned(ctx)
	if err == nil && lessons != "" {
		contextBuilder.WriteString("### Lessons Learned\n")
		contextBuilder.WriteString(lessons)
		contextBuilder.WriteString("\n")
	}

	return contextBuilder.String(), nil
}

func extractKeywords(text string) []string {
	text = strings.ToLower(text)
	words := strings.Fields(text)
	keywords := make(map[string]bool)

	importantWords := []string{"oversold", "overbought", "bullish", "bearish", "breakout",
		"support", "resistance", "volume", "trend", "momentum", "rsi", "macd",
		"imbalance", "spread", "volatility", "high", "low", "buy", "sell"}

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		for _, important := range importantWords {
			if strings.Contains(word, important) {
				keywords[word] = true
			}
		}
	}

	result := make([]string, 0, len(keywords))
	for k := range keywords {
		result = append(result, k)
	}
	return result
}

func calculateSimilarity(keywords []string, text string) float64 {
	if len(keywords) == 0 {
		return 0
	}
	text = strings.ToLower(text)
	matches := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			matches++
		}
	}
	return float64(matches) / float64(len(keywords))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (tm *TradeMemory) RecordLesson(ctx context.Context, category, pattern, lesson, tradeID string) error {
	query := `
		INSERT INTO ai_lessons (category, pattern, lesson, example_trade_id)
		VALUES (?, ?, ?, ?)
	`
	_, err := tm.db.ExecContext(ctx, query, category, pattern, lesson, tradeID)
	return err
}

func (tm *TradeMemory) RecordTradeDecisionJSON(decisionJSON string) error {
	var decision struct {
		Action      string  `json:"action"`
		Symbol      string  `json:"symbol"`
		SizePercent float64 `json:"size_pct"`
		Confidence  float64 `json:"confidence"`
		Reasoning   string  `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		return err
	}

	record := AITradeRecord{
		ID:            fmt.Sprintf("trade_%d", time.Now().UnixNano()),
		Timestamp:     time.Now(),
		Exchange:      "binance",
		Symbol:        decision.Symbol,
		Action:        decision.Action,
		SizePercent:   decision.SizePercent,
		Confidence:    decision.Confidence,
		Reasoning:     decision.Reasoning,
		MarketContext: decisionJSON,
	}
	return tm.RecordDecision(context.Background(), record)
}
