package utils

import "testing"

func TestSanitizeCacheKey(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"BTC/USDT", "BTC_USDT"},
		{"ETH-BTC", "ETH-BTC"},
		{"SOL.USD", "SOL.USD"},
		{"normal123", "normal123"},
		{"with space", "with_space"},
		{"a/b:c|d", "a_b_c_d"},
		{"", ""},
		{"UPPER", "UPPER"},
		{"mixed-Case_123.test", "mixed-Case_123.test"},
		{"verylongstringthatexceedsthe128bytelimitandshouldbetruncatedtothemaxlengthallowedbythesanitizationfunction", "verylongstringthatexceedsthe128bytelimitandshouldbetruncatedtothemaxlengthallowedbythesanitizationfunction"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := SanitizeCacheKey(tc.input)
			if got != tc.expected {
				t.Fatalf("SanitizeCacheKey(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
