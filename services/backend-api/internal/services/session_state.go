package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusPaused    SessionStatus = "paused"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

type SessionState struct {
	ID                  string                 `json:"id"`
	Status              SessionStatus          `json:"status"`
	QuestID             string                 `json:"quest_id,omitempty"`
	Symbol              string                 `json:"symbol"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	ConversationHistory []llm.Message          `json:"conversation_history"`
	ToolCallsMade       []ToolCallRecord       `json:"tool_calls_made,omitempty"`
	LoadedSkills        []string               `json:"loaded_skills,omitempty"`
	MarketSnapshot      *MarketContextSnapshot `json:"market_snapshot,omitempty"`
	PortfolioSnapshot   *PortfolioSnapshot     `json:"portfolio_snapshot,omitempty"`
	AnalysisResult      *AnalystAnalysis       `json:"analysis_result,omitempty"`
	TradingDecision     *TradingDecision       `json:"trading_decision,omitempty"`
	RiskAssessment      *risk.RiskAssessment   `json:"risk_assessment,omitempty"`
	ExecutionResult     *ExecutionLoopResult   `json:"execution_result,omitempty"`
	IterationCount      int                    `json:"iteration_count"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
	Checksum            string                 `json:"checksum"`
}

type ToolCallRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type MarketContextSnapshot struct {
	Symbol       string    `json:"symbol"`
	Exchange     string    `json:"exchange"`
	CurrentPrice float64   `json:"current_price"`
	Volatility   float64   `json:"volatility"`
	Liquidity    float64   `json:"liquidity"`
	Trend        string    `json:"trend"`
	Volume24h    float64   `json:"volume_24h"`
	SnapshotTime time.Time `json:"snapshot_time"`
	SignalsJSON  string    `json:"signals_json,omitempty"`
}

type PortfolioSnapshot struct {
	TotalEquity      float64   `json:"total_equity"`
	AvailableCapital float64   `json:"available_capital"`
	CurrentDrawdown  float64   `json:"current_drawdown"`
	OpenPositions    int       `json:"open_positions"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	SnapshotTime     time.Time `json:"snapshot_time"`
	PositionsJSON    string    `json:"positions_json,omitempty"`
}

type SessionStateRepository interface {
	Save(ctx context.Context, state *SessionState) error
	Load(ctx context.Context, id string) (*SessionState, error)
	LoadByQuest(ctx context.Context, questID string) (*SessionState, error)
	ListActive(ctx context.Context, limit int) ([]*SessionState, error)
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id string, status SessionStatus) error
}

type SessionSerializer struct {
	repo SessionStateRepository
}

func NewSessionSerializer(repo SessionStateRepository) *SessionSerializer {
	return &SessionSerializer{repo: repo}
}

func (s *SessionSerializer) Serialize(state *SessionState) ([]byte, error) {
	state.UpdatedAt = time.Now()
	checksum, err := s.calculateChecksum(state)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}
	state.Checksum = checksum
	return json.Marshal(state)
}

func (s *SessionSerializer) Deserialize(data []byte) (*SessionState, error) {
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to deserialize session state: %w", err)
	}
	expectedChecksum, err := s.calculateChecksum(&state)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate expected checksum: %w", err)
	}
	if state.Checksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: session data may be corrupted")
	}
	return &state, nil
}

func (s *SessionSerializer) Save(ctx context.Context, state *SessionState) error {
	if state.ID == "" {
		state.ID = generateSessionID()
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}
	state.UpdatedAt = time.Now()
	checksum, err := s.calculateChecksum(state)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}
	state.Checksum = checksum
	return s.repo.Save(ctx, state)
}

func (s *SessionSerializer) Load(ctx context.Context, id string) (*SessionState, error) {
	return s.repo.Load(ctx, id)
}

func (s *SessionSerializer) LoadByQuest(ctx context.Context, questID string) (*SessionState, error) {
	return s.repo.LoadByQuest(ctx, questID)
}

func (s *SessionSerializer) CreateFromExecutionLoop(
	loopID string,
	symbol string,
	questID string,
	marketCtx MarketContext,
	portfolio PortfolioState,
	messages []llm.Message,
	toolCalls []ToolCallResult,
	skills []string,
	result *ExecutionLoopResult,
) *SessionState {
	now := time.Now()
	toolCallRecords := make([]ToolCallRecord, len(toolCalls))
	for i, tc := range toolCalls {
		toolCallRecords[i] = ToolCallRecord{
			ID:        tc.ToolID,
			Name:      tc.ToolName,
			Arguments: tc.Arguments,
			Result:    tc.Result,
			Error:     tc.Error,
			Timestamp: now.Add(-tc.Duration),
		}
	}

	iterationCount := 0
	metadata := make(map[string]string)
	if result != nil {
		iterationCount = result.Iterations
		metadata = result.Metadata
	}

	state := &SessionState{
		ID:                  loopID,
		Status:              SessionStatusActive,
		QuestID:             questID,
		Symbol:              symbol,
		CreatedAt:           now,
		UpdatedAt:           now,
		ConversationHistory: messages,
		ToolCallsMade:       toolCallRecords,
		LoadedSkills:        skills,
		MarketSnapshot:      convertMarketContext(marketCtx),
		PortfolioSnapshot:   convertPortfolioState(portfolio),
		ExecutionResult:     result,
		IterationCount:      iterationCount,
		Metadata:            metadata,
	}
	checksum, err := s.calculateChecksum(state)
	if err != nil {
		telemetry.Logger().Warn("Failed to calculate checksum for new session state", "error", err, "session_id", loopID)
		// Continue with empty checksum - will be recalculated on save
	}
	state.Checksum = checksum
	return state
}

func (s *SessionSerializer) calculateChecksum(state *SessionState) (string, error) {
	stateCopy := *state
	stateCopy.Checksum = ""
	// Exclude UpdatedAt from checksum to allow timestamp updates without breaking integrity
	stateCopy.UpdatedAt = time.Time{}
	data, err := json.Marshal(stateCopy)
	if err != nil {
		return "", fmt.Errorf("failed to marshal session state for checksum: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), generateRandomString(8))
}

func generateRandomString(length int) string {
	bytes := make([]byte, length)
	n, err := rand.Read(bytes)
	if err != nil || n != length {
		// crypto/rand failure is critical - fall back to timestamp-based ID
		telemetry.Logger().Error("crypto/rand failed, using fallback random generation", "error", err, "bytes_read", n)
		// Use timestamp + counter as fallback for uniqueness
		fallback := fmt.Sprintf("%d%d", time.Now().UnixNano(), len(bytes))
		return fallback[:min(length, len(fallback))]
	}
	return hex.EncodeToString(bytes)[:length]
}

func convertMarketContext(ctx MarketContext) *MarketContextSnapshot {
	var signalsJSON string
	if len(ctx.Signals) > 0 {
		data, err := json.Marshal(ctx.Signals)
		if err != nil {
			telemetry.Logger().Warn("Failed to marshal market context signals", "error", err, "symbol", ctx.Symbol)
		} else {
			signalsJSON = string(data)
		}
	}
	return &MarketContextSnapshot{
		Symbol:       ctx.Symbol,
		Exchange:     "",
		CurrentPrice: ctx.CurrentPrice,
		Volatility:   ctx.Volatility,
		Liquidity:    ctx.Liquidity,
		Trend:        ctx.Trend,
		Volume24h:    ctx.Volume24h,
		SnapshotTime: time.Now(),
		SignalsJSON:  signalsJSON,
	}
}

func convertPortfolioState(p PortfolioState) *PortfolioSnapshot {
	return &PortfolioSnapshot{
		TotalEquity:      p.TotalValue,
		AvailableCapital: p.AvailableCash,
		CurrentDrawdown:  p.CurrentDrawdown,
		OpenPositions:    p.OpenPositions,
		UnrealizedPnL:    p.UnrealizedPnL,
		SnapshotTime:     time.Now(),
		PositionsJSON:    "",
	}
}

type DatabaseSessionRepository struct {
	db *pgxpool.Pool
}

func NewDatabaseSessionRepository(db *pgxpool.Pool) *DatabaseSessionRepository {
	return &DatabaseSessionRepository{db: db}
}

func (r *DatabaseSessionRepository) Save(ctx context.Context, state *SessionState) error {
	serializer := NewSessionSerializer(nil)
	data, err := serializer.Serialize(state)
	if err != nil {
		return fmt.Errorf("failed to serialize session: %w", err)
	}
	query := `
		INSERT INTO ai_sessions (id, quest_id, symbol, status, state_data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			state_data = EXCLUDED.state_data,
			updated_at = EXCLUDED.updated_at
	`
	_, err = r.db.Exec(ctx, query,
		state.ID,
		nullString(state.QuestID),
		state.Symbol,
		string(state.Status),
		data,
		state.CreatedAt,
		state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (r *DatabaseSessionRepository) Load(ctx context.Context, id string) (*SessionState, error) {
	query := `SELECT state_data FROM ai_sessions WHERE id = $1`
	var data []byte
	err := r.db.QueryRow(ctx, query, id).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}
	serializer := NewSessionSerializer(nil)
	return serializer.Deserialize(data)
}

func (r *DatabaseSessionRepository) LoadByQuest(ctx context.Context, questID string) (*SessionState, error) {
	query := `SELECT state_data FROM ai_sessions WHERE quest_id = $1 ORDER BY updated_at DESC LIMIT 1`
	var data []byte
	err := r.db.QueryRow(ctx, query, questID).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to load session by quest: %w", err)
	}
	serializer := NewSessionSerializer(nil)
	return serializer.Deserialize(data)
}

func (r *DatabaseSessionRepository) ListActive(ctx context.Context, limit int) ([]*SessionState, error) {
	query := `SELECT state_data FROM ai_sessions WHERE status = 'active' ORDER BY updated_at DESC LIMIT $1`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list active sessions: %w", err)
	}
	defer rows.Close()
	serializer := NewSessionSerializer(nil)
	var sessions []*SessionState
	skippedCount := 0
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			telemetry.Logger().Warn("Failed to scan session row", "error", err)
			skippedCount++
			continue
		}
		state, err := serializer.Deserialize(data)
		if err != nil {
			telemetry.Logger().Warn("Failed to deserialize session, data may be corrupted", "error", err, "data_len", len(data))
			skippedCount++
			continue
		}
		sessions = append(sessions, state)
	}
	if skippedCount > 0 {
		telemetry.Logger().Info("ListActive completed with skipped sessions", "returned", len(sessions), "skipped", skippedCount)
	}
	return sessions, nil
}

func (r *DatabaseSessionRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM ai_sessions WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

func (r *DatabaseSessionRepository) UpdateStatus(ctx context.Context, id string, status SessionStatus) error {
	state, err := r.Load(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to load session for status update: %w", err)
	}
	state.Status = status
	state.UpdatedAt = time.Now()
	state.Checksum = ""
	return r.Save(ctx, state)
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var _ SessionStateRepository = (*DatabaseSessionRepository)(nil)

func NewSessionStateRepositoryFromPostgres(db *database.PostgresDB) *DatabaseSessionRepository {
	if db == nil || db.Pool == nil {
		return nil
	}
	return NewDatabaseSessionRepository(db.Pool)
}
