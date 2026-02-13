package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestAPIKeyPermissionConfig_Defaults(t *testing.T) {
	config := DefaultAPIKeyPermissionConfig()

	if !config.RequireTradeOnly {
		t.Error("expected RequireTradeOnly to be true")
	}

	if len(config.DeniedPermissions) != 3 {
		t.Errorf("expected 3 denied permissions, got %d", len(config.DeniedPermissions))
	}
}

func TestAPIKeyPermissionValidator_ValidateKey_NoWithdraw(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	result, err := validator.ValidateKey(context.Background(), "key1", "binance", []APIKeyPermission{PermissionTrade, PermissionRead})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsValid {
		t.Errorf("expected key to be valid, got error: %s", result.ErrorMessage)
	}

	if !result.IsTradeOnly {
		t.Error("expected key to be trade-only")
	}

	if result.HasWithdraw {
		t.Error("expected no withdraw permission")
	}
}

func TestAPIKeyPermissionValidator_ValidateKey_WithWithdraw(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	result, err := validator.ValidateKey(context.Background(), "key1", "binance", []APIKeyPermission{PermissionTrade, PermissionWithdraw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsValid {
		t.Error("expected key to be invalid due to withdraw permission")
	}

	if !result.HasWithdraw {
		t.Error("expected withdraw permission to be detected")
	}
}

func TestAPIKeyPermissionValidator_ValidateForTrading(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	err := validator.ValidateForTrading(context.Background(), "key1", "binance", []APIKeyPermission{PermissionTrade, PermissionRead})
	if err != nil {
		t.Errorf("expected valid key to pass, got: %v", err)
	}

	err = validator.ValidateForTrading(context.Background(), "key1", "binance", []APIKeyPermission{PermissionTrade, PermissionWithdraw})
	if err == nil {
		t.Error("expected error for key with withdraw permission")
	}
}

func TestAPIKeyPermissionValidator_QuickValidate(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	if !validator.QuickValidate([]APIKeyPermission{PermissionTrade, PermissionRead}) {
		t.Error("expected safe permissions to pass quick validate")
	}

	if validator.QuickValidate([]APIKeyPermission{PermissionTrade, PermissionWithdraw}) {
		t.Error("expected unsafe permissions to fail quick validate")
	}
}

func TestAPIKeyPermissionValidator_CalculateRiskScore(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	safeScore := validator.CalculateRiskScore([]APIKeyPermission{PermissionTrade, PermissionRead})
	if safeScore.LessThan(decimal.NewFromInt(80)) {
		t.Errorf("expected safe permissions to have high score, got %s", safeScore)
	}

	dangerousScore := validator.CalculateRiskScore([]APIKeyPermission{PermissionWithdraw})
	if dangerousScore.GreaterThan(decimal.NewFromInt(30)) {
		t.Errorf("expected dangerous permissions to have low score, got %s", dangerousScore)
	}
}

func TestAPIKeyPermissionValidator_Metrics(t *testing.T) {
	validator := NewAPIKeyPermissionValidator(nil, DefaultAPIKeyPermissionConfig())

	if _, err := validator.ValidateKey(context.Background(), "key1", "binance", []APIKeyPermission{PermissionTrade}); err != nil {
		t.Errorf("ValidateKey key1 failed: %v", err)
	}
	if _, err := validator.ValidateKey(context.Background(), "key2", "binance", []APIKeyPermission{PermissionWithdraw}); err != nil {
		t.Errorf("ValidateKey key2 failed: %v", err)
	}

	metrics := validator.GetMetrics()

	if metrics.TotalValidations != 2 {
		t.Errorf("expected 2 total validations, got %d", metrics.TotalValidations)
	}

	if metrics.PassedValidations != 1 {
		t.Errorf("expected 1 passed validation, got %d", metrics.PassedValidations)
	}

	if metrics.FailedValidations != 1 {
		t.Errorf("expected 1 failed validation, got %d", metrics.FailedValidations)
	}
}
