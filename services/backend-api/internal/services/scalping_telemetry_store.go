// Package services scalping telemetry investigation workflow:
// 1. rejection_histogram - which filters kill the most candidates.
// 2. gate_block_summary - which gates block most frequently.
// 3. regime_outcomes - how win rate correlates with market regime.
// 4. policy_adjustment_impact - how specific adjustments affect outcomes.
// 5. win_rate_trend - time-series win rate evolution.
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/jackc/pgx/v5"
)

type ScalpingTelemetryStore struct {
	db          database.DBPool
	logger      *slog.Logger
	placeholder placeholderStyle
}

type placeholderStyle int

const (
	placeholderQuestion placeholderStyle = iota
	placeholderDollar
)

type CycleRecord struct {
	ID                     string
	ChatID                 string
	Exchange               string
	OrderID                string
	CycleAt                time.Time
	Symbol                 string
	Action                 string
	Confidence             float64
	UniverseCount          int
	RankedCount            int
	ViableCount            int
	RejectionCountsJSON    string
	Regime                 string
	Expectancy             float64
	ExpectancySampleSize   int
	GateBlockCode          string
	GateBlockReason        string
	AccountTier            string
	EffectiveMinConfidence float64
	EffectiveMaxCapitalPct float64
	PolicyAdjustmentsJSON  string
	SignalPrice            *float64
	BidAskSpreadPct        *float64
	OrderBookImbalance     *float64
	RangePosition24h       *float64
	PriceChange24hPct      *float64
	RecentPriceChangePct   *float64
	RecentChangeAgeSec     *float64
}

type ScalpingOutcomeRecord struct {
	Outcome             string
	PnL                 string
	HoldDurationSeconds int
	ClosedAt            time.Time
}

type GateBlockStat struct {
	BlockCode   string  `json:"block_code"`
	Count       int     `json:"count"`
	WinRate     float64 `json:"win_rate"`
	AvgPnL      float64 `json:"avg_pnl"`
	TotalTrades int     `json:"total_trades"`
}

type RegimeOutcomeStat struct {
	Regime  string  `json:"regime"`
	Count   int     `json:"count"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"win_rate"`
	AvgPnL  float64 `json:"avg_pnl"`
}

type AdjustmentImpactStat struct {
	Adjustment string  `json:"adjustment"`
	Count      int     `json:"count"`
	WinRate    float64 `json:"win_rate"`
	AvgPnL     float64 `json:"avg_pnl"`
}

type WinRateBucket struct {
	BucketStart time.Time `json:"bucket_start"`
	BucketEnd   time.Time `json:"bucket_end"`
	TotalTrades int       `json:"total_trades"`
	Wins        int       `json:"wins"`
	WinRate     float64   `json:"win_rate"`
}

func NewScalpingTelemetryStore(db database.DBPool, logger *slog.Logger) *ScalpingTelemetryStore {
	if db == nil {
		return nil
	}
	if logger == nil {
		logger = telemetry.Logger()
	}
	return &ScalpingTelemetryStore{
		db:          db,
		logger:      logger,
		placeholder: detectPlaceholderStyle(db),
	}
}

func NewScalpingTelemetryStoreFromSQLDB(db *sql.DB, logger *slog.Logger) *ScalpingTelemetryStore {
	if db == nil {
		return nil
	}
	return NewScalpingTelemetryStore(sqlDBPool{db: db}, logger)
}

func (s *ScalpingTelemetryStore) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("scalping telemetry store requires database")
	}

	preDedup := []string{
		`CREATE TABLE IF NOT EXISTS scalping_cycle_telemetry (
			id TEXT PRIMARY KEY,
			chat_id TEXT,
			exchange TEXT,
			order_id TEXT,
			cycle_at TIMESTAMP,
			symbol TEXT,
			action TEXT,
			confidence REAL,
			universe_count INT,
			ranked_count INT,
			viable_count INT,
			rejection_counts TEXT,
			regime TEXT,
			expectancy REAL,
			expectancy_sample_size INT,
			gate_block_code TEXT,
			gate_block_reason TEXT,
			account_tier TEXT,
			effective_min_confidence REAL,
			effective_max_capital_pct REAL,
			policy_adjustments TEXT,
			signal_price REAL,
			bid_ask_spread_pct REAL,
			order_book_imbalance REAL,
			range_position_24h REAL,
			price_change_24h_pct REAL,
			recent_price_change_pct REAL,
			recent_change_age_sec REAL,
			outcome TEXT,
			pnl NUMERIC,
			hold_duration_seconds INT,
			closed_at TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_chat_id ON scalping_cycle_telemetry(chat_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_cycle_at ON scalping_cycle_telemetry(cycle_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_chat_id_cycle_at ON scalping_cycle_telemetry(chat_id, cycle_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_order_id ON scalping_cycle_telemetry(order_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_outcome ON scalping_cycle_telemetry(outcome)`,
	}

	for _, stmt := range preDedup {
		if _, err := s.db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("scalping telemetry schema statement failed: %w", err)
		}
	}

	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "signal_price", definition: "REAL"},
		{name: "bid_ask_spread_pct", definition: "REAL"},
		{name: "order_book_imbalance", definition: "REAL"},
		{name: "range_position_24h", definition: "REAL"},
		{name: "price_change_24h_pct", definition: "REAL"},
		{name: "recent_price_change_pct", definition: "REAL"},
		{name: "recent_change_age_sec", definition: "REAL"},
	} {
		if err := s.ensureCycleTelemetryColumn(ctx, column.name, column.definition); err != nil {
			return err
		}
	}

	s.deduplicateOrderIDs(ctx)

	postDedup := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_scalping_cycle_telemetry_order_id_unique
			ON scalping_cycle_telemetry(order_id)
			WHERE order_id IS NOT NULL AND order_id != ''`,
	}

	for _, stmt := range postDedup {
		if _, err := s.db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("scalping telemetry schema statement failed: %w", err)
		}
	}

	return nil
}

func (s *ScalpingTelemetryStore) deduplicateOrderIDs(ctx context.Context) {
	var dupCount int
	err := s.db.QueryRow(ctx, s.bindQuery(`SELECT COUNT(*) FROM (
		SELECT order_id FROM scalping_cycle_telemetry
		WHERE order_id IS NOT NULL AND order_id != ''
		GROUP BY order_id HAVING COUNT(*) > 1
	) AS dup`)).Scan(&dupCount)
	if err != nil || dupCount == 0 {
		return
	}
	s.logger.Warn("deduplicating order_id rows in telemetry", "duplicates", dupCount)
	_, _ = s.db.Exec(ctx, s.bindQuery(`DELETE FROM scalping_cycle_telemetry
	 WHERE id IN (
	 	SELECT id FROM (
	 		SELECT id,
	 			ROW_NUMBER() OVER (
	 				PARTITION BY order_id
	 				ORDER BY CASE WHEN outcome IS NOT NULL AND outcome != '' THEN 0 ELSE 1 END,
	 					cycle_at DESC,
	 					id DESC
	 			) AS rn
	 		FROM scalping_cycle_telemetry
	 		WHERE order_id IS NOT NULL AND order_id != ''
	 	) ranked
	 	WHERE rn > 1
	 )`))
}

func (s *ScalpingTelemetryStore) InsertCycleRecord(ctx context.Context, record CycleRecord) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("scalping telemetry store unavailable")
	}

	cycleID := strings.TrimSpace(record.ID)
	if cycleID == "" {
		cycleID = "scalp-" + uuid.NewString()
	}
	record.ID = cycleID
	cycleAt := record.CycleAt.UTC()
	if cycleAt.IsZero() {
		cycleAt = time.Now().UTC()
	}

	_, err := s.db.Exec(ctx, s.bindQuery(`
		INSERT INTO scalping_cycle_telemetry (
			id, chat_id, exchange, order_id, cycle_at, symbol, action, confidence,
			universe_count, ranked_count, viable_count, rejection_counts, regime,
			expectancy, expectancy_sample_size, gate_block_code, gate_block_reason,
			account_tier, effective_min_confidence, effective_max_capital_pct,
			policy_adjustments, signal_price, bid_ask_spread_pct,
			order_book_imbalance, range_position_24h, price_change_24h_pct,
			recent_price_change_pct, recent_change_age_sec
		) VALUES (
			?,?,?,?,?,?,?,?,
			?,?,?,?,?,
			?,?,?,?,
			?,?,?,?,?,?,?,?,?,?,?
		)
	`),
		cycleID,
		strings.TrimSpace(record.ChatID),
		strings.TrimSpace(record.Exchange),
		strings.TrimSpace(record.OrderID),
		cycleAt,
		strings.TrimSpace(record.Symbol),
		strings.TrimSpace(record.Action),
		record.Confidence,
		record.UniverseCount,
		record.RankedCount,
		record.ViableCount,
		record.RejectionCountsJSON,
		strings.TrimSpace(record.Regime),
		record.Expectancy,
		record.ExpectancySampleSize,
		strings.TrimSpace(record.GateBlockCode),
		strings.TrimSpace(record.GateBlockReason),
		strings.TrimSpace(record.AccountTier),
		record.EffectiveMinConfidence,
		record.EffectiveMaxCapitalPct,
		record.PolicyAdjustmentsJSON,
		nullableFiniteFloat(record.SignalPrice),
		nullableFiniteFloat(record.BidAskSpreadPct),
		nullableFiniteFloat(record.OrderBookImbalance),
		nullableFiniteFloat(record.RangePosition24h),
		nullableFiniteFloat(record.PriceChange24hPct),
		nullableFiniteFloat(record.RecentPriceChangePct),
		nullableFiniteFloat(record.RecentChangeAgeSec),
	)
	if err != nil {
		return "", fmt.Errorf("insert cycle telemetry: %w", err)
	}

	return cycleID, nil
}

func (s *ScalpingTelemetryStore) ensureCycleTelemetryColumn(ctx context.Context, name, definition string) error {
	name = strings.TrimSpace(name)
	definition = strings.TrimSpace(definition)
	if !isSafeTelemetryColumnName(name) || definition != "REAL" {
		return fmt.Errorf("invalid scalping telemetry column definition")
	}
	_, err := s.db.Exec(ctx, fmt.Sprintf("ALTER TABLE scalping_cycle_telemetry ADD COLUMN %s %s", name, definition))
	if err == nil || isDuplicateColumnError(err) {
		return nil
	}
	return fmt.Errorf("add scalping telemetry column %s: %w", name, err)
}

func isSafeTelemetryColumnName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func finiteFloatPointer(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func finiteFloatPointerIf(value float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return finiteFloatPointer(value)
}

func nullableFiniteFloat(value *float64) any {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return *value
}

func (s *ScalpingTelemetryStore) LinkOrderToCycle(ctx context.Context, cycleID string, orderID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("scalping telemetry store unavailable")
	}

	if strings.TrimSpace(cycleID) == "" || strings.TrimSpace(orderID) == "" {
		return nil
	}

	result, err := s.db.Exec(ctx, s.bindQuery(`
		UPDATE scalping_cycle_telemetry
		SET order_id = ?
		WHERE id = ?
			AND (order_id IS NULL OR order_id = '')
	`), strings.TrimSpace(orderID), strings.TrimSpace(cycleID))
	if err != nil {
		return fmt.Errorf("link order to cycle: %w", err)
	}
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("link order to cycle: check affected rows: %w", rowsErr)
	}
	if affected == 0 {
		var existingOrderID sql.NullString
		queryErr := s.db.QueryRow(ctx, s.bindQuery(`
			SELECT order_id
			FROM scalping_cycle_telemetry
			WHERE id = ?
		`), strings.TrimSpace(cycleID)).Scan(&existingOrderID)
		if queryErr == nil && strings.TrimSpace(existingOrderID.String) == strings.TrimSpace(orderID) {
			return nil
		}
		if errors.Is(queryErr, sql.ErrNoRows) || errors.Is(queryErr, pgx.ErrNoRows) {
			return fmt.Errorf("link order to cycle: no matching cycle found for id=%s", strings.TrimSpace(cycleID))
		}
		if queryErr != nil {
			return fmt.Errorf("link order to cycle: lookup existing order link: %w", queryErr)
		}
		return fmt.Errorf("link order to cycle: cycle id=%s already linked to order_id=%s", strings.TrimSpace(cycleID), strings.TrimSpace(existingOrderID.String))
	}
	if affected > 1 {
		return fmt.Errorf("link order to cycle: multiple cycles matched for id=%s, expected unique order_id", strings.TrimSpace(cycleID))
	}

	return nil
}

func (s *ScalpingTelemetryStore) UpdateCycleOutcome(ctx context.Context, orderID string, record ScalpingOutcomeRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("scalping telemetry store unavailable")
	}

	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil
	}

	closedAt := record.ClosedAt.UTC()
	if closedAt.IsZero() {
		closedAt = time.Now().UTC()
	}

	result, err := s.db.Exec(ctx, s.bindQuery(`
		UPDATE scalping_cycle_telemetry
		SET outcome = ?,
			pnl = CAST(? AS NUMERIC),
			hold_duration_seconds = ?,
			closed_at = ?
		WHERE order_id = ?
	`), strings.TrimSpace(record.Outcome), record.PnL, record.HoldDurationSeconds, closedAt, orderID)
	if err != nil {
		return fmt.Errorf("update cycle outcome: %w", err)
	}
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("update cycle outcome: check affected rows: %w", rowsErr)
	}
	if affected == 0 {
		return fmt.Errorf("update cycle outcome: no matching cycle found for order_id=%s", orderID)
	}
	if affected > 1 {
		return fmt.Errorf("update cycle outcome: multiple cycles matched for order_id=%s, expected unique order_id", orderID)
	}

	return nil
}

func (s *ScalpingTelemetryStore) GetRejectionHistogram(ctx context.Context, chatID string, since time.Time) (map[string]int, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("scalping telemetry store unavailable")
	}

	rows, err := s.db.Query(ctx, s.bindQuery(`
		SELECT rejection_counts
		FROM scalping_cycle_telemetry
		WHERE chat_id = ? AND cycle_at >= ?
	`), strings.TrimSpace(chatID), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("query rejection histogram: %w", err)
	}
	defer rows.Close()

	histogram := make(map[string]int)
	for rows.Next() {
		var raw sql.NullString
		if scanErr := rows.Scan(&raw); scanErr != nil {
			return nil, fmt.Errorf("scan rejection histogram row: %w", scanErr)
		}
		if !raw.Valid || strings.TrimSpace(raw.String) == "" {
			continue
		}
		counts := make(map[string]int)
		if jsonErr := json.Unmarshal([]byte(raw.String), &counts); jsonErr != nil {
			s.logger.Error("malformed rejection_counts JSON", "error", jsonErr, "row", raw.String)
			continue
		}
		for key, value := range counts {
			if strings.TrimSpace(key) == "" || value == 0 {
				continue
			}
			histogram[key] += value
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate rejection histogram rows: %w", rows.Err())
	}

	return histogram, nil
}

func (s *ScalpingTelemetryStore) GetGateBlockSummary(ctx context.Context, chatID string, since time.Time) ([]GateBlockStat, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("scalping telemetry store unavailable")
	}

	rows, err := s.db.Query(ctx, s.bindQuery(`
		SELECT
			COALESCE(gate_block_code, '') AS block_code,
			COUNT(*) AS cycle_count,
			SUM(CASE WHEN outcome IS NOT NULL AND outcome != '' THEN 1 ELSE 0 END) AS total_trades,
			SUM(CASE WHEN outcome = 'win' THEN 1 ELSE 0 END) AS wins,
			COALESCE(AVG(CASE WHEN outcome IS NOT NULL AND outcome != '' THEN pnl END), 0) AS avg_pnl
		FROM scalping_cycle_telemetry
		WHERE chat_id = ?
			AND cycle_at >= ?
			AND gate_block_code IS NOT NULL
			AND gate_block_code != ''
		GROUP BY gate_block_code
		ORDER BY cycle_count DESC
	`), strings.TrimSpace(chatID), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("query gate block summary: %w", err)
	}
	defer rows.Close()

	stats := make([]GateBlockStat, 0)
	for rows.Next() {
		var stat GateBlockStat
		var wins int
		if scanErr := rows.Scan(&stat.BlockCode, &stat.Count, &stat.TotalTrades, &wins, &stat.AvgPnL); scanErr != nil {
			return nil, fmt.Errorf("scan gate block summary row: %w", scanErr)
		}
		stat.WinRate = safeRate(wins, stat.TotalTrades)
		stats = append(stats, stat)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate gate block summary rows: %w", rows.Err())
	}

	return stats, nil
}

func (s *ScalpingTelemetryStore) GetRegimeOutcomeCorrelation(ctx context.Context, chatID string, since time.Time) ([]RegimeOutcomeStat, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("scalping telemetry store unavailable")
	}

	rows, err := s.db.Query(ctx, s.bindQuery(`
		SELECT
			COALESCE(regime, '') AS regime,
			COUNT(*) AS sample_count,
			SUM(CASE WHEN outcome = 'win' THEN 1 ELSE 0 END) AS wins,
			COALESCE(AVG(pnl), 0) AS avg_pnl
		FROM scalping_cycle_telemetry
		WHERE chat_id = ?
			AND cycle_at >= ?
			AND outcome IS NOT NULL
			AND outcome != ''
		GROUP BY regime
		ORDER BY sample_count DESC
	`), strings.TrimSpace(chatID), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("query regime outcome correlation: %w", err)
	}
	defer rows.Close()

	stats := make([]RegimeOutcomeStat, 0)
	for rows.Next() {
		var stat RegimeOutcomeStat
		if scanErr := rows.Scan(&stat.Regime, &stat.Count, &stat.Wins, &stat.AvgPnL); scanErr != nil {
			return nil, fmt.Errorf("scan regime outcome row: %w", scanErr)
		}
		stat.WinRate = safeRate(stat.Wins, stat.Count)
		stats = append(stats, stat)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate regime outcome rows: %w", rows.Err())
	}

	return stats, nil
}

func (s *ScalpingTelemetryStore) GetPolicyAdjustmentImpact(ctx context.Context, chatID string, since time.Time) ([]AdjustmentImpactStat, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("scalping telemetry store unavailable")
	}

	rows, err := s.db.Query(ctx, s.bindQuery(`
		SELECT policy_adjustments, outcome, pnl
		FROM scalping_cycle_telemetry
		WHERE chat_id = ?
			AND cycle_at >= ?
			AND policy_adjustments IS NOT NULL
			AND policy_adjustments != ''
			AND outcome IS NOT NULL
			AND outcome != ''
	`), strings.TrimSpace(chatID), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("query policy adjustment impact: %w", err)
	}
	defer rows.Close()

	aggregator := newPolicyAggregator()

	for rows.Next() {
		var adjustmentsRaw sql.NullString
		var outcome sql.NullString
		var pnl sql.NullFloat64
		if scanErr := rows.Scan(&adjustmentsRaw, &outcome, &pnl); scanErr != nil {
			return nil, fmt.Errorf("scan policy adjustment row: %w", scanErr)
		}
		if !adjustmentsRaw.Valid || strings.TrimSpace(adjustmentsRaw.String) == "" {
			continue
		}
		adjustments := make([]string, 0)
		if jsonErr := json.Unmarshal([]byte(adjustmentsRaw.String), &adjustments); jsonErr != nil {
			s.logger.Error("malformed policy_adjustments JSON", "error", jsonErr, "row", adjustmentsRaw.String)
			continue
		}

		aggregator.add(adjustments, outcome, pnl)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate policy adjustment rows: %w", rows.Err())
	}

	return aggregator.stats(), nil
}

type adjustmentAgg struct {
	count int
	wins  int
	pnl   float64
}

type policyAggregator struct {
	entries map[string]*adjustmentAgg
}

func newPolicyAggregator() *policyAggregator {
	return &policyAggregator{entries: make(map[string]*adjustmentAgg)}
}

func (p *policyAggregator) add(adjustments []string, outcome sql.NullString, pnl sql.NullFloat64) {
	if p == nil {
		return
	}
	seen := make(map[string]struct{}, len(adjustments))
	for _, adjustment := range adjustments {
		adjustment = strings.TrimSpace(adjustment)
		if adjustment == "" {
			continue
		}
		if _, exists := seen[adjustment]; exists {
			continue
		}
		seen[adjustment] = struct{}{}

		entry := p.entries[adjustment]
		if entry == nil {
			entry = &adjustmentAgg{}
			p.entries[adjustment] = entry
		}
		entry.count++
		if outcome.Valid && strings.EqualFold(strings.TrimSpace(outcome.String), "win") {
			entry.wins++
		}
		if pnl.Valid {
			entry.pnl += pnl.Float64
		}
	}
}

func (p *policyAggregator) stats() []AdjustmentImpactStat {
	if p == nil {
		return nil
	}
	stats := make([]AdjustmentImpactStat, 0, len(p.entries))
	for adjustment, entry := range p.entries {
		avgPnL := 0.0
		if entry.count > 0 {
			avgPnL = entry.pnl / float64(entry.count)
		}
		stats = append(stats, AdjustmentImpactStat{
			Adjustment: adjustment,
			Count:      entry.count,
			WinRate:    safeRate(entry.wins, entry.count),
			AvgPnL:     avgPnL,
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count == stats[j].Count {
			return stats[i].Adjustment < stats[j].Adjustment
		}
		return stats[i].Count > stats[j].Count
	})
	return stats
}

func (s *ScalpingTelemetryStore) GetCycleWinRateTrend(ctx context.Context, chatID string, since time.Time, bucketMinutes int) ([]WinRateBucket, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("scalping telemetry store unavailable")
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 60
	}

	rows, err := s.db.Query(ctx, s.bindQuery(`
		SELECT cycle_at, outcome
		FROM scalping_cycle_telemetry
		WHERE chat_id = ? AND cycle_at >= ? AND outcome IS NOT NULL AND outcome != ''
		ORDER BY cycle_at ASC
	`), strings.TrimSpace(chatID), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("query cycle win rate trend: %w", err)
	}
	defer rows.Close()

	bucketDuration := time.Duration(bucketMinutes) * time.Minute
	bucketSeconds := int64(bucketDuration / time.Second)
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}

	bucketsByStart := make(map[int64]*WinRateBucket)
	orderedStarts := make([]int64, 0)

	for rows.Next() {
		var cycleAt time.Time
		var outcome sql.NullString
		if scanErr := rows.Scan(&cycleAt, &outcome); scanErr != nil {
			return nil, fmt.Errorf("scan win rate trend row: %w", scanErr)
		}

		bucketStartUnix := int64(math.Floor(float64(cycleAt.UTC().Unix())/float64(bucketSeconds))) * bucketSeconds
		bucket := bucketsByStart[bucketStartUnix]
		if bucket == nil {
			bucketStart := time.Unix(bucketStartUnix, 0).UTC()
			bucket = &WinRateBucket{
				BucketStart: bucketStart,
				BucketEnd:   bucketStart.Add(bucketDuration),
			}
			bucketsByStart[bucketStartUnix] = bucket
			orderedStarts = append(orderedStarts, bucketStartUnix)
		}

		bucket.TotalTrades++
		if strings.EqualFold(strings.TrimSpace(outcome.String), "win") {
			bucket.Wins++
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate win rate trend rows: %w", rows.Err())
	}

	sort.Slice(orderedStarts, func(i, j int) bool {
		return orderedStarts[i] < orderedStarts[j]
	})

	buckets := make([]WinRateBucket, 0, len(orderedStarts))
	for _, start := range orderedStarts {
		bucket := bucketsByStart[start]
		bucket.WinRate = safeRate(bucket.Wins, bucket.TotalTrades)
		buckets = append(buckets, *bucket)
	}

	return buckets, nil
}

func (s *ScalpingTelemetryStore) GetCycleCount(ctx context.Context, chatID string, since time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("scalping telemetry store unavailable")
	}

	var totalCycles int
	err := s.db.QueryRow(ctx, s.bindQuery(`
		SELECT COUNT(*)
		FROM scalping_cycle_telemetry
		WHERE chat_id = ? AND cycle_at >= ?
	`), strings.TrimSpace(chatID), since.UTC()).Scan(&totalCycles)
	if err != nil {
		return 0, fmt.Errorf("query cycle count: %w", err)
	}
	return totalCycles, nil
}

func safeRate(wins, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(wins) / float64(total)
}

type sqlDBPool struct {
	db *sql.DB
}

func detectPlaceholderStyle(db database.DBPool) placeholderStyle {
	switch concrete := db.(type) {
	case *database.SQLiteDB:
		return placeholderQuestion
	case *database.PostgresDB:
		return placeholderDollar
	case sqlDBPool:
		return detectPlaceholderStyleFromSQLDB(concrete.db)
	case *sqlDBPool:
		return detectPlaceholderStyleFromSQLDB(concrete.db)
	default:
		return placeholderDollar
	}
}

func detectPlaceholderStyleFromSQLDB(db *sql.DB) placeholderStyle {
	if db == nil {
		return placeholderDollar
	}
	driverType := strings.ToLower(fmt.Sprintf("%T", db.Driver()))
	if strings.Contains(driverType, "sqlite") {
		return placeholderQuestion
	}
	return placeholderDollar
}

func (s *ScalpingTelemetryStore) bindQuery(query string) string {
	if s == nil || s.placeholder == placeholderQuestion {
		return query
	}
	var builder strings.Builder
	index := 1
	for _, r := range query {
		if r == '?' {
			builder.WriteByte('$')
			builder.WriteString(strconv.Itoa(index))
			index++
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func (p sqlDBPool) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return database.SQLRows{Rows: rows}, nil
}

func (p sqlDBPool) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return database.SQLRow{Row: p.db.QueryRowContext(ctx, query, args...)}
}

func (p sqlDBPool) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	res, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return database.SQLResult{Result: res}, nil
}

func (p sqlDBPool) Begin(ctx context.Context) (database.Tx, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return database.SQLTx{Tx: tx}, nil
}
