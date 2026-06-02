package handlers

import "strings"

func sanitizeCacheKey(input string) string {
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
