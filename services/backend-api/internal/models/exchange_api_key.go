package models

import "time"

type ExchangeAPIKey struct {
	ID              string     `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	ExchangeName    string     `json:"exchange_name" db:"exchange_name"`
	KeyName         string     `json:"key_name" db:"key_name"`
	EncryptedKey    string     `json:"-" db:"encrypted_key"`
	EncryptedSecret string     `json:"-" db:"encrypted_secret"`
	Permissions     []string   `json:"permissions" db:"permissions"`
	IsActive        bool       `json:"is_active" db:"is_active"`
	LastUsedAt      *time.Time `json:"last_used_at" db:"last_used_at"`
	ExpiresAt       *time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

type ExchangeAPIKeyRequest struct {
	ExchangeName string   `json:"exchange_name" binding:"required"`
	KeyName      string   `json:"key_name" binding:"required"`
	APIKey       string   `json:"api_key" binding:"required"`
	APISecret    string   `json:"api_secret" binding:"required"`
	Permissions  []string `json:"permissions"`
}

type ExchangeAPIKeyResponse struct {
	ID           string     `json:"id"`
	ExchangeName string     `json:"exchange_name"`
	KeyName      string     `json:"key_name"`
	Permissions  []string   `json:"permissions"`
	IsActive     bool       `json:"is_active"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
}
