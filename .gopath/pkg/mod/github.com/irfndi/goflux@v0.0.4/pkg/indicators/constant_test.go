package indicators_test

import (
	"math"
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestConstantIndicator_Calculate(t *testing.T) {
	ci := indicators.NewConstantIndicator(4.56)

	testutils.DecimalEquals(t, 4.56, ci.Calculate(0))
	testutils.DecimalEquals(t, 4.56, ci.Calculate(-math.MaxInt64))
	testutils.DecimalEquals(t, 4.56, ci.Calculate(math.MaxInt64))
}
