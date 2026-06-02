package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/app/execution/liveguard"
)

// LiveGuardHandler exposes the live-trading safety guard over HTTP.
//
// All endpoints are mounted under the admin middleware group in routes.go.
// Operators must:
//
//	1. POST /api/v1/admin/live-guard/arm    with the configured confirmation phrase
//	2. POST /api/v1/admin/live-guard/approve/:intentID  to release each pending live order
//	3. POST /api/v1/admin/live-guard/disarm  to stop accepting live orders
//
// The guard is process-local: arming it on one process does not arm others.
// This is intentional — every process that may place live orders must be
// armed independently.
type LiveGuardHandler struct {
	guard *liveguard.Guard
}

// NewLiveGuardHandler constructs a LiveGuardHandler backed by the given guard.
func NewLiveGuardHandler(guard *liveguard.Guard) *LiveGuardHandler {
	return &LiveGuardHandler{guard: guard}
}

// ArmRequest is the body of POST /api/v1/admin/live-guard/arm.
type ArmRequest struct {
	Operator string `json:"operator"`
	Phrase   string `json:"phrase"`
	Reason   string `json:"reason"`
	Force    bool   `json:"force"`
}

// ArmResponse describes the resulting arm state.
type ArmResponse struct {
	Status     string    `json:"status"`
	Armed      bool      `json:"armed"`
	ArmedBy    string    `json:"armed_by"`
	ArmedAt    time.Time `json:"armed_at"`
	ArmReason  string    `json:"arm_reason"`
	PhraseHint string    `json:"phrase_hint"`
}

// ArmLiveTrading transitions the process to "live trading armed". Requires
// the configured confirmation phrase unless force=true (intended for tests).
// Returns 409 if already armed; 401 if phrase is wrong; 400 on bad body.
func (h *LiveGuardHandler) ArmLiveTrading(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	var req ArmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = "unknown"
	}
	if err := h.guard.Arm(operator, req.Phrase, req.Reason, req.Force); err != nil {
		switch {
		case errors.Is(err, liveguard.ErrAlreadyArmed):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, liveguard.ErrBadConfirmation):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "confirmation phrase does not match"})
		case errors.Is(err, liveguard.ErrGuardDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	zaplogrus.Warnf("LIVE_GUARD: process armed for live trading by %q reason=%q force=%t", operator, req.Reason, req.Force)
	st := h.guard.Status()
	c.JSON(http.StatusOK, ArmResponse{
		Status:     "armed",
		Armed:      true,
		ArmedBy:    st.ArmedBy,
		ArmedAt:    st.ArmedAt,
		ArmReason:  st.ArmReason,
		PhraseHint: st.PhraseHint,
	})
}

// DisarmRequest is the body of POST /api/v1/admin/live-guard/disarm.
type DisarmRequest struct {
	Operator string `json:"operator"`
	Reason   string `json:"reason"`
}

// DisarmLiveTrading transitions the process to "live trading disarmed".
// Pending orders remain in the queue and must be explicitly approved or
// rejected. Idempotent: returns 200 even if already disarmed.
func (h *LiveGuardHandler) DisarmLiveTrading(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	var req DisarmRequest
	_ = c.ShouldBindJSON(&req)
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = "unknown"
	}
	h.guard.Disarm(operator, req.Reason)
	zaplogrus.Warnf("LIVE_GUARD: process disarmed by %q reason=%q", operator, req.Reason)
	c.JSON(http.StatusOK, gin.H{
		"status": "disarmed",
	})
}

// ApproveRequest is the body of POST /api/v1/admin/live-guard/approve/:intentID.
type ApproveRequest struct {
	Operator string `json:"operator"`
	Reason   string `json:"reason"`
}

// ApprovePendingOrder releases a held live order. The caller is responsible
// for re-submitting the order through the execution pipeline. This endpoint
// only flips the guard state and returns the order details for re-submission.
func (h *LiveGuardHandler) ApprovePendingOrder(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	intentID := strings.TrimSpace(c.Param("intentID"))
	if intentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "intentID is required"})
		return
	}
	var req ApproveRequest
	_ = c.ShouldBindJSON(&req)
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = "unknown"
	}
	po, err := h.guard.ApproveOrder(intentID, operator)
	if err != nil {
		if errors.Is(err, liveguard.ErrUnknownOrder) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	zaplogrus.Warnf("LIVE_GUARD: pending order %s approved by %q (symbol=%s side=%s amount=%s capped=%s)",
		intentID, operator, po.Symbol, po.Side, po.Amount.String(), po.CappedAmount.String())
	c.JSON(http.StatusOK, gin.H{
		"status":     "approved",
		"intent_id":  po.IntentID,
		"symbol":     po.Symbol,
		"side":       po.Side,
		"type":       po.Type,
		"amount":     po.Amount,
		"capped":     po.CappedAmount,
		"was_capped": !po.CappedAmount.Equal(po.Amount),
		"chat_id":    po.ChatID,
		"strategy":   po.StrategyID,
	})
}

// RejectRequest is the body of POST /api/v1/admin/live-guard/reject/:intentID.
type RejectRequest struct {
	Operator string `json:"operator"`
	Reason   string `json:"reason"`
}

// RejectPendingOrder drops a held live order without placing it.
func (h *LiveGuardHandler) RejectPendingOrder(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	intentID := strings.TrimSpace(c.Param("intentID"))
	if intentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "intentID is required"})
		return
	}
	var req RejectRequest
	_ = c.ShouldBindJSON(&req)
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = "unknown"
	}
	if err := h.guard.RejectPending(intentID, operator, req.Reason); err != nil {
		if errors.Is(err, liveguard.ErrUnknownOrder) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	zaplogrus.Warnf("LIVE_GUARD: pending order %s rejected by %q reason=%q", intentID, operator, req.Reason)
	c.JSON(http.StatusOK, gin.H{
		"status":    "rejected",
		"intent_id": intentID,
	})
}

// LiveGuardStatus returns a point-in-time snapshot of the guard's state.
// Useful for dashboards, smoke tests, and operator confirmation.
func (h *LiveGuardHandler) LiveGuardStatus(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	c.JSON(http.StatusOK, h.guard.Status())
}

// LiveGuardPending returns the list of orders currently held for approval.
func (h *LiveGuardHandler) LiveGuardPending(c *gin.Context) {
	if h.guard == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live guard not configured"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pending": h.guard.PendingOrders(),
		"count":   len(h.guard.PendingOrders()),
	})
}
