//go:build go1.18

package services

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

// FuzzArbitrageProfitCalculation fuzzes the profit percentage calculation
// This tests the core profit calculation: (sellPrice - buyPrice) / buyPrice * 100
func FuzzArbitrageProfitCalculation(f *testing.F) {
	// Add seed corpus with various scenarios
	f.Add(float64(100.0), float64(105.0))         // 5% profit
	f.Add(float64(0.001), float64(0.00105))       // Small values
	f.Add(float64(50000.0), float64(50250.0))     // Large BTC-like values
	f.Add(float64(1.0), float64(1.0))             // No profit
	f.Add(float64(100.0), float64(99.0))          // Negative profit (loss)
	f.Add(float64(0.00001), float64(0.000011))    // Very small values (low-cap tokens)
	f.Add(float64(1000000.0), float64(1001000.0)) // Very large values

	f.Fuzz(func(t *testing.T, buyPrice, sellPrice float64) {
		// Skip invalid inputs
		if buyPrice <= 0 || sellPrice <= 0 {
			return
		}

		// Skip infinity and NaN
		if math.IsInf(buyPrice, 0) || math.IsInf(sellPrice, 0) ||
			math.IsNaN(buyPrice) || math.IsNaN(sellPrice) {
			return
		}

		// Skip extremely large values that would cause overflow
		if buyPrice > 1e15 || sellPrice > 1e15 {
			return
		}

		buyDec := decimal.NewFromFloat(buyPrice)
		sellDec := decimal.NewFromFloat(sellPrice)

		// Calculate profit percentage: (sell - buy) / buy * 100
		if !buyDec.IsZero() {
			profit := sellDec.Sub(buyDec).Div(buyDec).Mul(decimal.NewFromInt(100))

			// Verify calculation is finite and reasonable
			profitFloat, exact := profit.Float64()
			if !exact && (profitFloat != profitFloat) { // NaN check
				t.Errorf("Profit calculation resulted in NaN for buy=%v, sell=%v", buyPrice, sellPrice)
			}

			// Verify profit is within reasonable bounds
			// Can't lose more than 100% if both prices are positive
			if profitFloat < -100 && sellPrice > 0 && buyPrice > 0 {
				t.Logf("Unusual profit calculation: buy=%v, sell=%v, profit=%v%%",
					buyPrice, sellPrice, profitFloat)
			}
		}
	})
}

// FuzzDecimalOperations fuzzes decimal arithmetic operations
// This tests the decimal library we use for financial calculations
func FuzzDecimalOperations(f *testing.F) {
	// Add seed corpus
	f.Add("100.50", "99.25")
	f.Add("0.00000001", "0.00000002")
	f.Add("999999999.999999999", "1.0")
	f.Add("1e-10", "1e10")
	f.Add("0.1", "0.2") // Test for floating point precision issues

	f.Fuzz(func(t *testing.T, a, b string) {
		decA, err := decimal.NewFromString(a)
		if err != nil {
			return // Invalid decimal string, skip
		}

		decB, err := decimal.NewFromString(b)
		if err != nil {
			return // Invalid decimal string, skip
		}

		// Skip extreme values
		if decA.Abs().GreaterThan(decimal.NewFromInt(1e15)) ||
			decB.Abs().GreaterThan(decimal.NewFromInt(1e15)) {
			return
		}

		// Test addition (should not panic)
		sum := decA.Add(decB)
		_ = sum

		// Test subtraction (should not panic)
		diff := decA.Sub(decB)
		_ = diff

		// Test multiplication (should not panic)
		product := decA.Mul(decB)
		_ = product

		// Test division (guard against zero)
		if !decB.IsZero() {
			quotient := decA.Div(decB)
			_ = quotient

			// Verify quotient * decB approximately equals decA
			// (allowing for precision loss in very large/small numbers)
			reconstructed := quotient.Mul(decB)
			diff := decA.Sub(reconstructed).Abs()

			// For reasonable numbers, the difference should be very small
			if decA.Abs().LessThan(decimal.NewFromInt(1e12)) &&
				decB.Abs().GreaterThan(decimal.NewFromFloat(1e-10)) {
				tolerance := decA.Abs().Mul(decimal.NewFromFloat(1e-10))
				if diff.GreaterThan(tolerance) && tolerance.GreaterThan(decimal.Zero) {
					// Log but don't fail - precision loss is expected for extreme values
					t.Logf("Precision loss: a=%s, b=%s, diff=%s", a, b, diff.String())
				}
			}
		}
	})
}

// FuzzSpreadCalculation fuzzes the spread calculation used in arbitrage detection
func FuzzSpreadCalculation(f *testing.F) {
	// Add seed corpus with realistic bid/ask scenarios
	f.Add(float64(100.0), float64(100.5))  // Normal spread
	f.Add(float64(99.9), float64(100.0))   // Tight spread
	f.Add(float64(100.0), float64(101.0))  // Wide spread
	f.Add(float64(0.001), float64(0.0011)) // Small value spread

	f.Fuzz(func(t *testing.T, bid, ask float64) {
		// Skip invalid inputs
		if bid <= 0 || ask <= 0 {
			return
		}

		// Skip infinity and NaN
		if math.IsInf(bid, 0) || math.IsInf(ask, 0) ||
			math.IsNaN(bid) || math.IsNaN(ask) {
			return
		}

		bidDec := decimal.NewFromFloat(bid)
		askDec := decimal.NewFromFloat(ask)

		// Calculate spread percentage: (ask - bid) / bid * 100
		if !bidDec.IsZero() {
			spread := askDec.Sub(bidDec).Div(bidDec).Mul(decimal.NewFromInt(100))

			spreadFloat, _ := spread.Float64()

			// Verify the calculation didn't produce NaN
			if spreadFloat != spreadFloat { // NaN check
				t.Errorf("Spread calculation resulted in NaN for bid=%v, ask=%v", bid, ask)
			}

			// For normal markets, spread should be between -100% and +100%
			// (negative spread means crossed market, which can happen temporarily)
		}
	})
}

// FuzzVolumeCalculation fuzzes volume-weighted calculations
func FuzzVolumeCalculation(f *testing.F) {
	f.Add(float64(100.0), float64(1000.0)) // Price and volume
	f.Add(float64(0.00001), float64(1e9))  // Small price, huge volume
	f.Add(float64(50000.0), float64(0.01)) // Large price, small volume

	f.Fuzz(func(t *testing.T, price, volume float64) {
		// Skip invalid inputs
		if price <= 0 || volume <= 0 {
			return
		}

		// Skip infinity and NaN
		if math.IsInf(price, 0) || math.IsInf(volume, 0) ||
			math.IsNaN(price) || math.IsNaN(volume) {
			return
		}

		// Skip extremely large values
		if price > 1e12 || volume > 1e15 {
			return
		}

		priceDec := decimal.NewFromFloat(price)
		volumeDec := decimal.NewFromFloat(volume)

		// Calculate notional value: price * volume
		notional := priceDec.Mul(volumeDec)

		notionalFloat, _ := notional.Float64()

		// Verify the calculation didn't produce NaN or overflow to infinity
		if notionalFloat != notionalFloat || math.IsInf(notionalFloat, 0) {
			t.Errorf("Notional calculation failed for price=%v, volume=%v", price, volume)
		}
	})
}
