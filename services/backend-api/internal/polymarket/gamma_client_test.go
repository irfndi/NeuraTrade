package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		opts   []ClientOption
		expect struct {
			baseURL string
			timeout time.Duration
		}
	}{
		{
			name: "default client",
			opts: nil,
			expect: struct {
				baseURL string
				timeout time.Duration
			}{
				baseURL: DefaultBaseURL,
				timeout: DefaultTimeout,
			},
		},
		{
			name: "custom base url",
			opts: []ClientOption{WithBaseURL("https://custom.api.com/")},
			expect: struct {
				baseURL string
				timeout time.Duration
			}{
				baseURL: "https://custom.api.com",
				timeout: DefaultTimeout,
			},
		},
		{
			name: "custom timeout",
			opts: []ClientOption{WithTimeout(60 * time.Second)},
			expect: struct {
				baseURL string
				timeout time.Duration
			}{
				baseURL: DefaultBaseURL,
				timeout: 60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.opts...)
			defer c.Close()

			if c.baseURL != tt.expect.baseURL {
				t.Errorf("baseURL = %s, want %s", c.baseURL, tt.expect.baseURL)
			}
			if c.httpClient.Timeout != tt.expect.timeout {
				t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, tt.expect.timeout)
			}
		})
	}
}

func TestGetMarkets(t *testing.T) {
	mockMarkets := []Market{
		{
			ID:           "1",
			ConditionID:  "0x123",
			Question:     "Will X happen?",
			Slug:         "will-x-happen",
			Outcomes:     []string{"Yes", "No"},
			Volume:       "100000",
			VolumeNum:    100000,
			Liquidity:    "50000",
			LiquidityNum: 50000,
			Active:       true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockMarkets)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	markets, err := client.GetMarkets(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetMarkets() error = %v", err)
	}

	if len(markets) != 1 {
		t.Fatalf("len(markets) = %d, want 1", len(markets))
	}

	if markets[0].ConditionID != "0x123" {
		t.Errorf("ConditionID = %s, want 0x123", markets[0].ConditionID)
	}
}

func TestGetMarketsWithFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("active") != "true" {
			t.Errorf("expected active=true, got %s", r.URL.Query().Get("active"))
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("tag") != "politics" {
			t.Errorf("expected tag=politics, got %s", r.URL.Query().Get("tag"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Market{})
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	active := true
	filter := &MarketsFilter{
		Active: &active,
		Limit:  10,
		Tag:    "politics",
	}

	_, err := client.GetMarkets(context.Background(), filter)
	if err != nil {
		t.Fatalf("GetMarkets() error = %v", err)
	}
}

func TestGetMarket(t *testing.T) {
	mockMarket := Market{
		ID:          "1",
		ConditionID: "0xabc",
		Question:    "Test question?",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/0xabc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockMarket)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	market, err := client.GetMarket(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("GetMarket() error = %v", err)
	}

	if market.ConditionID != "0xabc" {
		t.Errorf("ConditionID = %s, want 0xabc", market.ConditionID)
	}
}

func TestSearchMarkets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != "bitcoin" {
			t.Errorf("expected query=bitcoin, got %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("expected limit=20, got %s", r.URL.Query().Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Market{})
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	_, err := client.SearchMarkets(context.Background(), "bitcoin", 20)
	if err != nil {
		t.Fatalf("SearchMarkets() error = %v", err)
	}
}

func TestFindSumToOneArbitrage(t *testing.T) {
	mockMarkets := []Market{
		{
			ID:            "1",
			ConditionID:   "0xarb1",
			Question:      "Arbitrage opportunity",
			Outcomes:      []string{"Yes", "No"},
			OutcomePrices: []string{"0.45", "0.50"},
			Volume:        "100000",
			VolumeNum:     100000,
			Liquidity:     "50000",
			LiquidityNum:  50000,
			Active:        true,
		},
		{
			ID:            "2",
			ConditionID:   "0xnoarb",
			Question:      "No arbitrage",
			Outcomes:      []string{"Yes", "No"},
			OutcomePrices: []string{"0.55", "0.50"},
			Volume:        "100000",
			VolumeNum:     100000,
			Liquidity:     "50000",
			LiquidityNum:  50000,
			Active:        true,
		},
		{
			ID:            "3",
			ConditionID:   "0xlowvol",
			Question:      "Low volume arb",
			Outcomes:      []string{"Yes", "No"},
			OutcomePrices: []string{"0.40", "0.50"},
			Volume:        "100",
			VolumeNum:     100,
			Liquidity:     "50",
			LiquidityNum:  50,
			Active:        true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockMarkets)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	opportunities, err := client.FindSumToOneArbitrage(context.Background(), 1000, 1000, 100)
	if err != nil {
		t.Fatalf("FindSumToOneArbitrage() error = %v", err)
	}

	if len(opportunities) != 1 {
		t.Fatalf("len(opportunities) = %d, want 1", len(opportunities))
	}

	arb := opportunities[0]
	if arb.ConditionID != "0xarb1" {
		t.Errorf("ConditionID = %s, want 0xarb1", arb.ConditionID)
	}

	expectedTotal := 0.45 + 0.50
	if arb.TotalPrice != expectedTotal {
		t.Errorf("TotalPrice = %f, want %f", arb.TotalPrice, expectedTotal)
	}

	if arb.ProfitMargin <= 0 {
		t.Errorf("ProfitMargin = %f, expected positive", arb.ProfitMargin)
	}
}

func TestHealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Market{})
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() error = %v", err)
	}
}

func TestHealthCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	defer client.Close()

	if err := client.HealthCheck(context.Background()); err == nil {
		t.Error("HealthCheck() expected error, got nil")
	}
}

func TestMarketsFilterToURLValues(t *testing.T) {
	active := true
	closed := false

	filter := &MarketsFilter{
		Active:      &active,
		Closed:      &closed,
		Limit:       50,
		Offset:      10,
		Tag:         "crypto",
		Slug:        "btc-price",
		ConditionID: "0x123",
		Query:       "bitcoin",
		Sort:        "volume",
		Order:       "desc",
	}

	v := filter.ToURLValues()

	if v.Get("active") != "true" {
		t.Errorf("active = %s, want true", v.Get("active"))
	}
	if v.Get("closed") != "false" {
		t.Errorf("closed = %s, want false", v.Get("closed"))
	}
	if v.Get("limit") != "50" {
		t.Errorf("limit = %s, want 50", v.Get("limit"))
	}
	if v.Get("tag") != "crypto" {
		t.Errorf("tag = %s, want crypto", v.Get("tag"))
	}
	if v.Get("query") != "bitcoin" {
		t.Errorf("query = %s, want bitcoin", v.Get("query"))
	}
}

func TestClientBaseURL(t *testing.T) {
	client := NewClient(WithBaseURL("https://test.api.com"))
	defer client.Close()

	if client.BaseURL() != "https://test.api.com" {
		t.Errorf("BaseURL() = %s, want https://test.api.com", client.BaseURL())
	}
}
