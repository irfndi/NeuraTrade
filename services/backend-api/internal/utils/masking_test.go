package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		config   MaskingConfig
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			config:   DefaultMaskingConfig,
			expected: "",
		},
		{
			name:     "short string fully masked",
			input:    "short",
			config:   DefaultMaskingConfig,
			expected: "*****",
		},
		{
			name:     "normal string masked",
			input:    "this-is-a-test-key",
			config:   DefaultMaskingConfig,
			expected: "this**********-key",
		},
		{
			name:     "custom config",
			input:    "abcdefgh12345678",
			config:   MaskingConfig{ShowFirst: 2, ShowLast: 2, MaskChar: '#', MinLength: 6},
			expected: "ab############78",
		},
		{
			name:     "exact min length",
			input:    "exactminlen",
			config:   MaskingConfig{ShowFirst: 4, ShowLast: 4, MaskChar: '*', MinLength: 11},
			expected: "exac***nlen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskString(tt.input, tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty key",
			input:    "",
			expected: "",
		},
		{
			name:     "short key fully masked",
			input:    "short",
			expected: "*****",
		},
		{
			name:     "normal API key",
			input:    "sk_live_abcdef123456789",
			expected: "sk_l***************6789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskAPIKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty secret",
			input:    "",
			expected: "",
		},
		{
			name:     "short secret fully masked",
			input:    "secret",
			expected: "******",
		},
		{
			name:     "normal secret",
			input:    "my-super-secret-value-12345",
			expected: "my***********************45",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSecret(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty token",
			input:    "",
			expected: "",
		},
		{
			name:     "JWT token",
			input:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			expected: "eyJhbG***.***.***sw5c",
		},
		{
			name:     "non-JWT token uses default masking",
			input:    "regular_token_value_here",
			expected: "regu****************here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskToken(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskPassword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty password",
			input:    "",
			expected: "",
		},
		{
			name:     "short password",
			input:    "pass",
			expected: "********",
		},
		{
			name:     "long password",
			input:    "thisisaverylongpassword123",
			expected: "********",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskPassword(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "PostgreSQL connection string",
			input:    "postgresql://user:secretpassword@localhost:5432/mydb",
			expected: "postgresql://user:***@localhost:5432/mydb",
		},
		{
			name:     "Redis connection string with user",
			input:    "redis://user:secretpassword@localhost:6379/0",
			expected: "redis://user:***@localhost:6379/0",
		},
		{
			name:     "plain string unchanged",
			input:    "just-a-plain-string",
			expected: "just-a-plain-string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskConnectionString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskWalletAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Ethereum address",
			input:    "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			expected: "0x742d...0bEb",
		},
		{
			name:     "Bitcoin address",
			input:    "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
			expected: "1A1zP1...vfNa",
		},
		{
			name:     "short address shows ellipsis",
			input:    "0x1234567890ab",
			expected: "0x1234...90ab",
		},
		{
			name:     "empty address",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskWalletAddress(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal email",
			input:    "john.doe@example.com",
			expected: "jo******@example.com",
		},
		{
			name:     "short local part",
			input:    "ab@test.com",
			expected: "**@test.com",
		},
		{
			name:     "single char local part",
			input:    "a@test.com",
			expected: "**@test.com",
		},
		{
			name:     "invalid email treated as string",
			input:    "not-an-email",
			expected: "no**********",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskEmail(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskPhone(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "US phone with dashes",
			input:    "555-123-4567",
			expected: "***-***-4567",
		},
		{
			name:     "US phone with parens",
			input:    "(555) 123-4567",
			expected: "(***) ***-4567",
		},
		{
			name:     "plain digits",
			input:    "5551234567",
			expected: "******4567",
		},
		{
			name:     "short phone fully masked",
			input:    "123",
			expected: "***",
		},
		{
			name:     "international phone",
			input:    "+1 555 123 4567",
			expected: "+* *** *** 4567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskPhone(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactMap(t *testing.T) {
	tests := []struct {
		name            string
		input           map[string]string
		sensitiveKeys   []string
		expectedMasked  []string
		expectedVisible []string
	}{
		{
			name: "default sensitive keys",
			input: map[string]string{
				"username":      "johndoe",
				"password":      "supersecret123",
				"api_key":       "sk_live_abc123",
				"database_url":  "postgresql://user:pass@localhost/db",
				"normal_config": "visible_value",
			},
			sensitiveKeys:   nil,
			expectedMasked:  []string{"password", "api_key", "database_url"},
			expectedVisible: []string{"username", "normal_config"},
		},
		{
			name: "custom sensitive keys",
			input: map[string]string{
				"field1": "value1",
				"field2": "value2",
				"secret": "hidden",
			},
			sensitiveKeys:   []string{"field1"},
			expectedMasked:  []string{"field1"},
			expectedVisible: []string{"field2", "secret"},
		},
		{
			name:            "empty map",
			input:           map[string]string{},
			sensitiveKeys:   nil,
			expectedMasked:  []string{},
			expectedVisible: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactMap(tt.input, tt.sensitiveKeys)

			for _, key := range tt.expectedMasked {
				assert.NotEqual(t, tt.input[key], result[key], "key %s should be masked", key)
				assert.Contains(t, result[key], "*", "masked value for %s should contain *", key)
			}

			for _, key := range tt.expectedVisible {
				assert.Equal(t, tt.input[key], result[key], "key %s should be visible", key)
			}
		})
	}
}

func TestSafeLog(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string // Substrings that should be in result
		notExpected []string // Substrings that should NOT be in result
	}{
		{
			name:        "masks API key",
			input:       "Request with api_key=sk_live_abcdef123456",
			expected:    []string{"api_key", "***"},
			notExpected: []string{"sk_live_abcdef123456"},
		},
		{
			name:        "masks password",
			input:       "User login with password=secret123",
			expected:    []string{"password", "*"},
			notExpected: []string{"secret123"},
		},
		{
			name:        "masks connection string",
			input:       "Connecting to postgresql://admin:pass123@localhost/mydb",
			expected:    []string{"postgresql://admin:***@localhost/mydb"},
			notExpected: []string{"pass123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SafeLog(tt.input)

			for _, exp := range tt.expected {
				assert.Contains(t, result, exp)
			}

			for _, notExp := range tt.notExpected {
				assert.NotContains(t, result, notExp)
			}
		})
	}
}

func TestMaskStringDoesNotModifyOriginal(t *testing.T) {
	original := "test-secret-key-12345"
	masked := MaskString(original, DefaultMaskingConfig)

	assert.Equal(t, "test*************2345", masked)
	assert.Equal(t, "test-secret-key-12345", original) // Original unchanged
}

func BenchmarkMaskString(b *testing.B) {
	testString := "this-is-a-long-secret-api-key-that-needs-masking"
	for i := 0; i < b.N; i++ {
		_ = MaskString(testString, DefaultMaskingConfig)
	}
}

func BenchmarkSafeLog(b *testing.B) {
	testString := "Request to postgresql://user:secret@localhost/db with api_key=sk_live_abc123 and password=mypass"
	for i := 0; i < b.N; i++ {
		_ = SafeLog(testString)
	}
}
