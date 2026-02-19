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
	stats := make(map[string]interface{})

	queries := map[string]string{
		"total_trades":   "SELECT COUNT(*) FROM ai_trade_memory WHERE outcome != 'pending'",
		"wins":           "SELECT COUNT(*) FROM ai_trade_memory WHERE outcome = 'win'",
		"losses":         "SELECT COUNT(*) FROM ai_trade_memory WHERE outcome = 'loss'",
		"total_pnl":      "SELECT COALESCE(SUM(pnl), 0) FROM ai_trade_memory",
		"avg_confidence": "SELECT AVG(confidence) FROM ai_trade_memory WHERE outcome != 'pending'",
	}

	for key, q := range queries {
		row := tm.db.QueryRowContext(ctx, q)
		var val interface{}
		if err := row.Scan(&val); err == nil {
			stats[key] = val
		}
	}

	if total, ok := stats["total_trades"].(int64); ok && total > 0 {
		if wins, ok := stats["wins"].(int64); ok {
			stats["win_rate"] = float64(wins) / float64(total) * 100
		}
	}

	return stats, nil
}

func (tm *TradeMemory) BuildMemoryContext(ctx context.Context, symbol string, currentContext string) (string, error) {
	var contextBuilder strings.Builder

	contextBuilder.WriteString("## Past Trading History\n\n")

	stats, err := tm.GetPerformanceStats(ctx)
	if err == nil {
		contextBuilder.WriteString("### Performance Stats\n")
		fmt.Fprintf(&contextBuilder, "- Total Trades: %v\n", stats["total_trades"])
		fmt.Fprintf(&contextBuilder, "- Win Rate: %.1f%%\n", stats["win_rate"])
		fmt.Fprintf(&contextBuilder, "- Total PnL: %v\n", stats["total_pnl"])
		contextBuilder.WriteString("\n")
	}

	similar, err := tm.FindSimilarPatterns(ctx, symbol, currentContext)
	if err == nil && len(similar) > 0 {
		contextBuilder.WriteString("### Similar Past Trades\n")
		for i, s := range similar {
			if i >= 5 {
				break
			}
			contextBuilder.WriteString(fmt.Sprintf("- %s: %s %s (confidence: %.2f) → %s (PnL: %.2f%%)\n",
				s.Timestamp.Format("2006-01-02 15:04"),
				s.Action, s.Symbol, s.Confidence, s.Outcome, s.PnLPercent))
			if s.LessonsLearned != "" {
				contextBuilder.WriteString(fmt.Sprintf("  Lesson: %s\n", s.LessonsLearned))
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
