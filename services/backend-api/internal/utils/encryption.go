package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidKey indicates the encryption key is invalid (wrong length).
	ErrInvalidKey = errors.New("encryption key must be 32 bytes for AES-256")
	// ErrInvalidCiphertext indicates the ciphertext cannot be decrypted.
	ErrInvalidCiphertext = errors.New("invalid ciphertext: too short or corrupted")
	// ErrDecryptionFailed indicates decryption failed (likely wrong key or tampered data).
	ErrDecryptionFailed = errors.New("decryption failed: authentication tag mismatch")
)

// Encryptor provides AES-256-GCM encryption and decryption capabilities.
// It uses a 32-byte key for AES-256 and generates a random nonce for each encryption.
type Encryptor struct {
	key        []byte
	gcm        cipher.AEAD
	base64Mode bool // If true, encode/decode ciphertext as base64 strings
}

// NewEncryptor creates a new Encryptor with the given 32-byte key.
// The key must be exactly 32 bytes for AES-256-GCM.
// If base64Mode is true, the Encryptor will work with base64-encoded strings.
func NewEncryptor(key []byte, base64Mode bool) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Encryptor{
		key:        key,
		gcm:        gcm,
		base64Mode: base64Mode,
	}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns the ciphertext with nonce prepended, optionally base64-encoded.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext cannot be empty")
	}

	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)

	if e.base64Mode {
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(ciphertext)))
		base64.StdEncoding.Encode(encoded, ciphertext)
		return encoded, nil
	}

	return ciphertext, nil
}

// EncryptString encrypts a string and returns a base64-encoded ciphertext.
func (e *Encryptor) EncryptString(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("plaintext cannot be empty")
	}

	ciphertext, err := e.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}

	return string(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM.
// The ciphertext must have the nonce prepended.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("ciphertext cannot be empty")
	}

	var data []byte
	if e.base64Mode {
		decoded := make([]byte, base64.StdEncoding.DecodedLen(len(ciphertext)))
		n, err := base64.StdEncoding.Decode(decoded, ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: base64 decode failed", ErrInvalidCiphertext)
		}
		data = decoded[:n]
	} else {
		data = ciphertext
	}

	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, encryptedData := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// DecryptString decrypts a base64-encoded ciphertext and returns the plaintext string.
func (e *Encryptor) DecryptString(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", errors.New("ciphertext cannot be empty")
	}

	plaintext, err := e.Decrypt([]byte(ciphertext))
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// GenerateKey generates a cryptographically secure random 32-byte key.
// This can be used to create a new encryption key for production use.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// GenerateKeyString generates a base64-encoded 32-byte key.
func GenerateKeyString() (string, error) {
	key, err := GenerateKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// ParseKey parses a base64-encoded key string into a 32-byte key.
func ParseKey(keyString string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(keyString)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes after decoding", ErrInvalidKey, len(key))
	}
	return key, nil
}
