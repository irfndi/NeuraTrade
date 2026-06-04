package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
)

// backtestRequest mirrors the API request shape for /api/v1/scalping/backtest.
// Only the fields the CLI exposes are included here; the API treats omitted
// fields as "use default".
type backtestRequest struct {
	StartTime      string   `json:"start_time"`
	EndTime        string   `json:"end_time"`
	Symbols        []string `json:"symbols,omitempty"`
	Exchange       string   `json:"exchange,omitempty"`
	InitialCapital string   `json:"initial_capital,omitempty"`
	Mode           string   `json:"mode,omitempty"`
}

// backtestResponse mirrors the API response. The CLI only reads the summary
// fields it prints; the full signal/trade streams are ignored to keep the
// command's stdout operator-friendly.
type backtestResponse struct {
	RunID       string                 `json:"run_id"`
	Status      string                 `json:"status"`
	Mode        string                 `json:"mode"`
	Summary     map[string]interface{} `json:"summary"`
	GateSummary []interface{}          `json:"gate_summary"`
}

// RunScalpingBacktest posts the backtest request and returns the parsed
// response. The endpoint is /api/v1/scalping/backtest; on success the API
// returns 200 with a JSON body matching backtestResponse.
func (c *APIClient) RunScalpingBacktest(req backtestRequest) (*backtestResponse, error) {
	respBody, err := c.makeRequest("POST", "/api/v1/scalping/backtest", req)
	if err != nil {
		return nil, err
	}
	var response backtestResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backtest response: %w", err)
	}
	return &response, nil
}

// backtestRunAction is the urfave/cli/v2 Action for `neuratrade backtest run`.
// It parses the date/symbol/mode flags, POSTs to the backtest endpoint via
// the shared APIClient, and prints a compact summary. Exits non-zero on
// validation or backend errors so the command composes in shell pipelines.
func backtestRunAction(cCtx *cli.Context) error {
	start := strings.TrimSpace(cCtx.String("start"))
	end := strings.TrimSpace(cCtx.String("end"))
	if start == "" || end == "" {
		return fmt.Errorf("--start and --end are required (RFC3339, e.g. 2025-01-01T00:00:00Z)")
	}
	if _, err := time.Parse(time.RFC3339, start); err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, end); err != nil {
		return fmt.Errorf("invalid --end: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(cCtx.String("mode")))
	switch mode {
	case "", "deterministic", "ai":
		// ok
	default:
		return fmt.Errorf("invalid --mode %q (expected 'deterministic' or 'ai')", mode)
	}

	var symbols []string
	if raw := strings.TrimSpace(cCtx.String("symbols")); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				symbols = append(symbols, s)
			}
		}
	}

	exchange := strings.TrimSpace(cCtx.String("exchange"))
	initialCapital := strings.TrimSpace(cCtx.String("initial-capital"))
	timeout := cCtx.Duration("timeout")

	req := backtestRequest{
		StartTime:      start,
		EndTime:        end,
		Symbols:        symbols,
		Exchange:       exchange,
		InitialCapital: initialCapital,
		Mode:           mode,
	}

	client := NewAPIClient(getBaseURL(), getAPIKey())
	if timeout > 0 {
		client.HTTPClient.Timeout = timeout
	}

	resp, err := client.RunScalpingBacktest(req)
	if err != nil {
		return fmt.Errorf("backtest request failed: %w", err)
	}

	fmt.Printf("RunID: %s\n", resp.RunID)
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Mode: %s\n", resp.Mode)
	if len(resp.Summary) == 0 {
		fmt.Println("Summary: (none returned)")
		return nil
	}
	fmt.Println("Summary:")
	printSummaryField(resp.Summary, "total_signals")
	printSummaryField(resp.Summary, "accepted_signals")
	printSummaryField(resp.Summary, "rejected_signals")
	printSummaryField(resp.Summary, "total_trades")
	printSummaryField(resp.Summary, "winning_trades")
	printSummaryField(resp.Summary, "losing_trades")
	printSummaryField(resp.Summary, "win_rate")
	printSummaryField(resp.Summary, "total_pnl")
	printSummaryField(resp.Summary, "total_pnl_pct")
	printSummaryField(resp.Summary, "max_drawdown_pct")
	return nil
}

// printSummaryField prints a single key from the summary map. Nil values are
// skipped so operators don't see noisy "key: <nil>" lines for fields the
// backend didn't populate.
func printSummaryField(summary map[string]interface{}, key string) {
	if v, ok := summary[key]; ok && v != nil {
		fmt.Printf("  %s: %v\n", key, v)
	}
}

// backtestCommand builds the urfave/cli/v2 Command tree for the
// `neuratrade backtest` subcommand. The subcommand is registered in main.go's
// Commands slice alongside `gateway`, `prompt`, etc.
func backtestCommand() *cli.Command {
	return &cli.Command{
		Name:  "backtest",
		Usage: "Run a scalping backtest against the backend",
		Subcommands: []*cli.Command{
			{
				Name:   "run",
				Usage:  "Run a scalping backtest with the given date range and mode",
				Action: backtestRunAction,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "start",
						Usage:    "backtest start time (RFC3339, e.g. 2025-01-01T00:00:00Z)",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "end",
						Usage:    "backtest end time (RFC3339, e.g. 2025-12-31T00:00:00Z)",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "mode",
						Usage: "decision pipeline: 'deterministic' (default) or 'ai'",
						Value: "deterministic",
					},
					&cli.StringFlag{
						Name:  "symbols",
						Usage: "comma-separated symbol list (default: backend scalping universe)",
					},
					&cli.StringFlag{
						Name:  "exchange",
						Usage: "exchange name (default: backend default)",
					},
					&cli.StringFlag{
						Name:  "initial-capital",
						Usage: "initial capital as a decimal string (default: backend default)",
					},
					&cli.DurationFlag{
						Name:  "timeout",
						Usage: "HTTP request timeout (default: 30s)",
						Value: defaultTimeout,
					},
				},
			},
		},
	}
}
