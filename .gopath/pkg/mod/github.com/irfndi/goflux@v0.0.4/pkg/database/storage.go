package database

// Storage represents a generic time series storage interface
type Storage interface {
	// StoreCandle stores a single candle in the database
	StoreCandle(symbol string, candle *Candle) error

	// StoreCandles stores multiple candles in the database
	StoreCandles(symbol string, candles []*Candle) error

	// GetCandles retrieves candles for a symbol within a time range
	GetCandles(symbol string, startTime int64, endTime int64) ([]*Candle, error)

	// GetLatestCandles retrieves the most recent N candles for a symbol
	GetLatestCandles(symbol string, limit int) ([]*Candle, error)

	// DeleteSymbol removes all data for a symbol
	DeleteSymbol(symbol string) error

	// Close closes the database connection
	Close() error
}

// Candle represents OHLCV data stored in database
type Candle struct {
	Symbol    string
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}
