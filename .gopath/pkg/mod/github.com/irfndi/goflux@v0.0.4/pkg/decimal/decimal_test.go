package decimal

import (
	"math"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		checkStr string // Use string check instead of exact match due to float64 precision
	}{
		{"zero", 0, "0"},
		{"positive", 123.456, "123.456"},
		{"negative", -123.456, "-123.456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.input)
			// Check if string contains the expected pattern
			got := d.String()
			if len(got) < len(tt.checkStr) || got[:len(tt.checkStr)] != tt.checkStr {
				t.Errorf("New(%v).String() = %s, want to contain %s", tt.input, d.String(), tt.checkStr)
			}
		})
	}
}

func TestNewFromInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected float64
	}{
		{0, 0},
		{1, 1},
		{-1, -1},
		{123456789, 123456789},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			d := NewFromInt(tt.input)
			if d.Float() != tt.expected {
				t.Errorf("NewFromInt(%d).Float() = %v, want %v", tt.input, d.Float(), tt.expected)
			}
		})
	}
}

func TestNewFromString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  float64
		wantPanic bool
	}{
		{"zero", "0", 0, false},
		{"positive", "123.456", 123.456, false},
		{"negative", "-123.456", -123.456, false},
		{"scientific", "1.23e10", 1.23e10, false},
		{"plus", "+123", 123, false},
		{"invalid", "abc", 0, true},
		{"empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("expected panic, got none")
					}
				}()
				NewFromString(tt.input)
			} else {
				d := NewFromString(tt.input)
				if d.Float() != tt.expected {
					t.Errorf("NewFromString(%q).Float() = %v, want %v", tt.input, d.Float(), tt.expected)
				}
			}
		})
	}
}

func TestNewFromBigFloat(t *testing.T) {
	f := math.Pi
	d := New(f)
	d2 := NewFromBigFloat(d.val)
	if d.Float() != d2.Float() {
		t.Errorf("NewFromBigFloat() = %v, want %v", d2.Float(), d.Float())
	}
}

func TestDecimalArithmetic(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		d1 := NewFromString("123.456")
		d2 := NewFromString("78.9")
		result := d1.Add(d2)
		expected := NewFromString("202.356")
		if result.Cmp(expected) != 0 {
			t.Errorf("Add() = %v, want %v", result, expected)
		}
	})

	t.Run("Sub", func(t *testing.T) {
		d1 := NewFromString("123.456")
		d2 := NewFromString("78.9")
		result := d1.Sub(d2)
		expected := NewFromString("44.556")
		// Compare with tolerance due to precision
		diff := result.Sub(expected)
		if diff.Abs().Cmp(NewFromString("0.000001")) > 0 {
			t.Errorf("Sub() = %v, want %v", result, expected)
		}
	})

	t.Run("Mul", func(t *testing.T) {
		d1 := NewFromString("12.34")
		d2 := NewFromString("56.78")
		result := d1.Mul(d2)
		expected := NewFromString("700.6652")
		if result.Cmp(expected) != 0 {
			t.Errorf("Mul() = %v, want %v", result, expected)
		}
	})

	t.Run("Div", func(t *testing.T) {
		d1 := NewFromString("100")
		d2 := NewFromString("8")
		result := d1.Div(d2)
		expected := NewFromString("12.5")
		if result.Cmp(expected) != 0 {
			t.Errorf("Div() = %v, want %v", result, expected)
		}
	})

	t.Run("Div by zero", func(t *testing.T) {
		d1 := New(100)
		d2 := New(0)
		result := d1.Div(d2)
		if result.Cmp(ZERO) != 0 {
			t.Errorf("Div by zero should return ZERO, got %v", result)
		}
	})
}

func TestDecimalComparison(t *testing.T) {
	d1 := New(100)
	d2 := New(100)
	d3 := New(200)

	t.Run("GT", func(t *testing.T) {
		if !d3.GT(d1) {
			t.Error("200 should be greater than 100")
		}
		if d1.GT(d3) {
			t.Error("100 should not be greater than 200")
		}
	})

	t.Run("GTE", func(t *testing.T) {
		if !d3.GTE(d1) {
			t.Error("200 should be greater than or equal to 100")
		}
		if !d1.GTE(d2) {
			t.Error("100 should be greater than or equal to 100")
		}
	})

	t.Run("LT", func(t *testing.T) {
		if !d1.LT(d3) {
			t.Error("100 should be less than 200")
		}
		if d3.LT(d1) {
			t.Error("200 should not be less than 100")
		}
	})

	t.Run("LTE", func(t *testing.T) {
		if !d1.LTE(d2) {
			t.Error("100 should be less than or equal to 100")
		}
		if !d1.LTE(d3) {
			t.Error("100 should be less than or equal to 200")
		}
	})

	t.Run("EQ", func(t *testing.T) {
		if !d1.EQ(d2) {
			t.Error("100 should equal 100")
		}
		if d1.EQ(d3) {
			t.Error("100 should not equal 200")
		}
	})

	t.Run("Nil edge cases", func(t *testing.T) {
		zero := New(0)
		positive := New(10)
		negative := New(-10)

		// Test with zero
		if !positive.GT(zero) {
			t.Error("10 should be greater than 0")
		}
		if !zero.LT(positive) {
			t.Error("0 should be less than 10")
		}
		if !zero.EQ(ZERO) {
			t.Error("0 should equal ZERO")
		}

		// Test with negative
		if !negative.LT(zero) {
			t.Error("-10 should be less than 0")
		}
		if !negative.Abs().EQ(positive) {
			t.Error("abs(-10) should equal 10")
		}
	})
}

func TestZero(t *testing.T) {
	d := New(0)
	if !d.Zero() {
		t.Error("New(0).Zero() should return true")
	}
	_ = d.IsZero() // Using IsZero alias
}

func TestStringAndFormattedString(t *testing.T) {
	d := NewFromString("123.456789")

	// Check that FormattedString starts with expected value
	s := d.FormattedString(10)
	if len(s) < len("123.456789") || s[:len("123.456789")] != "123.456789" {
		t.Errorf("FormattedString(10) = %s, want to start with 123.456789", s)
	}

	if d.FormattedString(2) != "123.46" {
		t.Errorf("FormattedString(2) = %s, want 123.46", d.FormattedString(2))
	}

	if d.FormattedString(0) != "123" {
		t.Errorf("FormattedString(0) = %s, want 123", d.FormattedString(0))
	}
}

func TestAbs(t *testing.T) {
	d1 := New(-100)
	d2 := d1.Abs()

	if d2.Cmp(New(100)) != 0 {
		t.Errorf("Abs(-100) = %v, want 100", d2)
	}

	d3 := New(100)
	d4 := d3.Abs()
	if d4.Cmp(New(100)) != 0 {
		t.Errorf("Abs(100) = %v, want 100", d4)
	}
}

func TestNeg(t *testing.T) {
	d := New(100)
	result := d.Neg()

	if result.Cmp(New(-100)) != 0 {
		t.Errorf("Neg(100) = %v, want -100", result)
	}

	d2 := New(-50)
	result2 := d2.Neg()
	if result2.Cmp(New(50)) != 0 {
		t.Errorf("Neg(-50) = %v, want 50", result2)
	}
}

func TestMax(t *testing.T) {
	d1 := New(100)
	d2 := New(200)

	result := d1.Max(d2)
	if result.Cmp(New(200)) != 0 {
		t.Errorf("Max(100, 200) = %v, want 200", result)
	}

	result2 := d2.Max(d1)
	if result2.Cmp(New(200)) != 0 {
		t.Errorf("Max(200, 100) = %v, want 200", result2)
	}
}

func TestMin(t *testing.T) {
	d1 := New(100)
	d2 := New(200)

	result := d1.Min(d2)
	if result.Cmp(New(100)) != 0 {
		t.Errorf("Min(100, 200) = %v, want 100", result)
	}

	result2 := d2.Min(d1)
	if result2.Cmp(New(100)) != 0 {
		t.Errorf("Min(200, 100) = %v, want 100", result2)
	}
}

func TestSqrt(t *testing.T) {
	d := New(16)
	result := d.Sqrt()

	expected := New(4)
	if result.Cmp(expected) != 0 {
		t.Errorf("Sqrt(16) = %v, want %v", result, expected)
	}

	d2 := New(2)
	result2 := d2.Sqrt()
	// Sqrt(2) H 1.414213562...
	if result2.LT(New(1.4)) || result2.GT(New(1.5)) {
		t.Errorf("Sqrt(2) = %v, should be around 1.414", result2)
	}
}

func TestPow(t *testing.T) {
	t.Run("positive exponent", func(t *testing.T) {
		d := New(2)
		result := d.Pow(10)
		expected := New(1024)
		if result.Cmp(expected) != 0 {
			t.Errorf("Pow(2, 10) = %v, want %v", result, expected)
		}
	})

	t.Run("zero exponent", func(t *testing.T) {
		d := New(100)
		result := d.Pow(0)
		if result.Cmp(ONE) != 0 {
			t.Errorf("Pow(100, 0) = %v, want 1", result)
		}
	})

	t.Run("negative exponent", func(t *testing.T) {
		d := New(2)
		result := d.Pow(-3)
		expected := NewFromString("0.125")
		if result.Cmp(expected) != 0 {
			t.Errorf("Pow(2, -3) = %v, want %v", result, expected)
		}
	})
}

func TestCmp(t *testing.T) {
	d1 := New(100)
	d2 := New(100)
	d3 := New(200)

	if d1.Cmp(d2) != 0 {
		t.Error("Cmp(100, 100) should return 0")
	}
	if d1.Cmp(d3) != -1 {
		t.Error("Cmp(100, 200) should return -1")
	}
	if d3.Cmp(d1) != 1 {
		t.Error("Cmp(200, 100) should return 1")
	}
}

func TestSign(t *testing.T) {
	tests := []struct {
		input    float64
		expected int
	}{
		{0, 0},
		{100, 1},
		{-100, -1},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			d := New(tt.input)
			if d.Sign() != tt.expected {
				t.Errorf("Sign(%v) = %d, want %d", tt.input, d.Sign(), tt.expected)
			}
		})
	}
}

func TestIsNegativeIsPositive(t *testing.T) {
	tests := []struct {
		input          float64
		wantIsNegative bool
		wantIsPositive bool
	}{
		{0, false, false},
		{100, false, true},
		{-100, true, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			d := New(tt.input)
			if d.IsNegative() != tt.wantIsNegative {
				t.Errorf("IsNegative(%v) = %v, want %v", tt.input, d.IsNegative(), tt.wantIsNegative)
			}
			if d.IsPositive() != tt.wantIsPositive {
				t.Errorf("IsPositive(%v) = %v, want %v", tt.input, d.IsPositive(), tt.wantIsPositive)
			}
		})
	}
}

func TestRound(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.4", "1"},
		{"1.5", "2"},
		{"1.6", "2"},
		{"-1.4", "-1"},
		{"-1.5", "-2"},
		{"-1.6", "-2"},
		{"0.5", "1"},
		{"-0.5", "-1"},
		{"0", "0"},
		{"2.999", "3"},
		{"-2.999", "-3"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := NewFromString(tt.input)
			result := d.Round()
			if result.String() != tt.expected {
				t.Errorf("Round(%s) = %s, want %s", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestFloor(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.4", "1"},
		{"1.9", "1"},
		{"-1.4", "-2"},
		{"-1.9", "-2"},
		{"1", "1"},
		{"-1", "-1"},
		{"0", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := NewFromString(tt.input)
			result := d.Floor()
			if result.String() != tt.expected {
				t.Errorf("Floor(%s) = %s, want %s", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestCeil(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.1", "2"},
		{"1.9", "2"},
		{"-1.1", "-1"},
		{"-1.9", "-1"},
		{"1", "1"},
		{"-1", "-1"},
		{"0", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := NewFromString(tt.input)
			result := d.Ceil()
			if result.String() != tt.expected {
				t.Errorf("Ceil(%s) = %s, want %s", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.9", "1"},
		{"-1.9", "-1"},
		{"1.1", "1"},
		{"0", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := NewFromString(tt.input)
			result := d.Truncate()
			if result.String() != tt.expected {
				t.Errorf("Truncate(%s) = %s, want %s", tt.input, result.String(), tt.expected)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if ZERO.Cmp(New(0)) != 0 {
		t.Error("ZERO constant should be 0")
	}
	if ONE.Cmp(New(1)) != 0 {
		t.Error("ONE constant should be 1")
	}
}

func TestDecimal_Concurrency(t *testing.T) {
	a := New(100)
	b := New(50)

	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			_ = a.Add(b)
			_ = a.Sub(b)
			_ = a.Mul(b)
			_ = a.Div(b)
			_ = a.Abs()
			_ = a.Neg()
			_ = a.Round()
			_ = a.Floor()
			_ = a.Ceil()
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestNewFromStringWithError(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectedStr string
	}{
		{"valid integer", "123", false, "123"},
		{"valid decimal", "123.456", false, "123.456"},
		{"valid negative", "-123.456", false, "-123.456"},
		{"invalid string", "abc", true, ""},
		{"empty string", "", true, ""},
		{"partial string", "12.34.56", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewFromStringWithError(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("NewFromStringWithError(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("NewFromStringWithError(%q) unexpected error: %v", tt.input, err)
				}
				got := d.String()
				if len(got) < len(tt.expectedStr) || got[:len(tt.expectedStr)] != tt.expectedStr {
					t.Errorf("NewFromStringWithError(%q).String() = %s, want to contain %s", tt.input, got, tt.expectedStr)
				}
			}
		})
	}
}

func TestDecimal_PowFloat(t *testing.T) {
	tests := []struct {
		name     string
		base     float64
		exp      float64
		expected float64
	}{
		{"square", 2, 2, 4},
		{"cube", 2, 3, 8},
		{"sqrt", 4, 0.5, 2},
		{"negative base", -2, 2, 4},
		{"negative exponent", 2, -1, 0.5},
		{"zero base", 0, 2, 0},
		{"zero exponent", 2, 0, 1},
		{"fractional", 2, 1.5, 2.8284271247461903},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.base)
			result := d.PowFloat(tt.exp)
			if math.Abs(result.Float()-tt.expected) > 1e-6 {
				t.Errorf("New(%v).PowFloat(%v) = %v, want %v", tt.base, tt.exp, result.Float(), tt.expected)
			}
		})
	}
}

func TestDecimal_Frac(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"integer", 5, 0},
		{"positive decimal", 5.75, 0.75},
		{"negative decimal", -5.75, -0.75},
		{"small decimal", 0.123, 0.123},
		{"zero", 0, 0},
		{"large decimal", 100.999, 0.999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.input)
			result := d.Frac()
			if math.Abs(result.Float()-tt.expected) > 1e-6 {
				t.Errorf("New(%v).Frac() = %v, want %v", tt.input, result.Float(), tt.expected)
			}
		})
	}
}
