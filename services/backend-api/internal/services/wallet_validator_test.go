package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestWalletValidatorConfig_Defaults(t *testing.T) {
	config := DefaultWalletValidatorConfig()

	if !config.MinimumUSDCBalance.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected MinimumUSDCBalance to be 100, got %s", config.MinimumUSDCBalance)
	}

	if !config.MinimumPortfolioValue.Equal(decimal.NewFromInt(500)) {
		t.Errorf("expected MinimumPortfolioValue to be 500, got %s", config.MinimumPortfolioValue)
	}

	if config.MinimumExchangeConnections != 1 {
		t.Errorf("expected MinimumExchangeConnections to be 1, got %d", config.MinimumExchangeConnections)
	}
}

func TestWalletValidationMetrics_Increment(t *testing.T) {
	m := WalletValidationMetrics{ChecksByExchange: make(map[string]int64)}

	m.IncrementTotalChecks()
	m.IncrementPassedChecks()
	m.IncrementFailedChecks()
	m.IncrementInsufficientBalance()
	m.IncrementInsufficientValue()
	m.IncrementNoExchanges()
	m.IncrementChecksByExchange("binance")

	metrics := m.GetMetrics()

	if metrics.TotalChecks != 1 {
		t.Errorf("expected TotalChecks to be 1, got %d", metrics.TotalChecks)
	}
	if metrics.PassedChecks != 1 {
		t.Errorf("expected PassedChecks to be 1, got %d", metrics.PassedChecks)
	}
	if metrics.FailedChecks != 1 {
		t.Errorf("expected FailedChecks to be 1, got %d", metrics.FailedChecks)
	}
	if metrics.InsufficientBalance != 1 {
		t.Errorf("expected InsufficientBalance to be 1, got %d", metrics.InsufficientBalance)
	}
	if metrics.InsufficientValue != 1 {
		t.Errorf("expected InsufficientValue to be 1, got %d", metrics.InsufficientValue)
	}
	if metrics.NoExchanges != 1 {
		t.Errorf("expected NoExchanges to be 1, got %d", metrics.NoExchanges)
	}
	if metrics.ChecksByExchange["binance"] != 1 {
		t.Errorf("expected ChecksByExchange[binance] to be 1, got %d", metrics.ChecksByExchange["binance"])
	}
}

func TestWalletValidationMetrics_Reset(t *testing.T) {
	m := WalletValidationMetrics{
		TotalChecks:         10,
		PassedChecks:        5,
		FailedChecks:        5,
		InsufficientBalance: 3,
		InsufficientValue:   2,
		NoExchanges:         1,
		ChecksByExchange:    map[string]int64{"binance": 5},
	}

	m.Reset()

	if m.TotalChecks != 0 {
		t.Errorf("expected TotalChecks to be 0 after reset, got %d", m.TotalChecks)
	}
	if m.PassedChecks != 0 {
		t.Errorf("expected PassedChecks to be 0 after reset, got %d", m.PassedChecks)
	}
	if len(m.ChecksByExchange) != 0 {
		t.Errorf("expected ChecksByExchange to be empty after reset, got %v", m.ChecksByExchange)
	}
}

func TestWalletValidator_NewValidator(t *testing.T) {
	config := DefaultWalletValidatorConfig()
	validator := NewWalletValidator(nil, config)

	if validator == nil {
		t.Fatal("expected validator to not be nil")
	}

	if !validator.GetConfig().MinimumUSDCBalance.Equal(config.MinimumUSDCBalance) {
		t.Errorf("config not set correctly")
	}
}

func TestWalletValidator_SetGetConfig(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	newConfig := WalletValidatorConfig{
		MinimumUSDCBalance:         decimal.NewFromInt(200),
		MinimumPortfolioValue:      decimal.NewFromInt(1000),
		MinimumExchangeConnections: 2,
	}

	validator.SetConfig(newConfig)
	gotConfig := validator.GetConfig()

	if !gotConfig.MinimumUSDCBalance.Equal(decimal.NewFromInt(200)) {
		t.Errorf("expected MinimumUSDCBalance to be 200, got %s", gotConfig.MinimumUSDCBalance)
	}

	if gotConfig.MinimumExchangeConnections != 2 {
		t.Errorf("expected MinimumExchangeConnections to be 2, got %d", gotConfig.MinimumExchangeConnections)
	}
}

func TestWalletValidator_CheckWalletMinimums_NilDB(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	_, err := validator.CheckWalletMinimums(context.Background(), "test-chat-id")
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestWalletValidator_ValidateForTrading_NilDB(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	err := validator.ValidateForTrading(context.Background(), "test-chat-id")
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestWalletValidator_QuickCheck_NilDB(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	result := validator.QuickCheck(context.Background(), "test-chat-id")
	if result {
		t.Error("expected QuickCheck to return false with nil database")
	}
}

func TestWalletValidator_EnsureWalletMinimums_NilDB(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	_, _, err := validator.EnsureWalletMinimums(context.Background(), "test-chat-id")
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestWalletValidator_GetMetrics(t *testing.T) {
	validator := NewWalletValidator(nil, DefaultWalletValidatorConfig())

	metrics := validator.GetMetrics()

	if metrics.ChecksByExchange == nil {
		t.Error("expected ChecksByExchange to be initialized")
	}
}

func TestWalletBalanceStatus_Defaults(t *testing.T) {
	status := &WalletBalanceStatus{
		ChatID:              "test-chat",
		IsValid:             true,
		FailedChecks:        []string{},
		USDCBalance:         decimal.Zero,
		PortfolioValue:      decimal.Zero,
		MinimumRequirements: DefaultWalletValidatorConfig(),
	}

	if status.ChatID != "test-chat" {
		t.Errorf("expected ChatID to be test-chat, got %s", status.ChatID)
	}

	if !status.IsValid {
		t.Error("expected IsValid to be true")
	}

	if len(status.FailedChecks) != 0 {
		t.Errorf("expected FailedChecks to be empty, got %v", status.FailedChecks)
	}
}
