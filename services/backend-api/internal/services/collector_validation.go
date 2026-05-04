package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// validateMarketData validates ticker data before saving to database
func (c *CollectorService) validateMarketData(ticker *models.MarketPrice, exchange, symbol string) error {
	// Check for zero or negative price
	if ticker.Price.IsZero() || ticker.Price.IsNegative() {
		return fmt.Errorf("invalid price: %s for %s on %s", ticker.Price, symbol, exchange)
	}

	// Check for extremely high prices (potential data corruption)
	maxPrice := decimal.NewFromFloat(10000000) // 10 million
	if ticker.Price.GreaterThan(maxPrice) {
		return fmt.Errorf("extremely high price: %s for %s on %s", ticker.Price, symbol, exchange)
	}

	// Check for negative volume
	if ticker.Volume.IsNegative() {
		return fmt.Errorf("negative volume: %s for %s on %s", ticker.Volume, symbol, exchange)
	}

	// Check for invalid timestamp
	timestamp := ticker.Timestamp
	now := time.Now()

	// Check if timestamp is in the future (more than 1 minute)
	if timestamp.After(now.Add(time.Minute)) {
		return fmt.Errorf("future timestamp: %s for %s on %s", timestamp, symbol, exchange)
	}

	// Check if timestamp is too old (more than 24 hours)
	if timestamp.Before(now.Add(-24 * time.Hour)) {
		return fmt.Errorf("old timestamp: %s for %s on %s", timestamp, symbol, exchange)
	}

	// Anti-manipulation: Check for price outliers (>50% move in 1 minute)
	if err := c.checkPriceOutlier(ticker, exchange, symbol); err != nil {
		return err
	}

	// Anti-manipulation: Check for volume anomalies (potential wash trading)
	if err := c.checkVolumeAnomaly(ticker, exchange, symbol); err != nil {
		return err
	}

	return nil
}

// checkPriceOutlier checks if price moved more than 50% in 1 minute (potential manipulation)
func (c *CollectorService) checkPriceOutlier(ticker *models.MarketPrice, exchange, symbol string) error {
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	var prevPrice decimal.Decimal
	var prevTime time.Time

	if cached, ok := c.lastPrice.Load(key); ok {
		entry := cached.(priceCacheEntry)
		prevPrice = entry.price
		prevTime = entry.timestamp
	}

	if prevPrice.IsZero() {
		c.lastPrice.Store(key, priceCacheEntry{
			price:     ticker.Price,
			timestamp: ticker.Timestamp,
		})
		return nil
	}

	timeDiff := ticker.Timestamp.Sub(prevTime)
	if timeDiff < 30*time.Second {
		c.lastPrice.Store(key, priceCacheEntry{
			price:     ticker.Price,
			timestamp: ticker.Timestamp,
		})
		return nil
	}

	priceChange := ticker.Price.Sub(prevPrice).Abs().Div(prevPrice).Mul(decimal.NewFromInt(100))

	maxAllowedChange := decimal.NewFromFloat(50.0)
	if priceChange.GreaterThan(maxAllowedChange) {
		c.logger.WithFields(map[string]interface{}{
			"exchange":       exchange,
			"symbol":         symbol,
			"previous_price": prevPrice,
			"current_price":  ticker.Price,
			"price_change":   priceChange,
			"time_diff_sec":  timeDiff.Seconds(),
		}).Warn("Price outlier detected - potential manipulation")

		c.blacklistCache.Add(
			key,
			fmt.Sprintf("Price moved %.1f%% in %.0fs", priceChange.InexactFloat64(), timeDiff.Seconds()),
			5*time.Minute,
		)

		return fmt.Errorf("price outlier detected: %.1f%% change in %.0fs (max 50%% allowed)", priceChange.InexactFloat64(), timeDiff.Seconds())
	}

	c.lastPrice.Store(key, priceCacheEntry{
		price:     ticker.Price,
		timestamp: ticker.Timestamp,
	})

	return nil
}

// checkVolumeAnomaly checks for suspicious volume patterns (potential wash trading)
func (c *CollectorService) checkVolumeAnomaly(ticker *models.MarketPrice, exchange, symbol string) error {
	// Skip if volume is zero (no trading)
	if ticker.Volume.IsZero() {
		return nil
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	var avgVolume decimal.Decimal
	var sampleCount int

	if cached, ok := c.volumeStats.Load(key); ok {
		entry := cached.(volumeStatsEntry)
		avgVolume = entry.avgVolume
		sampleCount = entry.sampleCount
	}

	if sampleCount < 5 {
		newAvg := avgVolume.Mul(decimal.NewFromInt(int64(sampleCount))).Add(ticker.Volume).
			Div(decimal.NewFromInt(int64(sampleCount + 1)))

		c.volumeStats.Store(key, volumeStatsEntry{
			avgVolume:   newAvg,
			sampleCount: sampleCount + 1,
		})
		return nil
	}

	volumeRatio := ticker.Volume.Div(avgVolume)
	maxVolumeRatio := decimal.NewFromFloat(10.0)

	if volumeRatio.GreaterThan(maxVolumeRatio) {
		c.logger.WithFields(map[string]interface{}{
			"exchange":       exchange,
			"symbol":         symbol,
			"current_volume": ticker.Volume,
			"avg_volume":     avgVolume,
			"volume_ratio":   volumeRatio,
		}).Debug("High volume detected - possible wash trading")

		newAvg := avgVolume.Mul(decimal.NewFromFloat(0.7)).Add(ticker.Volume.Mul(decimal.NewFromFloat(0.3)))
		c.volumeStats.Store(key, volumeStatsEntry{
			avgVolume:   newAvg,
			sampleCount: sampleCount + 1,
		})

		return nil
	}

	newAvg := avgVolume.Mul(decimal.NewFromFloat(0.9)).Add(ticker.Volume.Mul(decimal.NewFromFloat(0.1)))
	c.volumeStats.Store(key, volumeStatsEntry{
		avgVolume:   newAvg,
		sampleCount: sampleCount + 1,
	})

	return nil
}

// ensureTradingPairExists ensures a trading pair exists in the database
func (c *CollectorService) ensureTradingPairExists(exchangeID int, symbol string) error {
	_, err := c.getOrCreateTradingPair(exchangeID, symbol)
	return err
}

// getOrCreateTradingPair gets or creates a trading pair and returns its ID
func (c *CollectorService) getOrCreateTradingPair(exchangeID int, symbol string) (int, error) {
	// Try Redis cache first if available
	if c.redisClient != nil {
		cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
		cachedID, err := c.redisClient.Get(c.ctx, cacheKey).Result()
		if err == nil {
			if tradingPairID, parseErr := strconv.Atoi(cachedID); parseErr == nil {
				if c.cachedTradingPairIDExists(exchangeID, symbol, tradingPairID) {
					return tradingPairID, nil
				}
				c.redisClient.Del(c.ctx, cacheKey)
			}
		}
	}

	// First try to get existing trading pair for this exchange and symbol
	var tradingPairID int
	err := c.db.QueryRow(c.ctx, "SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?", exchangeID, symbol).Scan(&tradingPairID)
	if err == nil {
		// Cache the result if Redis is available
		if c.redisClient != nil {
			cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
			c.redisClient.Set(c.ctx, cacheKey, tradingPairID, 24*time.Hour)
		}
		return tradingPairID, nil
	}

	// If not found, create new trading pair
	baseCurrency, quoteCurrency := c.parseSymbol(symbol)
	if baseCurrency == "" || quoteCurrency == "" {
		return 0, fmt.Errorf("failed to parse symbol: %s", symbol)
	}

	// Insert new trading pair - SQLite compatible
	_, err = c.db.Exec(c.ctx,
		"INSERT OR IGNORE INTO trading_pairs (exchange_id, symbol, base_currency, quote_currency, is_active) VALUES (?, ?, ?, ?, 1)",
		exchangeID, symbol, baseCurrency, quoteCurrency)
	if err != nil {
		return 0, fmt.Errorf("failed to create trading pair: %w", err)
	}

	// Get the trading pair ID
	err = c.db.QueryRow(c.ctx, "SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?", exchangeID, symbol).Scan(&tradingPairID)
	if err != nil {
		return 0, fmt.Errorf("failed to get trading pair after insert: %w", err)
	}

	// Cache the newly created trading pair if Redis is available
	if c.redisClient != nil {
		cacheKey := fmt.Sprintf("trading_pair:%d:%s", exchangeID, symbol)
		c.redisClient.Set(c.ctx, cacheKey, tradingPairID, 24*time.Hour)
	}

	c.logger.WithFields(map[string]interface{}{
		"symbol":          symbol,
		"exchange_id":     exchangeID,
		"trading_pair_id": tradingPairID,
	}).Info("Created new trading pair")
	return tradingPairID, nil
}

// getOrCreateExchange gets or creates an exchange and returns its ID
func (c *CollectorService) getOrCreateExchange(ccxtID string) (int, error) {
	// Check Redis cache first (if redis client is available)
	cacheKey := fmt.Sprintf("exchange:ccxt_id:%s", ccxtID)
	if c.redisClient != nil {
		if cachedID, err := c.redisClient.Get(c.ctx, cacheKey).Result(); err == nil {
			if exchangeID, err := strconv.Atoi(cachedID); err == nil {
				if c.cachedExchangeIDExists(ccxtID, exchangeID) {
					return exchangeID, nil
				}
				c.redisClient.Del(c.ctx, cacheKey)
			}
		}
	}

	// First try to get existing exchange by ccxt_id
	var exchangeID int
	err := c.db.QueryRow(c.ctx, "SELECT id FROM exchanges WHERE ccxt_id = ?", ccxtID).Scan(&exchangeID)
	if err == nil {
		// Cache the result
		if c.redisClient != nil {
			c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)
		}
		return exchangeID, nil
	}

	// Also check by name in case exchange exists with different ccxt_id
	name := strings.ToLower(ccxtID)
	err = c.db.QueryRow(c.ctx, "SELECT id FROM exchanges WHERE LOWER(name) = ?", name).Scan(&exchangeID)
	if err == nil {
		c.logger.WithFields(map[string]interface{}{
			"name":        name,
			"exchange_id": exchangeID,
		}).Info("Found existing exchange by name")
		// Cache the result
		if c.redisClient != nil {
			c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)
		}
		return exchangeID, nil
	}

	// If not found, create new exchange with basic information
	caser := cases.Title(language.English)
	displayName := caser.String(ccxtID)

	// Insert new exchange - SQLite compatible (use INSERT OR IGNORE)
	_, err = c.db.Exec(c.ctx,
		"INSERT OR IGNORE INTO exchanges (name, display_name, ccxt_id, api_url, status, has_spot, has_futures) VALUES (?, ?, ?, ?, 'active', 1, 1)",
		name, displayName, ccxtID, fmt.Sprintf("https://api.%s.com", strings.ToLower(ccxtID)))
	if err != nil {
		return 0, fmt.Errorf("failed to create exchange: %w", err)
	}

	// Get the exchange ID
	err = c.db.QueryRow(c.ctx, "SELECT id FROM exchanges WHERE ccxt_id = ?", ccxtID).Scan(&exchangeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get exchange after insert: %w", err)
	}

	// Cache the newly created/updated exchange
	if c.redisClient != nil {
		c.redisClient.Set(c.ctx, cacheKey, exchangeID, 24*time.Hour)
	}

	c.logger.WithFields(map[string]interface{}{
		"ccxt_id":     ccxtID,
		"exchange_id": exchangeID,
	}).Info("Created or updated exchange")
	return exchangeID, nil
}

func (c *CollectorService) cachedTradingPairIDExists(exchangeID int, symbol string, tradingPairID int) bool {
	if c.db == nil {
		return false
	}
	var exists int
	err := c.db.QueryRow(
		c.ctx,
		"SELECT 1 FROM trading_pairs WHERE id = ? AND exchange_id = ? AND symbol = ?",
		tradingPairID,
		exchangeID,
		symbol,
	).Scan(&exists)
	if err == nil {
		return true
	}
	if !isCollectorNoRows(err) {
		c.logger.WithFields(map[string]interface{}{
			"exchange_id":      exchangeID,
			"symbol":           symbol,
			"trading_pair_id":  tradingPairID,
			"cache_validation": "trading_pair",
		}).WithError(err).Warn("Failed to validate cached trading pair ID")
	}
	return false
}

func (c *CollectorService) cachedExchangeIDExists(ccxtID string, exchangeID int) bool {
	if c.db == nil {
		return false
	}
	var exists int
	err := c.db.QueryRow(
		c.ctx,
		"SELECT 1 FROM exchanges WHERE id = ? AND (ccxt_id = ? OR LOWER(name) = ?)",
		exchangeID,
		ccxtID,
		strings.ToLower(ccxtID),
	).Scan(&exists)
	if err == nil {
		return true
	}
	if !isCollectorNoRows(err) {
		c.logger.WithFields(map[string]interface{}{
			"ccxt_id":          ccxtID,
			"exchange_id":      exchangeID,
			"cache_validation": "exchange",
		}).WithError(err).Warn("Failed to validate cached exchange ID")
	}
	return false
}

func isCollectorNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows)
}
