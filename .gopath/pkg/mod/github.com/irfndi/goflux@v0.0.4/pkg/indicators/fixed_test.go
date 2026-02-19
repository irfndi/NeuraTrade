package indicators_test

import (
	"math"
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestFixedIndicator_Calculate(t *testing.T) {
	fi := indicators.NewFixedIndicator(0, 1, 2, -100, math.MaxInt64)

	testutils.DecimalEquals(t, 0, fi.Calculate(0))
	testutils.DecimalEquals(t, 1, fi.Calculate(1))
	testutils.DecimalEquals(t, 2, fi.Calculate(2))
	testutils.DecimalEquals(t, -100, fi.Calculate(3))
	testutils.DecimalEquals(t, math.MaxInt64, fi.Calculate(4))
}
