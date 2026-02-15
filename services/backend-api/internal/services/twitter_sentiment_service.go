package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

type TwitterSentimentConfig struct {
	APIKey              string
	APISecret           string
	AccessToken         string
	AccessSecret        string
	BearerToken         string
	CacheDuration       time.Duration
	MinTweetVolume      int
	SentimentThresholds struct {
		Positive decimal.Decimal
		Negative decimal.Decimal
	}
}

type TwitterSentimentScore struct {
	Overall     decimal.Decimal `json:"overall"`      // -1 to 1
	PositivePct decimal.Decimal `json:"positive_pct"` // 0 to 1
	NegativePct decimal.Decimal `json:"negative_pct"` // 0 to 1
	NeutralPct  decimal.Decimal `json:"neutral_pct"`  // 0 to 1
	Volume      int             `json:"volume"`       // tweet count
	Confidence  decimal.Decimal `json:"confidence"`   // 0 to 1
	LastUpdated time.Time       `json:"last_updated"`
}

type Tweet struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	Likes     int       `json:"likes"`
	Retweets  int       `json:"retweets"`
	Sentiment string    `json:"sentiment"` // "positive", "negative", "neutral"
	Score     float64   `json:"score"`     // -1 to 1
}

type TwitterSentimentService struct {
	config     *TwitterSentimentConfig
	httpClient *http.Client
	redis      *redis.Client
	logger     Logger
}

func NewTwitterSentimentService(config *TwitterSentimentConfig, redisClient *redis.Client, logger Logger) *TwitterSentimentService {
	if config == nil {
		config = &TwitterSentimentConfig{
			CacheDuration: 15 * time.Minute,
		}
		config.SentimentThresholds.Positive = decimal.NewFromFloat(0.3)
		config.SentimentThresholds.Negative = decimal.NewFromFloat(-0.3)
	}

	return &TwitterSentimentService{
		config:     config,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		redis:      redisClient,
		logger:     logger,
	}
}

func (tss *TwitterSentimentService) GetSentiment(ctx context.Context, symbol string) (*TwitterSentimentScore, error) {
	cacheKey := fmt.Sprintf("sentiment:twitter:%s", strings.ToUpper(symbol))

	if tss.redis != nil {
		cached, err := tss.redis.Get(ctx, cacheKey).Result()
		if err == nil {
			var score TwitterSentimentScore
			if json.Unmarshal([]byte(cached), &score) == nil {
				return &score, nil
			}
		}
	}

	score, err := tss.fetchAndAnalyzeSentiment(ctx, symbol)
	if err != nil {
		return nil, err
	}

	if tss.redis != nil && score != nil {
		data, _ := json.Marshal(score)
		tss.redis.Set(ctx, cacheKey, data, tss.config.CacheDuration)
	}

	return score, nil
}

func (tss *TwitterSentimentService) fetchAndAnalyzeSentiment(ctx context.Context, symbol string) (*TwitterSentimentScore, error) {
	tweets, err := tss.fetchTweets(ctx, symbol)
	if err != nil {
		tss.logger.WithFields(map[string]interface{}{"symbol": symbol}).Warn("Failed to fetch tweets")
		return tss.getDefaultSentiment(), nil
	}

	if len(tweets) == 0 {
		return tss.getDefaultSentiment(), nil
	}

	return tss.analyzeTweets(tweets), nil
}

func (tss *TwitterSentimentService) fetchTweets(ctx context.Context, symbol string) ([]Tweet, error) {
	if tss.config.BearerToken == "" {
		return tss.getMockTweets(symbol), nil
	}

	url := fmt.Sprintf("https://api.twitter.com/2/tweets/search/recent?query=%s&max_results=100", symbol)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+tss.config.BearerToken)

	resp, err := tss.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return tss.getMockTweets(symbol), nil
	}

	var result struct {
		Data []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
			AuthorID  string `json:"author_id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	tweets := make([]Tweet, len(result.Data))
	for i, t := range result.Data {
		createdAt, _ := time.Parse(time.RFC3339, t.CreatedAt)
		tweets[i] = Tweet{
			ID:        t.ID,
			Text:      t.Text,
			Author:    t.AuthorID,
			CreatedAt: createdAt,
		}
	}

	return tweets, nil
}

func (tss *TwitterSentimentService) analyzeTweets(tweets []Tweet) *TwitterSentimentScore {
	positive := 0
	negative := 0
	neutral := 0
	totalScore := 0.0

	keywordsPositive := map[string]bool{
		"bullish": true, "moon": true, "gain": true, "profit": true,
		"up": true, "rally": true, "surge": true, "breakout": true,
		"buy": true, "long": true, "hodl": true, "to the moon": true,
		"green": true, "🚀": true, "💎": true, "diamond hands": true,
	}

	keywordsNegative := map[string]bool{
		"bearish": true, "dump": true, "crash": true, "lose": true,
		"down": true, "sell": true, "short": true, "scam": true,
		"rug": true, "pullback": true, "red": true, "danger": true,
		"warning": true, "alert": true, "liquidation": true,
	}

	for i := range tweets {
		tweet := &tweets[i]
		text := strings.ToLower(tweet.Text)

		posCount := 0
		negCount := 0

		for kw := range keywordsPositive {
			if strings.Contains(text, kw) {
				posCount++
			}
		}
		for kw := range keywordsNegative {
			if strings.Contains(text, kw) {
				negCount++
			}
		}

		if posCount > negCount {
			tweet.Sentiment = "positive"
			tweet.Score = 0.5
			positive++
			totalScore += 0.5
		} else if negCount > posCount {
			tweet.Sentiment = "negative"
			tweet.Score = -0.5
			negative++
			totalScore -= 0.5
		} else {
			tweet.Sentiment = "neutral"
			tweet.Score = 0
			neutral++
		}
	}

	total := len(tweets)
	if total == 0 {
		return tss.getDefaultSentiment()
	}

	result := &TwitterSentimentScore{
		Volume:      total,
		PositivePct: decimal.NewFromInt(int64(positive)).Div(decimal.NewFromInt(int64(total))),
		NegativePct: decimal.NewFromInt(int64(negative)).Div(decimal.NewFromInt(int64(total))),
		NeutralPct:  decimal.NewFromInt(int64(neutral)).Div(decimal.NewFromInt(int64(total))),
		LastUpdated: time.Now(),
	}

	if total > 0 {
		result.Overall = decimal.NewFromFloat(totalScore / float64(total))
		result.Confidence = decimal.NewFromInt(int64(total)).Div(decimal.NewFromInt(100))
		if result.Confidence.GreaterThan(decimal.NewFromFloat(1)) {
			result.Confidence = decimal.NewFromFloat(1)
		}
	}

	return result
}

func (tss *TwitterSentimentService) getDefaultSentiment() *TwitterSentimentScore {
	return &TwitterSentimentScore{
		Overall:     decimal.Zero,
		PositivePct: decimal.NewFromFloat(0.33),
		NegativePct: decimal.NewFromFloat(0.33),
		NeutralPct:  decimal.NewFromFloat(0.34),
		Volume:      0,
		Confidence:  decimal.Zero,
		LastUpdated: time.Now(),
	}
}

func (tss *TwitterSentimentService) getMockTweets(symbol string) []Tweet {
	keywords := map[string][]string{
		"BTC":  {"bullish", "moon", "bitcoin", "btc", "crypto", "pump", "dip", "hodl"},
		"ETH":  {"ethereum", "bullish", "eth", "merge", "upgrade", "gas"},
		"XRP":  {"ripple", "xrp", "court", "sec", "bullish"},
		"SOL":  {"solana", "sol", "bullish", "defi", "nft"},
		"ADA":  {"cardano", "ada", "bullish", "hydra"},
		"DOGE": {"dogecoin", "doge", "elon", "moon", "meme"},
	}

	sentiments := []string{"positive", "negative", "neutral"}
	tweets := make([]Tweet, 20)

	kw := keywords[symbol]
	if kw == nil {
		kw = []string{"crypto", "bullish", "trading"}
	}

	for i := 0; i < 20; i++ {
		sentiment := sentiments[i%3]
		score := 0.0
		switch sentiment {
		case "positive":
			score = 0.5
		case "negative":
			score = -0.5
		}

		tweets[i] = Tweet{
			ID:        fmt.Sprintf("tweet_%d", i),
			Text:      fmt.Sprintf("$%s is %s today! #crypto #%s", symbol, sentiment, kw[i%len(kw)]),
			Author:    fmt.Sprintf("user_%d", i),
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
			Likes:     i * 10,
			Retweets:  i * 5,
			Sentiment: sentiment,
			Score:     score,
		}
	}

	return tweets
}
