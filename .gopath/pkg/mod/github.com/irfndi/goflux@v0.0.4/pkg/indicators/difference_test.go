package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestDifferenceIndicator_Calculate(t *testing.T) {
	di := indicators.NewDifferenceIndicator(indicators.NewFixedIndicator(10, 9, 8), indicators.NewFixedIndicator(8, 9, 10))

	testutils.DecimalEquals(t, 2, di.Calculate(0))
	testutils.DecimalEquals(t, 0, di.Calculate(1))
	testutils.DecimalEquals(t, -2, di.Calculate(2))
}
