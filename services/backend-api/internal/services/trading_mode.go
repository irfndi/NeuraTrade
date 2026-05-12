package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/logging"
)

// OperationalMode represents the operational mode of the trading system
type OperationalMode string

const (
	// OpModeDry means shadow/dry mode - no orders executed
	OpModeDry OperationalMode = "dry"
	// OpModeLive means real trading - actual orders executed on exchanges
	OpModeLive OperationalMode = "live"
	// Agent trading styles
	ModeConservative OperationalMode = "conservative"
	ModeModerate     OperationalMode = "moderate"
	ModeAggressive   OperationalMode = "aggressive"
	ModePaper        OperationalMode = "paper"
)

const (
	envFeaturesPaperTrading = "FEATURES_PAPER_TRADING"
	envFeaturePaperTrading  = "FEATURE_PAPER_TRADING"
	envFeaturesRealTrading  = "FEATURES_REAL_TRADING"
	envFeatureRealTrading   = "FEATURE_REAL_TRADING"
)

// OperationalModeState represents the current operational mode state
type OperationalModeState struct {
	Mode          OperationalMode `json:"mode"`
	ChatID        string          `json:"chat_id"`
	ChangedAt     time.Time       `json:"changed_at"`
	ChangedBy     string          `json:"changed_by"`
	PreviousMode  OperationalMode `json:"previous_mode"`
	Confirmations int             `json:"confirmations"` // Number of confirmations required for live mode
}

// OperationalModeConfig holds configuration for the operational mode service
type OperationalModeConfig struct {
	DefaultMode         OperationalMode `json:"default_mode"`
	RequireConfirmation bool            `json:"require_confirmation"`
	ConfirmationCount   int             `json:"confirmation_count"`
}

// DefaultOperationalModeConfig returns the default operational mode configuration
func DefaultOperationalModeConfig() OperationalModeConfig {
	return OperationalModeConfig{
		DefaultMode:         OpModeDry,
		RequireConfirmation: true,
		ConfirmationCount:   2, // Require 2 confirmations to switch to live mode
	}
}

func runtimeModeOverrideFromEnv() (OperationalMode, bool) {
	paperEnabled, paperSet := boolEnvAny(true, envFeaturesPaperTrading, envFeaturePaperTrading)
	realEnabled, realSet := boolEnvAny(false, envFeaturesRealTrading, envFeatureRealTrading)
	if paperSet && paperEnabled && (!realSet || !realEnabled) {
		return ModePaper, true
	}
	if realSet && !realEnabled {
		return OpModeDry, true
	}
	return "", false
}

// boolEnvAny treats unrecognized present values as explicit false so typoed
// safety gates cannot silently preserve a persisted live mode. When aliases
// conflict, preferSafeTrue selects the value that keeps execution non-live.
func boolEnvAny(preferSafeTrue bool, names ...string) (bool, bool) {
	seenTrue := false
	seenFalse := false
	for _, name := range names {
		raw, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			seenTrue = true
		case "0", "false", "no", "off":
			seenFalse = true
		default:
			seenFalse = true
		}
	}
	if seenTrue && seenFalse {
		return preferSafeTrue, true
	}
	if seenTrue {
		return true, true
	}
	if seenFalse {
		return false, true
	}
	return false, false
}

// OperationalModeService manages the operational mode state
type OperationalModeService struct {
	config OperationalModeConfig
	db     DBPool
	logger logging.Logger
	mu     sync.RWMutex
	states map[string]*OperationalModeState // chatID -> state
}

// NewOperationalModeService creates a new operational mode service
func NewOperationalModeService(db DBPool, config OperationalModeConfig, logger logging.Logger) *OperationalModeService {
	s := &OperationalModeService{
		config: config,
		db:     db,
		logger: logger,
		states: make(map[string]*OperationalModeState),
	}

	// Ensure storage exists for environments where migrations are partial or delayed
	// (common in local SQLite setups).
	s.ensureStorage()

	// Load existing states from database
	s.loadStatesFromDB()

	return s
}

func (s *OperationalModeService) ensureStorage() {
	if s.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queries := []string{
		`CREATE TABLE IF NOT EXISTS trading_mode_states (
			chat_id TEXT PRIMARY KEY,
			mode TEXT NOT NULL DEFAULT 'dry',
			changed_at TIMESTAMP NOT NULL,
			changed_by TEXT,
			previous_mode TEXT,
			confirmations INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_mode_states_mode ON trading_mode_states(mode)`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(ctx, query); err != nil {
			s.logger.WithError(err).Warn("Failed to ensure trading_mode_states storage")
			return
		}
	}
}

// GetMode returns the current operational mode for a chat
func (s *OperationalModeService) GetMode(chatID string) OperationalMode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if state, ok := s.states[chatID]; ok {
		return state.Mode
	}
	return s.config.DefaultMode
}

// GetState returns the full operational mode state for a chat
func (s *OperationalModeService) GetState(chatID string) *OperationalModeState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if state, ok := s.states[chatID]; ok {
		// Return a copy
		copy := *state
		return &copy
	}

	// Return default state
	return &OperationalModeState{
		Mode:          s.config.DefaultMode,
		ChatID:        chatID,
		ChangedAt:     time.Now(),
		Confirmations: 0,
	}
}

// SetMode sets the operational mode for a chat
func (s *OperationalModeService) SetMode(ctx context.Context, chatID string, mode OperationalMode, changedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var previousMode OperationalMode
	if state, ok := s.states[chatID]; ok {
		previousMode = state.Mode
	} else {
		previousMode = s.config.DefaultMode
	}

	// If switching to live mode and confirmation is required
	if mode == OpModeLive && s.config.RequireConfirmation {
		if state, ok := s.states[chatID]; ok {
			if state.Confirmations < s.config.ConfirmationCount {
				return fmt.Errorf("switching to live mode requires %d confirmations (current: %d)",
					s.config.ConfirmationCount, state.Confirmations)
			}
		} else {
			return fmt.Errorf("switching to live mode requires %d confirmations", s.config.ConfirmationCount)
		}
	}

	now := time.Now()
	state := &OperationalModeState{
		Mode:          mode,
		ChatID:        chatID,
		ChangedAt:     now,
		ChangedBy:     changedBy,
		PreviousMode:  previousMode,
		Confirmations: 0, // Reset confirmations after mode change
	}

	s.states[chatID] = state

	// Persist to database
	if err := s.persistState(ctx, state); err != nil {
		s.logger.WithError(err).Error("Failed to persist operational mode state")
		return err
	}

	s.logger.Info("Operational mode changed",
		"chat_id", chatID,
		"previous_mode", previousMode,
		"new_mode", mode,
		"changed_by", changedBy)

	return nil
}

// AddConfirmation adds a confirmation for switching to live mode
func (s *OperationalModeService) AddConfirmation(ctx context.Context, chatID string, confirmedBy string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[chatID]
	if !ok {
		state = &OperationalModeState{
			Mode:          s.config.DefaultMode,
			ChatID:        chatID,
			ChangedAt:     time.Now(),
			Confirmations: 0,
		}
		s.states[chatID] = state
	}

	// Only add confirmations when in dry mode
	if state.Mode == OpModeLive {
		return 0, fmt.Errorf("already in live mode")
	}

	state.Confirmations++
	state.ChangedBy = confirmedBy
	state.ChangedAt = time.Now()

	// Persist to database
	if err := s.persistState(ctx, state); err != nil {
		s.logger.WithError(err).Error("Failed to persist operational mode confirmation")
		return state.Confirmations, err
	}

	s.logger.Info("Operational mode confirmation added",
		"chat_id", chatID,
		"confirmations", state.Confirmations,
		"required", s.config.ConfirmationCount,
		"confirmed_by", confirmedBy)

	return state.Confirmations, nil
}

// ResetConfirmations resets all confirmations for a chat
func (s *OperationalModeService) ResetConfirmations(ctx context.Context, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state, ok := s.states[chatID]; ok {
		state.Confirmations = 0
		state.ChangedAt = time.Now()

		if err := s.persistState(ctx, state); err != nil {
			return err
		}
	}

	s.logger.Info("Operational mode confirmations reset", "chat_id", chatID)
	return nil
}

// IsDry returns true if the system is in dry mode
func (s *OperationalModeService) IsDry(chatID string) bool {
	return s.GetMode(chatID) == OpModeDry
}

// IsPaper returns true if the system is in paper mode.
func (s *OperationalModeService) IsPaper(chatID string) bool {
	return s.GetMode(chatID) == ModePaper
}

// IsLive returns true if the system is in live mode
func (s *OperationalModeService) IsLive(chatID string) bool {
	return s.GetMode(chatID) == OpModeLive
}

// RequiredConfirmations returns the configured number of confirmations needed
// before switching to live mode.
func (s *OperationalModeService) RequiredConfirmations() int {
	return s.config.ConfirmationCount
}

// ToggleMode toggles between dry and live mode
func (s *OperationalModeService) ToggleMode(ctx context.Context, chatID string, changedBy string) (OperationalMode, error) {
	currentMode := s.GetMode(chatID)
	var newMode OperationalMode

	if currentMode == OpModeDry {
		newMode = OpModeLive
	} else {
		newMode = OpModeDry
	}

	if err := s.SetMode(ctx, chatID, newMode, changedBy); err != nil {
		return currentMode, err
	}

	return newMode, nil
}

// persistState persists the operational mode state to the database
func (s *OperationalModeService) persistState(ctx context.Context, state *OperationalModeState) error {
	if s.db == nil {
		return nil
	}

	query := `
		INSERT INTO trading_mode_states (chat_id, mode, changed_at, changed_by, previous_mode, confirmations)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id) DO UPDATE SET
			mode = EXCLUDED.mode,
			changed_at = EXCLUDED.changed_at,
			changed_by = EXCLUDED.changed_by,
			previous_mode = EXCLUDED.previous_mode,
			confirmations = EXCLUDED.confirmations
	`

	_, err := s.db.Exec(ctx, query,
		state.ChatID,
		string(state.Mode),
		state.ChangedAt,
		state.ChangedBy,
		string(state.PreviousMode),
		state.Confirmations,
	)

	return err
}

// loadStatesFromDB loads existing operational mode states from the database
func (s *OperationalModeService) loadStatesFromDB() {
	if s.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT chat_id, mode, changed_at, changed_by, previous_mode, confirmations
		FROM trading_mode_states
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to load operational mode states from database")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var state OperationalModeState
		var modeStr, prevModeStr string

		err := rows.Scan(
			&state.ChatID,
			&modeStr,
			&state.ChangedAt,
			&state.ChangedBy,
			&prevModeStr,
			&state.Confirmations,
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to scan operational mode state")
			continue
		}

		state.Mode = OperationalMode(modeStr)
		state.PreviousMode = OperationalMode(prevModeStr)
		s.states[state.ChatID] = &state
	}

	s.logger.Info("Loaded operational mode states from database", "count", len(s.states))
}

// GetModeInfo returns a human-readable description of the current mode
func (s *OperationalModeService) GetModeInfo(chatID string) string {
	state := s.GetState(chatID)

	var status string
	switch state.Mode {
	case OpModeLive:
		status = "🔴 LIVE MODE (Real Trading)\n\n" +
			"• Real orders WILL be executed\n" +
			"• Real money is at risk\n" +
			"• Be cautious!"
	case ModePaper:
		status = "🧪 PAPER MODE (Simulated Orders)\n\n" +
			"• Orders are simulated through the autonomy paper stage\n" +
			"• No real exchange orders will be sent\n" +
			"• Useful for validating execution behavior before live mode"
	default:
		status = "🧪 DRY MODE (Shadow/No Order Execution)\n\n" +
			"• No orders will be executed\n" +
			"• Strategy runs stay in shadow observation mode\n" +
			"• Safe for validating gating and reasoning"
	}

	status += fmt.Sprintf("\n\nChanged: %s", state.ChangedAt.Format("2006-01-02 15:04:05"))

	if state.Confirmations > 0 && state.Mode == OpModeDry {
		status += fmt.Sprintf("\nConfirmations: %d/%d", state.Confirmations, s.config.ConfirmationCount)
	}

	return status
}
