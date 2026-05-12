package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScalpingLivePaperSoakOptionNormalization(t *testing.T) {
	cycleCases := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero_defaults", in: 0, want: DefaultScalpingLivePaperSoakCycles},
		{name: "passthrough", in: 3, want: 3},
		{name: "cap_max", in: MaxScalpingLivePaperSoakCycles + 1, want: MaxScalpingLivePaperSoakCycles},
	}
	for _, tc := range cycleCases {
		t.Run("cycles_"+tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeScalpingLivePaperSoakCycles(tc.in))
		})
	}

	intervalCases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{name: "negative_to_zero", in: -time.Second, want: 0},
		{name: "passthrough", in: 2 * time.Second, want: 2 * time.Second},
		{name: "cap_max", in: MaxScalpingLivePaperSoakInterval + time.Second, want: MaxScalpingLivePaperSoakInterval},
	}
	for _, tc := range intervalCases {
		t.Run("interval_"+tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeScalpingLivePaperSoakInterval(tc.in))
		})
	}
}

func TestScalpingLivePaperSoakTimeoutScalesWithCyclesAndInterval(t *testing.T) {
	cases := []struct {
		name     string
		cycles   int
		interval time.Duration
		want     time.Duration
	}{
		{name: "scales_with_cycles_and_interval", cycles: 3, interval: 2 * time.Second, want: 2*time.Minute + 4*time.Second},
		{name: "caps_cycles_and_interval", cycles: MaxScalpingLivePaperSoakCycles + 5, interval: MaxScalpingLivePaperSoakInterval + time.Second, want: 35*time.Minute + 30*time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ScalpingLivePaperSoakTimeout(tc.cycles, tc.interval))
		})
	}
}
