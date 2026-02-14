package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

// SentimentHandler handles sentiment API endpoints
type SentimentHandler struct {
	sentimentService *services.SentimentService
}

// NewSentimentHandler creates a new sentiment handler
func NewSentimentHandler(sentimentService *services.SentimentService) *SentimentHandler {
	return &SentimentHandler{
		sentimentService: sentimentService,
	}
}

// GetSentimentResponse represents the API response for sentiment
type GetSentimentResponse struct {
	Status    string                        `json:"status"`
	Data      *services.AggregatedSentiment `json:"data,omitempty"`
	News      []services.NewsSentiment      `json:"news,omitempty"`
	Reddit    []services.RedditSentiment    `json:"reddit,omitempty"`
	Error     string                        `json:"error,omitempty"`
	FetchedAt time.Time                     `json:"fetched_at"`
}

// GetSentiment returns aggregated sentiment for a symbol
// GET /api/v1/sentiment/:symbol
func (h *SentimentHandler) GetSentiment(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Symbol is required"})
		return
	}

	sentiment, err := h.sentimentService.GetAggregatedSentiment(c.Request.Context(), symbol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, GetSentimentResponse{
		Status:    "success",
		Data:      sentiment,
		FetchedAt: time.Now().UTC(),
	})
}

// RefreshSentiment triggers a fresh fetch of sentiment data
// POST /api/v1/sentiment/refresh
func (h *SentimentHandler) RefreshSentiment(c *gin.Context) {
	var request struct {
		Sources []string `json:"sources"` // "news", "reddit", or both
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		request.Sources = []string{"news", "reddit"}
	}

	response := GetSentimentResponse{
		Status:    "success",
		FetchedAt: time.Now().UTC(),
	}

	// Fetch Reddit sentiment
	if len(request.Sources) == 0 || contains(request.Sources, "reddit") {
		subreddits := []string{"Cryptocurrency", "Bitcoin", "ethereum", "SOLCrypto"}
		redditData, err := h.sentimentService.FetchRedditSentiment(c.Request.Context(), subreddits)
		if err != nil {
			response.Error += "Reddit: " + err.Error() + "; "
		} else {
			response.Reddit = redditData
		}
	}

	// Fetch News sentiment
	if len(request.Sources) == 0 || contains(request.Sources, "news") {
		newsData, err := h.sentimentService.FetchNewsSentiment(c.Request.Context(), "news")
		if err != nil {
			response.Error += "News: " + err.Error() + "; "
		} else {
			response.News = newsData
		}
	}

	if response.Error == "" {
		response.Status = "success"
	} else {
		response.Status = "partial"
	}

	c.JSON(http.StatusOK, response)
}

// GetSentimentSources returns available sentiment sources
// GET /api/v1/sentiment/sources
func (h *SentimentHandler) GetSentimentSources(c *gin.Context) {
	sources := gin.H{
		"news": []gin.H{
			{"name": "cryptopanic", "type": "cryptopanic", "status": "available"},
		},
		"reddit": []gin.H{
			{"name": "Cryptocurrency", "subreddit": "r/Cryptocurrency", "status": "available"},
			{"name": "Bitcoin", "subreddit": "r/Bitcoin", "status": "available"},
			{"name": "ethereum", "subreddit": "r/ethereum", "status": "available"},
			{"name": "SOLCrypto", "subreddit": "r/SOLCrypto", "status": "available"},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   sources,
	})
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
