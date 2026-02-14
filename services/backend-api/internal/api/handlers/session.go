package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

type SessionHandler struct {
	serializer *services.SessionSerializer
	repo       services.SessionStateRepository
}

func NewSessionHandler(serializer *services.SessionSerializer, repo services.SessionStateRepository) *SessionHandler {
	return &SessionHandler{
		serializer: serializer,
		repo:       repo,
	}
}

type SessionResponse struct {
	Status   string                   `json:"status"`
	Message  string                   `json:"message,omitempty"`
	Data     *services.SessionState   `json:"data,omitempty"`
	Sessions []*services.SessionState `json:"sessions,omitempty"`
}

func (h *SessionHandler) GetSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	session, err := h.serializer.Load(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		Status: "success",
		Data:   session,
	})
}

func (h *SessionHandler) GetSessionByQuest(c *gin.Context) {
	questID := c.Query("quest_id")
	if questID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quest_id query parameter is required"})
		return
	}

	session, err := h.serializer.LoadByQuest(c.Request.Context(), questID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found for quest"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		Status: "success",
		Data:   session,
	})
}

func (h *SessionHandler) ListActiveSessions(c *gin.Context) {
	limit := 10
	if limitStr := c.Query("limit"); limitStr != "" {
		if l := parseInt(limitStr, 10); l > 0 {
			limit = l
		}
	}

	sessions, err := h.serializer.ListActive(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sessions"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		Status:   "success",
		Sessions: sessions,
	})
}

func (h *SessionHandler) UpdateSessionStatus(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validStatuses := map[string]bool{
		string(services.SessionStatusActive):    true,
		string(services.SessionStatusPaused):    true,
		string(services.SessionStatusCompleted): true,
		string(services.SessionStatusFailed):    true,
	}

	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	err := h.repo.UpdateStatus(c.Request.Context(), sessionID, services.SessionStatus(req.Status))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update session status"})
		return
	}

	session, _ := h.serializer.Load(c.Request.Context(), sessionID)
	c.JSON(http.StatusOK, SessionResponse{
		Status:  "success",
		Message: "status updated",
		Data:    session,
	})
}

func (h *SessionHandler) DeleteSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	err := h.repo.Delete(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete session"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		Status:  "success",
		Message: "session deleted",
	})
}

func (h *SessionHandler) PauseSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	err := h.repo.UpdateStatus(c.Request.Context(), sessionID, services.SessionStatusPaused)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	session, _ := h.serializer.Load(c.Request.Context(), sessionID)
	c.JSON(http.StatusOK, SessionResponse{
		Status:  "success",
		Message: "session paused",
		Data:    session,
	})
}

func (h *SessionHandler) ResumeSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	session, err := h.serializer.Load(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	if session.Status != services.SessionStatusPaused {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only paused sessions can be resumed"})
		return
	}

	session.Status = services.SessionStatusActive
	session.UpdatedAt = time.Now()
	session.Checksum = ""

	if err := h.serializer.Save(c.Request.Context(), session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resume session"})
		return
	}

	c.JSON(http.StatusOK, SessionResponse{
		Status:  "success",
		Message: "session resumed - ready to continue execution",
		Data:    session,
	})
}

func (h *SessionHandler) GetSessionHistory(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
		return
	}

	session, err := h.serializer.Load(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	type HistoryResponse struct {
		Status              string                          `json:"status"`
		SessionID           string                          `json:"session_id"`
		QuestID             string                          `json:"quest_id"`
		Symbol              string                          `json:"symbol"`
		IterationCount      int                             `json:"iteration_count"`
		ConversationHistory []interface{}                   `json:"conversation_history"`
		ToolCallsMade       []services.ToolCallRecord       `json:"tool_calls_made"`
		LoadedSkills        []string                        `json:"loaded_skills"`
		MarketSnapshot      *services.MarketContextSnapshot `json:"market_snapshot,omitempty"`
		PortfolioSnapshot   *services.PortfolioSnapshot     `json:"portfolio_snapshot,omitempty"`
	}

	history := make([]interface{}, len(session.ConversationHistory))
	for i, msg := range session.ConversationHistory {
		history[i] = msg
	}

	c.JSON(http.StatusOK, HistoryResponse{
		Status:              "success",
		SessionID:           session.ID,
		QuestID:             session.QuestID,
		Symbol:              session.Symbol,
		IterationCount:      session.IterationCount,
		ConversationHistory: history,
		ToolCallsMade:       session.ToolCallsMade,
		LoadedSkills:        session.LoadedSkills,
		MarketSnapshot:      session.MarketSnapshot,
		PortfolioSnapshot:   session.PortfolioSnapshot,
	})
}

func parseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return defaultValue
		}
		result = result*10 + int(c-'0')
	}
	return result
}
