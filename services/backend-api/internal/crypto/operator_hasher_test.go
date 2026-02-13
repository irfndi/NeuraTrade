package crypto

import (
	"testing"
)

func TestOperatorIdentityHasher_NewOperatorIdentityHasher(t *testing.T) {
	hasher := NewOperatorIdentityHasher()
	if hasher == nil {
		t.Fatal("NewOperatorIdentityHasher() returned nil")
	}
}

func TestOperatorIdentityHasher_HashPassword(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	hash, err := hasher.HashPassword("testpassword123")
	if err != nil {
		t.Fatalf("HashPassword() returned error: %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty hash")
	}
}

func TestOperatorIdentityHasher_HashPassword_Empty(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	_, err := hasher.HashPassword("")
	if err == nil {
		t.Fatal("HashPassword() should return error for empty password")
	}
}

func TestOperatorIdentityHasher_VerifyPassword_Success(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	password := "testpassword123"
	hash, err := hasher.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() returned error: %v", err)
	}

	matched, err := hasher.VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword() returned error: %v", err)
	}

	if !matched {
		t.Fatal("VerifyPassword() should return true for correct password")
	}
}

func TestOperatorIdentityHasher_VerifyPassword_Wrong(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	password := "testpassword123"
	hash, err := hasher.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() returned error: %v", err)
	}

	matched, err := hasher.VerifyPassword("wrongpassword", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() returned error: %v", err)
	}

	if matched {
		t.Fatal("VerifyPassword() should return false for wrong password")
	}
}

func TestOperatorIdentityHasher_VerifyPassword_EmptyPassword(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	hash, _ := hasher.HashPassword("testpassword")

	_, err := hasher.VerifyPassword("", hash)
	if err == nil {
		t.Fatal("VerifyPassword() should return error for empty password")
	}
}

func TestOperatorIdentityHasher_VerifyPassword_EmptyHash(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	_, err := hasher.VerifyPassword("testpassword", "")
	if err == nil {
		t.Fatal("VerifyPassword() should return error for empty hash")
	}
}

func TestOperatorIdentityHasher_VerifyPassword_InvalidHash(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	_, err := hasher.VerifyPassword("testpassword", "invalidhash")
	if err == nil {
		t.Fatal("VerifyPassword() should return error for invalid hash")
	}
}

func TestOperatorIdentityHasher_NeedsRehash(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	hash, _ := hasher.HashPassword("testpassword")

	rehash := hasher.NeedsRehash(hash)
	if rehash {
		t.Fatal("NeedsRehash() should return false for valid hash")
	}
}

func TestOperatorIdentityHasher_NeedsRehash_Invalid(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	rehash := hasher.NeedsRehash("invalidhash")
	if !rehash {
		t.Fatal("NeedsRehash() should return true for invalid hash")
	}
}

func TestOperatorIdentityHasher_DifferentHashes(t *testing.T) {
	hasher := NewOperatorIdentityHasher()

	password := "testpassword123"

	hash1, _ := hasher.HashPassword(password)
	hash2, _ := hasher.HashPassword(password)

	if hash1 == hash2 {
		t.Fatal("HashPassword() should produce different hashes for same password (due to random salt)")
	}

	matched1, _ := hasher.VerifyPassword(password, hash1)
	matched2, _ := hasher.VerifyPassword(password, hash2)

	if !matched1 || !matched2 {
		t.Fatal("Both hashes should verify correctly")
	}
}
