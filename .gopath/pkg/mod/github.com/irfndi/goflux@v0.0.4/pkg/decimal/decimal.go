package decimal

import (
	"fmt"
	"math"
	"math/big"
)

// Decimal represents a high-precision decimal number.
// It wraps math/big.Float to provide convenient methods for financial calculations.
type Decimal struct {
	val *big.Float
}

func (d Decimal) bigFloatOrZero() *big.Float {
	if d.val == nil {
		return big.NewFloat(0)
	}
	return d.val
}

var (
	// ZERO is a Decimal with value 0
	ZERO = New(0)
	// ONE is a Decimal with value 1
	ONE = New(1)
)

// New creates a new Decimal from a float64
func New(f float64) Decimal {
	return Decimal{val: new(big.Float).SetFloat64(f)}
}

// NewFromInt creates a new Decimal from an int64
func NewFromInt(i int64) Decimal {
	return Decimal{val: new(big.Float).SetInt64(i)}
}

// NewFromString creates a new Decimal from a string.
// It panics if string is not a valid number.
func NewFromString(s string) Decimal {
	val, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil {
		panic(fmt.Sprintf("invalid decimal string: %s", s))
	}
	return Decimal{val: val}
}

// NewFromStringWithError creates a new Decimal from a string.
// Returns error if string is not a valid number.
func NewFromStringWithError(s string) (Decimal, error) {
	val, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil {
		return Decimal{}, err
	}
	return Decimal{val: val}, nil
}

// NewFromBigFloat creates a new Decimal from a big.Float
func NewFromBigFloat(f *big.Float) Decimal {
	return Decimal{val: new(big.Float).Copy(f)}
}

// Add returns d + d2
func (d Decimal) Add(d2 Decimal) Decimal {
	if d2.val == nil {
		return d
	}
	if d.val == nil {
		return d2
	}
	return Decimal{val: new(big.Float).Add(d.val, d2.val)}
}

// Sub returns d - d2
func (d Decimal) Sub(d2 Decimal) Decimal {
	if d2.val == nil {
		return d
	}
	if d.val == nil {
		return d2.Neg()
	}
	return Decimal{val: new(big.Float).Sub(d.val, d2.val)}
}

// Mul returns d * d2
func (d Decimal) Mul(d2 Decimal) Decimal {
	if d.val == nil || d2.val == nil {
		return Decimal{}
	}
	return Decimal{val: new(big.Float).Mul(d.val, d2.val)}
}

// Div returns d / d2
func (d Decimal) Div(d2 Decimal) Decimal {
	if d2.Zero() {
		return Decimal{val: big.NewFloat(0)}
	}
	if d.val == nil || d2.val == nil {
		return Decimal{}
	}
	return Decimal{val: new(big.Float).Quo(d.val, d2.val)}
}

// GT returns true if d > d2
func (d Decimal) GT(d2 Decimal) bool {
	if d.val == nil || d2.val == nil {
		return false
	}
	return d.val.Cmp(d2.val) > 0
}

// GTE returns true if d >= d2
func (d Decimal) GTE(d2 Decimal) bool {
	if d.val == nil || d2.val == nil {
		return d.val == d2.val
	}
	return d.val.Cmp(d2.val) >= 0
}

// LT returns true if d < d2
func (d Decimal) LT(d2 Decimal) bool {
	if d.val == nil || d2.val == nil {
		return false
	}
	return d.val.Cmp(d2.val) < 0
}

// LTE returns true if d <= d2
func (d Decimal) LTE(d2 Decimal) bool {
	if d.val == nil || d2.val == nil {
		return d.val == d2.val
	}
	return d.val.Cmp(d2.val) <= 0
}

// EQ returns true if d == d2
func (d Decimal) EQ(d2 Decimal) bool {
	if d.val == nil || d2.val == nil {
		return d.Sign() == d2.Sign()
	}
	return d.val.Cmp(d2.val) == 0
}

// Zero returns true if d == 0
func (d Decimal) Zero() bool {
	return d.Sign() == 0
}

// Float returns float64 representation of d
func (d Decimal) Float() float64 {
	if d.val == nil {
		return 0
	}
	f, _ := d.val.Float64()
	return f
}

// String returns string representation of d
func (d Decimal) String() string {
	if d.val == nil {
		return "0"
	}
	return d.val.Text('f', -1)
}

// FormattedString returns string representation of d with fixed precision
func (d Decimal) FormattedString(precision int) string {
	if d.val == nil {
		return "0"
	}
	return d.val.Text('f', precision)
}

// Abs returns absolute value of d
func (d Decimal) Abs() Decimal {
	if d.val == nil {
		return ZERO
	}
	return Decimal{val: new(big.Float).Abs(d.val)}
}

// Neg returns -d
func (d Decimal) Neg() Decimal {
	if d.val == nil {
		return ZERO
	}
	return Decimal{val: new(big.Float).Neg(d.val)}
}

// Max returns larger of d and d2
func (d Decimal) Max(d2 Decimal) Decimal {
	if d.GT(d2) {
		return d
	}
	return d2
}

// Min returns smaller of d and d2
func (d Decimal) Min(d2 Decimal) Decimal {
	if d.LT(d2) {
		return d
	}
	return d2
}

// Sqrt returns square root of d
func (d Decimal) Sqrt() Decimal {
	if d.val == nil {
		return ZERO
	}
	return Decimal{val: new(big.Float).Sqrt(d.val)}
}

// Pow returns d^y where y is an integer
func (d Decimal) Pow(y int) Decimal {
	if y == 0 {
		return ONE
	}

	absY := y
	neg := false
	if y < 0 {
		absY = -y
		neg = true
	}

	result := ONE
	base := d
	for absY > 0 {
		if absY&1 == 1 {
			result = result.Mul(base)
		}
		base = base.Mul(base)
		absY >>= 1
	}

	if neg {
		return ONE.Div(result)
	}
	return result
}

// PowFloat returns d^y where y is a float64 using math.Pow
func (d Decimal) PowFloat(y float64) Decimal {
	f := math.Pow(d.Float(), y)
	return New(f)
}

// Cmp compares d and d2 and returns:
//
//	-1 if d <  d2
//	 0 if d == d2
//	+1 if d >  d2
func (d Decimal) Cmp(d2 Decimal) int {
	return d.bigFloatOrZero().Cmp(d2.bigFloatOrZero())
}

// Sign returns -1 if d < 0, 0 if d == 0, +1 if d > 0
func (d Decimal) Sign() int {
	if d.val == nil {
		return 0
	}
	return d.val.Sign()
}

// IsZero returns true if d == 0
func (d Decimal) IsZero() bool {
	return d.Sign() == 0
}

// IsNegative returns true if d < 0
func (d Decimal) IsNegative() bool {
	return d.Sign() < 0
}

// IsPositive returns true if d > 0
func (d Decimal) IsPositive() bool {
	return d.Sign() > 0
}

// Round returns d rounded to the nearest integer, with ties rounding away from zero
func (d Decimal) Round() Decimal {
	if d.IsZero() {
		return d
	}

	f := d.Float()
	if d.IsPositive() {
		return New(float64(int(f + 0.5)))
	}
	return New(float64(int(f - 0.5)))
}

// Floor returns the greatest integer value less than or equal to d
func (d Decimal) Floor() Decimal {
	if d.val == nil {
		return ZERO
	}
	z := new(big.Int)
	d.val.Int(z)
	result := new(big.Float).SetInt(z)

	if d.val.Cmp(result) < 0 {
		result.Sub(result, new(big.Float).SetInt64(1))
	}
	return Decimal{val: result}
}

// Ceil returns the least integer value greater than or equal to d
func (d Decimal) Ceil() Decimal {
	if d.val == nil {
		return ZERO
	}
	z := new(big.Int)
	d.val.Int(z)
	result := new(big.Float).SetInt(z)

	if d.val.Cmp(result) > 0 {
		result.Add(result, new(big.Float).SetInt64(1))
	}
	return Decimal{val: result}
}

// Truncate returns the integer part of d, dropping any fractional part
func (d Decimal) Truncate() Decimal {
	if d.val == nil {
		return ZERO
	}
	z := new(big.Int)
	d.val.Int(z)
	return Decimal{val: new(big.Float).SetInt(z)}
}

// Frac returns the fractional part of d
func (d Decimal) Frac() Decimal {
	if d.val == nil {
		return ZERO
	}
	z := new(big.Int)
	d.val.Int(z)
	result := new(big.Float).SetInt(z)
	return Decimal{val: new(big.Float).Sub(d.val, result)}
}
