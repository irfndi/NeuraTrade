package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	OperatorSaltSize   = 16
	OperatorHashLength = 32
)

var (
	ErrInvalidPassword        = errors.New("invalid password: password cannot be empty")
	ErrInvalidHash            = errors.New("invalid hash: stored hash is malformed")
	ErrHashVerificationFailed = errors.New("hash verification failed: password does not match")
)

type OperatorIdentityHasher struct {
	time       uint32
	memory     uint32
	threads    uint8
	hashLength uint32
	saltSize   uint32
}

func NewOperatorIdentityHasher() *OperatorIdentityHasher {
	return &OperatorIdentityHasher{
		time:       uint32(Argon2Time),
		memory:     uint32(Argon2Memory),
		threads:    uint8(Argon2Threads),
		hashLength: OperatorHashLength,
		saltSize:   OperatorSaltSize,
	}
}

func (h *OperatorIdentityHasher) HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrInvalidPassword
	}

	salt := make([]byte, h.saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		h.time,
		h.memory,
		h.threads,
		h.hashLength,
	)

	combined := make([]byte, h.saltSize+h.hashLength)
	copy(combined[:h.saltSize], salt)
	copy(combined[h.saltSize:], hash)

	return base64.StdEncoding.EncodeToString(combined), nil
}

func (h *OperatorIdentityHasher) VerifyPassword(password string, storedHash string) (bool, error) {
	if password == "" {
		return false, ErrInvalidPassword
	}

	if storedHash == "" {
		return false, ErrInvalidHash
	}

	combined, err := base64.StdEncoding.DecodeString(storedHash)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidHash, err)
	}

	minLength := int(h.saltSize + h.hashLength)
	if len(combined) < minLength {
		return false, ErrInvalidHash
	}

	salt := combined[:h.saltSize]
	storedHashBytes := combined[h.saltSize : h.saltSize+h.hashLength]

	computedHash := argon2.IDKey(
		[]byte(password),
		salt,
		h.time,
		h.memory,
		h.threads,
		h.hashLength,
	)

	if len(computedHash) != len(storedHashBytes) {
		return false, ErrHashVerificationFailed
	}

	var diff byte
	for i := range computedHash {
		diff |= computedHash[i] ^ storedHashBytes[i]
	}

	if diff != 0 {
		return false, nil
	}

	return true, nil
}

func (h *OperatorIdentityHasher) NeedsRehash(storedHash string) bool {
	_, err := base64.StdEncoding.DecodeString(storedHash)
	return err != nil
}
