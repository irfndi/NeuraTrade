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
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionStatus represents the current status of an AI session
type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusPaused    SessionStatus = "paused"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

// SessionState represents the serializable state of an AI agent session.
// It captures all the context needed to resume an interrupted session.
type SessionState struct {
	// ID is the unique session identifier
	ID string `json:"id"`

	// Status is the current session status
	Status SessionStatus `json:"status"`

	// QuestID links this session to a quest (optional)
	QuestID string `json:"quest_id,omitempty"`

	// Symbol is the trading pair being analyzed
	Symbol string `json:"symbol"`

	// CreatedAt is when the session was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the session was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// ConversationHistory contains all messages exchanged with the LLM
	ConversationHistory []llm.Message `json:"conversation_history"`

	// ToolCallsMade contains all tool calls made during this session
	ToolCallsMade []ToolCallRecord `json:"tool_calls_made,omitempty"`

	// LoadedSkills contains the IDs of skills loaded into this session
	LoadedSkills []string `json:"loaded_skills,omitempty"`

	// MarketSnapshot contains the market context at session start
	MarketSnapshot *MarketContextSnapshot `json:"market_snapshot,omitempty"`

	// PortfolioSnapshot contains the portfolio state at session start
	PortfolioSnapshot *PortfolioSnapshot `json:"portfolio_snapshot,omitempty"`

	// AnalysisResult contains the current analysis state
	AnalysisResult *AnalystAnalysis `json:"analysis_result,omitempty"`

	// TradingDecision contains the current trading decision
	TradingDecision *TradingDecision `json:"trading_decision,omitempty"`

	// RiskAssessment contains the current risk assessment
	RiskAssessment *risk.RiskAssessment `json:"risk_assessment,omitempty"`

	// ExecutionResult contains the current execution result
	ExecutionResult *ExecutionLoopResult `json:"execution_result,omitempty"`

	// IterationCount tracks how many LLM iterations have occurred
	IterationCount int `json:"iteration_count"`

	// Metadata contains additional session metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Checksum ensures data integrity
	Checksum string `json:"checksum"`
}

// ToolCallRecord represents a recorded tool call with its result
type ToolCallRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// MarketContextSnapshot captures market state at a point in time
type MarketContextSnapshot struct {
	Symbol       string    `json:"symbol"`
	Exchange     string    `json:"exchange"`
	CurrentPrice float64   `json:"current_price"`
	Volatility   float64   `json:"volatility"`
	Liquidity    float64   `json:"liquidity"`
	Trend        string    `json:"trend"`
	Volume24h    float64   `json:"volume_24h"`
	SnapshotTime time.Time `json:"snapshot_time"`
	SignalsJSON  string    `json:"signals_json,omitempty"` // JSON-encoded signals
}

// PortfolioSnapshot captures portfolio state at a point in time
type PortfolioSnapshot struct {
	TotalEquity      float64   `json:"total_equity"`
	AvailableCapital float64   `json:"available_capital"`
	CurrentDrawdown  float64   `json:"current_drawdown"`
	OpenPositions    int       `json:"open_positions"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	SnapshotTime     time.Time `json:"snapshot_time"`
	PositionsJSON    string    `json:"positions_json,omitempty"` // JSON-encoded positions
}

// SessionStateRepository defines the interface for session state persistence
type SessionStateRepository interface {
	// Save persists a session state
	Save(ctx context.Context, state *SessionState) error

	// Load retrieves a session state by ID
	Load(ctx context.Context, id string) (*SessionState, error)

	// LoadByQuest retrieves a session state by quest ID
	LoadByQuest(ctx context.Context, questID string) (*SessionState, error)

	// ListActive returns all active sessions
	ListActive(ctx context.Context, limit int) ([]*SessionState, error)

	// Delete removes a session state
	Delete(ctx context.Context, id string) error

	// UpdateStatus updates just the session status
	UpdateStatus(ctx context.Context, id string, status SessionStatus) error
}

// SessionSerializer handles serialization and deserialization of session state
type SessionSerializer struct {
	repo SessionStateRepository
}

// NewSessionSerializer creates a new session serializer
func NewSessionSerializer(repo SessionStateRepository) *SessionSerializer {
	return &SessionSerializer{repo: repo}
}

func (s *SessionSerializer) Serialize(state *SessionState) ([]byte, error) {
	state.UpdatedAt = time.Now()
	state.Checksum = s.calculateChecksum(state)

	return json.Marshal(state)
}

// Deserialize converts JSON bytes to a session state
func (s *SessionSerializer) Deserialize(data []byte) (*SessionState, error) {
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to deserialize session state: %w", err)
	}

	// Verify checksum
	expectedChecksum := s.calculateChecksum(&state)
	if state.Checksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: session data may be corrupted")
	}

	return &state, nil
}

// Save persists a session state
func (s *SessionSerializer) Save(ctx context.Context, state *SessionState) error {
	if state.ID == "" {
		state.ID = generateSessionID()
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}
	state.UpdatedAt = time.Now()
	state.Checksum = s.calculateChecksum(state)

	return s.repo.Save(ctx, state)
}

// Load retrieves a session state by ID
func (s *SessionSerializer) Load(ctx context.Context, id string) (*SessionState, error) {
	return s.repo.Load(ctx, id)
}

// LoadByQuest retrieves a session state by quest ID
func (s *SessionSerializer) LoadByQuest(ctx context.Context, questID string) (*SessionState, error) {
	return s.repo.LoadByQuest(ctx, questID)
}

// CreateFromExecutionLoop creates a session state from an execution loop result
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

	// Convert tool call results to records
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
		IterationCount:      result.Iterations,
		Metadata:            result.Metadata,
	}
	state.Checksum = s.calculateChecksum(state)

	return state
}

// calculateChecksum generates a SHA-256 checksum of the session state
func (s *SessionSerializer) calculateChecksum(state *SessionState) string {
	// Create a copy without the checksum for hashing
	copy := *state
	copy.Checksum = ""

	data, err := json.Marshal(copy)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), generateRandomString(8))
}

func generateRandomString(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based seed if crypto rand fails
		for i := range bytes {
			bytes[i] = byte(time.Now().UnixNano() % 256)
		}
	}
	return hex.EncodeToString(bytes)[:length]
}

func convertMarketContext(ctx MarketContext) *MarketContextSnapshot {
	var signalsJSON string
	if len(ctx.Signals) > 0 {
		if data, err := json.Marshal(ctx.Signals); err == nil {
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

// DatabaseSessionRepository implements SessionStateRepository using PostgreSQL
type DatabaseSessionRepository struct {
	db *pgxpool.Pool
}

// NewDatabaseSessionRepository creates a new database-backed repository
func NewDatabaseSessionRepository(db *pgxpool.Pool) *DatabaseSessionRepository {
	return &DatabaseSessionRepository{db: db}
}

// Save persists a session state to the database
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

// Load retrieves a session state from the database
func (r *DatabaseSessionRepository) Load(ctx context.Context, id string) (*SessionState, error) {
	query := `
		SELECT state_data FROM ai_sessions WHERE id = $1
	`

	var data []byte
	err := r.db.QueryRow(ctx, query, id).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	serializer := NewSessionSerializer(nil)
	return serializer.Deserialize(data)
}

// LoadByQuest retrieves a session state by quest ID
func (r *DatabaseSessionRepository) LoadByQuest(ctx context.Context, questID string) (*SessionState, error) {
	query := `
		SELECT state_data FROM ai_sessions WHERE quest_id = $1 ORDER BY updated_at DESC LIMIT 1
	`

	var data []byte
	err := r.db.QueryRow(ctx, query, questID).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("failed to load session by quest: %w", err)
	}

	serializer := NewSessionSerializer(nil)
	return serializer.Deserialize(data)
}

// ListActive returns all active sessions
func (r *DatabaseSessionRepository) ListActive(ctx context.Context, limit int) ([]*SessionState, error) {
	query := `
		SELECT state_data FROM ai_sessions 
		WHERE status = 'active' 
		ORDER BY updated_at DESC 
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list active sessions: %w", err)
	}
	defer rows.Close()

	serializer := NewSessionSerializer(nil)
	var sessions []*SessionState

	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("failed to scan session row: %w", err)
		}

		state, err := serializer.Deserialize(data)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize session: %w", err)
		}

		sessions = append(sessions, state)
	}

	return sessions, nil
}

// Delete removes a session state from the database
func (r *DatabaseSessionRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM ai_sessions WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// UpdateStatus updates just the session status
func (r *DatabaseSessionRepository) UpdateStatus(ctx context.Context, id string, status SessionStatus) error {
	query := `UPDATE ai_sessions SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := r.db.Exec(ctx, query, string(status), time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}
	return nil
}

// nullString returns a nullable string
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Ensure DatabaseSessionRepository implements SessionStateRepository
var _ SessionStateRepository = (*DatabaseSessionRepository)(nil)

// Ensure compatibility with database.PostgresDB if needed
// The repository uses pgxpool.Pool directly for flexibility

// NewSessionStateRepositoryFromPostgres creates a repository from PostgresDB
func NewSessionStateRepositoryFromPostgres(db *database.PostgresDB) *DatabaseSessionRepository {
	if db == nil || db.Pool == nil {
		return nil
	}
	return NewDatabaseSessionRepository(db.Pool)
}
