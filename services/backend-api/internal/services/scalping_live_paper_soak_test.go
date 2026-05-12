package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScalpingLivePaperSoakOptionNormalization(t *testing.T) {
	require.Equal(t, DefaultScalpingLivePaperSoakCycles, NormalizeScalpingLivePaperSoakCycles(0))
	require.Equal(t, 3, NormalizeScalpingLivePaperSoakCycles(3))
	require.Equal(t, MaxScalpingLivePaperSoakCycles, NormalizeScalpingLivePaperSoakCycles(MaxScalpingLivePaperSoakCycles+1))

	require.Equal(t, time.Duration(0), NormalizeScalpingLivePaperSoakInterval(-time.Second))
	require.Equal(t, 2*time.Second, NormalizeScalpingLivePaperSoakInterval(2*time.Second))
	require.Equal(t, MaxScalpingLivePaperSoakInterval, NormalizeScalpingLivePaperSoakInterval(MaxScalpingLivePaperSoakInterval+time.Second))
}

func TestScalpingLivePaperSoakTimeoutScalesWithCyclesAndInterval(t *testing.T) {
	timeout := ScalpingLivePaperSoakTimeout(3, 2*time.Second)
	require.Equal(t, 2*time.Minute+4*time.Second, timeout)

	capped := ScalpingLivePaperSoakTimeout(MaxScalpingLivePaperSoakCycles+5, MaxScalpingLivePaperSoakInterval+time.Second)
	require.Equal(t, 35*time.Minute+30*time.Second, capped)
}
