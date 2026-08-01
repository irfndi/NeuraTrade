package api

import (
	"testing"
)

func FuzzParseRequiredOrderDecimalsNeverPanics(f *testing.F) {
	f.Add("0.1", "100")
	f.Add("", "")
	f.Add("not-a-decimal", "100")
	f.Add("0.000000000000000001", "70000.123456789012345678")

	f.Fuzz(func(t *testing.T, size, price string) {
		_, _, _ = parseRequiredOrderDecimals(liveFuturesOrderRequest{
			Size:  size,
			Price: price,
		})
	})
}
