package utils

import (
	"bytes"
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name       string
		key        []byte
		base64Mode bool
		wantErr    bool
	}{
		{
			name:       "valid 32-byte key",
			key:        make([]byte, 32),
			base64Mode: true,
			wantErr:    false,
		},
		{
			name:       "invalid 16-byte key",
			key:        make([]byte, 16),
			base64Mode: true,
			wantErr:    true,
		},
		{
			name:       "invalid 64-byte key",
			key:        make([]byte, 64),
			base64Mode: true,
			wantErr:    true,
		},
		{
			name:       "nil key",
			key:        nil,
			base64Mode: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewEncryptor(tt.key, tt.base64Mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && enc == nil {
				t.Error("NewEncryptor() returned nil encryptor without error")
			}
		})
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, err := NewEncryptor(key, false)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext []byte
		wantErr   bool
	}{
		{"simple text", []byte("hello world"), false},
		{"empty", []byte{}, true},
		{"binary data", []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, false},
		{"long text", bytes.Repeat([]byte("a"), 1000), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := enc.Encrypt(tt.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if bytes.Equal(ciphertext, tt.plaintext) {
				t.Error("ciphertext should not equal plaintext")
			}

			decrypted, err := enc.Decrypt(ciphertext)
			if err != nil {
				t.Errorf("Decrypt() error = %v", err)
				return
			}

			if !bytes.Equal(decrypted, tt.plaintext) {
				t.Errorf("decrypted = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptDecryptString(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, err := NewEncryptor(key, true)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		wantErr   bool
	}{
		{"simple text", "hello world", false},
		{"empty string", "", true},
		{"special chars", "!@#$%^&*()_+-=[]{}|;':\",./<>?", false},
		{"unicode", "日本語テスト 🚀", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := enc.EncryptString(tt.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncryptString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			decrypted, err := enc.DecryptString(ciphertext)
			if err != nil {
				t.Errorf("DecryptString() error = %v", err)
				return
			}

			if decrypted != tt.plaintext {
				t.Errorf("decrypted = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, err := NewEncryptor(key, false)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	tests := []struct {
		name       string
		ciphertext []byte
		wantErr    error
	}{
		{"empty", []byte{}, nil},
		{"too short", []byte{0x01, 0x02}, ErrInvalidCiphertext},
		{"corrupted", []byte("not valid ciphertext at all - too short"), ErrDecryptionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decrypt(tt.ciphertext)
			if err == nil {
				if tt.wantErr != nil {
					t.Error("expected error, got nil")
				}
				return
			}
		})
	}
}

func TestDifferentKeysCannotDecrypt(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	enc1, err := NewEncryptor(key1, false)
	if err != nil {
		t.Fatalf("failed to create encryptor1: %v", err)
	}

	enc2, err := NewEncryptor(key2, false)
	if err != nil {
		t.Fatalf("failed to create encryptor2: %v", err)
	}

	plaintext := []byte("secret message")
	ciphertext, err := enc1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	_, err = enc2.Decrypt(ciphertext)
	if err == nil {
		t.Error("expected decryption to fail with different key")
	}
}

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if len(key1) != 32 {
		t.Errorf("key length = %d, want 32", len(key1))
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	if bytes.Equal(key1, key2) {
		t.Error("consecutive keys should be different")
	}
}

func TestGenerateAndParseKeyString(t *testing.T) {
	keyStr, err := GenerateKeyString()
	if err != nil {
		t.Fatalf("GenerateKeyString() error = %v", err)
	}

	key, err := ParseKey(keyStr)
	if err != nil {
		t.Fatalf("ParseKey() error = %v", err)
	}

	if len(key) != 32 {
		t.Errorf("parsed key length = %d, want 32", len(key))
	}
}

func TestParseKeyInvalid(t *testing.T) {
	tests := []struct {
		name    string
		keyStr  string
		wantErr bool
	}{
		{"invalid base64", "not-valid-base64!!!", true},
		{"wrong length base64", "YQ==", true},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKey(tt.keyStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNonceUniqueness(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, err := NewEncryptor(key, false)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	plaintext := []byte("same message")
	nonces := make(map[string]bool)

	for i := 0; i < 100; i++ {
		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		nonceSize := enc.gcm.NonceSize()
		if len(ciphertext) < nonceSize {
			t.Fatalf("ciphertext too short: %d", len(ciphertext))
		}

		nonce := string(ciphertext[:nonceSize])
		if nonces[nonce] {
			t.Error("duplicate nonce generated - this is a security issue")
		}
		nonces[nonce] = true
	}
}
