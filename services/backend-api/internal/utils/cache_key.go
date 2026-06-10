package utils

import "strings"

// SanitizeCacheKey sanitizes an input string for safe use as a cache key.
// It replaces any character outside [a-zA-Z0-9-_.] with '_' and caps the
// length at 128 bytes to prevent cache-key-poisoning via oversized or
// crafted input.
func SanitizeCacheKey(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input) && b.Len() < 128; i++ {
		c := input[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
