package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestOpsCommandRegistersBuiltTools(t *testing.T) {
	cmd := opsCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "ops", cmd.Name)

	names := make([]string, 0, len(cmd.Subcommands))
	for _, sub := range cmd.Subcommands {
		names = append(names, sub.Name)
	}

	assert.ElementsMatch(t, []string{
		"scalping-soak",
		"paper-validation",
		"paper-readiness",
		"collect-candles",
		"seed-test-candles",
		"seed-paper-trades",
		"backfill-paper-trades",
		"fetch-real-candles",
	}, names)
}

func TestOpsCommandPassesOnlyExplicitFlags(t *testing.T) {
	var gotBinary string
	var gotArgs []string
	originalRunner := runOpsBinary
	runOpsBinary = func(_ context.Context, binary string, args []string) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runOpsBinary = originalRunner })

	app := &cli.App{
		Name:     "test",
		Commands: []*cli.Command{opsCommand()},
	}

	err := app.Run([]string{
		"test",
		"ops",
		"paper-readiness",
		"--start", "2026-01-01T00:00:00Z",
		"--strategies", "scalping,arbitrage",
		"--fail-if-not-ready",
	})

	require.NoError(t, err)
	assert.Equal(t, "neuratrade-paper-readiness", filepath.Base(gotBinary))
	assert.Equal(t, []string{
		"--start=2026-01-01T00:00:00Z",
		"--strategies=scalping,arbitrage",
		"--fail-if-not-ready=true",
	}, gotArgs)
}

func TestOpsCommandReturnsRunnerFailure(t *testing.T) {
	originalRunner := runOpsBinary
	runOpsBinary = func(_ context.Context, _ string, _ []string) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { runOpsBinary = originalRunner })

	app := &cli.App{
		Name:     "test",
		Commands: []*cli.Command{opsCommand()},
	}

	err := app.Run([]string{"test", "ops", "seed-paper-trades", "--seed", "7"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seed-paper-trades failed")
	assert.Contains(t, err.Error(), "boom")
}

func TestOpsCommandAllowsParentTimeoutOverride(t *testing.T) {
	var gotDeadline time.Time
	var gotDeadlineOK bool
	originalRunner := runOpsBinary
	runOpsBinary = func(ctx context.Context, _ string, _ []string) error {
		gotDeadline, gotDeadlineOK = ctx.Deadline()
		return nil
	}
	t.Cleanup(func() { runOpsBinary = originalRunner })

	app := &cli.App{
		Name:     "test",
		Commands: []*cli.Command{opsCommand()},
	}

	err := app.Run([]string{"test", "ops", "--ops-timeout", "2h", "seed-paper-trades"})
	require.NoError(t, err)
	require.True(t, gotDeadlineOK)
	assert.Greater(t, time.Until(gotDeadline), time.Hour)
}

func TestOpsCommandDerivesLongScalpingSoakTimeout(t *testing.T) {
	var gotDeadline time.Time
	var gotDeadlineOK bool
	originalRunner := runOpsBinary
	runOpsBinary = func(ctx context.Context, _ string, _ []string) error {
		gotDeadline, gotDeadlineOK = ctx.Deadline()
		return nil
	}
	t.Cleanup(func() { runOpsBinary = originalRunner })

	app := &cli.App{
		Name:     "test",
		Commands: []*cli.Command{opsCommand()},
	}

	err := app.Run([]string{"test", "ops", "scalping-soak", "--timeout-seconds", "7200"})
	require.NoError(t, err)
	require.True(t, gotDeadlineOK)
	assert.Greater(t, time.Until(gotDeadline), time.Hour)
}
