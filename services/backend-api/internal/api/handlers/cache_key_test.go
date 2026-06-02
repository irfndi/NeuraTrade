package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeCacheKey(t *testing.T) {
	t.Run("alphanumeric preserved", func(t *testing.T) {
		assert.Equal(t, "BTCUSDT", sanitizeCacheKey("BTCUSDT"))
	})

	t.Run("colons replaced", func(t *testing.T) {
		assert.Equal(t, "BTC_USDT", sanitizeCacheKey("BTC:USDT"))
	})

	t.Run("special chars replaced", func(t *testing.T) {
		assert.Equal(t, "binance_1__", sanitizeCacheKey("binance\n1\t\r"))
	})

	t.Run("spaces replaced", func(t *testing.T) {
		assert.Equal(t, "hello_world", sanitizeCacheKey("hello world"))
	})

	t.Run("injection chars replaced", func(t *testing.T) {
		assert.Equal(t, "a_b_c_d_e_f", sanitizeCacheKey("a\nb\rc\td:e f"))
	})

	t.Run("path traversal chars replaced", func(t *testing.T) {
		assert.Equal(t, "_.._etc_passwd", sanitizeCacheKey("/../etc/passwd"))
	})

	t.Run("newlines in input", func(t *testing.T) {
		assert.Equal(t, "abc_123", sanitizeCacheKey("abc\n123"))
	})

	t.Run("empty string", func(t *testing.T) {
		assert.Equal(t, "", sanitizeCacheKey(""))
	})

	t.Run("very long input bounded to 128 chars", func(t *testing.T) {
		long := ""
		for i := 0; i < 200; i++ {
			long += "a"
		}
		result := sanitizeCacheKey(long)
		assert.Len(t, result, 128)
	})

	t.Run("mixed case and digits preserved", func(t *testing.T) {
		assert.Equal(t, "Binance_123_TEST", sanitizeCacheKey("Binance:123 TEST"))
	})
}
