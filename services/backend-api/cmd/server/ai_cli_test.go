package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max length",
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "string equal to max length",
			input:    "exactly",
			maxLen:   7,
			expected: "exactly",
		},
		{
			name:     "string longer than max length",
			input:    "this is a very long string",
			maxLen:   10,
			expected: "this is...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "max length of 3 returns full string with ellipsis",
			input:    "hello",
			maxLen:   3,
			expected: "...",
		},
		{
			name:     "max length of 4 truncates correctly",
			input:    "testing",
			maxLen:   4,
			expected: "t...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "maxLen 0 returns ellipsis only",
			input:    "test",
			maxLen:   0,
			expected: "...",
		},
		{
			name:     "maxLen 1 returns ellipsis only",
			input:    "test",
			maxLen:   1,
			expected: "...",
		},
		{
			name:     "maxLen 2 returns ellipsis only",
			input:    "test",
			maxLen:   2,
			expected: "...",
		},
		{
			name:     "unicode string truncates by bytes not runes",
			input:    "こんにちは",
			maxLen:   5,
			expected: "\xe3\x81...",
		},
		{
			name:     "unicode string truncates correctly at 10 bytes",
			input:    "こんにちは",
			maxLen:   10,
			expected: "こん\xe3...",
		},
		{
			name:     "ascii string truncates correctly",
			input:    "Hello",
			maxLen:   6,
			expected: "Hello",
		},
		{
			name:     "short string returns original (len <= maxLen)",
			input:    "Hi",
			maxLen:   3,
			expected: "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestProbeAIProvider_OpenAISuccess(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")

	handlerErrs := make(chan error, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			reportProbeHandlerError(handlerErrs, w, "unexpected path: %s", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			reportProbeHandlerError(handlerErrs, w, "unexpected authorization header: %q", got)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"created":1700000000,
			"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}
		}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		APIKey:   "test-key",
		BaseURL:  server.URL,
	}, []string{"--json"}, &out)
	if err != nil {
		t.Fatalf("probeAIProvider returned error: %v", err)
	}
	assertNoProbeHandlerError(t, handlerErrs)

	var result aiProviderProbeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("expected successful probe, got %#v", result)
	}
	if !result.ResponseMatched || result.ExpectedContent != "OK" {
		t.Fatalf("expected default response validation to pass, got %#v", result)
	}
	if result.Provider != "openai" || result.Model != "gpt-test" {
		t.Fatalf("unexpected provider/model: %#v", result)
	}
	if result.Usage.TotalTokens != 6 {
		t.Fatalf("expected total token usage to be reported, got %#v", result.Usage)
	}
	if strings.Contains(out.String(), "test-key") {
		t.Fatal("probe output leaked API key")
	}
}

func TestProbeAIProvider_ResponseMismatchFails(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"created":1700000000,
			"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"1. **Analyze the Request**"},"finish_reason":"length"}],
			"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}
		}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		APIKey:   "test-key",
		BaseURL:  server.URL,
	}, []string{"--json"}, &out)
	if err == nil {
		t.Fatal("expected non-matching provider response to fail")
	}

	var result aiProviderProbeResult
	if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", decodeErr, out.String())
	}
	if result.OK {
		t.Fatalf("expected failed probe, got %#v", result)
	}
	if result.ResponseMatched {
		t.Fatalf("expected response match to be false, got %#v", result)
	}
	if !strings.Contains(out.String(), `"response_matched": false`) {
		t.Fatalf("expected failed JSON to include response_matched=false, got %s", out.String())
	}
	if result.ExpectedContent != "OK" {
		t.Fatalf("expected default expected content to be reported, got %q", result.ExpectedContent)
	}
	if !strings.Contains(result.Error, "did not match expected content") {
		t.Fatalf("expected content mismatch error, got %q", result.Error)
	}
	if result.ContentPreview != "1. **Analyze the Request**" {
		t.Fatalf("expected content preview for diagnosis, got %q", result.ContentPreview)
	}
}

func TestProbeAIProvider_EmptyExpectedContentAllowsTransportOnlyProbe(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"created":1700000000,
			"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"unexpected but reachable"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
		}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		APIKey:   "test-key",
		BaseURL:  server.URL,
	}, []string{"--json", "--expect", ""}, &out)
	if err != nil {
		t.Fatalf("probeAIProvider returned error: %v\n%s", err, out.String())
	}

	var result aiProviderProbeResult
	if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", decodeErr, out.String())
	}
	if !result.OK || !result.ResponseMatched {
		t.Fatalf("expected transport-only probe success, got %#v", result)
	}
	if !strings.Contains(out.String(), `"response_matched": true`) {
		t.Fatalf("expected transport-only JSON to include response_matched=true, got %s", out.String())
	}
	if result.ExpectedContent != "" {
		t.Fatalf("expected empty expected content, got %q", result.ExpectedContent)
	}
}

func TestProbeAIProvider_MissingAPIKeyFailsWithoutSecretOutput(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")
	t.Setenv("NEURATRADE_AI_PROVIDER_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  "https://example.test/v1",
	}, []string{"--json"}, &out)
	if err == nil {
		t.Fatal("expected missing API key to fail")
	}

	var result aiProviderProbeResult
	if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", decodeErr, out.String())
	}
	if result.OK {
		t.Fatalf("expected failed probe, got %#v", result)
	}
	if !strings.Contains(result.Error, "no usable AI provider probe nodes configured") {
		t.Fatalf("expected no usable nodes error, got %q", result.Error)
	}
	if len(result.SkippedProviders) != 1 || !strings.Contains(result.SkippedProviders[0], "missing API key") {
		t.Fatalf("expected missing-key skipped provider, got %#v", result.SkippedProviders)
	}
}

func TestProbeAIProvider_FailoverUsesFundedFallback(t *testing.T) {
	handlerErrs := make(chan error, 4)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			reportProbeHandlerError(handlerErrs, w, "unexpected primary path: %s", r.URL.Path)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient balance/resource package","type":"insufficient_quota","code":"1113"}}`))
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			reportProbeHandlerError(handlerErrs, w, "unexpected fallback path: %s", r.URL.Path)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "fallback-key" {
			reportProbeHandlerError(handlerErrs, w, "unexpected fallback key header: %q", got)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"model":"claude-test",
			"content":[{"type":"text","text":"OK"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":4,"output_tokens":1}
		}`))
	}))
	defer fallback.Close()

	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "fallback-key")
	t.Setenv("ANTHROPIC_BASE_URL", fallback.URL)
	t.Setenv("ANTHROPIC_MODEL", "claude-test")

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		APIKey:   "primary-key",
		BaseURL:  primary.URL,
	}, []string{"--json", "--max-retries", "0", "--failover-max-hops", "1"}, &out)
	if err != nil {
		t.Fatalf("probeAIProvider returned error: %v\n%s", err, out.String())
	}
	assertNoProbeHandlerError(t, handlerErrs)

	var result aiProviderProbeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("expected fallback success, got %#v", result)
	}
	if result.Provider != "anthropic" || result.Model != "claude-test" {
		t.Fatalf("unexpected fallback provider/model: %#v", result)
	}
	if strings.Join(result.AttemptedProviders, ",") != "openai,anthropic" {
		t.Fatalf("unexpected attempted providers: %#v", result.AttemptedProviders)
	}
	if strings.Join(result.FailedProviders, ",") != "openai" {
		t.Fatalf("unexpected failed providers: %#v", result.FailedProviders)
	}
	if !result.FailoverAttempted || !result.FailoverSucceeded {
		t.Fatalf("expected failover to be reported, got %#v", result)
	}
	if strings.Contains(out.String(), "primary-key") || strings.Contains(out.String(), "fallback-key") {
		t.Fatal("probe output leaked API key")
	}
}

func TestProbeAIProvider_NoConfiguredProviderFails(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")

	var out bytes.Buffer
	err := probeAIProvider(context.Background(), config.AIConfig{}, []string{"--json"}, &out)
	if err == nil {
		t.Fatal("expected missing provider configuration to fail")
	}

	var result aiProviderProbeResult
	if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
		t.Fatalf("failed to decode probe result: %v\n%s", decodeErr, out.String())
	}
	if result.OK {
		t.Fatalf("expected failed probe, got %#v", result)
	}
	if !strings.Contains(result.Error, "ai provider is not configured") {
		t.Fatalf("expected provider configuration error, got %q", result.Error)
	}
}

func TestWriteAIProviderProbeResult_ReturnsTextWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	err := writeAIProviderProbeResult(failingWriter{err: writeErr}, false, aiProviderProbeResult{OK: true})
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error to be returned, got %v", err)
	}
}

func TestProbeAIProvider_ErrorPathReturnsWriteError(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")

	writeErr := errors.New("write failed")
	err := probeAIProvider(context.Background(), config.AIConfig{}, nil, failingWriter{err: writeErr})
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error to be joined, got %v", err)
	}
	if !strings.Contains(err.Error(), "ai provider is not configured") {
		t.Fatalf("expected original probe error to be preserved, got %v", err)
	}
}

func TestBuildAIProviderProbeNodes_BaseURLOverrideAppliesToPrimaryProvider(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")
	t.Setenv("NEURATRADE_AI_PROVIDER_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")

	nodes, _, err := buildAIProviderProbeNodes(config.AIConfig{
		Provider: "openai",
		Model:    "gpt-test",
		APIKey:   "test-key",
		BaseURL:  "https://configured.example/v1",
	}, aiProviderProbeOptions{BaseURL: "https://override.example/v1"})
	if err != nil {
		t.Fatalf("buildAIProviderProbeNodes returned error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %#v", nodes)
	}
	if nodes[0].BaseURL != "https://override.example/v1" {
		t.Fatalf("expected base URL override to apply to primary provider, got %q", nodes[0].BaseURL)
	}
}

func TestBuildAIProviderProbeNodes_ProviderEnvOverridesGenericPrimaryConfig(t *testing.T) {
	t.Setenv("NEURATRADE_AI_PROVIDER_CHAIN", "")
	t.Setenv("DEEPSEEK_API_KEY", "provider-key")
	t.Setenv("DEEPSEEK_BASE_URL", "https://deepseek.example/v1")
	t.Setenv("DEEPSEEK_MODEL", "deepseek-chat")

	nodes, _, err := buildAIProviderProbeNodes(config.AIConfig{
		Provider: "deepseek",
		Model:    "stale-generic-model",
		APIKey:   "stale-generic-key",
		BaseURL:  "https://stale.example/v1",
	}, aiProviderProbeOptions{})
	if err != nil {
		t.Fatalf("buildAIProviderProbeNodes returned error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %#v", nodes)
	}
	if nodes[0].APIKey != "provider-key" {
		t.Fatalf("expected provider-specific API key, got %q", nodes[0].APIKey)
	}
	if nodes[0].BaseURL != "https://deepseek.example/v1" {
		t.Fatalf("expected provider-specific base URL, got %q", nodes[0].BaseURL)
	}
	if nodes[0].Model != "deepseek-chat" {
		t.Fatalf("expected provider-specific model, got %q", nodes[0].Model)
	}
}

func TestAIProviderProbeOverallTimeoutCoversFailoverAttempts(t *testing.T) {
	nodes := []aiProviderProbeNode{{Provider: "openai"}, {Provider: "anthropic"}, {Provider: "zai"}}
	opts := aiProviderProbeOptions{Timeout: 2 * time.Second, FailoverMaxHops: 1}

	if got := aiProviderProbeOverallTimeout(nodes, opts); got != 4*time.Second {
		t.Fatalf("expected two attempt windows, got %s", got)
	}
	opts.FailoverMaxHops = 0
	if got := aiProviderProbeOverallTimeout(nodes, opts); got != 2*time.Second {
		t.Fatalf("expected single attempt window when failover disabled, got %s", got)
	}
}

func TestParseAIScalpingDecisionProbeOptionsDefaults(t *testing.T) {
	opts, err := parseAIScalpingDecisionProbeOptions(nil)
	if err != nil {
		t.Fatalf("parseAIScalpingDecisionProbeOptions returned error: %v", err)
	}
	if opts.Exchange != "bitget" {
		t.Fatalf("expected default exchange bitget, got %q", opts.Exchange)
	}
	if opts.Capital.String() != "48" {
		t.Fatalf("expected default capital 48, got %s", opts.Capital.String())
	}
	if !opts.RequireHealthy {
		t.Fatalf("expected healthy runtime requirement by default")
	}
	if !opts.RequireValid {
		t.Fatalf("expected valid contract requirement by default")
	}
	if opts.MinSignalQuality.String() != "1" {
		t.Fatalf("expected full signal quality coverage by default, got %s", opts.MinSignalQuality.String())
	}
	if !opts.RequireReasoningClean || opts.MaxReasoningDiagnostics != 0 {
		t.Fatalf("expected zero reasoning diagnostics by default, enabled=%t max=%d", opts.RequireReasoningClean, opts.MaxReasoningDiagnostics)
	}
	if opts.Cycles != 1 {
		t.Fatalf("expected one cycle by default, got %d", opts.Cycles)
	}
}

func TestParseAIScalpingDecisionProbeOptionsAllowsDiagnosticRelaxation(t *testing.T) {
	opts, err := parseAIScalpingDecisionProbeOptions([]string{
		"--provider", "DeepSeek",
		"--exchange", " bitget ",
		"--capital", "50.5",
		"--min-signal-quality", "0.5",
		"--max-hold-ratio", "0.75",
		"--cycles", "3",
		"--interval-ms", "250",
		"--min-actionable-cycles", "1",
		"--min-paper-trades", "2",
		"--min-paper-net-pnl", "0",
		"--min-paper-avg-net-pnl", "0.001",
		"--min-paper-profit-factor", "1.25",
		"--max-paper-drawdown", "0.25",
		"--max-paper-drawdown-pct", "0.01",
		"--max-reasoning-diagnostics", "2",
		"--require-live-trial-ready",
		"--allow-degraded",
		"--allow-invalid-contract",
	})
	if err != nil {
		t.Fatalf("parseAIScalpingDecisionProbeOptions returned error: %v", err)
	}
	if opts.Provider != "deepseek" {
		t.Fatalf("expected normalized provider, got %q", opts.Provider)
	}
	if opts.Exchange != "bitget" {
		t.Fatalf("expected trimmed exchange, got %q", opts.Exchange)
	}
	if opts.RequireHealthy {
		t.Fatalf("expected degraded runtime allowance")
	}
	if opts.RequireValid {
		t.Fatalf("expected invalid contract allowance")
	}
	if opts.Capital.String() != "50.5" {
		t.Fatalf("expected parsed capital, got %s", opts.Capital.String())
	}
	if opts.MinSignalQuality.String() != "0.5" {
		t.Fatalf("expected parsed signal quality, got %s", opts.MinSignalQuality.String())
	}
	if opts.Cycles != 3 {
		t.Fatalf("expected parsed cycles, got %d", opts.Cycles)
	}
	if opts.MinActionableCycles != 1 {
		t.Fatalf("expected parsed min actionable cycles, got %d", opts.MinActionableCycles)
	}
	if !opts.RequireMaxHoldRatio || opts.MaxHoldRatio.String() != "0.75" {
		t.Fatalf("expected parsed max hold ratio gate, enabled=%t value=%s", opts.RequireMaxHoldRatio, opts.MaxHoldRatio.String())
	}
	if opts.Interval != 250*time.Millisecond {
		t.Fatalf("expected parsed interval, got %s", opts.Interval)
	}
	if opts.MinPaperTrades != 2 {
		t.Fatalf("expected parsed min paper trades, got %d", opts.MinPaperTrades)
	}
	if !opts.RequirePaperNetPnL || opts.MinPaperNetPnL.String() != "0" {
		t.Fatalf("expected parsed min paper net pnl gate, enabled=%t value=%s", opts.RequirePaperNetPnL, opts.MinPaperNetPnL.String())
	}
	if !opts.RequirePaperAvgNetPnL || opts.MinPaperAvgNetPnL.String() != "0.001" {
		t.Fatalf("expected parsed min paper avg net pnl gate, enabled=%t value=%s", opts.RequirePaperAvgNetPnL, opts.MinPaperAvgNetPnL.String())
	}
	if !opts.RequirePaperProfitFactor || opts.MinPaperProfitFactor.String() != "1.25" {
		t.Fatalf("expected parsed paper profit factor gate, enabled=%t value=%s", opts.RequirePaperProfitFactor, opts.MinPaperProfitFactor.String())
	}
	if !opts.RequirePaperDrawdown || opts.MaxPaperDrawdown.String() != "0.25" {
		t.Fatalf("expected parsed paper drawdown gate, enabled=%t value=%s", opts.RequirePaperDrawdown, opts.MaxPaperDrawdown.String())
	}
	if !opts.RequirePaperDrawdownPct || opts.MaxPaperDrawdownPct.String() != "0.01" {
		t.Fatalf("expected parsed paper drawdown pct gate, enabled=%t value=%s", opts.RequirePaperDrawdownPct, opts.MaxPaperDrawdownPct.String())
	}
	if !opts.RequireReasoningClean || opts.MaxReasoningDiagnostics != 2 {
		t.Fatalf("expected parsed reasoning diagnostics gate, enabled=%t max=%d", opts.RequireReasoningClean, opts.MaxReasoningDiagnostics)
	}
	if !opts.RequireLiveTrialReady {
		t.Fatal("expected parsed live trial readiness gate")
	}
}

func TestParseAIScalpingDecisionProbeOptionsRejectsInvalidQualityGate(t *testing.T) {
	_, err := parseAIScalpingDecisionProbeOptions([]string{"--min-signal-quality", "1.1"})
	if err == nil {
		t.Fatal("expected invalid min-signal-quality error")
	}
	if !strings.Contains(err.Error(), "--min-signal-quality") {
		t.Fatalf("expected min-signal-quality error, got %v", err)
	}
}

func TestParseAIScalpingDecisionProbeOptionsRejectsInvalidPaperGates(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "negative paper trade count",
			args:    []string{"--min-paper-trades", "-1"},
			wantErr: "--min-paper-trades",
		},
		{
			name:    "negative actionable cycle count",
			args:    []string{"--min-actionable-cycles", "-1"},
			wantErr: "--min-actionable-cycles",
		},
		{
			name:    "actionable cycles above total cycles",
			args:    []string{"--cycles", "1", "--min-actionable-cycles", "2"},
			wantErr: "--min-actionable-cycles",
		},
		{
			name:    "invalid max hold ratio decimal",
			args:    []string{"--max-hold-ratio", "not-a-decimal"},
			wantErr: `--max-hold-ratio value "not-a-decimal"`,
		},
		{
			name:    "negative max hold ratio",
			args:    []string{"--max-hold-ratio", "-0.1"},
			wantErr: "--max-hold-ratio",
		},
		{
			name:    "max hold ratio above one",
			args:    []string{"--max-hold-ratio", "1.1"},
			wantErr: "--max-hold-ratio",
		},
		{
			name:    "invalid paper net pnl decimal",
			args:    []string{"--min-paper-net-pnl", "not-a-decimal"},
			wantErr: `--min-paper-net-pnl value "not-a-decimal"`,
		},
		{
			name:    "invalid paper average net pnl decimal",
			args:    []string{"--min-paper-avg-net-pnl", "not-a-decimal"},
			wantErr: `--min-paper-avg-net-pnl value "not-a-decimal"`,
		},
		{
			name:    "invalid paper profit factor decimal",
			args:    []string{"--min-paper-profit-factor", "not-a-decimal"},
			wantErr: `--min-paper-profit-factor value "not-a-decimal"`,
		},
		{
			name:    "negative paper profit factor",
			args:    []string{"--min-paper-profit-factor", "-1"},
			wantErr: `--min-paper-profit-factor value "-1"`,
		},
		{
			name:    "invalid paper drawdown decimal",
			args:    []string{"--max-paper-drawdown", "not-a-decimal"},
			wantErr: `--max-paper-drawdown value "not-a-decimal"`,
		},
		{
			name:    "negative paper drawdown",
			args:    []string{"--max-paper-drawdown", "-1"},
			wantErr: `--max-paper-drawdown value "-1"`,
		},
		{
			name:    "invalid paper drawdown pct decimal",
			args:    []string{"--max-paper-drawdown-pct", "not-a-decimal"},
			wantErr: `--max-paper-drawdown-pct value "not-a-decimal"`,
		},
		{
			name:    "negative paper drawdown pct",
			args:    []string{"--max-paper-drawdown-pct", "-1"},
			wantErr: `--max-paper-drawdown-pct value "-1"`,
		},
		{
			name:    "invalid reasoning diagnostic count",
			args:    []string{"--max-reasoning-diagnostics", "not-an-int"},
			wantErr: `--max-reasoning-diagnostics value "not-an-int"`,
		},
		{
			name:    "negative reasoning diagnostic count",
			args:    []string{"--max-reasoning-diagnostics", "-1"},
			wantErr: `--max-reasoning-diagnostics value "-1"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAIScalpingDecisionProbeOptions(tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestBuildAIScalpingDecisionProbeSummaryAggregatesCycles(t *testing.T) {
	summary := buildAIScalpingDecisionProbeSummary([]*services.ScalpingLLMDecisionProbeResult{
		{
			SignalCount:           8,
			SignalQualityCount:    8,
			ContractValid:         true,
			LLMDegraded:           false,
			Provider:              "deepseek",
			Decision:              &services.AITradingDecision{Action: "hold"},
			SignalQualityCoverage: mustDecimal("1"),
			ReasoningDiagnostics:  []string{"wide spread claim contradicted by spread threshold"},
		},
		{
			SignalCount:        8,
			SignalQualityCount: 6,
			ContractValid:      false,
			LLMDegraded:        true,
			Provider:           "deepseek",
			Decision:           &services.AITradingDecision{Action: "buy"},
			PaperTrade: &services.ScalpingLLMProbeTrade{
				Fees:    mustDecimal("0.001"),
				NetPnL:  mustDecimal("0.01"),
				Outcome: "win",
			},
			SignalQualityCoverage: mustDecimal("0.75"),
		},
		{
			SignalCount:        4,
			SignalQualityCount: 4,
			ContractValid:      true,
			LLMDegraded:        false,
			Provider:           "deepseek",
			Decision:           &services.AITradingDecision{Action: "sell"},
			PaperTrade: &services.ScalpingLLMProbeTrade{
				Fees:    mustDecimal("0.002"),
				NetPnL:  mustDecimal("-0.02"),
				Outcome: "loss",
			},
			SignalQualityCoverage: mustDecimal("1"),
		},
	}, 3, mustDecimal("50"))

	if summary.CompletedCycles != 3 {
		t.Fatalf("expected completed cycles, got %d", summary.CompletedCycles)
	}
	if summary.TotalSignals != 20 {
		t.Fatalf("expected total signals, got %d", summary.TotalSignals)
	}
	if summary.SignalQualityCoverage.String() != "0.9" {
		t.Fatalf("expected aggregate signal quality coverage, got %s", summary.SignalQualityCoverage.String())
	}
	if summary.ValidContractCycles != 2 {
		t.Fatalf("expected two valid contract cycles, got %d", summary.ValidContractCycles)
	}
	if summary.LLMDegradedCycles != 1 {
		t.Fatalf("expected one degraded cycle, got %d", summary.LLMDegradedCycles)
	}
	if summary.ReasoningDiagnosticCycles != 1 || summary.ReasoningDiagnosticCount != 1 {
		t.Fatalf("unexpected reasoning diagnostic summary: cycles=%d count=%d", summary.ReasoningDiagnosticCycles, summary.ReasoningDiagnosticCount)
	}
	if summary.ActionCounts["hold"] != 1 || summary.ActionCounts["buy"] != 1 || summary.ActionCounts["sell"] != 1 {
		t.Fatalf("unexpected action counts: %#v", summary.ActionCounts)
	}
	if summary.ActionableCycles != 2 || summary.HoldRatio.String() != "0.3333333333333333" {
		t.Fatalf("unexpected actionability summary: actionable=%d hold_ratio=%s", summary.ActionableCycles, summary.HoldRatio.String())
	}
	if summary.PaperTrades != 2 || summary.PaperWins != 1 || summary.PaperLosses != 1 {
		t.Fatalf("unexpected paper trade counts: trades=%d wins=%d losses=%d", summary.PaperTrades, summary.PaperWins, summary.PaperLosses)
	}
	if summary.PaperNetPnL.String() != "-0.01" || summary.PaperFees.String() != "0.003" || summary.PaperAvgNetPnL.String() != "-0.005" {
		t.Fatalf("unexpected paper pnl summary: net=%s fees=%s avg=%s", summary.PaperNetPnL.String(), summary.PaperFees.String(), summary.PaperAvgNetPnL.String())
	}
	if summary.PaperProfitFactor.String() != "0.5" || summary.PaperMaxDrawdown.String() != "0.02" || summary.PaperMaxDrawdownPct.String() != "0.0004" {
		t.Fatalf("unexpected paper risk summary: profit_factor=%s max_drawdown=%s max_drawdown_pct=%s", summary.PaperProfitFactor.String(), summary.PaperMaxDrawdown.String(), summary.PaperMaxDrawdownPct.String())
	}
	if summary.PaperProfitFactorUnbounded {
		t.Fatal("did not expect bounded win/loss window to be marked unbounded")
	}
	if summary.PaperLiveTrialReadiness.Ready {
		t.Fatalf("expected small lossy paper sample to fail live trial readiness: %+v", summary.PaperLiveTrialReadiness)
	}
	if summary.PaperLiveTrialReadiness.MinClosedTrades != services.DefaultScalpingLiveTrialMinClosedTrades {
		t.Fatalf("unexpected min closed trades: %d", summary.PaperLiveTrialReadiness.MinClosedTrades)
	}
	require.Contains(t, summary.PaperLiveTrialReadiness.Reasons, "paper_trades_below_live_trial_minimum")
	require.Contains(t, summary.PaperLiveTrialReadiness.Reasons, "paper_net_pnl_not_positive")
}

func TestBuildAIScalpingDecisionProbeSummaryMarksPaperLiveTrialReady(t *testing.T) {
	results := make([]*services.ScalpingLLMDecisionProbeResult, 0, services.DefaultScalpingLiveTrialMinClosedTrades)
	for i := 0; i < services.DefaultScalpingLiveTrialMinClosedTrades; i++ {
		outcome := "win"
		netPnL := mustDecimal("0.02")
		if i%5 == 0 {
			outcome = "loss"
			netPnL = mustDecimal("-0.01")
		}
		results = append(results, &services.ScalpingLLMDecisionProbeResult{
			SignalCount:        8,
			SignalQualityCount: 8,
			ContractValid:      true,
			Provider:           "deepseek",
			Decision:           &services.AITradingDecision{Action: "buy"},
			PaperTrade: &services.ScalpingLLMProbeTrade{
				Fees:    mustDecimal("0.001"),
				NetPnL:  netPnL,
				Outcome: outcome,
			},
			SignalQualityCoverage: mustDecimal("1"),
		})
	}

	summary := buildAIScalpingDecisionProbeSummary(results, len(results), mustDecimal("50"))

	require.True(t, summary.PaperLiveTrialReadiness.Ready)
	require.Empty(t, summary.PaperLiveTrialReadiness.Reasons)
	require.Equal(t, services.DefaultScalpingLiveTrialMinClosedTrades, summary.PaperLiveTrialReadiness.MinClosedTrades)
	require.Equal(t, 20, summary.PaperTrades)
	require.Equal(t, 16, summary.PaperWins)
	require.Equal(t, 4, summary.PaperLosses)
	require.True(t, summary.PaperNetPnL.GreaterThan(decimal.Zero))
	require.True(t, summary.PaperMaxDrawdownPct.GreaterThan(decimal.Zero))
}

func TestBuildAIScalpingDecisionProbeSummaryMarksNoLossProfitFactorUnbounded(t *testing.T) {
	summary := buildAIScalpingDecisionProbeSummary([]*services.ScalpingLLMDecisionProbeResult{
		{
			SignalCount:        8,
			SignalQualityCount: 8,
			ContractValid:      true,
			Provider:           "deepseek",
			Decision:           &services.AITradingDecision{Action: "buy"},
			PaperTrade: &services.ScalpingLLMProbeTrade{
				Fees:    mustDecimal("0.001"),
				NetPnL:  mustDecimal("0.01"),
				Outcome: "win",
			},
			SignalQualityCoverage: mustDecimal("1"),
		},
	}, 1, mustDecimal("50"))

	if !summary.PaperProfitFactorUnbounded {
		t.Fatal("expected no-loss paper profit factor to be marked unbounded")
	}
	if !summary.PaperProfitFactor.IsZero() {
		t.Fatalf("expected unbounded paper profit factor numeric field to stay neutral, got %s", summary.PaperProfitFactor.String())
	}
}

func TestValidateAIScalpingDecisionProbeSummaryEnforcesHealthyValidQualityGates(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                2,
		CompletedCycles:       2,
		ValidContractCycles:   1,
		LLMDegradedCycles:     1,
		SignalQualityCoverage: mustDecimal("0.5"),
	}
	err := validateAIScalpingDecisionProbeSummary(summary, aiScalpingDecisionProbeOptions{
		Cycles:           2,
		RequireHealthy:   true,
		RequireValid:     true,
		MinSignalQuality: mustDecimal("1"),
	})
	if err == nil {
		t.Fatal("expected summary gate error")
	}
	if !strings.Contains(err.Error(), "degraded_cycles") {
		t.Fatalf("expected degraded cycle error first, got %v", err)
	}
}

func TestValidateAIScalpingDecisionProbeSummaryEnforcesActionabilityGates(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                4,
		CompletedCycles:       4,
		ValidContractCycles:   4,
		SignalQualityCoverage: mustDecimal("1"),
		ActionableCycles:      1,
		HoldRatio:             mustDecimal("0.75"),
	}

	tests := []struct {
		name    string
		opts    aiScalpingDecisionProbeOptions
		wantErr string
	}{
		{
			name: "actionable cycles gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:              4,
				RequireHealthy:      true,
				RequireValid:        true,
				MinSignalQuality:    mustDecimal("1"),
				MinActionableCycles: 2,
			},
			wantErr: "actionable_cycles",
		},
		{
			name: "hold ratio gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:              4,
				RequireHealthy:      true,
				RequireValid:        true,
				MinSignalQuality:    mustDecimal("1"),
				RequireMaxHoldRatio: true,
				MaxHoldRatio:        mustDecimal("0.5"),
			},
			wantErr: "hold_ratio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAIScalpingDecisionProbeSummary(summary, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateAIScalpingDecisionProbeSummaryEnforcesPaperTradeGates(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                2,
		CompletedCycles:       2,
		ValidContractCycles:   2,
		SignalQualityCoverage: mustDecimal("1"),
		PaperTrades:           1,
		PaperNetPnL:           mustDecimal("-0.01"),
		PaperAvgNetPnL:        mustDecimal("-0.01"),
		PaperProfitFactor:     mustDecimal("0.5"),
		PaperMaxDrawdown:      mustDecimal("0.02"),
		PaperMaxDrawdownPct:   mustDecimal("0.0004"),
	}

	tests := []struct {
		name    string
		opts    aiScalpingDecisionProbeOptions
		wantErr string
	}{
		{
			name: "paper trade count gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:           2,
				RequireHealthy:   true,
				RequireValid:     true,
				MinSignalQuality: mustDecimal("1"),
				MinPaperTrades:   2,
			},
			wantErr: "paper_trades",
		},
		{
			name: "paper net pnl gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:             2,
				RequireHealthy:     true,
				RequireValid:       true,
				MinSignalQuality:   mustDecimal("1"),
				RequirePaperNetPnL: true,
				MinPaperNetPnL:     mustDecimal("0"),
			},
			wantErr: "paper_net_pnl",
		},
		{
			name: "paper average net pnl gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:                2,
				RequireHealthy:        true,
				RequireValid:          true,
				MinSignalQuality:      mustDecimal("1"),
				RequirePaperAvgNetPnL: true,
				MinPaperAvgNetPnL:     mustDecimal("0"),
			},
			wantErr: "paper_avg_net_pnl",
		},
		{
			name: "paper profit factor gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:                   2,
				RequireHealthy:           true,
				RequireValid:             true,
				MinSignalQuality:         mustDecimal("1"),
				RequirePaperProfitFactor: true,
				MinPaperProfitFactor:     mustDecimal("1"),
			},
			wantErr: "paper_profit_factor",
		},
		{
			name: "paper drawdown gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:               2,
				RequireHealthy:       true,
				RequireValid:         true,
				MinSignalQuality:     mustDecimal("1"),
				RequirePaperDrawdown: true,
				MaxPaperDrawdown:     mustDecimal("0.01"),
			},
			wantErr: "paper_max_drawdown",
		},
		{
			name: "paper drawdown pct gate",
			opts: aiScalpingDecisionProbeOptions{
				Cycles:                  2,
				RequireHealthy:          true,
				RequireValid:            true,
				MinSignalQuality:        mustDecimal("1"),
				RequirePaperDrawdownPct: true,
				MaxPaperDrawdownPct:     mustDecimal("0.0001"),
			},
			wantErr: "paper_max_drawdown_pct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAIScalpingDecisionProbeSummary(summary, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateAIScalpingDecisionProbeSummaryTreatsNoLossProfitFactorAsUnbounded(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                     1,
		CompletedCycles:            1,
		ValidContractCycles:        1,
		SignalQualityCoverage:      mustDecimal("1"),
		PaperTrades:                1,
		PaperWins:                  1,
		PaperProfitFactor:          decimal.Zero,
		PaperProfitFactorUnbounded: true,
	}

	err := validateAIScalpingDecisionProbeSummary(summary, aiScalpingDecisionProbeOptions{
		Cycles:                   1,
		RequireHealthy:           true,
		RequireValid:             true,
		MinSignalQuality:         mustDecimal("1"),
		RequirePaperProfitFactor: true,
		MinPaperProfitFactor:     mustDecimal("10000"),
	})
	require.NoError(t, err)
}

func TestValidateAIScalpingDecisionProbeSummaryEnforcesReasoningDiagnosticGate(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                   2,
		CompletedCycles:          2,
		ValidContractCycles:      2,
		SignalQualityCoverage:    mustDecimal("1"),
		ReasoningDiagnosticCount: 1,
	}

	err := validateAIScalpingDecisionProbeSummary(summary, aiScalpingDecisionProbeOptions{
		Cycles:                  2,
		RequireHealthy:          true,
		RequireValid:            true,
		MinSignalQuality:        mustDecimal("1"),
		RequireReasoningClean:   true,
		MaxReasoningDiagnostics: 0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reasoning_diagnostic_count")
}

func TestValidateAIScalpingDecisionProbeSummaryCanRequirePaperLiveTrialReadiness(t *testing.T) {
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                     2,
		CompletedCycles:            2,
		ValidContractCycles:        2,
		SignalQualityCoverage:      mustDecimal("1"),
		PaperTrades:                1,
		PaperWins:                  1,
		PaperNetPnL:                mustDecimal("0.01"),
		PaperAvgNetPnL:             mustDecimal("0.01"),
		PaperLiveTrialReadiness:    services.ScalpingLiveTrialReadiness{Ready: false, Reasons: []string{"paper_trades_below_live_trial_minimum"}, MinClosedTrades: services.DefaultScalpingLiveTrialMinClosedTrades},
		PaperProfitFactorUnbounded: true,
	}

	err := validateAIScalpingDecisionProbeSummary(summary, aiScalpingDecisionProbeOptions{
		Cycles:                2,
		RequireHealthy:        true,
		RequireValid:          true,
		MinSignalQuality:      mustDecimal("1"),
		RequireLiveTrialReady: true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "paper_live_trial_readiness.ready=false")
}

func mustDecimal(value string) decimal.Decimal {
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func reportProbeHandlerError(errs chan<- error, w http.ResponseWriter, format string, args ...any) {
	errs <- fmt.Errorf(format, args...)
	http.Error(w, "handler validation failed", http.StatusInternalServerError)
}

func assertNoProbeHandlerError(t *testing.T, errs <-chan error) {
	t.Helper()

	select {
	case err := <-errs:
		t.Fatalf("handler validation failed: %v", err)
	default:
	}
}
