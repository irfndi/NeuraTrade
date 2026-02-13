package models

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// User represents a registered user of the platform.
// It stores authentication and profile information.
type User struct {
	ID               string    `json:"id" db:"id"`
	Email            string    `json:"email" db:"email"`
	PasswordHash     string    `json:"-" db:"password_hash"`
	TelegramChatID   *string   `json:"telegram_chat_id" db:"telegram_chat_id"`
	SubscriptionTier string    `json:"subscription_tier" db:"subscription_tier"`
	SelectedAIModel  *string   `json:"selected_ai_model" db:"selected_ai_model"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// UserAlert represents a custom alert configured by a user.
// Alerts can be triggered by various market conditions.
type UserAlert struct {
	ID         string          `json:"id" db:"id"`
	UserID     string          `json:"user_id" db:"user_id"`
	AlertType  string          `json:"alert_type" db:"alert_type"`
	Conditions json.RawMessage `json:"conditions" db:"conditions"`
	IsActive   bool            `json:"is_active" db:"is_active"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
	User       *User           `json:"user,omitempty"`
}

// Portfolio represents an asset holding within a user's portfolio.
type Portfolio struct {
	ID        string          `json:"id" db:"id"`
	UserID    string          `json:"user_id" db:"user_id"`
	Symbol    string          `json:"symbol" db:"symbol"`
	Quantity  decimal.Decimal `json:"quantity" db:"quantity"`
	AvgPrice  decimal.Decimal `json:"avg_price" db:"avg_price"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
	User      *User           `json:"user,omitempty"`
}

// UserRequest represents the payload for user registration or profile updates.
type UserRequest struct {
	Email            string `json:"email" binding:"required,email"`
	Password         string `json:"password" binding:"required,min=8"`
	TelegramChatID   string `json:"telegram_chat_id"`
	SubscriptionTier string `json:"subscription_tier"`
	SelectedAIModel  string `json:"selected_ai_model"`
}

// UserResponse represents the public user information returned by the API.
type UserResponse struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	TelegramChatID   string    `json:"telegram_chat_id"`
	SubscriptionTier string    `json:"subscription_tier"`
	SelectedAIModel  string    `json:"selected_ai_model"`
	CreatedAt        time.Time `json:"created_at"`
}

// AlertConditions defines the specific criteria that trigger a user alert.
type AlertConditions struct {
	PriceThreshold  *decimal.Decimal `json:"price_threshold,omitempty"`
	ProfitThreshold *decimal.Decimal `json:"profit_threshold,omitempty"`
	VolumeThreshold *decimal.Decimal `json:"volume_threshold,omitempty"`
	Symbol          string           `json:"symbol,omitempty"`
	Exchange        string           `json:"exchange,omitempty"`
}

// ToUserResponse converts a User to UserResponse
func (u *User) ToUserResponse() UserResponse {
	resp := UserResponse{
		ID:               u.ID,
		Email:            u.Email,
		SubscriptionTier: u.SubscriptionTier,
		CreatedAt:        u.CreatedAt,
	}

	if u.TelegramChatID != nil {
		resp.TelegramChatID = *u.TelegramChatID
	}

	if u.SelectedAIModel != nil {
		resp.SelectedAIModel = *u.SelectedAIModel
	}

	return resp
}
