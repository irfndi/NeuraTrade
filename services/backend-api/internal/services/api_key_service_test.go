package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAPIKeyService_EmptyKeyReturnsError(t *testing.T) {
	svc, err := NewAPIKeyService(nil, "")
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, ErrEncryptionKeyNotConfigured)
}

func TestNewAPIKeyService_WhitespaceKeyReturnsError(t *testing.T) {
	svc, err := NewAPIKeyService(nil, "   ")
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, ErrEncryptionKeyNotConfigured)
}

func TestNewAPIKeyService_ValidKey(t *testing.T) {
	// Base64-encoded 32-byte key for AES-256-GCM
	key := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	svc, err := NewAPIKeyService(nil, key)
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.True(t, svc.IsEncryptionEnabled())
}
