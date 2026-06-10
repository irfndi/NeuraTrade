package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"
)

// agentRunAction is the urfave/cli/v2 Action for `neuratrade agent run`.
// PR-6 skeleton: this command surfaces the documented way to launch
// the agent-control service, but does not itself spawn the process —
// that is the gateway's job (see `neuratrade gateway start`). The
// rationale is that the agent runtime requires the full gateway
// supervision graph (backend health probes, gateway-state.json
// writes, log rotation) and the CLI should not bypass that.
//
// If the operator really wants a standalone agent run, the command
// prints the exact command line they need to invoke, including the
// relevant NEURATRADE_* env vars to set.
func agentRunAction(cCtx *cli.Context) error {
	fmt.Println("The agent service is NOT yet managed by `neuratrade gateway`.")
	fmt.Println("`gateway start` only launches backend, ccxt, and telegram;")
	fmt.Println("the agent-control process must be started separately.")
	fmt.Println()
	fmt.Println("For a standalone agent run, invoke the agent-control binary directly")
	fmt.Println("from its own module (the repo root has no go.mod for the gateway):")
	fmt.Println()
	fmt.Println("    cd services/agent-control")
	fmt.Println("    go run ./cmd/agent")
	fmt.Println()
	fmt.Println("Or build and run the agent binary directly:")
	fmt.Println()
	fmt.Println("    go -C services/agent-control build -o bin/neuratrade-agent ./cmd/agent")
	fmt.Println("    ./services/agent-control/bin/neuratrade-agent")
	fmt.Println()
	fmt.Println("Required environment variables (read from .env or config.json):")
	fmt.Println("    BACKEND_API_URL          (default: http://localhost:8080)")
	fmt.Println("    BACKEND_EVENT_URL        (default: http://localhost:8080/api/v1/agent/events)")
	fmt.Println("    ADMIN_API_KEY            (no default; required for write endpoints)")
	fmt.Println("    POLICY_MAX_ORDER_SIZE    (default: 1.0)")
	fmt.Println("    POLICY_MAX_LEVERAGE       (default: 5.0)")
	fmt.Println("    POLICY_MAX_DAILY_LOSS     (default: 1000.0)")
	return nil
}

// agentStatusAction is the urfave/cli/v2 Action for `neuratrade agent
// status`. PR-6 skeleton: it reads ~/.neuratrade/pids/agent.pid (if
// present) and reports whether the agent process is alive, then tails
// the last N lines of ~/.neuratrade/logs/agent.log. The PID file is
// expected to be written by the gateway once it learns to manage the
// agent service; until that lands, this command reports
// 'not-managed' and exits 0 (not an error).
func agentStatusAction(cCtx *cli.Context) error {
	home := defaultNeuraTradeHome()
	pidPath := filepath.Join(home, "pids", "agent.pid")
	logPath := filepath.Join(home, "logs", "agent.log")
	tailLines := cCtx.Int("tail")

	fmt.Println("🤖 NeuraTrade Agent Status")
	fmt.Println("============================")
	fmt.Println()

	pidAlive, pidStr, pidErr := readPIDFile(pidPath)
	switch {
	case pidErr == nil && pidAlive:
		fmt.Printf("Process: RUNNING (PID %s)\n", pidStr)
		fmt.Printf("PID file: %s\n", pidPath)
	case pidErr == nil && !pidAlive:
		fmt.Printf("Process: STALE PID (PID %s not alive)\n", pidStr)
		fmt.Printf("PID file: %s\n", pidPath)
	default:
		fmt.Println("Process: NOT MANAGED")
		fmt.Println("  (no agent.pid file — gateway does not yet supervise the agent service)")
		fmt.Println("  Run `neuratrade agent run` for setup instructions.")
	}

	fmt.Println()
	fmt.Printf("Log file: %s\n", logPath)
	if _, err := os.Stat(logPath); err == nil {
		lines, readErr := tailFile(logPath, tailLines)
		if readErr != nil {
			fmt.Printf("(failed to read log: %v)\n", readErr)
		} else if len(lines) == 0 {
			fmt.Println("(log file is empty)")
		} else {
			fmt.Printf("Last %d lines:\n", len(lines))
			for _, line := range lines {
				fmt.Printf("  %s\n", line)
			}
		}
	} else {
		fmt.Println("(log file does not exist yet)")
	}
	return nil
}

// readPIDFile reads a PID file and reports whether the process is
// still alive via signal 0. Returns (alive, pid-string, error). The
// error is non-nil only when the file cannot be read (not when the
// process is dead). The PID is returned as a string so the caller
// can echo it in the status output even when alive=false.
func readPIDFile(path string) (alive bool, pidStr string, err error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	pidStr = strings.TrimSpace(string(content))
	pid, convErr := strconv.Atoi(pidStr)
	if convErr != nil {
		return false, pidStr, fmt.Errorf("invalid pid in %s: %q", path, pidStr)
	}
	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		// On Unix, FindProcess always succeeds (the process object
		// exists regardless of liveness). On other platforms it may
		// fail for non-existent PIDs; treat that as not-alive.
		return false, pidStr, nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, pidStr, nil
	}
	return true, pidStr, nil
}

// tailFile returns the last n lines of the file in order. Streams
// the file line-by-line so memory usage stays O(n) — agent log files
// can grow to hundreds of MB over a long-running session and
// reading the whole file would blow up the CLI process. If n <= 0,
// returns an empty slice. If n >= total-line-count, returns all lines.
func tailFile(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long log lines (default bufio buffer is 64KB; agent log
	// lines can be larger when a full event payload is dumped).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Keep the most recent n lines in a slice that grows up to capacity
	// n; once full, shift the slice left on each new line to keep only
	// the trailing window. This is O(n) per line for the typical n=20
	// case, which is negligible compared to the I/O cost of the
	// scanner.
	ring := make([]string, 0, n)
	for scanner.Scan() {
		if len(ring) < n {
			ring = append(ring, scanner.Text())
		} else {
			ring = append(ring[1:], scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}

// agentCommand builds the urfave/cli/v2 Command tree for the
// `neuratrade agent` subcommand. The subcommand is registered in
// main.go's Commands slice alongside `gateway`, `prompt`, `backtest`.
//
// PR-6 skeleton surface:
//
//	agent run    — print setup instructions (no process spawn)
//	agent status — show PID file + last N log lines
//
// Future PRs can add `agent stop`, `agent inspect` (dump policy +
// playbook config), and `agent run --once <playbook>` (one-shot
// playbook execution).
func agentCommand() *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "Manage the autonomous agent runtime",
		Subcommands: []*cli.Command{
			{
				Name:   "run",
				Usage:  "Show how to launch the agent service (managed by the gateway)",
				Action: agentRunAction,
			},
			{
				Name:   "status",
				Usage:  "Show agent runtime status (PID + recent log lines)",
				Action: agentStatusAction,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "tail",
						Usage: "number of recent log lines to display",
						Value: 20,
					},
				},
			},
		},
	}
}

// Compile-time assertion that agentCommand returns a non-nil *cli.Command.
// This catches refactors that accidentally break the command tree by
// failing compilation if the function is removed or its return type changes.
var _ *cli.Command = agentCommand()
