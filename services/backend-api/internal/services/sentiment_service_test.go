package services

import (
	"testing"
	"time"
)

func TestNewSentimentService(t *testing.T) {
	config := DefaultSentimentServiceConfig()

	t.Run("nil db", func(t *testing.T) {
		svc := NewSentimentService(config, nil)
		if svc == nil {
			t.Error("NewSentimentService() returned nil")
		}
		if svc.config != config {
			t.Error("config not set correctly")
		}
	})
}

func TestSentimentServiceConfig(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		config := DefaultSentimentServiceConfig()

		if config.FetchTimeout != 30*time.Second {
			t.Errorf("FetchTimeout = %v, want 30s", config.FetchTimeout)
		}

		if config.CacheDuration != 5*time.Minute {
			t.Errorf("CacheDuration = %v, want 5m", config.CacheDuration)
		}

		if config.RedditUserAgent != "NeuraTrade/1.0" {
			t.Errorf("RedditUserAgent = %v, want NeuraTrade/1.0", config.RedditUserAgent)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		config := SentimentServiceConfig{
			RedditClientID:     "test-client-id",
			RedditClientSecret: "test-secret",
			CryptoPanicToken:   "test-token",
			FetchTimeout:       60 * time.Second,
			CacheDuration:      10 * time.Minute,
		}

		if config.RedditClientID != "test-client-id" {
			t.Errorf("RedditClientID not set correctly")
		}
		if config.CryptoPanicToken != "test-token" {
			t.Errorf("CryptoPanicToken not set correctly")
		}
	})
}

func TestCalculateTextSentiment(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantMin float64
		wantMax float64
	}{
		{
			name:    "strongly bullish",
			text:    "Bitcoin is going to the moon! Great buy signal, price will rise significantly",
			wantMin: 0.3,
			wantMax: 1.0,
		},
		{
			name:    "strongly bearish",
			text:    "Crypto crash coming, selling everything, drop expected, loss inevitable",
			wantMin: -1.0,
			wantMax: -0.3,
		},
		{
			name:    "neutral",
			text:    "Bitcoin price is currently at 50000 dollars",
			wantMin: -0.2,
			wantMax: 0.2,
		},
		{
			name:    "mixed sentiment",
			text:    "Bitcoin up but Ethereum down, mixed signals today",
			wantMin: -0.5,
			wantMax: 0.5,
		},
		{
			name:    "empty string",
			text:    "",
			wantMin: -0.1,
			wantMax: 0.1,
		},
		{
			name:    "bullish keywords",
			text:    "bull buy rise gain profit up growth surge rally breakout moon high positive",
			wantMin: 0.5,
			wantMax: 1.0,
		},
		{
			name:    "bearish keywords",
			text:    "bear sell drop fall loss down crash dump breakdown low negative fear hack scam",
			wantMin: -1.0,
			wantMax: -0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTextSentiment(tt.text)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateTextSentiment(%q) = %v, want between %v and %v", tt.text, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestGetSentimentLabel(t *testing.T) {
	tests := []struct {
		score    float64
		expected string
	}{
		{1.0, "bullish"},
		{0.8, "bullish"},
		{0.5, "bullish"},
		{0.3, "bullish"},
		{0.2, "neutral"},
		{0.1, "neutral"},
		{0.0, "neutral"},
		{-0.1, "neutral"},
		{-0.2, "neutral"},
		{-0.3, "bearish"},
		{-0.5, "bearish"},
		{-0.8, "bearish"},
		{-1.0, "bearish"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := getSentimentLabel(tt.score)
			if got != tt.expected {
				t.Errorf("getSentimentLabel(%v) = %v, want %v", tt.score, got, tt.expected)
			}
		})
	}
}

func TestExtractCryptoSymbols(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "no symbols",
			text:     "The market is moving today",
			expected: nil,
		},
		{
			name:     "empty text",
			text:     "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCryptoSymbols(tt.text)

			if len(got) != len(tt.expected) {
				t.Errorf("extractCryptoSymbols(%q) = %v, want %v", tt.text, got, tt.expected)
				return
			}

			for _, exp := range tt.expected {
				found := false
				for _, g := range got {
					if g == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractCryptoSymbols(%q) missing expected %v, got %v", tt.text, exp, got)
				}
			}
		})
	}
}

func TestSentimentScoreConstants(t *testing.T) {
	if SentimentBearish != -1.0 {
		t.Errorf("SentimentBearish = %v, want -1.0", SentimentBearish)
	}

	if SentimentNeutral != 0.0 {
		t.Errorf("SentimentNeutral = %v, want 0.0", SentimentNeutral)
	}

	if SentimentBullish != 1.0 {
		t.Errorf("SentimentBullish = %v, want 1.0", SentimentBullish)
	}
}

func TestNewsSentimentStruct(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		news := NewsSentiment{
			Title: "Test Article",
			URL:   "https://example.com/article",
		}

		if news.Title != "Test Article" {
			t.Error("Title not set")
		}
		if news.URL != "https://example.com/article" {
			t.Error("URL not set")
		}
	})
}

func TestRedditSentimentStruct(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		reddit := RedditSentiment{
			PostID:    "test123",
			Subreddit: "Cryptocurrency",
			Title:     "Test Post",
			URL:       "https://reddit.com/test",
			Score:     100,
		}

		if reddit.PostID != "test123" {
			t.Error("PostID not set")
		}
		if reddit.Subreddit != "Cryptocurrency" {
			t.Error("Subreddit not set")
		}
		if reddit.Score != 100 {
			t.Error("Score not set")
		}
	})
}

func TestAggregatedSentimentStruct(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		agg := AggregatedSentiment{
			Symbol:         "BTC",
			SentimentScore: 0.5,
			BullishRatio:   0.7,
			TotalMentions:  100,
			SampleSize:     100,
		}

		if agg.Symbol != "BTC" {
			t.Error("Symbol not set")
		}
		if agg.SentimentScore != 0.5 {
			t.Error("SentimentScore not set")
		}
		if agg.BullishRatio != 0.7 {
			t.Error("BullishRatio not set")
		}
		if agg.TotalMentions != 100 {
			t.Error("TotalMentions not set")
		}
	})
}

func TestAppendUnique(t *testing.T) {
	t.Run("add new item", func(t *testing.T) {
		slice := []string{"BTC", "ETH"}
		result := appendUnique(slice, "SOL")
		if len(result) != 3 {
			t.Errorf("len = %v, want 3", len(result))
		}
	})

	t.Run("skip duplicate", func(t *testing.T) {
		slice := []string{"BTC", "ETH"}
		result := appendUnique(slice, "BTC")
		if len(result) != 2 {
			t.Errorf("len = %v, want 2", len(result))
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		slice := []string{}
		result := appendUnique(slice, "BTC")
		if len(result) != 1 {
			t.Errorf("len = %v, want 1", len(result))
		}
	})
}
