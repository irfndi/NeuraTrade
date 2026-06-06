package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name    string
		key     []byte
		wantErr error
	}{
		{
			name:    "valid 32-byte key",
			key:     make([]byte, KeyLength),
			wantErr: nil,
		},
		{
			name:    "key too short",
			key:     make([]byte, 16),
			wantErr: ErrInvalidKey,
		},
		{
			name:    "key too long",
			key:     make([]byte, 64),
			wantErr: ErrInvalidKey,
		},
		{
			name:    "nil key",
			key:     nil,
			wantErr: ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.key) == KeyLength {
				if _, err := rand.Read(tt.key); err != nil {
					t.Fatalf("rand.Read failed: %v", err)
				}
			}
			enc, err := NewEncryptor(tt.key)
			if err != tt.wantErr {
				t.Errorf("NewEncryptor() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				enc.Close()
			}
		})
	}
}

func TestNewEncryptorFromPassphrase(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		salt       []byte
		wantErr    bool
	}{
		{
			name:       "valid passphrase with generated salt",
			passphrase: "my-secret-passphrase",
			salt:       nil,
			wantErr:    false,
		},
		{
			name:       "valid passphrase with provided salt",
			passphrase: "my-secret-passphrase",
			salt:       make([]byte, SaltSize),
			wantErr:    false,
		},
		{
			name:       "empty passphrase",
			passphrase: "",
			salt:       nil,
			wantErr:    false,
		},
		{
			name:       "salt too short",
			passphrase: "my-secret-passphrase",
			salt:       make([]byte, 8),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.salt) >= SaltSize {
				if _, err := rand.Read(tt.salt); err != nil {
					t.Fatalf("rand.Read failed: %v", err)
				}
			}
			enc, salt, err := NewEncryptorFromPassphrase(tt.passphrase, tt.salt)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptorFromPassphrase() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				defer enc.Close()
				if len(salt) != SaltSize {
					t.Errorf("salt length = %d, want %d", len(salt), SaltSize)
				}
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}
	defer enc.Close()

	plaintexts := []string{
		"hello world",
		"api_key_12345678",
		"",
		"a",
		string(make([]byte, 1024)),
	}

	for i, pt := range plaintexts {
		t.Run("plaintext_"+string(rune('0'+i)), func(t *testing.T) {
			ciphertext, err := enc.EncryptString(pt)
			if err != nil {
				t.Fatalf("EncryptString() error = %v", err)
			}

			decrypted, err := enc.DecryptString(ciphertext)
			if err != nil {
				t.Fatalf("DecryptString() error = %v", err)
			}

			if decrypted != pt {
				t.Errorf("decrypted = %q, want %q", decrypted, pt)
			}
		})
	}
}

func TestEncryptDecryptBytes(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}
	defer enc.Close()

	plaintext := []byte("this is binary data with \x00 null bytes")

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted = %v, want %v", decrypted, plaintext)
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}
	defer enc.Close()

	plaintext := "same plaintext"

	ct1, err := enc.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	ct2, err := enc.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	if ct1 == ct2 {
		t.Error("same plaintext produced identical ciphertexts - random nonce not working")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	enc1, _ := NewEncryptor(key1)
	defer enc1.Close()

	enc2, _ := NewEncryptor(key2)
	defer enc2.Close()

	plaintext := "secret data"
	ciphertext, err := enc1.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	_, err = enc2.DecryptString(ciphertext)
	if err != ErrDecryptionFailed {
		t.Errorf("DecryptString() error = %v, want %v", err, ErrDecryptionFailed)
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	enc, _ := NewEncryptor(key)
	defer enc.Close()

	tests := []struct {
		name       string
		ciphertext string
		wantErr    error
	}{
		{
			name:       "invalid base64",
			ciphertext: "not-valid-base64!!!",
			wantErr:    nil,
		},
		{
			name:       "too short",
			ciphertext: "YWJjZA==",
			wantErr:    ErrInvalidCiphertext,
		},
		{
			name:       "empty string",
			ciphertext: "",
			wantErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.DecryptString(tt.ciphertext)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestPassphraseDerivationConsistency(t *testing.T) {
	passphrase := "my-secret-passphrase"
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}

	enc1, _, err := NewEncryptorFromPassphrase(passphrase, salt)
	if err != nil {
		t.Fatalf("NewEncryptorFromPassphrase() error = %v", err)
	}
	defer enc1.Close()

	enc2, _, err := NewEncryptorFromPassphrase(passphrase, salt)
	if err != nil {
		t.Fatalf("NewEncryptorFromPassphrase() error = %v", err)
	}
	defer enc2.Close()

	plaintext := "test data"
	ciphertext, err := enc1.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	decrypted, err := enc2.DecryptString(ciphertext)
	if err != nil {
		t.Fatalf("DecryptString() error = %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	if len(key1) != KeyLength {
		t.Errorf("key length = %d, want %d", len(key1), KeyLength)
	}

	if bytes.Equal(key1, key2) {
		t.Error("GenerateKey() produced identical keys")
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "standard key",
			key:  "abcdefghijklmnopqrst",
			want: "abcd****qrst",
		},
		{
			name: "short key",
			key:  "abc",
			want: "********",
		},
		{
			name: "exact 8 chars",
			key:  "12345678",
			want: "********",
		},
		{
			name: "empty string",
			key:  "",
			want: "********",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskKey(tt.key)
			if got != tt.want {
				t.Errorf("MaskKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHashKey(t *testing.T) {
	key := "my-api-key"
	hash1 := HashKey(key)
	hash2 := HashKey(key)

	if hash1 != hash2 {
		t.Error("HashKey() not deterministic")
	}

	differentKey := "different-api-key"
	hash3 := HashKey(differentKey)
	if hash1 == hash3 {
		t.Error("HashKey() produced same hash for different keys")
	}

	if len(hash1) == 0 {
		t.Error("HashKey() returned empty string")
	}
}

func TestCloseClearsKey(t *testing.T) {
	key, _ := GenerateKey()
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	enc, _ := NewEncryptor(key)
	enc.Close()

	enc2, _ := NewEncryptor(keyCopy)
	defer enc2.Close()

	_, err := enc.DecryptString("YWJjZA==")
	if err == nil {
		t.Error("expected error after Close()")
	}
}

func TestNewEncryptorFromHexKey(t *testing.T) {
	tests := []struct {
		name    string
		hexKey  string
		wantErr bool
	}{
		{
			name:    "valid hex key",
			hexKey:  fmt.Sprintf("%x", make([]byte, KeyLength)),
			wantErr: false,
		},
		{
			name:    "valid hex key with 0x prefix stripped",
			hexKey:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			wantErr: false,
		},
		{
			name:    "invalid hex - too short",
			hexKey:  "abcd",
			wantErr: true,
		},
		{
			name:    "invalid hex - wrong length",
			hexKey:  fmt.Sprintf("%x", make([]byte, 16)),
			wantErr: true,
		},
		{
			name:    "invalid hex characters",
			hexKey:  "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			wantErr: true,
		},
		{
			name:    "empty string",
			hexKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewEncryptorFromHexKey(tt.hexKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptorFromHexKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				defer enc.Close()
			}
		})
	}
}

func TestGenerateKeyHex(t *testing.T) {
	key1, err := GenerateKeyHex()
	if err != nil {
		t.Fatalf("GenerateKeyHex() error = %v", err)
	}

	key2, err := GenerateKeyHex()
	if err != nil {
		t.Fatalf("GenerateKeyHex() error = %v", err)
	}

	if len(key1) == 0 {
		t.Error("GenerateKeyHex() returned empty string")
	}

	if key1 == key2 {
		t.Error("GenerateKeyHex() produced identical keys")
	}

	decoded, err := hex.DecodeString(key1)
	if err != nil {
		t.Errorf("GenerateKeyHex() returned invalid hex: %v", err)
	}

	if len(decoded) != KeyLength {
		t.Errorf("decoded key length = %d, want %d", len(decoded), KeyLength)
	}
}

func TestDecodeKey(t *testing.T) {
	validKey := make([]byte, KeyLength)
	if _, err := rand.Read(validKey); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}

	tests := []struct {
		name       string
		encodedKey string
		wantErr    bool
	}{
		{
			name:       "valid hex encoding",
			encodedKey: fmt.Sprintf("%x", validKey),
			wantErr:    false,
		},
		{
			name:       "valid base64 encoding",
			encodedKey: base64.StdEncoding.EncodeToString(validKey),
			wantErr:    false,
		},
		{
			name:       "invalid hex - too short",
			encodedKey: "abcd",
			wantErr:    true,
		},
		{
			name:       "invalid base64 - too short",
			encodedKey: base64.StdEncoding.EncodeToString(make([]byte, 16)),
			wantErr:    true,
		},
		{
			name:       "empty string",
			encodedKey: "",
			wantErr:    true,
		},
		{
			name:       "garbage string",
			encodedKey: "not-a-valid-key-encoding",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := decodeKey(tt.encodedKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(key) != KeyLength {
				t.Errorf("decodeKey() key length = %d, want %d", len(key), KeyLength)
			}
		})
	}
}

func TestEncodeKey(t *testing.T) {
	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}

	encoded := encodeKey(key)
	if len(encoded) == 0 {
		t.Error("encodeKey() returned empty string")
	}

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Errorf("encodeKey() returned invalid hex: %v", err)
	}

	if !bytes.Equal(decoded, key) {
		t.Error("encodeKey() round-trip failed")
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key, err := GenerateKey()
	if err != nil {
		b.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		b.Fatal(err)
	}
	defer enc.Close()

	plaintext := []byte("benchmark test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encrypt(plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key, err := GenerateKey()
	if err != nil {
		b.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		b.Fatal(err)
	}
	defer enc.Close()

	plaintext := []byte("benchmark test data")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Decrypt(ciphertext); err != nil {
			b.Fatal(err)
		}
	}
}
