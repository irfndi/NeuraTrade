package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type WalletValidatorConfig struct {
	MinimumUSDCBalance         decimal.Decimal `json:"minimum_usdc_balance"`
	MinimumPortfolioValue      decimal.Decimal `json:"minimum_portfolio_value"`
	MinimumExchangeConnections int             `json:"minimum_exchange_connections"`
}

func DefaultWalletValidatorConfig() WalletValidatorConfig {
	return WalletValidatorConfig{
		MinimumUSDCBalance:         decimal.NewFromInt(100),
		MinimumPortfolioValue:      decimal.NewFromInt(500),
		MinimumExchangeConnections: 1,
	}
}

type WalletBalanceStatus struct {
	UserID              string                `json:"user_id"`
	ChatID              string                `json:"chat_id"`
	IsValid             bool                  `json:"is_valid"`
	USDCBalance         decimal.Decimal       `json:"usdc_balance"`
	PortfolioValue      decimal.Decimal       `json:"portfolio_value"`
	ExchangeCount       int                   `json:"exchange_count"`
	FailedChecks        []string              `json:"failed_checks,omitempty"`
	CheckedAt           time.Time             `json:"checked_at"`
	MinimumRequirements WalletValidatorConfig `json:"minimum_requirements"`
}

type WalletValidationMetrics struct {
	mu                  sync.RWMutex
	TotalChecks         int64            `json:"total_checks"`
	PassedChecks        int64            `json:"passed_checks"`
	FailedChecks        int64            `json:"failed_checks"`
	InsufficientBalance int64            `json:"insufficient_balance"`
	InsufficientValue   int64            `json:"insufficient_value"`
	NoExchanges         int64            `json:"no_exchanges"`
	ChecksByExchange    map[string]int64 `json:"checks_by_exchange"`
}

func (m *WalletValidationMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalChecks = 0
	m.PassedChecks = 0
	m.FailedChecks = 0
	m.InsufficientBalance = 0
	m.InsufficientValue = 0
	m.NoExchanges = 0
	m.ChecksByExchange = make(map[string]int64)
}

func (m *WalletValidationMetrics) IncrementTotalChecks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalChecks++
}

func (m *WalletValidationMetrics) IncrementPassedChecks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PassedChecks++
}

func (m *WalletValidationMetrics) IncrementFailedChecks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedChecks++
}

func (m *WalletValidationMetrics) IncrementInsufficientBalance() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InsufficientBalance++
}

func (m *WalletValidationMetrics) IncrementInsufficientValue() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InsufficientValue++
}

func (m *WalletValidationMetrics) IncrementNoExchanges() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.NoExchanges++
}

func (m *WalletValidationMetrics) IncrementChecksByExchange(exchange string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ChecksByExchange == nil {
		m.ChecksByExchange = make(map[string]int64)
	}
	m.ChecksByExchange[exchange]++
}

func (m *WalletValidationMetrics) GetMetrics() WalletValidationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return WalletValidationMetrics{
		TotalChecks:         m.TotalChecks,
		PassedChecks:        m.PassedChecks,
		FailedChecks:        m.FailedChecks,
		InsufficientBalance: m.InsufficientBalance,
		InsufficientValue:   m.InsufficientValue,
		NoExchanges:         m.NoExchanges,
		ChecksByExchange:    m.ChecksByExchange,
	}
}

type WalletValidator struct {
	config  WalletValidatorConfig
	db      DBPool
	metrics WalletValidationMetrics
	mu      sync.RWMutex
}

func NewWalletValidator(db DBPool, config WalletValidatorConfig) *WalletValidator {
	return &WalletValidator{
		config:  config,
		db:      db,
		metrics: WalletValidationMetrics{ChecksByExchange: make(map[string]int64)},
	}
}

func (wv *WalletValidator) CheckWalletMinimums(ctx context.Context, chatID string) (*WalletBalanceStatus, error) {
	wv.metrics.IncrementTotalChecks()

	status := &WalletBalanceStatus{
		ChatID:              chatID,
		CheckedAt:           time.Now().UTC(),
		IsValid:             true,
		FailedChecks:        make([]string, 0),
		MinimumRequirements: wv.config,
		USDCBalance:         decimal.Zero,
		PortfolioValue:      decimal.Zero,
	}

	exchangeCount, err := wv.getExchangeCount(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange count: %w", err)
	}
	status.ExchangeCount = exchangeCount

	if exchangeCount < wv.config.MinimumExchangeConnections {
		status.IsValid = false
		status.FailedChecks = append(status.FailedChecks, "exchange_minimum")
		wv.metrics.IncrementNoExchanges()
	}

	balances, err := wv.getWalletBalances(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet balances: %w", err)
	}

	if usdc, ok := balances["USDC"]; ok {
		status.USDCBalance = usdc
	} else if usdcLower, ok := balances["usdc"]; ok {
		status.USDCBalance = usdcLower
	}

	for _, balance := range balances {
		status.PortfolioValue = status.PortfolioValue.Add(balance)
	}

	if status.USDCBalance.LessThan(wv.config.MinimumUSDCBalance) {
		status.IsValid = false
		status.FailedChecks = append(status.FailedChecks, "usdc_balance_minimum")
		wv.metrics.IncrementInsufficientBalance()
	}

	if status.PortfolioValue.LessThan(wv.config.MinimumPortfolioValue) {
		status.IsValid = false
		status.FailedChecks = append(status.FailedChecks, "portfolio_value_minimum")
		wv.metrics.IncrementInsufficientValue()
	}

	if status.IsValid {
		wv.metrics.IncrementPassedChecks()
	} else {
		wv.metrics.IncrementFailedChecks()
	}

	return status, nil
}

func (wv *WalletValidator) ValidateForTrading(ctx context.Context, chatID string) error {
	status, err := wv.CheckWalletMinimums(ctx, chatID)
	if err != nil {
		return fmt.Errorf("wallet validation failed: %w", err)
	}

	if !status.IsValid {
		return fmt.Errorf("wallet minimum requirements not met: %v", status.FailedChecks)
	}

	return nil
}

func (wv *WalletValidator) getExchangeCount(ctx context.Context, chatID string) (int, error) {
	if wv.db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	var count int
	err := wv.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT provider)
		FROM telegram_operator_wallets
		WHERE chat_id = $1
		  AND wallet_type = 'exchange'
		  AND status = 'connected'
	`, chatID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count exchanges: %w", err)
	}

	return count, nil
}

func (wv *WalletValidator) getWalletBalances(ctx context.Context, chatID string) (map[string]decimal.Decimal, error) {
	if wv.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	balances := make(map[string]decimal.Decimal)

	var walletCount int
	err := wv.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM telegram_operator_wallets
		WHERE chat_id = $1
		  AND status = 'connected'
	`, chatID).Scan(&walletCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count wallets: %w", err)
	}

	if walletCount == 0 {
		return balances, nil
	}

	// TODO: Implement actual balance fetching via CCXT service (neura-adu)
	// Returning empty balances will cause validation to fail until implemented

	return balances, nil
}

func (wv *WalletValidator) GetMetrics() WalletValidationMetrics {
	return wv.metrics.GetMetrics()
}

func (wv *WalletValidator) SetConfig(config WalletValidatorConfig) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	wv.config = config
}

func (wv *WalletValidator) GetConfig() WalletValidatorConfig {
	wv.mu.RLock()
	defer wv.mu.RUnlock()
	return wv.config
}

func (wv *WalletValidator) QuickCheck(ctx context.Context, chatID string) bool {
	status, err := wv.CheckWalletMinimums(ctx, chatID)
	if err != nil {
		return false
	}
	return status.IsValid
}

func (wv *WalletValidator) EnsureWalletMinimums(ctx context.Context, chatID string) (bool, []string, error) {
	status, err := wv.CheckWalletMinimums(ctx, chatID)
	if err != nil {
		return false, nil, err
	}
	return status.IsValid, status.FailedChecks, nil
}
