package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestParabolicSARIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108)
	sar := indicators.NewParabolicSARIndicator(ts)

	assert.NotNil(t, sar)

	// Test calculation
	val := sar.Calculate(5)
	assert.NotNil(t, val)
}
