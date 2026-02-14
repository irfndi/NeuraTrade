package services

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestTwitterSentiment_AnalyzeTweets(t *testing.T) {
	service := &TwitterSentimentService{}

	tweets := []Tweet{
		{Text: "BTC is bullish today moon!", Sentiment: "positive"},
		{Text: "Great gains on bitcoin", Sentiment: "positive"},
		{Text: "Bitcoin dump incoming", Sentiment: "negative"},
		{Text: "Crypto market analysis", Sentiment: "neutral"},
		{Text: "To the moon 🚀", Sentiment: "positive"},
		{Text: "Bearish on btc", Sentiment: "negative"},
	}

	result := service.analyzeTweets(tweets)

	if result.Volume != 6 {
		t.Errorf("Expected volume 6, got %d", result.Volume)
	}

	if result.PositivePct.LessThan(decimal.NewFromFloat(0.3)) {
		t.Errorf("Expected positive pct >= 0.3, got %s", result.PositivePct)
	}

	if result.NegativePct.LessThan(decimal.NewFromFloat(0.2)) {
		t.Errorf("Expected negative pct >= 0.2, got %s", result.NegativePct)
	}
}

func TestTwitterSentiment_GetDefaultSentiment(t *testing.T) {
	service := &TwitterSentimentService{}

	result := service.getDefaultSentiment()

	if !result.Overall.Equal(decimal.Zero) {
		t.Errorf("Expected overall 0, got %s", result.Overall)
	}

	if result.Volume != 0 {
		t.Errorf("Expected volume 0, got %d", result.Volume)
	}

	if result.Confidence.GreaterThan(decimal.Zero) {
		t.Error("Expected zero confidence for default sentiment")
	}
}

func TestTwitterSentiment_GetMockTweets(t *testing.T) {
	service := &TwitterSentimentService{}

	tweets := service.getMockTweets("BTC")

	if len(tweets) != 20 {
		t.Errorf("Expected 20 mock tweets, got %d", len(tweets))
	}

	if tweets[0].Sentiment != "positive" {
		t.Error("Expected first tweet to be positive")
	}

	if tweets[1].Sentiment != "negative" {
		t.Error("Expected second tweet to be negative")
	}

	if tweets[2].Sentiment != "neutral" {
		t.Error("Expected third tweet to be neutral")
	}
}
