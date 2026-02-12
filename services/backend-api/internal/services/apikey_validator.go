package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type APIKeyPermission string

const (
	PermissionTrade    APIKeyPermission = "trade"
	PermissionRead     APIKeyPermission = "read"
	PermissionWithdraw APIKeyPermission = "withdraw"
	PermissionTransfer APIKeyPermission = "transfer"
	PermissionDeposit  APIKeyPermission = "deposit"
)

type APIKeyValidationResult struct {
	KeyID        string             `json:"key_id"`
	Exchange     string             `json:"exchange"`
	IsValid      bool               `json:"is_valid"`
	Permissions  []APIKeyPermission `json:"permissions"`
	HasWithdraw  bool               `json:"has_withdraw"`
	HasTransfer  bool               `json:"has_transfer"`
	IsTradeOnly  bool               `json:"is_trade_only"`
	RiskLevel    string             `json:"risk_level"`
	ValidatedAt  time.Time          `json:"validated_at"`
	ErrorMessage string             `json:"error_message,omitempty"`
}

type APIKeyPermissionConfig struct {
	RequireTradeOnly   bool               `json:"require_trade_only"`
	AllowedPermissions []APIKeyPermission `json:"allowed_permissions"`
	DeniedPermissions  []APIKeyPermission `json:"denied_permissions"`
}

func DefaultAPIKeyPermissionConfig() APIKeyPermissionConfig {
	return APIKeyPermissionConfig{
		RequireTradeOnly:   true,
		AllowedPermissions: []APIKeyPermission{PermissionTrade, PermissionRead},
		DeniedPermissions:  []APIKeyPermission{PermissionWithdraw, PermissionTransfer, PermissionDeposit},
	}
}

type APIKeyPermissionValidator struct {
	config  APIKeyPermissionConfig
	db      DBPool
	metrics APIKeyValidationMetrics
	mu      sync.RWMutex
}

type APIKeyValidationMetrics struct {
	mu                    sync.RWMutex
	TotalValidations      int64            `json:"total_validations"`
	PassedValidations     int64            `json:"passed_validations"`
	FailedValidations     int64            `json:"failed_validations"`
	WithdrawalBlocked     int64            `json:"withdrawal_blocked"`
	TransferBlocked       int64            `json:"transfer_blocked"`
	ValidationsByExchange map[string]int64 `json:"validations_by_exchange"`
}

func (m *APIKeyValidationMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalValidations++
}

func (m *APIKeyValidationMetrics) IncrementPassed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PassedValidations++
}

func (m *APIKeyValidationMetrics) IncrementFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedValidations++
}

func (m *APIKeyValidationMetrics) IncrementWithdrawalBlocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WithdrawalBlocked++
}

func (m *APIKeyValidationMetrics) IncrementTransferBlocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TransferBlocked++
}

func (m *APIKeyValidationMetrics) IncrementByExchange(exchange string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ValidationsByExchange == nil {
		m.ValidationsByExchange = make(map[string]int64)
	}
	m.ValidationsByExchange[exchange]++
}

func (m *APIKeyValidationMetrics) GetMetrics() APIKeyValidationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return APIKeyValidationMetrics{
		TotalValidations:      m.TotalValidations,
		PassedValidations:     m.PassedValidations,
		FailedValidations:     m.FailedValidations,
		WithdrawalBlocked:     m.WithdrawalBlocked,
		TransferBlocked:       m.TransferBlocked,
		ValidationsByExchange: m.ValidationsByExchange,
	}
}

func NewAPIKeyPermissionValidator(db DBPool, config APIKeyPermissionConfig) *APIKeyPermissionValidator {
	return &APIKeyPermissionValidator{
		config:  config,
		db:      db,
		metrics: APIKeyValidationMetrics{ValidationsByExchange: make(map[string]int64)},
	}
}

func (v *APIKeyPermissionValidator) ValidateKey(ctx context.Context, keyID, exchange string, permissions []APIKeyPermission) (*APIKeyValidationResult, error) {
	v.metrics.IncrementTotal()
	v.metrics.IncrementByExchange(exchange)

	result := &APIKeyValidationResult{
		KeyID:       keyID,
		Exchange:    exchange,
		Permissions: permissions,
		IsValid:     true,
		ValidatedAt: time.Now().UTC(),
	}

	for _, p := range permissions {
		if p == PermissionWithdraw {
			result.HasWithdraw = true
			v.metrics.IncrementWithdrawalBlocked()
		}
		if p == PermissionTransfer {
			result.HasTransfer = true
			v.metrics.IncrementTransferBlocked()
		}
	}

	for _, denied := range v.config.DeniedPermissions {
		for _, p := range permissions {
			if p == denied {
				result.IsValid = false
				result.ErrorMessage = fmt.Sprintf("denied permission detected: %s", denied)
				break
			}
		}
		if !result.IsValid {
			break
		}
	}

	if v.config.RequireTradeOnly {
		hasTrade := false
		for _, p := range permissions {
			if p == PermissionTrade {
				hasTrade = true
			}
		}
		result.IsTradeOnly = hasTrade && !result.HasWithdraw && !result.HasTransfer
		if !result.IsTradeOnly && result.IsValid {
			result.RiskLevel = "elevated"
		}
	}

	if result.IsTradeOnly {
		result.RiskLevel = "safe"
	} else if result.HasWithdraw || result.HasTransfer {
		result.RiskLevel = "dangerous"
	} else {
		result.RiskLevel = "moderate"
	}

	if result.IsValid {
		v.metrics.IncrementPassed()
	} else {
		v.metrics.IncrementFailed()
	}

	return result, nil
}

func (v *APIKeyPermissionValidator) ValidateForTrading(ctx context.Context, keyID, exchange string, permissions []APIKeyPermission) error {
	result, err := v.ValidateKey(ctx, keyID, exchange, permissions)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if !result.IsValid {
		return fmt.Errorf("API key validation failed: %s", result.ErrorMessage)
	}

	if result.HasWithdraw || result.HasTransfer {
		return fmt.Errorf("API key has dangerous permissions (withdraw/transfer)")
	}

	return nil
}

func (v *APIKeyPermissionValidator) GetMetrics() APIKeyValidationMetrics {
	return v.metrics.GetMetrics()
}

func (v *APIKeyPermissionValidator) ValidateStoredKey(ctx context.Context, keyID string) (*APIKeyValidationResult, error) {
	if v.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var exchange string
	var permissionsJSON []byte
	err := v.db.QueryRow(ctx, `
		SELECT exchange, permissions
		FROM exchange_api_keys
		WHERE key_id = $1
	`, keyID).Scan(&exchange, &permissionsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	permissions := parsePermissions(permissionsJSON)
	return v.ValidateKey(ctx, keyID, exchange, permissions)
}

func (v *APIKeyPermissionValidator) SetConfig(config APIKeyPermissionConfig) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.config = config
}

func (v *APIKeyPermissionValidator) GetConfig() APIKeyPermissionConfig {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.config
}

func parsePermissions(data []byte) []APIKeyPermission {
	if len(data) == 0 {
		return []APIKeyPermission{}
	}

	permissions := make([]APIKeyPermission, 0)
	str := string(data)
	if contains(str, "trade") {
		permissions = append(permissions, PermissionTrade)
	}
	if contains(str, "read") {
		permissions = append(permissions, PermissionRead)
	}
	if contains(str, "withdraw") {
		permissions = append(permissions, PermissionWithdraw)
	}
	if contains(str, "transfer") {
		permissions = append(permissions, PermissionTransfer)
	}
	if contains(str, "deposit") {
		permissions = append(permissions, PermissionDeposit)
	}
	return permissions
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (v *APIKeyPermissionValidator) QuickValidate(permissions []APIKeyPermission) bool {
	for _, p := range permissions {
		if p == PermissionWithdraw || p == PermissionTransfer {
			return false
		}
	}
	return true
}

func (v *APIKeyPermissionValidator) CalculateRiskScore(permissions []APIKeyPermission) decimal.Decimal {
	score := decimal.NewFromInt(100)

	for _, p := range permissions {
		switch p {
		case PermissionWithdraw:
			score = score.Sub(decimal.NewFromInt(80))
		case PermissionTransfer:
			score = score.Sub(decimal.NewFromInt(60))
		case PermissionDeposit:
			score = score.Sub(decimal.NewFromInt(20))
		case PermissionTrade:
			score = score.Sub(decimal.NewFromInt(0))
		case PermissionRead:
			score = score.Sub(decimal.NewFromInt(0))
		}
	}

	if score.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return score
}
