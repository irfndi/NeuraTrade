package utils

import (
	"regexp"
	"strings"
)

// MaskingConfig configures how sensitive data should be masked.
type MaskingConfig struct {
	// ShowFirst determines how many characters to show at the start
	ShowFirst int
	// ShowLast determines how many characters to show at the end
	ShowLast int
	// MaskChar is the character used for masking (default: '*')
	MaskChar rune
	// MinLength is the minimum length below which the entire string is masked
	MinLength int
}

// DefaultMaskingConfig provides secure defaults for masking.
var DefaultMaskingConfig = MaskingConfig{
	ShowFirst: 4,
	ShowLast:  4,
	MaskChar:  '*',
	MinLength: 12,
}

// MaskString masks a string, showing only first and last N characters.
// If the string is shorter than MinLength, it's fully masked.
func MaskString(s string, config MaskingConfig) string {
	if s == "" {
		return ""
	}

	if len(s) < config.MinLength {
		return strings.Repeat(string(config.MaskChar), len(s))
	}

	if config.ShowFirst+config.ShowLast >= len(s) {
		return strings.Repeat(string(config.MaskChar), len(s))
	}

	first := s[:config.ShowFirst]
	last := s[len(s)-config.ShowLast:]
	middleLen := len(s) - config.ShowFirst - config.ShowLast

	return first + strings.Repeat(string(config.MaskChar), middleLen) + last
}

// MaskAPIKey masks an API key with default configuration.
func MaskAPIKey(key string) string {
	return MaskString(key, DefaultMaskingConfig)
}

// MaskSecret masks a secret (shows less for higher security).
func MaskSecret(secret string) string {
	config := MaskingConfig{
		ShowFirst: 2,
		ShowLast:  2,
		MaskChar:  '*',
		MinLength: 8,
	}
	return MaskString(secret, config)
}

// MaskToken masks an authentication token.
func MaskToken(token string) string {
	// JWT tokens have specific format: header.payload.signature
	if strings.Count(token, ".") == 2 {
		parts := strings.Split(token, ".")
		// Show first 6 chars of header, mask rest
		if len(parts[0]) > 6 {
			parts[0] = parts[0][:6] + "***"
		}
		// Mask entire payload
		parts[1] = "***"
		// Show last 4 chars of signature
		if len(parts[2]) > 4 {
			parts[2] = "***" + parts[2][len(parts[2])-4:]
		}
		return strings.Join(parts, ".")
	}
	return MaskString(token, DefaultMaskingConfig)
}

// MaskPassword fully masks a password with fixed length to avoid leaking info.
func MaskPassword(password string) string {
	if password == "" {
		return ""
	}
	return "********"
}

// MaskConnectionString masks sensitive data in database connection strings.
func MaskConnectionString(connStr string) string {
	// Mask password in postgresql://user:password@host:port/db format
	passwordPattern := regexp.MustCompile(`(postgresql://[^:]+:)([^@]+)(@)`)
	connStr = passwordPattern.ReplaceAllString(connStr, "${1}***${3}")

	// Mask password in redis://user:password@host format
	redisPattern := regexp.MustCompile(`(redis://)([^:]+:)([^@]+)(@)`)
	connStr = redisPattern.ReplaceAllString(connStr, "${1}${2}***${4}")

	return connStr
}

// MaskJSON masks sensitive fields in JSON strings.
// This is a simple implementation that masks common sensitive field patterns.
func MaskJSON(jsonStr string, sensitiveFields []string) string {
	if len(sensitiveFields) == 0 {
		sensitiveFields = []string{
			"password", "secret", "token", "key", "api_key", "apikey",
			"private_key", "access_token", "refresh_token", "auth_token",
			"credential", "credentials", "passwd", "pwd",
		}
	}

	result := jsonStr
	for _, field := range sensitiveFields {
		pattern := regexp.MustCompile(`("` + field + `"\s*:\s*")([^"]*)"`)
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) >= 3 {
				return parts[1] + MaskSecret(parts[2]) + `"`
			}
			return match
		})

		singlePattern := regexp.MustCompile(`'` + field + `'\s*:\s*'([^']*)'`)
		result = singlePattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := singlePattern.FindStringSubmatch(match)
			if len(parts) >= 2 {
				return `'` + field + `': '` + MaskSecret(parts[1]) + `'`
			}
			return match
		})
	}

	return result
}

// MaskWalletAddress masks a cryptocurrency wallet address.
// Shows first 6 and last 4 characters, masks the middle.
func MaskWalletAddress(address string) string {
	if len(address) < 12 {
		return strings.Repeat("*", len(address))
	}
	return address[:6] + "..." + address[len(address)-4:]
}

// MaskEmail masks an email address, showing only domain and first 2 chars of local part.
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return MaskString(email, MaskingConfig{ShowFirst: 2, ShowLast: 0, MaskChar: '*', MinLength: 4})
	}

	local := parts[0]
	domain := parts[1]

	if len(local) <= 2 {
		return "**@" + domain
	}

	return local[:2] + strings.Repeat("*", len(local)-2) + "@" + domain
}

// MaskPhone masks a phone number, showing only last 4 digits.
func MaskPhone(phone string) string {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(phone, "")
	if len(digits) < 4 {
		return strings.Repeat("*", len(phone))
	}

	masked := strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:]

	// Preserve original formatting if possible
	if len(phone) > len(digits) {
		result := ""
		digitIdx := 0
		for _, char := range phone {
			if char >= '0' && char <= '9' {
				if digitIdx < len(digits)-4 {
					result += "*"
				} else {
					result += string(char)
				}
				digitIdx++
			} else {
				result += string(char)
			}
		}
		return result
	}

	return masked
}

// RedactMap returns a copy of the map with sensitive values masked.
// Useful for logging configuration maps.
func RedactMap(data map[string]string, sensitiveKeys []string) map[string]string {
	if len(sensitiveKeys) == 0 {
		sensitiveKeys = []string{
			"password", "secret", "token", "key", "api_key", "apikey",
			"private_key", "access_token", "refresh_token", "auth_token",
			"credential", "credentials", "passwd", "pwd", "jwt_secret",
			"encryption_key", "database_url", "redis_url", "webhook_secret",
		}
	}

	result := make(map[string]string, len(data))
	for k, v := range data {
		lowerKey := strings.ToLower(k)
		isSensitive := false
		for _, sensitive := range sensitiveKeys {
			if strings.Contains(lowerKey, sensitive) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			result[k] = MaskSecret(v)
		} else {
			result[k] = v
		}
	}

	return result
}

// SafeLog returns a safe-to-log version of a string, masking known patterns.
func SafeLog(input string) string {
	// Mask connection strings
	safe := MaskConnectionString(input)

	// Mask API keys (common patterns)
	apiKeyPattern := regexp.MustCompile(`(?i)(api[_-]?key["\']?\s*[:=]\s*["\']?)([a-zA-Z0-9_-]+)`)
	safe = apiKeyPattern.ReplaceAllString(safe, "${1}"+MaskAPIKey("${2}"))

	// Mask secrets
	secretPattern := regexp.MustCompile(`(?i)(secret["\']?\s*[:=]\s*["\']?)([a-zA-Z0-9_-]+)`)
	safe = secretPattern.ReplaceAllString(safe, "${1}"+MaskSecret("${2}"))

	// Mask tokens
	tokenPattern := regexp.MustCompile(`(?i)(token["\']?\s*[:=]\s*["\']?)([a-zA-Z0-9_-]+)`)
	safe = tokenPattern.ReplaceAllString(safe, "${1}"+MaskToken("${2}"))

	// Mask passwords
	passwordPattern := regexp.MustCompile(`(?i)(password["\']?\s*[:=]\s*["\']?)([^\s"',}]*)`)
	safe = passwordPattern.ReplaceAllString(safe, "${1}"+MaskPassword("${2}"))

	return safe
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
