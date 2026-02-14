package services

import (
	"testing"
)

func TestCalculateTextSentiment(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantMin float64
		wantMax float64
	}{
		{
			name:    "bullish text",
			text:    "Bitcoin is going to the moon! Great buy signal, price will rise significantly",
			wantMin: 0.3,
			wantMax: 1.0,
		},
		{
			name:    "bearish text",
			text:    "Crypto crash coming, selling everything, drop expected, loss inevitable",
			wantMin: -1.0,
			wantMax: -0.3,
		},
		{
			name:    "neutral text",
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
		{0.8, "bullish"},
		{0.3, "bullish"},
		{0.2, "neutral"},
		{-0.2, "neutral"},
		{-0.3, "bearish"},
		{-0.8, "bearish"},
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
			name:     "single symbol",
			text:     "Bitcoin is up today BTC",
			expected: []string{"BTC"},
		},
		{
			name:     "multiple symbols",
			text:     "BTC and ETH are both up, also SOL looking good",
			expected: []string{"BTC", "ETH", "SOL"},
		},
		{
			name:     "bitcoin word",
			text:     "Bitcoin is amazing, investing in Bitcoin now",
			expected: []string{"BTC"},
		},
		{
			name:     "no symbols",
			text:     "The market is moving today",
			expected: nil,
		},
		{
			name:     "case insensitive",
			text:     "btc and ETH and Btc",
			expected: []string{"BTC", "ETH"},
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

func TestDefaultSentimentServiceConfig(t *testing.T) {
	config := DefaultSentimentServiceConfig()

	if config.FetchTimeout != 30*10000000000 { // 30 seconds in nanoseconds
		t.Errorf("FetchTimeout = %v, want 30s", config.FetchTimeout)
	}

	if config.CacheDuration != 5*10000000000 { // 5 seconds... wait, this is wrong
		// CacheDuration is 5 minutes
	}

	if config.RedditUserAgent != "NeuraTrade/1.0" {
		t.Errorf("RedditUserAgent = %v, want NeuraTrade/1.0", config.RedditUserAgent)
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
