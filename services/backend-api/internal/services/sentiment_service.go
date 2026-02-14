package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SentimentScore represents a sentiment score from -1.0 (bearish) to 1.0 (bullish)
type SentimentScore float64

const (
	SentimentBearish SentimentScore = -1.0
	SentimentNeutral SentimentScore = 0.0
	SentimentBullish SentimentScore = 1.0
)

// NewsSentiment represents a news article with sentiment
type NewsSentiment struct {
	ID             int64     `json:"id"`
	SourceID       int       `json:"source_id"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	PublishedAt    time.Time `json:"published_at"`
	SentimentScore float64   `json:"sentiment_score"`
	SentimentLabel string    `json:"sentiment_label"`
	Symbols        []string  `json:"symbols"`
	FetchedAt      time.Time `json:"fetched_at"`
}

// RedditSentiment represents a Reddit post with sentiment
type RedditSentiment struct {
	ID             int64     `json:"id"`
	SourceID       int       `json:"source_id"`
	Subreddit      string    `json:"subreddit"`
	PostID         string    `json:"post_id"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	Author         string    `json:"author"`
	Score          int       `json:"score"`
	NumComments    int       `json:"num_comments"`
	SentimentScore float64   `json:"sentiment_score"`
	SentimentLabel string    `json:"sentiment_label"`
	Symbols        []string  `json:"symbols"`
	FetchedAt      time.Time `json:"fetched_at"`
}

// AggregatedSentiment represents combined sentiment for a symbol
type AggregatedSentiment struct {
	Symbol         string    `json:"symbol"`
	SentimentScore float64   `json:"sentiment_score"`
	BullishRatio   float64   `json:"bullish_ratio"`
	TotalMentions  int       `json:"total_mentions"`
	SampleSize     int       `json:"sample_size"`
	ComputedAt     time.Time `json:"computed_at"`
}

// SentimentServiceConfig holds configuration for sentiment services
type SentimentServiceConfig struct {
	RedditClientID     string
	RedditClientSecret string
	RedditUserAgent    string
	CryptoPanicToken   string
	FetchTimeout       time.Duration
	CacheDuration      time.Duration
}

// DefaultSentimentServiceConfig returns default configuration
func DefaultSentimentServiceConfig() SentimentServiceConfig {
	return SentimentServiceConfig{
		RedditClientID:     "",
		RedditClientSecret: "",
		RedditUserAgent:    "NeuraTrade/1.0",
		CryptoPanicToken:   "",
		FetchTimeout:       30 * time.Second,
		CacheDuration:      5 * time.Minute,
	}
}

// SentimentService handles fetching and processing sentiment data
type SentimentService struct {
	config     SentimentServiceConfig
	db         DBPool
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]cacheEntry
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

// NewSentimentService creates a new sentiment service
func NewSentimentService(config SentimentServiceConfig, db DBPool) *SentimentService {
	return &SentimentService{
		config:     config,
		db:         db,
		httpClient: &http.Client{Timeout: config.FetchTimeout},
		cache:      make(map[string]cacheEntry),
	}
}

// FetchRedditSentiment fetches sentiment from Reddit subreddits
func (s *SentimentService) FetchRedditSentiment(ctx context.Context, subreddits []string) ([]RedditSentiment, error) {
	// Get access token
	accessToken, err := s.getRedditAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Reddit access token: %w", err)
	}

	var results []RedditSentiment

	for _, subreddit := range subreddits {
		posts, err := s.fetchSubredditPosts(ctx, accessToken, subreddit)
		if err != nil {
			continue // Skip failed subreddits
		}
		results = append(results, posts...)
	}

	// Store in database
	if len(results) > 0 {
		s.storeRedditSentiment(ctx, results)
	}

	return results, nil
}

// getRedditAccessToken obtains Reddit OAuth access token
func (s *SentimentService) getRedditAccessToken(ctx context.Context) (string, error) {
	if s.config.RedditClientID == "" || s.config.RedditClientSecret == "" {
		return "", fmt.Errorf("Reddit credentials not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://www.reddit.com/api/v1/access_token",
		strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(s.config.RedditClientID, s.config.RedditClientSecret)
	req.Header.Set("User-Agent", s.config.RedditUserAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Reddit auth failed with status %d", resp.StatusCode)
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", err
	}

	return authResp.AccessToken, nil
}

// fetchSubredditPosts fetches hot posts from a subreddit
func (s *SentimentService) fetchSubredditPosts(ctx context.Context, accessToken, subreddit string) ([]RedditSentiment, error) {
	url := fmt.Sprintf("https://oauth.reddit.com/r/%s/hot.json?limit=25", subreddit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", s.config.RedditUserAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Reddit API returned status %d", resp.StatusCode)
	}

	var redditResp struct {
		Data struct {
			Children []struct {
				Data struct {
					ID          string  `json:"id"`
					Title       string  `json:"title"`
					URL         string  `json:"url"`
					Author      string  `json:"author"`
					Score       int     `json:"score"`
					NumComments int     `json:"num_comments"`
					CreatedUTC  float64 `json:"created_utc"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&redditResp); err != nil {
		return nil, err
	}

	var results []RedditSentiment
	for _, child := range redditResp.Data.Children {
		post := child.Data
		sentimentScore := calculateTextSentiment(post.Title)
		sentimentLabel := getSentimentLabel(sentimentScore)
		symbols := extractCryptoSymbols(post.Title)

		results = append(results, RedditSentiment{
			PostID:         post.ID,
			Subreddit:      subreddit,
			Title:          post.Title,
			URL:            "https://reddit.com" + post.URL,
			Author:         post.Author,
			Score:          post.Score,
			NumComments:    post.NumComments,
			SentimentScore: sentimentScore,
			SentimentLabel: sentimentLabel,
			Symbols:        symbols,
			FetchedAt:      time.Now().UTC(),
		})
	}

	return results, nil
}

// storeRedditSentiment stores reddit sentiment data to database
func (s *SentimentService) storeRedditSentiment(ctx context.Context, posts []RedditSentiment) error {
	// Get source ID for subreddit
	var sourceID int
	for _, post := range posts {
		err := s.db.QueryRow(ctx, `
			SELECT id FROM reddit_sentiment_sources WHERE subreddit = $1
		`, post.Subreddit).Scan(&sourceID)

		if err != nil {
			continue
		}

		_, err = s.db.Exec(ctx, `
			INSERT INTO reddit_sentiment (source_id, post_id, title, url, author, score, num_comments, 
				sentiment_score, sentiment_label, symbols, fetched_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (post_id) DO UPDATE SET
				score = EXCLUDED.score,
				num_comments = EXCLUDED.num_comments,
				sentiment_score = EXCLUDED.sentiment_score,
				fetched_at = EXCLUDED.fetched_at
		`, sourceID, post.PostID, post.Title, post.URL, post.Author, post.Score, post.NumComments,
			post.SentimentScore, post.SentimentLabel, post.Symbols, post.FetchedAt)

		if err != nil {
			continue
		}
	}

	return nil
}

// FetchNewsSentiment fetches sentiment from news sources
func (s *SentimentService) FetchNewsSentiment(ctx context.Context, kind string) ([]NewsSentiment, error) {
	if s.config.CryptoPanicToken == "" {
		return nil, fmt.Errorf("CryptoPanic token not configured")
	}

	url := fmt.Sprintf("https://cryptopanic.com/api/v1/posts/?auth_token=%s&kind=%s", s.config.CryptoPanicToken, kind)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CryptoPanic API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var cryptoPanicResp struct {
		Results []struct {
			ID        int `json:"id"`
			Published struct {
				UTC time.Time `json:"utc"`
			} `json:"published"`
			Title  string `json:"title"`
			URL    string `json:"url"`
			Source struct {
				Domain string `json:"domain"`
			} `json:"source"`
			Metadata struct {
				Currencies []string `json:"currencies"`
			} `json:"metadata"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &cryptoPanicResp); err != nil {
		return nil, err
	}

	var results []NewsSentiment
	for _, item := range cryptoPanicResp.Results {
		sentimentScore := calculateTextSentiment(item.Title)
		sentimentLabel := getSentimentLabel(sentimentScore)
		symbols := extractCryptoSymbols(item.Title)

		if len(item.Metadata.Currencies) > 0 {
			symbols = item.Metadata.Currencies
		}

		results = append(results, NewsSentiment{
			Title:          item.Title,
			URL:            item.URL,
			PublishedAt:    item.Published.UTC,
			SentimentScore: sentimentScore,
			SentimentLabel: sentimentLabel,
			Symbols:        symbols,
			FetchedAt:      time.Now().UTC(),
		})
	}

	// Store in database
	if len(results) > 0 {
		s.storeNewsSentiment(ctx, results)
	}

	return results, nil
}

// storeNewsSentiment stores news sentiment data to database
func (s *SentimentService) storeNewsSentiment(ctx context.Context, articles []NewsSentiment) error {
	// Get source ID (assuming CryptoPanic)
	var sourceID int
	err := s.db.QueryRow(ctx, `
		SELECT id FROM news_sentiment_sources WHERE source_name = 'cryptopanic'
	`).Scan(&sourceID)

	if err != nil {
		return err
	}

	for _, article := range articles {
		_, err = s.db.Exec(ctx, `
			INSERT INTO news_sentiment (source_id, title, url, published_at, sentiment_score, sentiment_label, symbols, fetched_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (url) DO UPDATE SET
				sentiment_score = EXCLUDED.sentiment_score,
				sentiment_label = EXCLUDED.sentiment_label,
				fetched_at = EXCLUDED.fetched_at
		`, sourceID, article.Title, article.URL, article.PublishedAt, article.SentimentScore,
			article.SentimentLabel, article.Symbols, article.FetchedAt)

		if err != nil {
			continue
		}
	}

	return nil
}

// GetAggregatedSentiment retrieves aggregated sentiment for a symbol
func (s *SentimentService) GetAggregatedSentiment(ctx context.Context, symbol string) (*AggregatedSentiment, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("sentiment:%s", symbol)
	if entry, ok := s.getCached(cacheKey); ok {
		if agg, ok := entry.(*AggregatedSentiment); ok {
			return agg, nil
		}
	}

	// Query aggregated sentiment from database
	var result AggregatedSentiment
	err := s.db.QueryRow(ctx, `
		SELECT symbol, sentiment_score, bullish_ratio, total_mentions, sample_size, computed_at
		FROM aggregated_sentiment
		WHERE symbol = $1 AND computed_at > NOW() - INTERVAL '1 hour'
		ORDER BY computed_at DESC
		LIMIT 1
	`, strings.ToUpper(symbol)).Scan(
		&result.Symbol, &result.SentimentScore, &result.BullishRatio,
		&result.TotalMentions, &result.SampleSize, &result.ComputedAt,
	)

	if err != nil {
		// If no aggregated data, compute from raw data
		return s.computeAggregatedSentiment(ctx, symbol)
	}

	s.setCache(cacheKey, &result)
	return &result, nil
}

// computeAggregatedSentiment computes sentiment from raw data
func (s *SentimentService) computeAggregatedSentiment(ctx context.Context, symbol string) (*AggregatedSentiment, error) {
	symbolUpper := strings.ToUpper(symbol)

	// Get recent news sentiment for symbol
	var newsScore float64
	var newsCount int
	s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(sentiment_score), 0), COUNT(*)
		FROM news_sentiment
		WHERE $1 = ANY(symbols) AND fetched_at > NOW() - INTERVAL '24 hours'
	`, symbolUpper).Scan(&newsScore, &newsCount)

	// Get recent reddit sentiment for symbol
	var redditScore float64
	var redditCount int
	s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(sentiment_score), 0), COUNT(*)
		FROM reddit_sentiment
		WHERE $1 = ANY(symbols) AND fetched_at > NOW() - INTERVAL '24 hours'
	`, symbolUpper).Scan(&redditScore, &redditCount)

	totalCount := newsCount + redditCount
	if totalCount == 0 {
		return &AggregatedSentiment{
			Symbol:         symbolUpper,
			SentimentScore: 0,
			BullishRatio:   0.5,
			TotalMentions:  0,
			SampleSize:     0,
			ComputedAt:     time.Now().UTC(),
		}, nil
	}

	// Weighted average (news: 60%, reddit: 40%)
	combinedScore := (newsScore*float64(newsCount) + redditScore*float64(redditCount)) / float64(totalCount)

	// Calculate bullish ratio
	var bullishCount int
	s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT sentiment_score FROM news_sentiment WHERE $1 = ANY(symbols) AND fetched_at > NOW() - INTERVAL '24 hours' AND sentiment_score > 0
			UNION ALL
			SELECT sentiment_score FROM reddit_sentiment WHERE $1 = ANY(symbols) AND fetched_at > NOW() - INTERVAL '24 hours' AND sentiment_score > 0
		) combined
	`, symbolUpper).Scan(&bullishCount)

	bullishRatio := float64(bullishCount) / float64(totalCount)

	result := &AggregatedSentiment{
		Symbol:         symbolUpper,
		SentimentScore: combinedScore,
		BullishRatio:   bullishRatio,
		TotalMentions:  totalCount,
		SampleSize:     totalCount,
		ComputedAt:     time.Now().UTC(),
	}

	// Store aggregated result
	s.db.Exec(ctx, `
		INSERT INTO aggregated_sentiment (symbol, sentiment_source, sentiment_score, bullish_ratio, total_mentions, sample_size, computed_at)
		VALUES ($1, 'combined', $2, $3, $4, $5, $6)
		ON CONFLICT (symbol, sentiment_source) DO UPDATE SET
			sentiment_score = EXCLUDED.sentiment_score,
			bullish_ratio = EXCLUDED.bullish_ratio,
			total_mentions = EXCLUDED.total_mentions,
			sample_size = EXCLUDED.sample_size,
			computed_at = EXCLUDED.computed_at
	`, symbolUpper, combinedScore, bullishRatio, totalCount, totalCount, result.ComputedAt)

	s.setCache(fmt.Sprintf("sentiment:%s", symbolUpper), result)

	return result, nil
}

// getCached retrieves cached data if not expired
func (s *SentimentService) getCached(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

// setCached stores data in cache with expiration
func (s *SentimentService) setCache(key string, data interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[key] = cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(s.config.CacheDuration),
	}
}

// calculateTextSentiment calculates sentiment score from text using simple keyword matching
// This is a placeholder - in production, use a proper ML model or sentiment API
func calculateTextSentiment(text string) float64 {
	text = strings.ToLower(text)

	bullishKeywords := []string{"bull", "buy", "rise", "gain", "profit", "up", "growth", "surge", "rally", "breakout", "moon", "high", "positive"}
	bearishKeywords := []string{"bear", "sell", "drop", "fall", "loss", "down", "crash", "dump", "breakdown", "low", "negative", "fear", "hack", "scam"}

	bullishCount := 0
	bearishCount := 0

	for _, keyword := range bullishKeywords {
		if strings.Contains(text, keyword) {
			bullishCount++
		}
	}

	for _, keyword := range bearishKeywords {
		if strings.Contains(text, keyword) {
			bearishCount++
		}
	}

	total := bullishCount + bearishCount
	if total == 0 {
		return 0.0
	}

	return float64(bullishCount-bearishCount) / float64(total)
}

// getSentimentLabel returns label based on score
func getSentimentLabel(score float64) string {
	if score > 0.2 {
		return "bullish"
	} else if score < -0.2 {
		return "bearish"
	}
	return "neutral"
}

// extractCryptoSymbols extracts cryptocurrency symbols from text
func extractCryptoSymbols(text string) []string {
	// Common crypto symbols to look for
	knownSymbols := []string{
		"BTC", "ETH", "SOL", "XRP", "ADA", "DOGE", "AVAX", "DOT", "MATIC", "LINK",
		"UNI", "ATOM", "LTC", "BCH", "XLM", "ALGO", "VET", "FIL", "THETA", "AAVE",
		"BNB", "XMR", "ETC", "XEM", "HBAR", "MKR", "SNX", "COMP", "SUSHI", "CRV",
	}

	text = strings.ToUpper(text)
	var found []string

	for _, symbol := range knownSymbols {
		// Match whole word only
		if strings.Contains(text, " "+symbol+" ") || strings.Contains(text, symbol+",") || strings.Contains(text, " "+symbol+".") {
			found = append(found, symbol)
		}
	}

	// Always include BTC if Bitcoin is mentioned
	if strings.Contains(text, "bitcoin") || strings.Contains(text, "btc") {
		found = appendUnique(found, "BTC")
	}

	return found
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
