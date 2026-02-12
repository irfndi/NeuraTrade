package models

import "time"

type OneTimeCode struct {
	ID        string     `json:"id" db:"id"`
	UserID    string     `json:"user_id" db:"user_id"`
	Code      string     `json:"-" db:"code"`
	Purpose   string     `json:"purpose" db:"purpose"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	UsedAt    *time.Time `json:"used_at" db:"used_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

const (
	OTPPurposeTelegramBinding = "telegram_binding"
	OTPPurposePasswordReset   = "password_reset"
	OTPPurposeEmailVerify     = "email_verify"
)

type OTPRequest struct {
	Purpose string `json:"purpose" binding:"required,oneof=telegram_binding password_reset email_verify"`
}

type OTPVerifyRequest struct {
	Code    string `json:"code" binding:"required,len=6"`
	Purpose string `json:"purpose" binding:"required"`
}

type OTPResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}
