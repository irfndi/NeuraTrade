package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type SentimentConfig struct {
	APIKey         string
	APISecret      string
	AccessToken    string
	AccessSecret   string
	BearerToken    string
	BaseURL        string
	RequestTimeout time.Duration
	CacheTTL       time.Duration
}

func DefaultSentimentConfig() SentimentConfig {
	return SentimentConfig{
		BaseURL:        "https://api.twitter.com/2",
		RequestTimeout: 30 * time.Second,
		CacheTTL:       5 * time.Minute,
	}
}

type SentimentScore int

const (
	SentimentBearish SentimentScore = iota - 1
	SentimentNeutral
	SentimentBullish
)

func (s SentimentScore) String() string {
	switch s {
	case SentimentBearish:
		return "bearish"
	case SentimentNeutral:
		return "neutral"
	case SentimentBullish:
		return "bullish"
	default:
		return "unknown"
	}
}

type SentimentResult struct {
	Symbol       string         `json:"symbol"`
	Score        SentimentScore `json:"score"`
	ScoreLabel   string         `json:"score_label"`
	Confidence   float64        `json:"confidence"`
	TweetCount   int            `json:"tweet_count"`
	LastUpdated  time.Time      `json:"last_updated"`
	Keywords     []string       `json:"keywords"`
	RawSentiment interface{}    `json:"raw_sentiment,omitempty"`
}

type TwitterSearchResponse struct {
	Data     []TweetData `json:"data"`
	Meta     TweetMeta   `json:"meta"`
	Includes struct {
		Users []User `json:"users"`
	} `json:"includes"`
}

type TweetData struct {
	ID            string       `json:"id"`
	Text          string       `json:"text"`
	CreatedAt     string       `json:"created_at"`
	AuthorID      string       `json:"author_id"`
	PublicMetrics TweetMetrics `json:"public_metrics"`
}

type TweetMetrics struct {
	RetweetCount int `json:"retweet_count"`
	ReplyCount   int `json:"reply_count"`
	LikeCount    int `json:"like_count"`
	QuoteCount   int `json:"quote_count"`
}

type TweetMeta struct {
	ResultCount int    `json:"result_count"`
	NextToken   string `json:"next_token,omitempty"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type TwitterClient struct {
	config  SentimentConfig
	logger  *zap.Logger
	client  *http.Client
	cacheMu sync.RWMutex
	cache   map[string]CacheEntry
}

type CacheEntry struct {
	Result *SentimentResult
	Expiry time.Time
}

func NewTwitterClient(config SentimentConfig, logger *zap.Logger) *TwitterClient {
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}

	return &TwitterClient{
		config: config,
		logger: logger,
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		cache: make(map[string]CacheEntry),
	}
}

func (c *TwitterClient) GetSentiment(ctx context.Context, symbol string, keywords []string) (*SentimentResult, error) {
	cacheKey := strings.ToUpper(symbol)

	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached, nil
	}

	query := c.buildSearchQuery(symbol, keywords)
	result, err := c.fetchAndAnalyzeSentiment(ctx, query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get sentiment: %w", err)
	}

	c.setCache(cacheKey, result)
	return result, nil
}

func (c *TwitterClient) buildSearchQuery(symbol string, keywords []string) string {
	parts := []string{symbol}
	parts = append(parts, keywords...)
	return strings.Join(parts, " OR ")
}

func (c *TwitterClient) fetchAndAnalyzeSentiment(ctx context.Context, query, symbol string) (*SentimentResult, error) {
	encodedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("%s/tweets/search/recent?query=%s&tweet.fields=created_at,public_metrics&max_results=100",
		c.config.BaseURL, encodedQuery)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Twitter API error (status %d): %s", resp.StatusCode, string(body))
	}

	var twitterResp TwitterSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&twitterResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	score, confidence := c.analyzeTweets(twitterResp.Data)

	return &SentimentResult{
		Symbol:      symbol,
		Score:       score,
		ScoreLabel:  score.String(),
		Confidence:  confidence,
		TweetCount:  len(twitterResp.Data),
		LastUpdated: time.Now(),
		Keywords:    strings.Split(query, " OR "),
	}, nil
}

func (c *TwitterClient) analyzeTweets(tweets []TweetData) (SentimentScore, float64) {
	if len(tweets) == 0 {
		return SentimentNeutral, 0.0
	}

	var totalScore float64
	var totalWeight float64

	bullishKeywords := []string{"buy", "bull", "long", "moon", "pump", "gain", "profit", "up", "high", "breakout"}
	bearishKeywords := []string{"sell", "bear", "short", "dump", "crash", "loss", "down", "low", "breakdown", "panic"}

	for _, tweet := range tweets {
		text := strings.ToLower(tweet.Text)

		weight := float64(tweet.PublicMetrics.LikeCount + tweet.PublicMetrics.RetweetCount*2)
		if weight == 0 {
			weight = 1
		}

		score := 0
		for _, kw := range bullishKeywords {
			if strings.Contains(text, kw) {
				score++
			}
		}
		for _, kw := range bearishKeywords {
			if strings.Contains(text, kw) {
				score--
			}
		}

		totalScore += float64(score) * weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return SentimentNeutral, 0.0
	}

	avgScore := totalScore / totalWeight

	var sentiment SentimentScore
	var confidence float64

	if avgScore > 0.5 {
		sentiment = SentimentBullish
		confidence = minFloat(1.0, avgScore/2.0)
	} else if avgScore < -0.5 {
		sentiment = SentimentBearish
		confidence = minFloat(1.0, -avgScore/2.0)
	} else {
		sentiment = SentimentNeutral
		confidence = 1.0 - minFloat(1.0, abs(avgScore)/2.0)
	}

	return sentiment, confidence
}

func (c *TwitterClient) getFromCache(key string) (*SentimentResult, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.Expiry) {
		return nil, false
	}
	return entry.Result, true
}

func (c *TwitterClient) setCache(key string, result *SentimentResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache[key] = CacheEntry{
		Result: result,
		Expiry: time.Now().Add(c.config.CacheTTL),
	}
}

func (c *TwitterClient) ClearCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache = make(map[string]CacheEntry)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
