package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestBacktestRunActionRejectsInvertedOrEqualDateRange(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{
			name:  "equal range",
			start: "2026-01-01T00:00:00Z",
			end:   "2026-01-01T00:00:00Z",
		},
		{
			name:  "inverted range",
			start: "2026-01-02T00:00:00Z",
			end:   "2026-01-01T00:00:00Z",
		},
		{
			name:  "fractional equal range",
			start: "2026-01-01T00:00:00.123456789Z",
			end:   "2026-01-01T00:00:00.123456789Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := flag.NewFlagSet("backtest", flag.ContinueOnError)
			flags.String("start", "", "")
			flags.String("end", "", "")
			flags.String("mode", "deterministic", "")
			flags.String("symbols", "", "")
			flags.String("exchange", "", "")
			flags.String("initial-capital", "", "")
			flags.Duration("timeout", defaultTimeout, "")
			require.NoError(t, flags.Parse([]string{"--start", tt.start, "--end", tt.end}))

			err := backtestRunAction(cli.NewContext(cli.NewApp(), flags, nil))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--start must be before --end")
			assert.Contains(t, err.Error(), tt.start)
			assert.Contains(t, err.Error(), tt.end)
		})
	}
}

func TestBacktestRunActionQuotesInvalidTimestampValues(t *testing.T) {
	flags := flag.NewFlagSet("backtest", flag.ContinueOnError)
	flags.String("start", "", "")
	flags.String("end", "", "")
	flags.String("mode", "deterministic", "")
	flags.String("symbols", "", "")
	flags.String("exchange", "", "")
	flags.String("initial-capital", "", "")
	flags.Duration("timeout", defaultTimeout, "")
	require.NoError(t, flags.Parse([]string{
		"--start", "not-a-time",
		"--end", "2026-01-01T00:00:00Z",
	}))

	err := backtestRunAction(cli.NewContext(cli.NewApp(), flags, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid --start "not-a-time"`)
}

func TestBacktestCommandUsesActualTimeoutDefaultInHelp(t *testing.T) {
	cmd := backtestCommand()
	require.Len(t, cmd.Subcommands, 1)

	var timeoutFlag cli.Flag
	for _, flag := range cmd.Subcommands[0].Flags {
		if names := flag.Names(); len(names) > 0 && names[0] == "timeout" {
			timeoutFlag = flag
			break
		}
	}

	require.NotNil(t, timeoutFlag)
	assert.Contains(t, timeoutFlag.String(), "default: 5s")
}
