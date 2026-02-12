package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/utils"
)

var (
	ErrEncryptionKeyNotConfigured = errors.New("encryption key not configured")
	ErrAPIKeyNotFound             = errors.New("API key not found")
	ErrDuplicateAPIKey            = errors.New("API key with this name already exists for this exchange")
)

type APIKeyService struct {
	db        database.DBPool
	encryptor *utils.Encryptor
}

func NewAPIKeyService(db database.DBPool, encryptionKey string) (*APIKeyService, error) {
	if encryptionKey == "" {
		return &APIKeyService{db: db, encryptor: nil}, nil
	}

	key, err := utils.ParseKey(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: %w", err)
	}

	encryptor, err := utils.NewEncryptor(key, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	return &APIKeyService{
		db:        db,
		encryptor: encryptor,
	}, nil
}

func (s *APIKeyService) IsEncryptionEnabled() bool {
	return s.encryptor != nil
}

func (s *APIKeyService) CreateAPIKey(ctx context.Context, userID string, req *models.ExchangeAPIKeyRequest) (*models.ExchangeAPIKey, error) {
	if s.encryptor == nil {
		return nil, ErrEncryptionKeyNotConfigured
	}

	encryptedKey, err := s.encryptor.EncryptString(req.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	encryptedSecret, err := s.encryptor.EncryptString(req.APISecret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API secret: %w", err)
	}

	permissions := req.Permissions
	if len(permissions) == 0 {
		permissions = []string{"read"}
	}

	apiKey := &models.ExchangeAPIKey{
		ID:              uuid.New().String(),
		UserID:          userID,
		ExchangeName:    req.ExchangeName,
		KeyName:         req.KeyName,
		EncryptedKey:    encryptedKey,
		EncryptedSecret: encryptedSecret,
		Permissions:     permissions,
		IsActive:        true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	query := `
		INSERT INTO exchange_api_keys (id, user_id, exchange_name, key_name, encrypted_key, encrypted_secret, permissions, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`

	row := s.db.QueryRow(ctx, query,
		apiKey.ID,
		apiKey.UserID,
		apiKey.ExchangeName,
		apiKey.KeyName,
		apiKey.EncryptedKey,
		apiKey.EncryptedSecret,
		apiKey.Permissions,
		apiKey.IsActive,
		apiKey.CreatedAt,
		apiKey.UpdatedAt,
	)

	err = row.Scan(&apiKey.ID)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrDuplicateAPIKey
		}
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return apiKey, nil
}

func (s *APIKeyService) GetAPIKey(ctx context.Context, userID, keyID string) (*models.ExchangeAPIKey, error) {
	query := `
		SELECT id, user_id, exchange_name, key_name, encrypted_key, encrypted_secret, permissions, is_active, last_used_at, expires_at, created_at, updated_at
		FROM exchange_api_keys
		WHERE id = $1 AND user_id = $2`

	row := s.db.QueryRow(ctx, query, keyID, userID)

	var apiKey models.ExchangeAPIKey
	var lastUsedAt, expiresAt *time.Time

	err := row.Scan(
		&apiKey.ID,
		&apiKey.UserID,
		&apiKey.ExchangeName,
		&apiKey.KeyName,
		&apiKey.EncryptedKey,
		&apiKey.EncryptedSecret,
		&apiKey.Permissions,
		&apiKey.IsActive,
		&lastUsedAt,
		&expiresAt,
		&apiKey.CreatedAt,
		&apiKey.UpdatedAt,
	)
	if err != nil {
		return nil, ErrAPIKeyNotFound
	}

	apiKey.LastUsedAt = lastUsedAt
	apiKey.ExpiresAt = expiresAt

	return &apiKey, nil
}

func (s *APIKeyService) ListAPIKeys(ctx context.Context, userID string) ([]*models.ExchangeAPIKeyResponse, error) {
	query := `
		SELECT id, exchange_name, key_name, permissions, is_active, last_used_at, expires_at, created_at
		FROM exchange_api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}
	defer rows.Close()

	var keys []*models.ExchangeAPIKeyResponse
	for rows.Next() {
		var key models.ExchangeAPIKeyResponse
		var lastUsedAt, expiresAt *time.Time

		err := rows.Scan(
			&key.ID,
			&key.ExchangeName,
			&key.KeyName,
			&key.Permissions,
			&key.IsActive,
			&lastUsedAt,
			&expiresAt,
			&key.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}

		key.LastUsedAt = lastUsedAt
		key.ExpiresAt = expiresAt
		keys = append(keys, &key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating API keys: %w", err)
	}

	return keys, nil
}

func (s *APIKeyService) DecryptAPIKey(ctx context.Context, userID, keyID string) (apiKey, apiSecret string, err error) {
	if s.encryptor == nil {
		return "", "", ErrEncryptionKeyNotConfigured
	}

	key, err := s.GetAPIKey(ctx, userID, keyID)
	if err != nil {
		return "", "", err
	}

	if !key.IsActive {
		return "", "", errors.New("API key is not active")
	}

	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return "", "", errors.New("API key has expired")
	}

	decryptedKey, err := s.encryptor.DecryptString(key.EncryptedKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt API key: %w", err)
	}

	decryptedSecret, err := s.encryptor.DecryptString(key.EncryptedSecret)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt API secret: %w", err)
	}

	now := time.Now()
	updateQuery := `UPDATE exchange_api_keys SET last_used_at = $1, updated_at = $2 WHERE id = $3`
	_, _ = s.db.Exec(ctx, updateQuery, now, now, keyID)

	return decryptedKey, decryptedSecret, nil
}

func (s *APIKeyService) DeleteAPIKey(ctx context.Context, userID, keyID string) error {
	query := `DELETE FROM exchange_api_keys WHERE id = $1 AND user_id = $2`
	result, err := s.db.Exec(ctx, query, keyID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

func (s *APIKeyService) DeactivateAPIKey(ctx context.Context, userID, keyID string) error {
	query := `UPDATE exchange_api_keys SET is_active = false, updated_at = $1 WHERE id = $2 AND user_id = $3`
	result, err := s.db.Exec(ctx, query, time.Now(), keyID, userID)
	if err != nil {
		return fmt.Errorf("failed to deactivate API key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

func isDuplicateKeyError(err error) bool {
	return err != nil && (err.Error() == "pq: duplicate key value violates unique constraint \"unique_user_exchange_key\"" ||
		err.Error() == "UNIQUE constraint failed: exchange_api_keys.user_id, exchange_api_keys.exchange_name, exchange_api_keys.key_name")
}
