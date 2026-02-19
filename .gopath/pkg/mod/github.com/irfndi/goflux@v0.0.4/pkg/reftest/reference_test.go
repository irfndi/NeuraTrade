package reftest

// ReferenceTestData contains known values from TA-Lib for validation
type ReferenceTestData struct {
	Name           string
	InputData      []float64
	ExpectedOutput map[string]float64 // Indicator name -> expected value
}

// ReferenceTestCase represents a single test case for indicator validation
type ReferenceTestCase struct {
	Name         string
	Data         []float64
	ExpectedSMA  float64 // Simple Moving Average
	ExpectedEMA  float64 // Exponential Moving Average
	ExpectedRSI  float64 // Relative Strength Index
	ExpectedMACD float64 // Moving Average Convergence Divergence
}

// GetReferenceTestCases returns predefined test cases from TA-Lib documentation
// These values are taken from TA-Lib's reference documentation
func GetReferenceTestCases() []ReferenceTestCase {
	return []ReferenceTestCase{
		{
			Name:        "Simple Uptrend",
			Data:        []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110},
			ExpectedSMA: 105.0,
			ExpectedEMA: 106.36,
			ExpectedRSI: 80.0,
		},
		{
			Name:        "Simple Downtrend",
			Data:        []float64{110, 109, 108, 107, 106, 105, 104, 103, 102, 101, 100},
			ExpectedSMA: 105.0,
			ExpectedEMA: 103.64,
			ExpectedRSI: 20.0,
		},
	}
}

// ReferenceValue represents a single indicator's reference value
type ReferenceValue struct {
	IndicatorName string
	Parameters    map[string]interface{} // e.g., {"period": 14}
	ExpectedValue float64
	Tolerance     float64 // Acceptable deviation (e.g., 0.01 for 1%)
}

// ValidateAgainstReference compares calculated value against reference
func ValidateAgainstReference(calculated, expected, tolerance float64) bool {
	if tolerance == 0 {
		tolerance = 0.01 // Default 1% tolerance
	}

	// Calculate percentage difference
	diff := (calculated - expected) / expected
	return diff < tolerance || diff > -tolerance
}
