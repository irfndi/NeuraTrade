package services

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
)

var (
	ErrOTPNotFound       = errors.New("one-time code not found or expired")
	ErrOTPAlreadyUsed    = errors.New("one-time code already used")
	ErrOTPExpired        = errors.New("one-time code has expired")
	ErrInvalidOTPPurpose = errors.New("invalid one-time code purpose")
)

const (
	defaultOTPExpiry = 10 * time.Minute
	otpLength        = 6
	cleanupBatchSize = 100
)

type OTPService struct {
	db         database.DBPool
	codeExpiry time.Duration
}

func NewOTPService(db database.DBPool) *OTPService {
	return &OTPService{
		db:         db,
		codeExpiry: defaultOTPExpiry,
	}
}

func NewOTPServiceWithExpiry(db database.DBPool, expiry time.Duration) *OTPService {
	return &OTPService{
		db:         db,
		codeExpiry: expiry,
	}
}

func (s *OTPService) GenerateCode(ctx context.Context, userID, purpose string) (*models.OneTimeCode, error) {
	if !isValidPurpose(purpose) {
		return nil, ErrInvalidOTPPurpose
	}

	code, err := generateNumericCode(otpLength)
	if err != nil {
		return nil, fmt.Errorf("failed to generate code: %w", err)
	}

	now := time.Now()
	otp := &models.OneTimeCode{
		ID:        uuid.New().String(),
		UserID:    userID,
		Code:      code,
		Purpose:   purpose,
		ExpiresAt: now.Add(s.codeExpiry),
		CreatedAt: now,
	}

	query := `
		INSERT INTO one_time_codes (id, user_id, code, purpose, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	row := s.db.QueryRow(ctx, query,
		otp.ID,
		otp.UserID,
		otp.Code,
		otp.Purpose,
		otp.ExpiresAt,
		otp.CreatedAt,
	)

	err = row.Scan(&otp.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to create one-time code: %w", err)
	}

	return otp, nil
}

func (s *OTPService) VerifyCode(ctx context.Context, userID, code, purpose string) (*models.OneTimeCode, error) {
	if !isValidPurpose(purpose) {
		return nil, ErrInvalidOTPPurpose
	}

	query := `
		SELECT id, user_id, code, purpose, expires_at, used_at, created_at
		FROM one_time_codes
		WHERE user_id = $1 AND code = $2 AND purpose = $3 AND used_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`

	row := s.db.QueryRow(ctx, query, userID, code, purpose)

	var otp models.OneTimeCode
	err := row.Scan(
		&otp.ID,
		&otp.UserID,
		&otp.Code,
		&otp.Purpose,
		&otp.ExpiresAt,
		&otp.UsedAt,
		&otp.CreatedAt,
	)
	if err != nil {
		return nil, ErrOTPNotFound
	}

	if otp.ExpiresAt.Before(time.Now()) {
		return nil, ErrOTPExpired
	}

	now := time.Now()
	updateQuery := `UPDATE one_time_codes SET used_at = $1 WHERE id = $2`
	_, err = s.db.Exec(ctx, updateQuery, now, otp.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark code as used: %w", err)
	}

	otp.UsedAt = &now
	return &otp, nil
}

func (s *OTPService) InvalidateUserCodes(ctx context.Context, userID, purpose string) error {
	query := `UPDATE one_time_codes SET used_at = $1 WHERE user_id = $2 AND purpose = $3 AND used_at IS NULL`
	_, err := s.db.Exec(ctx, query, time.Now(), userID, purpose)
	if err != nil {
		return fmt.Errorf("failed to invalidate codes: %w", err)
	}
	return nil
}

func (s *OTPService) CleanupExpiredCodes(ctx context.Context) (int64, error) {
	query := `DELETE FROM one_time_codes WHERE expires_at < $1`
	result, err := s.db.Exec(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired codes: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rows, nil
}

func generateNumericCode(length int) (string, error) {
	const digits = "0123456789"
	code := make([]byte, length)
	for i := range code {
		num, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[num.Int64()]
	}
	return string(code), nil
}

func isValidPurpose(purpose string) bool {
	switch purpose {
	case models.OTPPurposeTelegramBinding,
		models.OTPPurposePasswordReset,
		models.OTPPurposeEmailVerify:
		return true
	default:
		return false
	}
}
