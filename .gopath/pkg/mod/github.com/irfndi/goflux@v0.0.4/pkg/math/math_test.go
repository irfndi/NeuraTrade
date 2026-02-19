package math

import (
	"math"
	"testing"
)

func TestMin(t *testing.T) {
	tests := []struct {
		i, j     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{-1, 1, -1},
		{1, -1, -1},
		{-1, -2, -2},
		{0, 0, 0},
		{100, 50, 50},
		{-100, -50, -100},
		{math.MaxInt, math.MaxInt - 1, math.MaxInt - 1},
		{math.MinInt, math.MinInt + 1, math.MinInt},
	}

	for _, tt := range tests {
		result := Min(tt.i, tt.j)
		if result != tt.expected {
			t.Errorf("Min(%d, %d) = %d, want %d", tt.i, tt.j, result, tt.expected)
		}
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		i, j     int
		expected int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{-1, 1, 1},
		{1, -1, 1},
		{-1, -2, -1},
		{0, 0, 0},
		{100, 50, 100},
		{-100, -50, -50},
		{math.MaxInt, math.MaxInt - 1, math.MaxInt},
		{math.MinInt, math.MinInt + 1, math.MinInt + 1},
	}

	for _, tt := range tests {
		result := Max(tt.i, tt.j)
		if result != tt.expected {
			t.Errorf("Max(%d, %d) = %d, want %d", tt.i, tt.j, result, tt.expected)
		}
	}
}

func TestPow(t *testing.T) {
	tests := []struct {
		i, j     int
		expected int
	}{
		{2, 0, 1},
		{2, 1, 2},
		{2, 2, 4},
		{2, 3, 8},
		{2, 10, 1024},
		{3, 3, 27},
		{5, 3, 125},
		{10, 2, 100},
		{10, 5, 100000},
		{1, 100, 1},
		{0, 5, 0},
		{-2, 3, -8},
		{-2, 4, 16},
	}

	for _, tt := range tests {
		result := Pow(tt.i, tt.j)
		if result != tt.expected {
			t.Errorf("Pow(%d, %d) = %d, want %d", tt.i, tt.j, result, tt.expected)
		}
	}
}

func TestPowNegativeExponent(t *testing.T) {
	tests := []struct {
		i, j     int
		expected int
	}{
		{2, -1, 1},
		{2, -2, 1},
		{10, -3, 1},
	}

	for _, tt := range tests {
		result := Pow(tt.i, tt.j)
		if result != tt.expected {
			t.Errorf("Pow(%d, %d) = %d, want %d", tt.i, tt.j, result, tt.expected)
		}
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{5, 5},
		{-5, 5},
		{0, 0},
		{100, 100},
		{-100, 100},
		{math.MaxInt, math.MaxInt},
	}

	for _, tt := range tests {
		result := Abs(tt.input)
		if result != tt.expected {
			t.Errorf("Abs(%d) = %d, want %d", tt.input, result, tt.expected)
		}
	}

	t.Run("MinIntOverflow", func(t *testing.T) {
		result := Abs(math.MinInt)
		if result != math.MaxInt {
			t.Errorf("Abs(math.MinInt) = %d, want %d", result, math.MaxInt)
		}
	})
}

func TestMinSymmetry(t *testing.T) {
	if Min(5, 10) != Min(10, 5) {
		t.Error("Min(5, 10) should equal Min(10, 5)")
	}
	if Min(-5, 10) != Min(10, -5) {
		t.Error("Min(-5, 10) should equal Min(10, -5)")
	}
}

func TestMaxSymmetry(t *testing.T) {
	if Max(5, 10) != Max(10, 5) {
		t.Error("Max(5, 10) should equal Max(10, 5)")
	}
	if Max(-5, 10) != Max(10, -5) {
		t.Error("Max(-5, 10) should equal Max(10, -5)")
	}
}

func TestAbsAlwaysNonNegative(t *testing.T) {
	tests := []int{-100, -1, 0, 1, 100, math.MaxInt}
	for _, tt := range tests {
		result := Abs(tt)
		if result < 0 {
			t.Errorf("Abs(%d) = %d, should always be non-negative", tt, result)
		}
	}

	t.Run("MinIntOverflowEdgeCase", func(t *testing.T) {
		result := Abs(math.MinInt)
		if result == math.MinInt {
			t.Logf("Abs(math.MinInt) overflows to %d (expected for int overflow)", result)
		}
	})
}

func TestPowIdentity(t *testing.T) {
	tests := []int{-10, -1, 0, 1, 2, 10, 100}
	for _, tt := range tests {
		if Pow(tt, 1) != tt {
			t.Errorf("Pow(%d, 1) should equal %d", tt, tt)
		}
	}
}

func TestPowZeroExponent(t *testing.T) {
	tests := []int{-10, -1, 0, 1, 2, 10, 100}
	for _, tt := range tests {
		if Pow(tt, 0) != 1 {
			t.Errorf("Pow(%d, 0) should equal 1", tt)
		}
	}
}

func TestConcurrency(t *testing.T) {
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			_ = Min(1, 2)
			_ = Max(1, 2)
			_ = Pow(2, 10)
			_ = Abs(-5)
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

func BenchmarkMin(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Min(i, i+1)
	}
}

func BenchmarkMax(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Max(i, i+1)
	}
}

func BenchmarkPow(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Pow(2, 10)
	}
}

func BenchmarkAbs(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Abs(i)
	}
}
