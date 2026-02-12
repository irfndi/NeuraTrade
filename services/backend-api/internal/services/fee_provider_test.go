package services

import (
	"context"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestDBFeeProvider_GetTakerFee_UsesDatabase(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	mockPool.ExpectQuery("SELECT etp.taker_fee, etp.maker_fee").
		WithArgs("binance", "BTC/USDT").
		WillReturnRows(pgxmock.NewRows([]string{"taker_fee", "maker_fee"}).
			AddRow(decimal.NewFromFloat(0.001), decimal.NewFromFloat(0.001)))

	provider := NewDBFeeProvider(dbPool, decimal.NewFromFloat(0.001), decimal.NewFromFloat(0.001))

	fee, err := provider.GetTakerFee(context.Background(), "binance", "BTC/USDT")
	assert.NoError(t, err)
	assert.Equal(t, decimal.NewFromFloat(0.001), fee)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestDBFeeProvider_GetMakerFee_DefaultOnError(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	mockPool.ExpectQuery("SELECT etp.taker_fee, etp.maker_fee").
		WithArgs("binance", "ETH/USDT").
		WillReturnError(assert.AnError)

	mockPool.ExpectQuery("SELECT ef.taker_fee, ef.maker_fee").
		WithArgs("binance").
		WillReturnError(assert.AnError)

	provider := NewDBFeeProvider(dbPool, decimal.NewFromFloat(0.001), decimal.NewFromFloat(0.0005))

	fee, err := provider.GetMakerFee(context.Background(), "binance", "ETH/USDT")
	assert.NoError(t, err)
	assert.Equal(t, decimal.NewFromFloat(0.0005), fee)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestDBFeeProvider_GetTakerFee_FallbackToExchange(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	// Pair not found
	mockPool.ExpectQuery("SELECT etp.taker_fee, etp.maker_fee").
		WithArgs("bybit", "BTC/USDT").
		WillReturnRows(pgxmock.NewRows([]string{"taker_fee", "maker_fee"}))

	// Fallback to exchange level
	mockPool.ExpectQuery("SELECT ef.taker_fee, ef.maker_fee").
		WithArgs("bybit").
		WillReturnRows(pgxmock.NewRows([]string{"taker_fee", "maker_fee"}).
			AddRow(decimal.NewFromFloat(0.0005), decimal.NewFromFloat(0.0002)))

	provider := NewDBFeeProvider(dbPool, decimal.NewFromFloat(0.001), decimal.NewFromFloat(0.001))

	fee, err := provider.GetTakerFee(context.Background(), "bybit", "BTC/USDT")
	assert.NoError(t, err)
	assert.Equal(t, decimal.NewFromFloat(0.0005), fee)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}
