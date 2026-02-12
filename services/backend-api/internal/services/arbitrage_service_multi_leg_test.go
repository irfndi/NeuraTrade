package services

import (
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestArbitrageService_StoreMultiLegOpportunities(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	dbPool := database.NewMockDBPool(mockPool)
	cfg := &config.Config{}

	service := NewArbitrageService(dbPool, cfg, nil)

	opps := []models.MultiLegOpportunity{
		{
			ID:               "test-id",
			ExchangeName:     "binance",
			ProfitPercentage: decimal.NewFromFloat(1.5),
			DetectedAt:       time.Now(),
			ExpiresAt:        time.Now().Add(1 * time.Minute),
			Legs: []models.ArbitrageLeg{
				{Symbol: "BTC/USDT", Side: "buy", Price: decimal.NewFromInt(50000)},
				{Symbol: "ETH/BTC", Side: "buy", Price: decimal.NewFromFloat(0.05)},
				{Symbol: "ETH/USDT", Side: "sell", Price: decimal.NewFromInt(2600)},
			},
		},
	}

	// Expectations
	mockPool.ExpectBegin()

	// Expect main opportunity insert
	mockPool.ExpectExec("INSERT INTO multi_leg_opportunities").
		WithArgs("test-id", "binance", decimal.NewFromFloat(1.5), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Expect 3 legs inserts
	for i := 0; i < 3; i++ {
		mockPool.ExpectExec("INSERT INTO multi_leg_legs").
			WithArgs("test-id", i, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
	}

	mockPool.ExpectCommit()

	err = service.storeMultiLegOpportunities(opps)
	assert.NoError(t, err)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}
