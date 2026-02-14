package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestTwitterClient_GetSentiment_CacheHit(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	result := &SentimentResult{
		Symbol:      "BTC",
		Score:       SentimentNeutral,
		ScoreLabel:  "neutral",
		Confidence:  0.5,
		TweetCount:  0,
		LastUpdated: time.Now(),
	}

	client.setCache("BTC", result)

	cached, err := client.GetSentiment(context.Background(), "BTC", []string{"bitcoin"})
	assert.NoError(t, err)
	assert.Equal(t, SentimentNeutral, cached.Score)
	assert.Equal(t, "neutral", cached.ScoreLabel)
}

func TestTwitterClient_AnalyzeTweets_Bullish(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	tweets := []TweetData{
		{Text: "Just bought more BTC, moon time!", PublicMetrics: TweetMetrics{LikeCount: 100, RetweetCount: 50}},
		{Text: "BTC breaking out, bull run incoming", PublicMetrics: TweetMetrics{LikeCount: 80, RetweetCount: 30}},
		{Text: "Great gains on BTC today", PublicMetrics: TweetMetrics{LikeCount: 60, RetweetCount: 20}},
	}

	score, _ := client.analyzeTweets(tweets)

	assert.Equal(t, SentimentBullish, score)
}

func TestTwitterClient_AnalyzeTweets_Bearish(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	tweets := []TweetData{
		{Text: "Selling all my BTC, crash incoming", PublicMetrics: TweetMetrics{LikeCount: 100, RetweetCount: 50}},
		{Text: "Bear market confirmed, dump coming", PublicMetrics: TweetMetrics{LikeCount: 80, RetweetCount: 30}},
		{Text: "Lost money on BTC today", PublicMetrics: TweetMetrics{LikeCount: 60, RetweetCount: 20}},
	}

	score, confidence := client.analyzeTweets(tweets)

	assert.Equal(t, SentimentBearish, score)
	assert.Greater(t, confidence, 0.5)
}

func TestTwitterClient_AnalyzeTweets_Neutral(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	tweets := []TweetData{
		{Text: "BTC price is currently 50000", PublicMetrics: TweetMetrics{LikeCount: 10, RetweetCount: 5}},
		{Text: "Watching BTC market", PublicMetrics: TweetMetrics{LikeCount: 5, RetweetCount: 2}},
	}

	score, _ := client.analyzeTweets(tweets)

	assert.Equal(t, SentimentNeutral, score)
}

func TestTwitterClient_Cache(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	result := &SentimentResult{
		Symbol:      "BTC",
		Score:       SentimentBullish,
		ScoreLabel:  "bullish",
		Confidence:  0.8,
		TweetCount:  100,
		LastUpdated: time.Now(),
	}

	client.setCache("BTC", result)

	cached, ok := client.getFromCache("BTC")
	assert.True(t, ok)
	assert.Equal(t, SentimentBullish, cached.Score)
}

func TestTwitterClient_ClearCache(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultSentimentConfig()

	client := NewTwitterClient(config, logger)

	result := &SentimentResult{
		Symbol:      "BTC",
		Score:       SentimentBullish,
		ScoreLabel:  "bullish",
		Confidence:  0.8,
		TweetCount:  100,
		LastUpdated: time.Now(),
	}

	client.setCache("BTC", result)
	client.ClearCache()

	_, ok := client.getFromCache("BTC")
	assert.False(t, ok)
}

func TestSentimentScore_String(t *testing.T) {
	assert.Equal(t, "bearish", SentimentBearish.String())
	assert.Equal(t, "neutral", SentimentNeutral.String())
	assert.Equal(t, "bullish", SentimentBullish.String())
}
