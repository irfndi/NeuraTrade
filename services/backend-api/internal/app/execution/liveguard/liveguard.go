// Package liveguard provides process-level safety guards for live trading.
//
// The guard is an explicit, in-memory safety layer that must be passed before
// any live order can be placed by ExecutionActor. It is intentionally
// independent of the per-chat OperationalModeService confirmation flow: both
// must agree before a live order proceeds.
//
// Configuration is via environment variables:
//
//	NEURATRADE_LIVE_GUARD_ENABLED             default true; set false to disable the guard entirely
//	NEURATRADE_LIVE_GUARD_ARM_REQUIRED        default true; require explicit Arm() in this process lifetime
//	NEURATRADE_LIVE_GUARD_SIZE_CAP_PCT        default 0.10 (10% of the requested amount)
//	NEURATRADE_LIVE_GUARD_FIRST_N_HOLD       default 5; first N live orders require manual approval
//	NEURATRADE_LIVE_GUARD_CONFIRM_PHRASE     default auto-generated 64-char phrase; required in Arm body
//
// The guard is process-local: arming the guard on one process does not arm
// other processes. This is by design — every process that may place live
// orders must be armed independently.
package liveguard

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Errors returned by Guard.
var (
	ErrGuardDisabled     = errors.New("live trading guard is disabled (NEURATRADE_LIVE_GUARD_ENABLED=false)")
	ErrNotArmed          = errors.New("live trading not armed: process must be armed via POST /api/v1/admin/live-guard/arm before live orders are accepted")
	ErrAlreadyArmed      = errors.New("live trading already armed; use Disarm first or pass force=true")
	ErrBadConfirmation   = errors.New("confirmation phrase does not match NEURATRADE_LIVE_GUARD_CONFIRM_PHRASE")
	ErrChatNotInLiveMode = errors.New("chat is not in OpModeLive with sufficient confirmations")
	ErrOrderPending      = errors.New("order is pending manual approval; POST /api/v1/admin/live-guard/approve/:intentID to release")
	ErrUnknownOrder      = errors.New("order is not pending approval")
)

// Config holds guard configuration loaded from environment.
type Config struct {
	Enabled       bool
	ArmRequired   bool
	SizeCapPct    decimal.Decimal
	FirstNHold    int
	ConfirmPhrase string
}

// LoadConfig reads guard configuration from the process environment.
func LoadConfig() Config {
	cfg := Config{
		Enabled:       envBool("NEURATRADE_LIVE_GUARD_ENABLED", true),
		ArmRequired:   envBool("NEURATRADE_LIVE_GUARD_ARM_REQUIRED", true),
		SizeCapPct:    envDecimal("NEURATRADE_LIVE_GUARD_SIZE_CAP_PCT", decimal.NewFromFloat(0.10)),
		FirstNHold:    envInt("NEURATRADE_LIVE_GUARD_FIRST_N_HOLD", 5),
		ConfirmPhrase: os.Getenv("NEURATRADE_LIVE_GUARD_CONFIRM_PHRASE"),
	}
	if cfg.SizeCapPct.LessThan(decimal.Zero) {
		cfg.SizeCapPct = decimal.Zero
	}
	if cfg.SizeCapPct.GreaterThan(decimal.NewFromInt(1)) {
		cfg.SizeCapPct = decimal.NewFromInt(1)
	}
	if cfg.FirstNHold < 0 {
		cfg.FirstNHold = 0
	}
	if strings.TrimSpace(cfg.ConfirmPhrase) == "" {
		// Auto-generate a 64-char hex phrase on first load. The phrase is also
		// returned by Status() and surfaced at startup so the operator can
		// capture it for later use. The phrase is process-local and regenerated
		// on every restart.
		cfg.ConfirmPhrase = mustGeneratePhrase(32)
	}
	return cfg
}

// Guard is the process-level live-trading safety guard.
//
// It is safe for concurrent use. State changes are serialized via the embedded
// mutex; Status() acquires only a read lock.
type Guard struct {
	cfg Config

	mu             sync.RWMutex
	armed          bool
	armedBy        string
	armedAt        time.Time
	armReason      string
	placedLive     int
	approved       int
	rejected       int
	capped         int
	pendingByID    map[string]PendingOrder
	recentRejects  []RejectEvent
	disarmHistory  []ArmDisarmEvent
	chatCache      map[string]bool
	chatCacheAt    time.Time
	chatCacheTTL   time.Duration
	phraseLoggedAt time.Time
}

// PendingOrder is a live order held for explicit operator approval.
type PendingOrder struct {
	IntentID     string          `json:"intent_id"`
	Symbol       string          `json:"symbol"`
	Side         string          `json:"side"`
	Type         string          `json:"type"`
	Amount       decimal.Decimal `json:"amount"`
	CappedAmount decimal.Decimal `json:"capped_amount"`
	ChatID       string          `json:"chat_id"`
	StrategyID   string          `json:"strategy_id"`
	QueuedAt     time.Time       `json:"queued_at"`
	QueuedBy     string          `json:"queued_by"`
}

// RejectEvent records a refused live order for audit.
type RejectEvent struct {
	At     time.Time `json:"at"`
	Intent string    `json:"intent_id"`
	Reason string    `json:"reason"`
	ChatID string    `json:"chat_id"`
}

// ArmDisarmEvent records arm/disarm transitions.
type ArmDisarmEvent struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Reason string    `json:"reason"`
	Action string    `json:"action"` // "arm" or "disarm"
}

// New constructs a Guard from the given configuration.
func New(cfg Config) *Guard {
	return &Guard{
		cfg:           cfg,
		pendingByID:   make(map[string]PendingOrder),
		recentRejects: make([]RejectEvent, 0, 16),
		disarmHistory: make([]ArmDisarmEvent, 0, 8),
		chatCache:     make(map[string]bool),
		chatCacheTTL:  2 * time.Second,
	}
}

// Cfg returns the guard's configuration (immutable).
func (g *Guard) Cfg() Config { return g.cfg }

// CheckResult describes the outcome of a live-order check.
type CheckResult struct {
	Allowed      bool            // true if the order may proceed
	CappedAmount decimal.Decimal // possibly-capped amount (zero if not allowed)
	WasCapped    bool            // true if SizeCap was applied
	Pending      bool            // true if order is queued for manual approval
	Reason       string          // human-readable explanation
}

// CheckOrder evaluates a candidate live order against the guard. It returns
// (result, error) where error is non-nil only for hard configuration problems.
//
// chatIsLive is supplied by the caller (typically OperationalModeService) and
// indicates whether the originating chat is in OpModeLive with sufficient
// confirmations. The guard does not call into the mode service directly; this
// keeps the dependency surface small and testable.
func (g *Guard) CheckOrder(intentID, chatID, strategyID, symbol, side, orderType string, requestedAmount decimal.Decimal, chatIsLive bool) (CheckResult, error) {
	if g == nil {
		return CheckResult{Allowed: false, Reason: "guard is nil"}, errors.New("guard is nil")
	}
	if !g.cfg.Enabled {
		return CheckResult{Allowed: false, Reason: "guard disabled"}, ErrGuardDisabled
	}
	if requestedAmount.LessThanOrEqual(decimal.Zero) {
		return CheckResult{Allowed: false, Reason: "non-positive amount"}, nil
	}

	g.mu.RLock()
	armed := g.armed
	armedBy := g.armedBy
	placedLive := g.placedLive
	pending, isPending := g.pendingByID[intentID]
	g.mu.RUnlock()

	if g.cfg.ArmRequired && !armed {
		g.recordReject(intentID, chatID, "not armed")
		return CheckResult{Allowed: false, Reason: "process not armed: POST /api/v1/admin/live-guard/arm"}, ErrNotArmed
	}

	if !chatIsLive {
		g.recordReject(intentID, chatID, "chat not in OpModeLive")
		return CheckResult{Allowed: false, Reason: "chat not in OpModeLive with sufficient confirmations"}, ErrChatNotInLiveMode
	}

	// If we already saw this intentID as pending, surface the same status so
	// the caller knows the operator must approve it. We do NOT auto-allow.
	if isPending {
		return CheckResult{
			Allowed:      false,
			CappedAmount: pending.CappedAmount,
			Pending:      true,
			Reason:       "order pending operator approval",
		}, ErrOrderPending
	}

	cappedAmount := requestedAmount
	wasCapped := false
	if g.cfg.SizeCapPct.LessThan(decimal.NewFromInt(1)) {
		cap := requestedAmount.Mul(g.cfg.SizeCapPct)
		if cap.LessThan(cappedAmount) {
			cappedAmount = cap
			wasCapped = true
		}
	}

	if placedLive < g.cfg.FirstNHold {
		// Hold for manual approval.
		po := PendingOrder{
			IntentID:     intentID,
			Symbol:       symbol,
			Side:         side,
			Type:         orderType,
			Amount:       requestedAmount,
			CappedAmount: cappedAmount,
			ChatID:       chatID,
			StrategyID:   strategyID,
			QueuedAt:     time.Now().UTC(),
			QueuedBy:     armedBy,
		}
		g.mu.Lock()
		g.pendingByID[intentID] = po
		if wasCapped {
			g.capped++
		}
		g.mu.Unlock()
		return CheckResult{
			Allowed:      false,
			CappedAmount: cappedAmount,
			Pending:      true,
			WasCapped:    wasCapped,
			Reason:       fmt.Sprintf("first %d live orders require manual approval; this is order #%d", g.cfg.FirstNHold, placedLive+1),
		}, ErrOrderPending
	}

	if wasCapped {
		g.mu.Lock()
		g.capped++
		g.mu.Unlock()
	}

	return CheckResult{
		Allowed:      true,
		CappedAmount: cappedAmount,
		WasCapped:    wasCapped,
		Reason:       "allowed",
	}, nil
}

// RecordPlaced marks a live order as successfully placed. Callers MUST call
// this when a place-order message survives the actor pipeline and reaches the
// exchange gateway. Without this call, the FirstNHold gate will never release.
func (g *Guard) RecordPlaced(intentID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pendingByID, intentID)
	g.placedLive++
	g.approved++
}

// RecordApproved moves a pending order to approved but not yet placed. The
// caller (approve handler) decides when to actually place.
func (g *Guard) RecordApproved(intentID string) (PendingOrder, error) {
	if g == nil {
		return PendingOrder{}, errors.New("guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	po, ok := g.pendingByID[intentID]
	if !ok {
		return PendingOrder{}, ErrUnknownOrder
	}
	delete(g.pendingByID, intentID)
	g.approved++
	return po, nil
}

// ApproveOrder marks a pending order as approved for placement. Returns the
// order so the caller can re-submit it through the actor pipeline.
func (g *Guard) ApproveOrder(intentID, operator string) (PendingOrder, error) {
	if g == nil {
		return PendingOrder{}, errors.New("guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	po, ok := g.pendingByID[intentID]
	if !ok {
		return PendingOrder{}, ErrUnknownOrder
	}
	delete(g.pendingByID, intentID)
	g.approved++
	po.QueuedBy = operator + " (approved)"
	return po, nil
}

// RejectPending marks a pending order as rejected by the operator.
func (g *Guard) RejectPending(intentID, operator, reason string) error {
	if g == nil {
		return errors.New("guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.pendingByID[intentID]; !ok {
		return ErrUnknownOrder
	}
	delete(g.pendingByID, intentID)
	g.rejected++
	if reason == "" {
		reason = "operator rejected"
	}
	if operator != "" {
		reason = operator + ": " + reason
	}
	g.recentRejects = append(g.recentRejects, RejectEvent{
		At:     time.Now().UTC(),
		Intent: intentID,
		Reason: reason,
	})
	if len(g.recentRejects) > 64 {
		g.recentRejects = g.recentRejects[len(g.recentRejects)-64:]
	}
	return nil
}

// PendingOrders returns a sorted copy of the pending-orders map.
func (g *Guard) PendingOrders() []PendingOrder {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]PendingOrder, 0, len(g.pendingByID))
	for _, po := range g.pendingByID {
		out = append(out, po)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueuedAt.Before(out[j].QueuedAt) })
	return out
}

// Arm transitions the guard to armed. Requires the configured confirmation
// phrase unless force is true (force is intended for test setups only; logging
// will reflect that the arming was forced).
func (g *Guard) Arm(operator, phrase, reason string, force bool) error {
	if g == nil {
		return errors.New("guard is nil")
	}
	if !g.cfg.Enabled {
		return ErrGuardDisabled
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.armed {
		return ErrAlreadyArmed
	}
	if !force && !g.cfg.ArmRequired {
		// fall through: ArmRequired=false means any arm is allowed without phrase
	} else if !force {
		if phrase != g.cfg.ConfirmPhrase {
			g.rejected++
			return ErrBadConfirmation
		}
	}
	now := time.Now().UTC()
	g.armed = true
	g.armedBy = operator
	g.armedAt = now
	g.armReason = reason
	g.disarmHistory = append(g.disarmHistory, ArmDisarmEvent{
		At:     now,
		By:     operator,
		Reason: reason,
		Action: "arm",
	})
	if force {
		g.armReason = "FORCED: " + reason
	}
	return nil
}

// Disarm transitions the guard to disarmed. Safe to call when already
// disarmed.
func (g *Guard) Disarm(operator, reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.armed {
		return
	}
	g.armed = false
	g.armedBy = ""
	g.armedAt = time.Time{}
	g.armReason = ""
	g.disarmHistory = append(g.disarmHistory, ArmDisarmEvent{
		At:     time.Now().UTC(),
		By:     operator,
		Reason: reason,
		Action: "disarm",
	})
	if len(g.disarmHistory) > 32 {
		g.disarmHistory = g.disarmHistory[len(g.disarmHistory)-32:]
	}
}

// IsArmed returns the current arm state.
func (g *Guard) IsArmed() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.armed
}

// Status is a point-in-time snapshot of guard state for /status and admin
// endpoints.
type Status struct {
	Enabled          bool             `json:"enabled"`
	ArmRequired      bool             `json:"arm_required"`
	Armed            bool             `json:"armed"`
	ArmedBy          string           `json:"armed_by,omitempty"`
	ArmedAt          time.Time        `json:"armed_at,omitempty"`
	ArmReason        string           `json:"arm_reason,omitempty"`
	SizeCapPct       decimal.Decimal  `json:"size_cap_pct"`
	FirstNHold       int              `json:"first_n_hold"`
	FirstNHoldPlaced int              `json:"first_n_hold_placed"`
	PlacedLive       int              `json:"placed_live"`
	Approved         int              `json:"approved"`
	Rejected         int              `json:"rejected"`
	Capped           int              `json:"capped"`
	Pending          []PendingOrder   `json:"pending"`
	RecentRejects    []RejectEvent    `json:"recent_rejects,omitempty"`
	ArmDisarmEvents  []ArmDisarmEvent `json:"arm_disarm_events,omitempty"`
	PhraseHint       string           `json:"phrase_hint,omitempty"`
}

// Status returns a snapshot of the guard's state. phraseHint is the last 6
// characters of the configured phrase so an admin endpoint can confirm
// the phrase is in effect without leaking the full secret.
func (g *Guard) Status() Status {
	if g == nil {
		return Status{Enabled: false}
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	pending := make([]PendingOrder, 0, len(g.pendingByID))
	for _, po := range g.pendingByID {
		pending = append(pending, po)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].QueuedAt.Before(pending[j].QueuedAt) })
	rejects := append([]RejectEvent(nil), g.recentRejects...)
	events := append([]ArmDisarmEvent(nil), g.disarmHistory...)
	hint := ""
	if len(g.cfg.ConfirmPhrase) >= 6 {
		hint = "..." + g.cfg.ConfirmPhrase[len(g.cfg.ConfirmPhrase)-6:]
	}
	return Status{
		Enabled:          g.cfg.Enabled,
		ArmRequired:      g.cfg.ArmRequired,
		Armed:            g.armed,
		ArmedBy:          g.armedBy,
		ArmedAt:          g.armedAt,
		ArmReason:        g.armReason,
		SizeCapPct:       g.cfg.SizeCapPct,
		FirstNHold:       g.cfg.FirstNHold,
		FirstNHoldPlaced: g.placedLive,
		PlacedLive:       g.placedLive,
		Approved:         g.approved,
		Rejected:         g.rejected,
		Capped:           g.capped,
		Pending:          pending,
		RecentRejects:    rejects,
		ArmDisarmEvents:  events,
		PhraseHint:       hint,
	}
}

func (g *Guard) recordReject(intentID, chatID, reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rejected++
	g.recentRejects = append(g.recentRejects, RejectEvent{
		At:     time.Now().UTC(),
		Intent: intentID,
		Reason: reason,
		ChatID: chatID,
	})
	if len(g.recentRejects) > 64 {
		g.recentRejects = g.recentRejects[len(g.recentRejects)-64:]
	}
}

// envBool returns the parsed bool or default.
func envBool(name string, def bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// envDecimal returns the parsed decimal or default.
func envDecimal(name string, def decimal.Decimal) decimal.Decimal {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	d, err := decimal.NewFromString(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return d
}

// envInt returns the parsed int or default.
func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n := 0
	for _, r := range strings.TrimSpace(v) {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

// mustGeneratePhrase returns a hex-encoded random phrase of n bytes.
func mustGeneratePhrase(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a deterministic but readable phrase.
		return fmt.Sprintf("neuratrade-live-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
