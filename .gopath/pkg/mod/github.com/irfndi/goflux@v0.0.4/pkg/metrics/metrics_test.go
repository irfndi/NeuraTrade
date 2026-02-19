package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
)

func TestSharpeRatio(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.03),
	}

	sr := SharpeRatio(returns, decimal.New(0.005))
	assert.True(t, sr.GT(decimal.ZERO))
}

func TestSortinoRatio(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.03),
	}

	sr := SortinoRatio(returns, decimal.New(0.005))
	assert.True(t, sr.GT(decimal.ZERO))
}

func TestCalmarRatio(t *testing.T) {
	cr := CalmarRatio(decimal.New(0.15), decimal.New(0.05))
	assert.InEpsilon(t, 3.0, cr.Float(), 0.0001)
}

func TestCAGR(t *testing.T) {
	cagr := CAGR(decimal.New(100), decimal.New(121), 2)
	assert.InEpsilon(t, 0.1, cagr.Float(), 0.0001)
}

func TestBurkeRatio(t *testing.T) {
	br := BurkeRatio(decimal.New(0.1), []float64{0.05, 0.02})
	assert.True(t, br.GT(decimal.ZERO))
}
