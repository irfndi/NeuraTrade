package models

import "time"

// CorrelationMatrix represents pairwise correlation results for a set of symbols.
type CorrelationMatrix struct {
	Exchange    string      `json:"exchange"`
	Symbols     []string    `json:"symbols"`
	Matrix      [][]float64 `json:"matrix"`
	WindowSize  int         `json:"window_size"`
	GeneratedAt time.Time   `json:"generated_at"`
}

// MarketRegime represents detected market regime characteristics.
type MarketRegime struct {
	Symbol          string    `json:"symbol"`
	Exchange        string    `json:"exchange"`
	Trend           string    `json:"trend"`            // bullish, bearish, sideways
	Volatility      string    `json:"volatility"`       // low, normal, high
	TrendStrength   float64   `json:"trend_strength"`   // 0-1
	VolatilityScore float64   `json:"volatility_score"` // 0-1
	Confidence      float64   `json:"confidence"`       // 0-1
	WindowSize      int       `json:"window_size"`
	GeneratedAt     time.Time `json:"generated_at"`
}

// ForecastPoint represents a single forecasted value with variance.
type ForecastPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Variance  float64   `json:"variance"`
}

// ForecastResult contains the output of a forecasting run.
type ForecastResult struct {
	Symbol       string          `json:"symbol"`
	Exchange     string          `json:"exchange"`
	Model        string          `json:"model"`
	SeriesType   string          `json:"series_type"` // price or funding
	Horizon      int             `json:"horizon"`
	LastObserved float64         `json:"last_observed"`
	Points       []ForecastPoint `json:"points"`
	GeneratedAt  time.Time       `json:"generated_at"`
}
