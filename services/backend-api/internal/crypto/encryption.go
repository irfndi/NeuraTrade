// Package crypto provides encryption utilities for sensitive data.
// This package implements AES-256-GCM encryption for API keys and other secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	// KeyLength is the length of the encryption key in bytes (256 bits).
	KeyLength = 32
	// NonceSize is the size of the GCM nonce in bytes.
	NonceSize = 12
	// SaltSize is the size of the salt for key derivation.
	SaltSize = 16
	// Argon2Time is the number of passes over memory for Argon2.
	Argon2Time = 3
	// Argon2Memory is the memory cost in KiB for Argon2.
	Argon2Memory = 64 * 1024 // 64 MB
	// Argon2Threads is the number of threads for Argon2.
	Argon2Threads = 4
)

var (
	// ErrInvalidKey is returned when the encryption key is invalid.
	ErrInvalidKey = errors.New("invalid encryption key: must be 32 bytes")
	// ErrInvalidCiphertext is returned when the ciphertext cannot be decrypted.
	ErrInvalidCiphertext = errors.New("invalid ciphertext: too short or malformed")
	// ErrDecryptionFailed is returned when decryption fails.
	ErrDecryptionFailed = errors.New("decryption failed: authentication tag mismatch")
)

// Encryptor provides AES-256-GCM encryption capabilities.
type Encryptor struct {
	key []byte
}

// NewEncryptor creates a new Encryptor with the given key.
// The key must be exactly 32 bytes (256 bits).
//
// Parameters:
//   - key: The encryption key. Must be 32 bytes.
//
// Returns:
//   - *Encryptor: Initialized encryptor.
//   - error: Error if key is invalid.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != KeyLength {
		return nil, ErrInvalidKey
	}

	// Copy the key to prevent external modification
	keyCopy := make([]byte, KeyLength)
	copy(keyCopy, key)

	return &Encryptor{key: keyCopy}, nil
}

// NewEncryptorFromPassphrase creates a new Encryptor from a passphrase using Argon2.
// This is the recommended way to create an encryptor for user-provided passphrases.
//
// Parameters:
//   - passphrase: The user-provided passphrase.
//   - salt: Optional salt. If nil, a random salt is generated and returned.
//
// Returns:
//   - *Encryptor: Initialized encryptor.
//   - []byte: The salt used for key derivation (needed for future decryption).
//   - error: Error if key derivation fails.
func NewEncryptorFromPassphrase(passphrase string, salt []byte) (*Encryptor, []byte, error) {
	if salt == nil {
		salt = make([]byte, SaltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, nil, fmt.Errorf("failed to generate salt: %w", err)
		}
	} else if len(salt) < SaltSize {
		return nil, nil, fmt.Errorf("salt must be at least %d bytes", SaltSize)
	}

	// Derive key using Argon2id
	key := argon2.IDKey(
		[]byte(passphrase),
		salt[:SaltSize],
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		KeyLength,
	)

	encryptor, err := NewEncryptor(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	return encryptor, salt[:SaltSize], nil
}

// NewEncryptorFromHexKey creates a new Encryptor from a hex-encoded key string.
//
// Parameters:
//   - hexKey: The hex-encoded 32-byte key.
//
// Returns:
//   - *Encryptor: Initialized encryptor.
//   - error: Error if key is invalid.
func NewEncryptorFromHexKey(hexKey string) (*Encryptor, error) {
	key, err := decodeKey(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	return NewEncryptor(key)
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns a base64-encoded string containing nonce + ciphertext.
//
// Parameters:
//   - plaintext: The data to encrypt.
//
// Returns:
//   - string: Base64-encoded ciphertext.
//   - error: Error if encryption fails.
func (e *Encryptor) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and seal
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Return base64-encoded result
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext using AES-256-GCM.
//
// Parameters:
//   - encodedCiphertext: Base64-encoded ciphertext from Encrypt.
//
// Returns:
//   - []byte: Decrypted plaintext.
//   - error: Error if decryption fails.
func (e *Encryptor) Decrypt(encodedCiphertext string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptString encrypts a string and returns the base64-encoded ciphertext.
//
// Parameters:
//   - plaintext: The string to encrypt.
//
// Returns:
//   - string: Base64-encoded ciphertext.
//   - error: Error if encryption fails.
func (e *Encryptor) EncryptString(plaintext string) (string, error) {
	return e.Encrypt([]byte(plaintext))
}

// DecryptString decrypts a base64-encoded ciphertext and returns the plaintext string.
//
// Parameters:
//   - encodedCiphertext: Base64-encoded ciphertext.
//
// Returns:
//   - string: Decrypted plaintext.
//   - error: Error if decryption fails.
func (e *Encryptor) DecryptString(encodedCiphertext string) (string, error) {
	plaintext, err := e.Decrypt(encodedCiphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Close securely clears the encryption key from memory.
func (e *Encryptor) Close() {
	for i := range e.key {
		e.key[i] = 0
	}
}

// GenerateKey generates a cryptographically secure random key.
//
// Returns:
//   - []byte: Random 32-byte key.
//   - error: Error if generation fails.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeyLength)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// GenerateKeyHex generates a hex-encoded random key.
//
// Returns:
//   - string: Hex-encoded 32-byte key.
//   - error: Error if generation fails.
func GenerateKeyHex() (string, error) {
	key, err := GenerateKey()
	if err != nil {
		return "", err
	}
	return encodeKey(key), nil
}

// HashKey creates a SHA-256 hash of a key for storage or comparison.
// This is useful for API key verification without storing the actual key.
//
// Parameters:
//   - key: The key to hash.
//
// Returns:
//   - string: Base64-encoded SHA-256 hash.
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// MaskKey masks a key for safe display in logs or UI.
// Shows only the first and last 4 characters with asterisks in between.
//
// Parameters:
//   - key: The key to mask.
//
// Returns:
//   - string: Masked key.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// decodeKey decodes a base64 or hex-encoded key.
func decodeKey(encodedKey string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(encodedKey); err == nil && len(key) == KeyLength {
		return key, nil
	}

	if key, err := hex.DecodeString(encodedKey); err == nil && len(key) == KeyLength {
		return key, nil
	}

	return nil, ErrInvalidKey
}

// encodeKey encodes a key as hex.
func encodeKey(key []byte) string {
	return fmt.Sprintf("%x", key)
}
