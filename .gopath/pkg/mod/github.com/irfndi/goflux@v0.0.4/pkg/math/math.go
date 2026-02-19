package math

import "math"

// Min returns the smaller integer of the two integers passed in
func Min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

// Max returns the larger of the two integers passed in
func Max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

// Pow returns the first integer to the power of the second integer
func Pow(i, j int) int {
	if j < 0 {
		return 1
	}
	p := 1
	for j > 0 {
		if j&1 != 0 {
			p *= i
		}
		j >>= 1
		i *= i
	}

	return p
}

// Abs returns the absolute value of the passed-in integer
func Abs(b int) int {
	if b < 0 {
		if b == math.MinInt {
			return math.MaxInt
		}
		return -b
	}

	return b
}
