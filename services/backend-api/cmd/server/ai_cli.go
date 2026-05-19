package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func runAICLI() error {
	if len(os.Args) < 3 {
		printAIUsage()
		return fmt.Errorf("missing command")
	}

	command := os.Args[2]
	args := os.Args[3:]

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if command == "probe" {
		return probeAIProvider(ctx, cfg.AI, args, os.Stdout)
	}
	if command == "scalping-probe" {
		return probeAIScalpingDecision(ctx, cfg.AI, args, os.Stdout)
	}

	logrusLogger := zaplogrus.New()
	errorRecoveryManager := services.NewErrorRecoveryManager(logrusLogger)
	retryPolicies := services.DefaultRetryPolicies()
	for name, policy := range retryPolicies {
		errorRecoveryManager.RegisterRetryPolicy(name, policy)
	}

	var redisClient *redis.Client
	redisConn, err := database.NewRedisConnectionWithRetry(cfg.Redis, errorRecoveryManager)
	if err == nil && redisConn != nil {
		redisClient = redisConn.Client
		defer redisConn.Close()
	}

	registry := ai.NewRegistry(
		ai.WithRedis(redisClient),
		ai.WithLogger(zap.NewNop()),
	)

	switch command {
	case "models":
		return listModels(ctx, registry, args)
	case "providers":
		return listProviders(ctx, registry, args)
	case "search":
		return searchModels(ctx, registry, args)
	case "show":
		return showModel(ctx, registry, args)
	case "sync":
		return syncRegistry(ctx, registry, args)
	case "route":
		return routeModel(ctx, registry, args)
	case "capabilities":
		return listByCapabilities(ctx, registry, args)
	case "status":
		return showStatus(ctx, registry, args)
	default:
		printAIUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printAIUsage() {
	fmt.Println("NeuraTrade AI Model Registry CLI")
	fmt.Println()
	fmt.Println("Usage: neuratrade ai <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  models         List all available models")
	fmt.Println("  providers      List all providers")
	fmt.Println("  search <term>  Search models by name/description")
	fmt.Println("  show <id>      Show detailed model information")
	fmt.Println("  sync           Force sync registry from models.dev")
	fmt.Println("  route          Route to best model for task")
	fmt.Println("  capabilities   List models by capabilities")
	fmt.Println("  status         Show registry status")
	fmt.Println("  probe          Send a tiny live completion to configured provider(s)")
	fmt.Println("  scalping-probe Exercise the live scalping LLM decision contract without orders")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  neuratrade ai models --provider openai")
	fmt.Println("  neuratrade ai search gpt-4")
	fmt.Println("  neuratrade ai show gpt-4-turbo")
	fmt.Println("  neuratrade ai capabilities --tools --vision")
	fmt.Println("  neuratrade ai probe --provider zai --json")
	fmt.Println("  neuratrade ai scalping-probe --provider deepseek --json")
}

func listModels(ctx context.Context, registry *ai.Registry, args []string) error {
	models, err := registry.GetRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry: %w", err)
	}

	providerFilter := ""
	for i, arg := range args {
		if arg == "--provider" && i+1 < len(args) {
			providerFilter = args[i+1]
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tMODEL\tDISPLAY NAME\tTIER\tLATENCY\tTOOLS\tCOST/1M")

	for _, m := range models.Models {
		if providerFilter != "" && m.ProviderID != providerFilter {
			continue
		}
		if m.Status != "active" {
			continue
		}

		tools := "✗"
		if m.Capabilities.SupportsTools {
			tools = "✓"
		}

		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t$%s\n",
			m.ProviderID,
			m.ModelID,
			truncate(m.DisplayName, 30),
			m.Tier,
			m.LatencyClass,
			tools,
			cost.StringFixed(2),
		)
	}

	return w.Flush()
}

func listProviders(ctx context.Context, registry *ai.Registry, args []string) error {
	providers, err := registry.GetActiveProviders(ctx)
	if err != nil {
		return fmt.Errorf("failed to get providers: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tMODELS\tENV VARS")

	for _, p := range providers {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			p.ID,
			p.Name,
			len(p.Models),
			strings.Join(p.EnvVars, ", "),
		)
	}

	return w.Flush()
}

func searchModels(ctx context.Context, registry *ai.Registry, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing search term")
	}

	query := args[0]
	models, err := registry.GetRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry: %w", err)
	}

	query = strings.ToLower(query)
	var matches []ai.ModelInfo

	for _, m := range models.Models {
		if m.Status != "active" {
			continue
		}
		if strings.Contains(strings.ToLower(m.ModelID), query) ||
			strings.Contains(strings.ToLower(m.DisplayName), query) {
			matches = append(matches, m)
		}
	}

	if len(matches) == 0 {
		fmt.Println("No models found matching:", query)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tMODEL\tDISPLAY NAME")

	for _, m := range matches {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
			m.ProviderID,
			m.ModelID,
			m.DisplayName,
		)
	}

	return w.Flush()
}

func showModel(ctx context.Context, registry *ai.Registry, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing model ID")
	}

	modelID := args[0]
	model, err := registry.FindModel(ctx, modelID)
	if err != nil {
		return fmt.Errorf("model not found: %w", err)
	}

	fmt.Printf("Model: %s\n", model.DisplayName)
	fmt.Printf("ID: %s\n", model.ModelID)
	fmt.Printf("Provider: %s\n", model.ProviderID)
	fmt.Printf("Family: %s\n", model.Family)
	fmt.Printf("Tier: %s\n", model.Tier)
	fmt.Printf("Status: %s\n", model.Status)
	fmt.Printf("Latency Class: %s\n", model.LatencyClass)
	fmt.Println()

	fmt.Println("Capabilities:")
	fmt.Printf("  Tools: %v\n", model.Capabilities.SupportsTools)
	fmt.Printf("  Vision: %v\n", model.Capabilities.SupportsVision)
	fmt.Printf("  Reasoning: %v\n", model.Capabilities.SupportsReasoning)
	fmt.Printf("  Structured Output: %v\n", model.StructuredOutput)
	fmt.Printf("  Temperature: %v\n", model.Temperature)
	fmt.Println()

	fmt.Println("Costs (per 1M tokens):")
	fmt.Printf("  Input: $%s\n", model.Cost.InputCost.StringFixed(4))
	fmt.Printf("  Output: $%s\n", model.Cost.OutputCost.StringFixed(4))
	if model.Cost.ReasoningCost.GreaterThan(decimal.Zero) {
		fmt.Printf("  Reasoning: $%s\n", model.Cost.ReasoningCost.StringFixed(4))
	}
	fmt.Println()

	fmt.Println("Limits:")
	fmt.Printf("  Context: %d tokens\n", model.Limits.ContextLimit)
	fmt.Printf("  Input: %d tokens\n", model.Limits.InputLimit)
	fmt.Printf("  Output: %d tokens\n", model.Limits.OutputLimit)
	fmt.Println()

	if len(model.Aliases) > 0 {
		fmt.Printf("Aliases: %s\n", strings.Join(model.Aliases, ", "))
	}

	if model.Knowledge != "" {
		fmt.Printf("Knowledge Cutoff: %s\n", model.Knowledge)
	}

	return nil
}

func syncRegistry(ctx context.Context, registry *ai.Registry, args []string) error {
	force := false
	for _, arg := range args {
		if arg == "--force" {
			force = true
		}
	}

	fmt.Println("Syncing model registry from models.dev...")
	start := time.Now()

	var err error
	if force {
		_, err = registry.Refresh(ctx)
	} else {
		_, err = registry.FetchModels(ctx)
	}

	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	models, _ := registry.GetRegistry(ctx)
	duration := time.Since(start)

	fmt.Printf("Sync completed in %s\n", duration.Round(time.Millisecond))
	fmt.Printf("Models: %d\n", len(models.Models))
	fmt.Printf("Providers: %d\n", len(models.Providers))

	return nil
}

func routeModel(ctx context.Context, registry *ai.Registry, args []string) error {
	router := ai.NewRouter(registry)

	constraints := ai.RoutingConstraints{
		LatencyPreference: "balanced",
		AllowedProviders:  []string{},
		BlockedProviders:  []string{},
	}

	for i, arg := range args {
		switch arg {
		case "--tools":
			constraints.RequiredCaps.SupportsTools = true
		case "--vision":
			constraints.RequiredCaps.SupportsVision = true
		case "--reasoning":
			constraints.RequiredCaps.SupportsReasoning = true
		case "--fast":
			constraints.LatencyPreference = "fast"
		case "--accurate":
			constraints.LatencyPreference = "accurate"
		case "--provider":
			if i+1 < len(args) {
				constraints.AllowedProviders = []string{args[i+1]}
			}
		case "--max-cost":
			if i+1 < len(args) {
				cost, err := decimal.NewFromString(args[i+1])
				if err != nil {
					return fmt.Errorf("invalid max-cost value: %w", err)
				}
				constraints.MaxInputCost = cost
			}
		}
	}

	result, err := router.Route(ctx, constraints)
	if err != nil {
		return fmt.Errorf("routing failed: %w", err)
	}

	fmt.Printf("Selected Model: %s\n", result.Model.DisplayName)
	fmt.Printf("Provider: %s\n", result.Provider.Name)
	fmt.Printf("Score: %.2f\n", result.Score)
	fmt.Printf("Reason: %s\n", result.Reason)
	fmt.Println()

	if len(result.Alternatives) > 0 {
		fmt.Println("Alternatives:")
		for i, alt := range result.Alternatives {
			fmt.Printf("  %d. %s (%s)\n", i+1, alt.DisplayName, alt.ProviderID)
		}
	}

	return nil
}

func listByCapabilities(ctx context.Context, registry *ai.Registry, args []string) error {
	caps := ai.ModelCapability{}

	for _, arg := range args {
		switch arg {
		case "--tools":
			caps.SupportsTools = true
		case "--vision":
			caps.SupportsVision = true
		case "--reasoning":
			caps.SupportsReasoning = true
		}
	}

	models, err := registry.FindModelsByCapability(ctx, caps)
	if err != nil {
		return fmt.Errorf("failed to find models: %w", err)
	}

	if len(models) == 0 {
		fmt.Println("No models found with specified capabilities")
		return nil
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Cost.InputCost.LessThan(models[j].Cost.InputCost)
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tMODEL\tTIER\tCOST/1M")

	for _, m := range models {
		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t$%s\n",
			m.ProviderID,
			m.ModelID,
			m.Tier,
			cost.StringFixed(2),
		)
	}

	return w.Flush()
}

func showStatus(ctx context.Context, registry *ai.Registry, args []string) error {
	models, err := registry.GetRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to get registry: %w", err)
	}

	outputJSON := false
	for _, arg := range args {
		if arg == "--json" {
			outputJSON = true
		}
	}

	status := map[string]interface{}{
		"models_count":    len(models.Models),
		"providers_count": len(models.Providers),
		"last_fetched":    models.FetchedAt.Format(time.RFC3339),
		"source":          ai.ModelsDevAPIURL,
	}

	activeByProvider := make(map[string]int)
	toolsCapable := 0
	visionCapable := 0
	reasoningCapable := 0

	for _, m := range models.Models {
		if m.Status == "active" {
			activeByProvider[m.ProviderID]++
			if m.Capabilities.SupportsTools {
				toolsCapable++
			}
			if m.Capabilities.SupportsVision {
				visionCapable++
			}
			if m.Capabilities.SupportsReasoning {
				reasoningCapable++
			}
		}
	}

	status["active_by_provider"] = activeByProvider
	status["tools_capable"] = toolsCapable
	status["vision_capable"] = visionCapable
	status["reasoning_capable"] = reasoningCapable

	if outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	fmt.Println("AI Model Registry Status")
	fmt.Println()
	fmt.Printf("Total Models: %d\n", status["models_count"])
	fmt.Printf("Total Providers: %d\n", status["providers_count"])
	fmt.Printf("Last Sync: %s\n", status["last_fetched"])
	fmt.Println()
	fmt.Println("Active Models by Provider:")
	for p, count := range activeByProvider {
		fmt.Printf("  %s: %d\n", p, count)
	}
	fmt.Println()
	fmt.Printf("Tools Capable: %d\n", toolsCapable)
	fmt.Printf("Vision Capable: %d\n", visionCapable)
	fmt.Printf("Reasoning Capable: %d\n", reasoningCapable)

	return nil
}

type aiProviderProbeOptions struct {
	Provider        string
	Model           string
	BaseURL         string
	Prompt          string
	Expect          string
	OutputJSON      bool
	Timeout         time.Duration
	MaxRetries      int
	MaxTokens       int
	FailoverMaxHops int
}

type aiProviderProbeNode struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"-"`
}

type aiProviderProbeResult struct {
	OK                  bool             `json:"ok"`
	Provider            string           `json:"provider,omitempty"`
	Model               string           `json:"model,omitempty"`
	BaseURL             string           `json:"base_url,omitempty"`
	LatencyMs           int64            `json:"latency_ms,omitempty"`
	Usage               llm.UsageMetrics `json:"usage,omitempty"`
	FinishReason        string           `json:"finish_reason,omitempty"`
	ContentPreview      string           `json:"content_preview,omitempty"`
	ExpectedContent     string           `json:"expected_content,omitempty"`
	ResponseMatched     bool             `json:"response_matched"`
	ConfiguredProviders []string         `json:"configured_providers"`
	UsableProviders     []string         `json:"usable_providers"`
	SkippedProviders    []string         `json:"skipped_providers,omitempty"`
	AttemptedProviders  []string         `json:"attempted_providers,omitempty"`
	FailedProviders     []string         `json:"failed_providers,omitempty"`
	FailoverAttempted   bool             `json:"failover_attempted,omitempty"`
	FailoverSucceeded   bool             `json:"failover_succeeded,omitempty"`
	Error               string           `json:"error,omitempty"`
}

type aiScalpingDecisionProbeOptions struct {
	Provider                      string
	Model                         string
	BaseURL                       string
	OutputJSON                    bool
	Timeout                       time.Duration
	MaxRetries                    int
	FailoverMaxHops               int
	Exchange                      string
	Cycles                        int
	Interval                      time.Duration
	PaperHoldPeriod               time.Duration
	Capital                       decimal.Decimal
	RequireHealthy                bool
	RequireValid                  bool
	MinSignalQuality              decimal.Decimal
	MinActionableCycles           int
	MaxHoldRatio                  decimal.Decimal
	RequireMaxHoldRatio           bool
	MinPaperTrades                int
	MinPaperNetPnL                decimal.Decimal
	RequirePaperNetPnL            bool
	MinPaperAvgNetPnL             decimal.Decimal
	RequirePaperAvgNetPnL         bool
	MinPaperProfitFactor          decimal.Decimal
	RequirePaperProfitFactor      bool
	MaxPaperDrawdown              decimal.Decimal
	RequirePaperDrawdown          bool
	MaxPaperDrawdownPct           decimal.Decimal
	RequirePaperDrawdownPct       bool
	MaxReasoningDiagnostics       int
	RequireReasoningClean         bool
	RequireObservedLiveTrialReady bool
}

type aiScalpingDecisionProbeSummary struct {
	Cycles                             int                                        `json:"cycles"`
	CompletedCycles                    int                                        `json:"completed_cycles"`
	TotalSignals                       int                                        `json:"total_signals"`
	SignalQualityCount                 int                                        `json:"signal_quality_count"`
	SignalQualityCoverage              decimal.Decimal                            `json:"signal_quality_coverage"`
	ValidContractCycles                int                                        `json:"valid_contract_cycles"`
	LLMDegradedCycles                  int                                        `json:"llm_degraded_cycles"`
	ActionableCycles                   int                                        `json:"actionable_cycles"`
	HoldRatio                          decimal.Decimal                            `json:"hold_ratio"`
	PaperTrades                        int                                        `json:"paper_trades"`
	PaperObservedTrades                int                                        `json:"paper_observed_trades"`
	PaperWins                          int                                        `json:"paper_wins"`
	PaperLosses                        int                                        `json:"paper_losses"`
	PaperNetPnL                        decimal.Decimal                            `json:"paper_net_pnl"`
	PaperFees                          decimal.Decimal                            `json:"paper_fees"`
	PaperAvgNetPnL                     decimal.Decimal                            `json:"paper_avg_net_pnl"`
	PaperProfitFactor                  decimal.Decimal                            `json:"paper_profit_factor"`
	PaperProfitFactorUnbounded         bool                                       `json:"paper_profit_factor_unbounded"`
	PaperMaxDrawdown                   decimal.Decimal                            `json:"paper_max_drawdown"`
	PaperMaxDrawdownPct                decimal.Decimal                            `json:"paper_max_drawdown_pct"`
	ObservedPaperTrades                int                                        `json:"observed_paper_trades"`
	ObservedPaperOpenPositions         int                                        `json:"observed_paper_open_positions"`
	ObservedPaperWins                  int                                        `json:"observed_paper_wins"`
	ObservedPaperLosses                int                                        `json:"observed_paper_losses"`
	ObservedPaperNetPnL                decimal.Decimal                            `json:"observed_paper_net_pnl"`
	ObservedPaperFees                  decimal.Decimal                            `json:"observed_paper_fees"`
	ObservedPaperAvgNetPnL             decimal.Decimal                            `json:"observed_paper_avg_net_pnl"`
	ObservedPaperProfitFactor          decimal.Decimal                            `json:"observed_paper_profit_factor"`
	ObservedPaperProfitFactorUnbounded bool                                       `json:"observed_paper_profit_factor_unbounded"`
	ObservedPaperMaxDrawdown           decimal.Decimal                            `json:"observed_paper_max_drawdown"`
	ObservedPaperMaxDrawdownPct        decimal.Decimal                            `json:"observed_paper_max_drawdown_pct"`
	PaperLiveTrialReadiness            services.ScalpingLiveTrialReadiness        `json:"paper_live_trial_readiness"`
	ReasoningDiagnosticCycles          int                                        `json:"reasoning_diagnostic_cycles"`
	ReasoningDiagnosticCount           int                                        `json:"reasoning_diagnostic_count"`
	ActionCounts                       map[string]int                             `json:"action_counts"`
	ProviderCounts                     map[string]int                             `json:"provider_counts"`
	LastResult                         *services.ScalpingLLMDecisionProbeResult   `json:"last_result,omitempty"`
	Results                            []*services.ScalpingLLMDecisionProbeResult `json:"results,omitempty"`

	paperGrossWinningPnL     decimal.Decimal
	paperGrossLosingPnL      decimal.Decimal
	paperCumulativeNetPnL    decimal.Decimal
	paperPeakNetPnL          decimal.Decimal
	observedGrossWinningPnL  decimal.Decimal
	observedGrossLosingPnL   decimal.Decimal
	observedCumulativeNetPnL decimal.Decimal
	observedPeakNetPnL       decimal.Decimal
}

func probeAIProvider(ctx context.Context, cfg config.AIConfig, args []string, out io.Writer) error {
	opts, err := parseAIProviderProbeOptions(args)
	if err != nil {
		return err
	}

	nodes, result, err := buildAIProviderProbeNodes(cfg, opts)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return writeAIProviderProbeFailure(out, opts.OutputJSON, result, err)
	}

	client, closeFn := buildAIProviderProbeClient(nodes, opts)
	defer closeFn()

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "You are a NeuraTrade provider health probe. Reply with the exact token OK."},
			{Role: llm.RoleUser, Content: opts.Prompt},
		},
		MaxTokens: opts.MaxTokens,
		Metadata:  map[string]string{"purpose": "neuratrade_ai_provider_probe"},
	}
	if len(nodes) == 1 {
		req.Model = nodes[0].Model
	}
	if opts.Model != "" {
		req.Model = opts.Model
	}

	probeCtx, cancel := context.WithTimeout(ctx, aiProviderProbeOverallTimeout(nodes, opts))
	defer cancel()

	resp, err := client.Complete(probeCtx, req)
	if failover, ok := client.(*llm.FailoverClient); ok {
		stats := failover.Stats()
		result.AttemptedProviders = stats.LastAttempt.AttemptedProviders
		result.FailedProviders = stats.LastAttempt.FailedProviders
		result.FailoverAttempted = stats.LastAttempt.FailoverAttempted
		result.FailoverSucceeded = stats.LastAttempt.FailoverSucceeded
	}
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return writeAIProviderProbeFailure(out, opts.OutputJSON, result, fmt.Errorf("ai provider probe failed: %w", err))
	}
	if resp == nil {
		result.OK = false
		result.Error = "provider returned nil response"
		return writeAIProviderProbeFailure(out, opts.OutputJSON, result, fmt.Errorf("ai provider probe failed: provider returned nil response"))
	}

	result.Provider = string(resp.Provider)
	result.Model = strings.TrimSpace(resp.Model)
	result.LatencyMs = resp.LatencyMs
	result.Usage = resp.Usage
	result.FinishReason = strings.TrimSpace(resp.FinishReason)
	responseContent := strings.TrimSpace(resp.Message.Content)
	result.ContentPreview = truncate(responseContent, 120)
	result.ExpectedContent = opts.Expect
	result.ResponseMatched = aiProviderProbeResponseMatches(responseContent, opts.Expect)
	for _, node := range nodes {
		if node.Provider == result.Provider {
			result.BaseURL = node.BaseURL
			break
		}
	}
	if result.BaseURL == "" && len(nodes) > 0 {
		result.BaseURL = nodes[0].BaseURL
	}

	if opts.Expect != "" && !result.ResponseMatched {
		result.OK = false
		result.Error = fmt.Sprintf("provider response did not match expected content %q", opts.Expect)
		return writeAIProviderProbeFailure(out, opts.OutputJSON, result, fmt.Errorf("ai provider probe failed: %s", result.Error))
	}

	result.OK = true
	return writeAIProviderProbeResult(out, opts.OutputJSON, result)
}

func probeAIScalpingDecision(ctx context.Context, cfg config.AIConfig, args []string, out io.Writer) error {
	opts, err := parseAIScalpingDecisionProbeOptions(args)
	if err != nil {
		return err
	}
	providerOpts := aiProviderProbeOptions{
		Provider:        opts.Provider,
		Model:           opts.Model,
		BaseURL:         opts.BaseURL,
		Timeout:         opts.Timeout,
		MaxRetries:      opts.MaxRetries,
		MaxTokens:       1200,
		FailoverMaxHops: opts.FailoverMaxHops,
	}
	nodes, _, err := buildAIProviderProbeNodes(cfg, providerOpts)
	if err != nil {
		return err
	}
	client, closeFn := buildAIProviderProbeClient(nodes, providerOpts)
	defer closeFn()

	model := ""
	if len(nodes) > 0 {
		model = nodes[0].Model
	}
	portfolio := services.TradingPortfolio{
		USDTBalance:        opts.Capital.InexactFloat64(),
		USDTBalanceDecimal: opts.Capital,
		TotalValue:         opts.Capital.InexactFloat64(),
		TotalValueDecimal:  opts.Capital,
		StrategyPhase:      "probe",
		RecoveryEntryOK:    true,
	}
	probeCtx, cancel := context.WithTimeout(ctx, aiScalpingDecisionProbeOverallTimeout(nodes, providerOpts, opts))
	defer cancel()

	results, runErr := runAIScalpingDecisionProbeCycles(probeCtx, client, opts, model, portfolio)
	summary := buildAIScalpingDecisionProbeSummary(results, opts.Cycles, opts.Capital, opts.PaperHoldPeriod)
	if writeErr := writeAIScalpingDecisionProbeSummary(out, opts.OutputJSON, summary); writeErr != nil {
		if runErr != nil {
			return errors.Join(runErr, writeErr)
		}
		return writeErr
	}
	if runErr != nil {
		return runErr
	}
	return validateAIScalpingDecisionProbeSummary(summary, opts)
}

func runAIScalpingDecisionProbeCycles(
	ctx context.Context,
	client llm.Client,
	opts aiScalpingDecisionProbeOptions,
	model string,
	portfolio services.TradingPortfolio,
) ([]*services.ScalpingLLMDecisionProbeResult, error) {
	results := make([]*services.ScalpingLLMDecisionProbeResult, 0, opts.Cycles)
	signalHistory := make([]services.ScalpingLLMSignalSnapshot, 0, opts.Cycles*8)
	for cycle := 0; cycle < opts.Cycles; cycle++ {
		if cycle > 0 && opts.Interval > 0 {
			select {
			case <-ctx.Done():
				return results, fmt.Errorf("wait between scalping LLM probe cycles: %w", ctx.Err())
			case <-time.After(opts.Interval):
			}
		}
		result, err := services.RunPublicScalpingLLMDecisionProbe(ctx, client, services.ScalpingLLMDecisionProbeOptions{
			Exchange:      opts.Exchange,
			Model:         model,
			Portfolio:     portfolio,
			SignalHistory: signalHistory,
		})
		if result != nil {
			results = append(results, result)
			signalHistory = append(signalHistory, result.SignalSnapshots...)
		}
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

type observedScalpingProbePosition struct {
	result     *services.ScalpingLLMDecisionProbeResult
	symbol     string
	side       string
	notional   decimal.Decimal
	entryPrice decimal.Decimal
	size       decimal.Decimal
	stopLoss   decimal.Decimal
	takeProfit decimal.Decimal
	entryAt    time.Time
}

func applyObservedScalpingProbePaperExits(results []*services.ScalpingLLMDecisionProbeResult, holdPeriod time.Duration) int {
	if holdPeriod < 0 {
		holdPeriod = 0
	}
	if holdPeriod == 0 {
		holdPeriod = services.DefaultScalpingBacktestHoldPeriod
	}
	openPositions := make([]observedScalpingProbePosition, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		snapshots := scalpingProbeSnapshotMap(result.SignalSnapshots)
		stillOpen := openPositions[:0]
		for _, position := range openPositions {
			snapshot, ok := snapshots[position.symbol]
			if !ok || !observedScalpingProbePositionCanClose(position, snapshot, holdPeriod) {
				stillOpen = append(stillOpen, position)
				continue
			}
			position.result.ObservedPaperTrade = closeObservedScalpingProbePosition(position, snapshot)
		}
		openPositions = stillOpen

		position, ok := openObservedScalpingProbePosition(result)
		if ok {
			openPositions = append(openPositions, position)
		}
	}
	return len(openPositions)
}

func scalpingProbeSnapshotMap(snapshots []services.ScalpingLLMSignalSnapshot) map[string]services.ScalpingLLMSignalSnapshot {
	mapped := make(map[string]services.ScalpingLLMSignalSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		symbol := strings.ToUpper(strings.TrimSpace(snapshot.Symbol))
		if symbol == "" || !snapshot.Price.GreaterThan(decimal.Zero) {
			continue
		}
		mapped[symbol] = snapshot
	}
	return mapped
}

func openObservedScalpingProbePosition(result *services.ScalpingLLMDecisionProbeResult) (observedScalpingProbePosition, bool) {
	if result == nil || result.PaperTrade == nil || result.Decision == nil {
		return observedScalpingProbePosition{}, false
	}
	action := strings.ToLower(strings.TrimSpace(result.Decision.Action))
	if action != "buy" && action != "sell" {
		return observedScalpingProbePosition{}, false
	}
	entryPrice := result.PaperTrade.EntryPrice
	notional := result.PaperTrade.Notional
	if !entryPrice.GreaterThan(decimal.Zero) || !notional.GreaterThan(decimal.Zero) {
		return observedScalpingProbePosition{}, false
	}
	symbol := strings.ToUpper(strings.TrimSpace(result.PaperTrade.Symbol))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(result.Decision.Symbol))
	}
	if symbol == "" {
		return observedScalpingProbePosition{}, false
	}
	stopLoss := decimal.Zero
	if result.Decision.StopLoss != nil {
		stopLoss = *result.Decision.StopLoss
	}
	takeProfit := decimal.Zero
	if result.Decision.TakeProfit != nil {
		takeProfit = *result.Decision.TakeProfit
	}
	entryAt := result.ObservedAt
	if entryAt.IsZero() {
		entryAt = time.Now().UTC()
	}
	return observedScalpingProbePosition{
		result:     result,
		symbol:     symbol,
		side:       action,
		notional:   notional,
		entryPrice: entryPrice,
		size:       notional.Div(entryPrice),
		stopLoss:   stopLoss,
		takeProfit: takeProfit,
		entryAt:    entryAt,
	}, true
}

func observedScalpingProbePositionCanClose(position observedScalpingProbePosition, snapshot services.ScalpingLLMSignalSnapshot, holdPeriod time.Duration) bool {
	if !snapshot.Price.GreaterThan(decimal.Zero) || !snapshot.ObservedAt.After(position.entryAt) {
		return false
	}
	switch position.side {
	case "buy":
		if position.takeProfit.GreaterThan(decimal.Zero) && snapshot.Price.GreaterThanOrEqual(position.takeProfit) {
			return true
		}
		if position.stopLoss.GreaterThan(decimal.Zero) && snapshot.Price.LessThanOrEqual(position.stopLoss) {
			return true
		}
	case "sell":
		if position.takeProfit.GreaterThan(decimal.Zero) && snapshot.Price.LessThanOrEqual(position.takeProfit) {
			return true
		}
		if position.stopLoss.GreaterThan(decimal.Zero) && snapshot.Price.GreaterThanOrEqual(position.stopLoss) {
			return true
		}
	}
	return !snapshot.ObservedAt.Before(position.entryAt.Add(holdPeriod))
}

func closeObservedScalpingProbePosition(position observedScalpingProbePosition, snapshot services.ScalpingLLMSignalSnapshot) *services.ScalpingLLMProbeTrade {
	exitPrice := snapshot.Price
	exitReason := "mark_to_market"
	switch position.side {
	case "buy":
		if position.takeProfit.GreaterThan(decimal.Zero) && snapshot.Price.GreaterThanOrEqual(position.takeProfit) {
			exitPrice = position.takeProfit
			exitReason = "take_profit"
		} else if position.stopLoss.GreaterThan(decimal.Zero) && snapshot.Price.LessThanOrEqual(position.stopLoss) {
			exitPrice = position.stopLoss
			exitReason = "stop_loss"
		}
	case "sell":
		if position.takeProfit.GreaterThan(decimal.Zero) && snapshot.Price.LessThanOrEqual(position.takeProfit) {
			exitPrice = position.takeProfit
			exitReason = "take_profit"
		} else if position.stopLoss.GreaterThan(decimal.Zero) && snapshot.Price.GreaterThanOrEqual(position.stopLoss) {
			exitPrice = position.stopLoss
			exitReason = "stop_loss"
		}
	}
	slippage := decimal.NewFromFloat(services.DefaultScalpingBacktestSlippage)
	one := decimal.NewFromInt(1)
	if position.side == "buy" {
		exitPrice = exitPrice.Mul(one.Sub(slippage))
	} else {
		exitPrice = exitPrice.Mul(one.Add(slippage))
	}

	var grossPnL decimal.Decimal
	if position.side == "buy" {
		grossPnL = exitPrice.Sub(position.entryPrice).Mul(position.size)
	} else {
		grossPnL = position.entryPrice.Sub(exitPrice).Mul(position.size)
	}
	fees := position.notional.Mul(decimal.NewFromFloat(0.0006)).Mul(decimal.NewFromInt(2))
	netPnL := grossPnL.Sub(fees)
	pnlPct := decimal.Zero
	if !position.notional.IsZero() {
		pnlPct = netPnL.Div(position.notional).Mul(decimal.NewFromInt(100))
	}
	outcome := "breakeven"
	if netPnL.GreaterThan(decimal.Zero) {
		outcome = "win"
	} else if netPnL.LessThan(decimal.Zero) {
		outcome = "loss"
	}
	return &services.ScalpingLLMProbeTrade{
		Symbol:       position.symbol,
		Side:         position.side,
		Notional:     position.notional,
		EntryPrice:   position.entryPrice,
		ExitPrice:    exitPrice,
		GrossPnL:     grossPnL,
		Fees:         fees,
		NetPnL:       netPnL,
		PnLPct:       pnlPct,
		Outcome:      outcome,
		ExitReason:   exitReason,
		ExitObserved: true,
	}
}

func buildAIScalpingDecisionProbeSummary(
	results []*services.ScalpingLLMDecisionProbeResult,
	requestedCycles int,
	paperDrawdownBasis decimal.Decimal,
	paperHoldPeriod time.Duration,
) aiScalpingDecisionProbeSummary {
	observedOpenPositions := applyObservedScalpingProbePaperExits(results, paperHoldPeriod)
	summary := aiScalpingDecisionProbeSummary{
		Cycles:                     requestedCycles,
		CompletedCycles:            len(results),
		ObservedPaperOpenPositions: observedOpenPositions,
		ActionCounts:               map[string]int{},
		ProviderCounts:             map[string]int{},
		Results:                    results,
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		summary.LastResult = result
		summary.TotalSignals += result.SignalCount
		summary.SignalQualityCount += result.SignalQualityCount
		if result.ContractValid {
			summary.ValidContractCycles++
		}
		if result.LLMDegraded {
			summary.LLMDegradedCycles++
		}
		action := "unknown"
		if result.Decision != nil && strings.TrimSpace(result.Decision.Action) != "" {
			action = strings.ToLower(strings.TrimSpace(result.Decision.Action))
		}
		if action == "buy" || action == "sell" {
			summary.ActionableCycles++
		}
		if result.PaperTrade != nil {
			summary.PaperTrades++
			if result.PaperTrade.ExitObserved {
				summary.PaperObservedTrades++
				summary.addObservedPaperTrade(result.PaperTrade)
			}
			summary.PaperNetPnL = summary.PaperNetPnL.Add(result.PaperTrade.NetPnL)
			summary.PaperFees = summary.PaperFees.Add(result.PaperTrade.Fees)
			if result.PaperTrade.NetPnL.GreaterThan(decimal.Zero) {
				summary.paperGrossWinningPnL = summary.paperGrossWinningPnL.Add(result.PaperTrade.NetPnL)
			} else if result.PaperTrade.NetPnL.LessThan(decimal.Zero) {
				summary.paperGrossLosingPnL = summary.paperGrossLosingPnL.Add(result.PaperTrade.NetPnL.Abs())
			}
			summary.paperCumulativeNetPnL = summary.paperCumulativeNetPnL.Add(result.PaperTrade.NetPnL)
			if summary.paperCumulativeNetPnL.GreaterThan(summary.paperPeakNetPnL) {
				summary.paperPeakNetPnL = summary.paperCumulativeNetPnL
			}
			if drawdown := summary.paperPeakNetPnL.Sub(summary.paperCumulativeNetPnL); drawdown.GreaterThan(summary.PaperMaxDrawdown) {
				summary.PaperMaxDrawdown = drawdown
			}
			switch strings.ToLower(strings.TrimSpace(result.PaperTrade.Outcome)) {
			case "win":
				summary.PaperWins++
			case "loss":
				summary.PaperLosses++
			}
		}
		if result.ObservedPaperTrade != nil && !result.ObservedPaperTrade.ExitObserved {
			result.ObservedPaperTrade.ExitObserved = true
		}
		if result.ObservedPaperTrade != nil {
			summary.addObservedPaperTrade(result.ObservedPaperTrade)
		}
		summary.ActionCounts[action]++
		if provider := strings.TrimSpace(result.Provider); provider != "" {
			summary.ProviderCounts[provider]++
		}
		if len(result.ReasoningDiagnostics) > 0 {
			summary.ReasoningDiagnosticCycles++
			summary.ReasoningDiagnosticCount += len(result.ReasoningDiagnostics)
		}
	}
	if summary.TotalSignals > 0 {
		summary.SignalQualityCoverage = decimal.NewFromInt(int64(summary.SignalQualityCount)).Div(decimal.NewFromInt(int64(summary.TotalSignals)))
	}
	if summary.PaperTrades > 0 {
		summary.PaperAvgNetPnL = summary.PaperNetPnL.Div(decimal.NewFromInt(int64(summary.PaperTrades)))
	}
	if summary.ObservedPaperTrades > 0 {
		summary.ObservedPaperAvgNetPnL = summary.ObservedPaperNetPnL.Div(decimal.NewFromInt(int64(summary.ObservedPaperTrades)))
	}
	if summary.CompletedCycles > 0 {
		summary.HoldRatio = decimal.NewFromInt(int64(summary.ActionCounts["hold"])).Div(decimal.NewFromInt(int64(summary.CompletedCycles)))
	}
	if summary.paperGrossLosingPnL.GreaterThan(decimal.Zero) {
		summary.PaperProfitFactor = summary.paperGrossWinningPnL.Div(summary.paperGrossLosingPnL)
	} else if summary.paperGrossWinningPnL.GreaterThan(decimal.Zero) {
		summary.PaperProfitFactorUnbounded = true
	}
	if paperDrawdownBasis.GreaterThan(decimal.Zero) {
		summary.PaperMaxDrawdownPct = summary.PaperMaxDrawdown.Div(paperDrawdownBasis)
		summary.ObservedPaperMaxDrawdownPct = summary.ObservedPaperMaxDrawdown.Div(paperDrawdownBasis)
	}
	summary.PaperLiveTrialReadiness = buildAIScalpingProbeLiveTrialReadiness(summary)
	return summary
}

func (summary *aiScalpingDecisionProbeSummary) addObservedPaperTrade(trade *services.ScalpingLLMProbeTrade) {
	if summary == nil || trade == nil {
		return
	}
	summary.ObservedPaperTrades++
	summary.ObservedPaperNetPnL = summary.ObservedPaperNetPnL.Add(trade.NetPnL)
	summary.ObservedPaperFees = summary.ObservedPaperFees.Add(trade.Fees)
	if trade.NetPnL.GreaterThan(decimal.Zero) {
		summary.observedGrossWinningPnL = summary.observedGrossWinningPnL.Add(trade.NetPnL)
	} else if trade.NetPnL.LessThan(decimal.Zero) {
		summary.observedGrossLosingPnL = summary.observedGrossLosingPnL.Add(trade.NetPnL.Abs())
	}
	summary.observedCumulativeNetPnL = summary.observedCumulativeNetPnL.Add(trade.NetPnL)
	if summary.observedCumulativeNetPnL.GreaterThan(summary.observedPeakNetPnL) {
		summary.observedPeakNetPnL = summary.observedCumulativeNetPnL
	}
	if drawdown := summary.observedPeakNetPnL.Sub(summary.observedCumulativeNetPnL); drawdown.GreaterThan(summary.ObservedPaperMaxDrawdown) {
		summary.ObservedPaperMaxDrawdown = drawdown
	}
	switch strings.ToLower(strings.TrimSpace(trade.Outcome)) {
	case "win":
		summary.ObservedPaperWins++
	case "loss":
		summary.ObservedPaperLosses++
	}
	if summary.observedGrossLosingPnL.GreaterThan(decimal.Zero) {
		summary.ObservedPaperProfitFactor = summary.observedGrossWinningPnL.Div(summary.observedGrossLosingPnL)
	} else if summary.observedGrossWinningPnL.GreaterThan(decimal.Zero) {
		summary.ObservedPaperProfitFactorUnbounded = true
	}
}

func buildAIScalpingProbeLiveTrialReadiness(summary aiScalpingDecisionProbeSummary) services.ScalpingLiveTrialReadiness {
	readiness := services.ScalpingLiveTrialReadiness{
		MinClosedTrades: services.DefaultScalpingLiveTrialMinClosedTrades,
	}
	reasons := make([]string, 0, 10)
	if summary.ObservedPaperTrades < readiness.MinClosedTrades {
		reasons = appendUniqueReadinessReason(reasons, "paper_observed_trades_below_live_trial_minimum")
	}
	if summary.ObservedPaperOpenPositions > 0 {
		reasons = appendUniqueReadinessReason(reasons, "paper_positions_still_open")
	}
	if summary.PaperTrades > 0 && summary.ObservedPaperTrades < summary.PaperTrades {
		reasons = appendUniqueReadinessReason(reasons, "synthetic_paper_exits")
	}
	if summary.ObservedPaperWins == 0 {
		reasons = appendUniqueReadinessReason(reasons, "no_winning_paper_trades")
	}
	if summary.ObservedPaperLosses == 0 {
		reasons = appendUniqueReadinessReason(reasons, "no_losing_paper_trades")
	}
	if !summary.ObservedPaperNetPnL.GreaterThan(decimal.Zero) {
		reasons = appendUniqueReadinessReason(reasons, "paper_net_pnl_not_positive")
	}
	if !summary.ObservedPaperAvgNetPnL.GreaterThan(decimal.Zero) {
		reasons = appendUniqueReadinessReason(reasons, "paper_avg_net_pnl_not_positive")
	}
	if !summary.ObservedPaperMaxDrawdownPct.GreaterThan(decimal.Zero) {
		reasons = appendUniqueReadinessReason(reasons, "paper_drawdown_not_observed")
	}
	if summary.SignalQualityCoverage.LessThan(decimal.NewFromInt(1)) {
		reasons = appendUniqueReadinessReason(reasons, "signal_quality_incomplete")
	}
	if summary.LLMDegradedCycles > 0 {
		reasons = appendUniqueReadinessReason(reasons, "llm_degraded")
	}
	if summary.ValidContractCycles != summary.CompletedCycles {
		reasons = appendUniqueReadinessReason(reasons, "invalid_contract_cycles")
	}
	if summary.ReasoningDiagnosticCount > 0 {
		reasons = appendUniqueReadinessReason(reasons, "reasoning_diagnostics_present")
	}
	readiness.Ready = len(reasons) == 0
	readiness.Reasons = reasons
	return readiness
}

func appendUniqueReadinessReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func validateAIScalpingDecisionProbeSummary(summary aiScalpingDecisionProbeSummary, opts aiScalpingDecisionProbeOptions) error {
	if summary.CompletedCycles != opts.Cycles {
		return fmt.Errorf("scalping LLM decision probe completed_cycles=%d below cycles=%d", summary.CompletedCycles, opts.Cycles)
	}
	if opts.RequireHealthy && summary.LLMDegradedCycles > 0 {
		return fmt.Errorf("scalping LLM decision probe degraded_cycles=%d", summary.LLMDegradedCycles)
	}
	if opts.RequireValid && summary.ValidContractCycles != opts.Cycles {
		return fmt.Errorf("scalping LLM decision valid_contract_cycles=%d below cycles=%d", summary.ValidContractCycles, opts.Cycles)
	}
	if opts.MinSignalQuality.GreaterThan(decimal.Zero) && summary.SignalQualityCoverage.LessThan(opts.MinSignalQuality) {
		return fmt.Errorf("signal_quality_coverage=%s below minimum=%s", summary.SignalQualityCoverage.String(), opts.MinSignalQuality.String())
	}
	if opts.MinActionableCycles > 0 && summary.ActionableCycles < opts.MinActionableCycles {
		return fmt.Errorf("actionable_cycles=%d below minimum=%d", summary.ActionableCycles, opts.MinActionableCycles)
	}
	if opts.RequireMaxHoldRatio && summary.HoldRatio.GreaterThan(opts.MaxHoldRatio) {
		return fmt.Errorf("hold_ratio=%s above maximum=%s", summary.HoldRatio.String(), opts.MaxHoldRatio.String())
	}
	if opts.MinPaperTrades > 0 && summary.PaperTrades < opts.MinPaperTrades {
		return fmt.Errorf("paper_trades=%d below minimum=%d", summary.PaperTrades, opts.MinPaperTrades)
	}
	if opts.RequirePaperNetPnL && summary.PaperNetPnL.LessThan(opts.MinPaperNetPnL) {
		return fmt.Errorf("paper_net_pnl=%s below minimum=%s", summary.PaperNetPnL.String(), opts.MinPaperNetPnL.String())
	}
	if opts.RequirePaperAvgNetPnL && summary.PaperAvgNetPnL.LessThan(opts.MinPaperAvgNetPnL) {
		return fmt.Errorf("paper_avg_net_pnl=%s below minimum=%s", summary.PaperAvgNetPnL.String(), opts.MinPaperAvgNetPnL.String())
	}
	if opts.RequirePaperProfitFactor && !summary.PaperProfitFactorUnbounded && summary.PaperProfitFactor.LessThan(opts.MinPaperProfitFactor) {
		return fmt.Errorf("paper_profit_factor=%s below minimum=%s", summary.PaperProfitFactor.String(), opts.MinPaperProfitFactor.String())
	}
	if opts.RequirePaperDrawdown && summary.PaperMaxDrawdown.GreaterThan(opts.MaxPaperDrawdown) {
		return fmt.Errorf("paper_max_drawdown=%s above maximum=%s", summary.PaperMaxDrawdown.String(), opts.MaxPaperDrawdown.String())
	}
	if opts.RequirePaperDrawdownPct && summary.PaperMaxDrawdownPct.GreaterThan(opts.MaxPaperDrawdownPct) {
		return fmt.Errorf("paper_max_drawdown_pct=%s above maximum=%s", summary.PaperMaxDrawdownPct.String(), opts.MaxPaperDrawdownPct.String())
	}
	if opts.RequireReasoningClean && summary.ReasoningDiagnosticCount > opts.MaxReasoningDiagnostics {
		return fmt.Errorf("reasoning_diagnostic_count=%d above maximum=%d", summary.ReasoningDiagnosticCount, opts.MaxReasoningDiagnostics)
	}
	if opts.RequireObservedLiveTrialReady && !summary.PaperLiveTrialReadiness.Ready {
		return fmt.Errorf("paper_live_trial_readiness.ready=false reasons=%v", summary.PaperLiveTrialReadiness.Reasons)
	}
	return nil
}

func parseAIProviderProbeOptions(args []string) (aiProviderProbeOptions, error) {
	opts := aiProviderProbeOptions{
		Prompt:          "Reply with OK.",
		Expect:          "OK",
		Timeout:         30 * time.Second,
		MaxRetries:      0,
		MaxTokens:       8,
		FailoverMaxHops: 1,
	}

	fs := flag.NewFlagSet("ai probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeoutSeconds := fs.Int("timeout-seconds", int(opts.Timeout/time.Second), "completion timeout in seconds")
	fs.StringVar(&opts.Provider, "provider", "", "provider to probe; defaults to ai.provider and NEURATRADE_AI_PROVIDER_CHAIN")
	fs.StringVar(&opts.Model, "model", "", "model override for the probe")
	fs.StringVar(&opts.BaseURL, "base-url", "", "base URL override for the selected provider")
	fs.StringVar(&opts.Prompt, "prompt", opts.Prompt, "probe prompt")
	fs.StringVar(&opts.Expect, "expect", opts.Expect, "exact response content expected from the provider; empty disables content validation")
	fs.BoolVar(&opts.OutputJSON, "json", false, "write JSON output")
	fs.IntVar(&opts.MaxRetries, "max-retries", opts.MaxRetries, "HTTP retry count")
	fs.IntVar(&opts.MaxTokens, "max-tokens", opts.MaxTokens, "maximum output tokens")
	fs.IntVar(&opts.FailoverMaxHops, "failover-max-hops", opts.FailoverMaxHops, "number of fallback providers to try after the primary")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected ai probe arguments: %s", strings.Join(fs.Args(), " "))
	}
	if timeoutSeconds == nil || *timeoutSeconds <= 0 {
		return opts, fmt.Errorf("--timeout-seconds must be greater than zero")
	}
	if opts.MaxRetries < 0 {
		return opts, fmt.Errorf("--max-retries must be zero or greater")
	}
	if opts.MaxTokens <= 0 {
		return opts, fmt.Errorf("--max-tokens must be greater than zero")
	}
	if opts.FailoverMaxHops < 0 {
		return opts, fmt.Errorf("--failover-max-hops must be zero or greater")
	}
	opts.Timeout = time.Duration(*timeoutSeconds) * time.Second
	opts.Provider = ai.NormalizeProviderID(opts.Provider)
	opts.Model = strings.TrimSpace(opts.Model)
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.Prompt = strings.TrimSpace(opts.Prompt)
	opts.Expect = strings.TrimSpace(opts.Expect)
	if opts.Prompt == "" {
		opts.Prompt = "Reply with OK."
	}
	return opts, nil
}

func parseAIScalpingDecisionProbeOptions(args []string) (aiScalpingDecisionProbeOptions, error) {
	opts := aiScalpingDecisionProbeOptions{
		Timeout:                 60 * time.Second,
		FailoverMaxHops:         1,
		Exchange:                "bitget",
		Cycles:                  1,
		PaperHoldPeriod:         services.DefaultScalpingBacktestHoldPeriod,
		Capital:                 decimal.NewFromFloat(48),
		RequireHealthy:          true,
		RequireValid:            true,
		MinSignalQuality:        decimal.NewFromInt(1),
		RequireReasoningClean:   true,
		MaxReasoningDiagnostics: 0,
	}

	fs := flag.NewFlagSet("ai scalping-probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeoutSeconds := fs.Int("timeout-seconds", int(opts.Timeout/time.Second), "completion timeout in seconds")
	capital := fs.String("capital", opts.Capital.String(), "paper wallet basis in USDT for the prompt")
	minSignalQuality := fs.String("min-signal-quality", opts.MinSignalQuality.String(), "minimum signal quality coverage required")
	maxHoldRatio := fs.String("max-hold-ratio", "", "maximum hold cycles divided by completed cycles allowed; empty disables this gate")
	minPaperNetPnL := fs.String("min-paper-net-pnl", "", "minimum aggregate simulated paper net PnL required; empty disables this gate")
	minPaperAvgNetPnL := fs.String("min-paper-avg-net-pnl", "", "minimum average simulated paper net PnL per trade required; empty disables this gate")
	minPaperProfitFactor := fs.String("min-paper-profit-factor", "", "minimum simulated paper profit factor required; empty disables this gate")
	maxPaperDrawdown := fs.String("max-paper-drawdown", "", "maximum simulated paper drawdown allowed; empty disables this gate")
	maxPaperDrawdownPct := fs.String("max-paper-drawdown-pct", "", "maximum simulated paper drawdown divided by --capital allowed; empty disables this gate")
	maxReasoningDiagnostics := fs.String("max-reasoning-diagnostics", "0", "maximum reasoning diagnostics allowed; empty disables this gate")
	intervalMS := fs.Int("interval-ms", 0, "delay between scalping LLM probe cycles in milliseconds")
	paperHoldPeriodSeconds := fs.Int("paper-hold-period-seconds", int(opts.PaperHoldPeriod/time.Second), "minimum observed hold period before mark-to-market paper exit; SL/TP can close earlier")
	allowDegraded := fs.Bool("allow-degraded", false, "return success even when LLM runtime degrades to fallback")
	allowInvalidContract := fs.Bool("allow-invalid-contract", false, "return success even when the LLM decision contract is invalid")
	fs.BoolVar(&opts.RequireObservedLiveTrialReady, "require-observed-live-trial-ready", false, "require observed paper live-trial readiness before returning success")
	fs.StringVar(&opts.Provider, "provider", "", "provider to probe; defaults to ai.provider and NEURATRADE_AI_PROVIDER_CHAIN")
	fs.StringVar(&opts.Model, "model", "", "model override for the selected provider")
	fs.StringVar(&opts.BaseURL, "base-url", "", "base URL override for the selected provider")
	fs.StringVar(&opts.Exchange, "exchange", opts.Exchange, "public exchange to fetch market/order-book signals from")
	fs.IntVar(&opts.Cycles, "cycles", opts.Cycles, "number of no-order scalping LLM decision probe cycles")
	fs.IntVar(&opts.MinActionableCycles, "min-actionable-cycles", 0, "minimum buy/sell LLM decision cycles required")
	fs.IntVar(&opts.MinPaperTrades, "min-paper-trades", 0, "minimum simulated paper trades required from actionable LLM decisions")
	fs.BoolVar(&opts.OutputJSON, "json", false, "write JSON output")
	fs.IntVar(&opts.MaxRetries, "max-retries", 0, "HTTP retry count")
	fs.IntVar(&opts.FailoverMaxHops, "failover-max-hops", opts.FailoverMaxHops, "number of fallback providers to try after the primary")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected ai scalping-probe arguments: %s", strings.Join(fs.Args(), " "))
	}
	if timeoutSeconds == nil || *timeoutSeconds <= 0 {
		return opts, fmt.Errorf("--timeout-seconds must be greater than zero")
	}
	if opts.MaxRetries < 0 {
		return opts, fmt.Errorf("--max-retries must be zero or greater")
	}
	if opts.FailoverMaxHops < 0 {
		return opts, fmt.Errorf("--failover-max-hops must be zero or greater")
	}
	if opts.Cycles <= 0 {
		return opts, fmt.Errorf("--cycles must be greater than zero")
	}
	if opts.Cycles > services.MaxScalpingLivePaperSoakCycles {
		return opts, fmt.Errorf("--cycles must be %d or lower", services.MaxScalpingLivePaperSoakCycles)
	}
	if intervalMS == nil || *intervalMS < 0 {
		return opts, fmt.Errorf("--interval-ms must be zero or greater")
	}
	if paperHoldPeriodSeconds == nil || *paperHoldPeriodSeconds < 0 {
		return opts, fmt.Errorf("--paper-hold-period-seconds must be zero or greater")
	}
	if opts.MinPaperTrades < 0 {
		return opts, fmt.Errorf("--min-paper-trades must be zero or greater")
	}
	if opts.MinActionableCycles < 0 {
		return opts, fmt.Errorf("--min-actionable-cycles must be zero or greater")
	}
	if opts.MinActionableCycles > opts.Cycles {
		return opts, fmt.Errorf("--min-actionable-cycles must be less than or equal to --cycles")
	}
	parsedCapital, err := decimal.NewFromString(strings.TrimSpace(*capital))
	if err != nil {
		return opts, fmt.Errorf("parse --capital: %w", err)
	}
	if !parsedCapital.GreaterThan(decimal.Zero) {
		return opts, fmt.Errorf("--capital must be greater than zero")
	}
	parsedSignalQuality, err := decimal.NewFromString(strings.TrimSpace(*minSignalQuality))
	if err != nil {
		return opts, fmt.Errorf("parse --min-signal-quality: %w", err)
	}
	if parsedSignalQuality.IsNegative() || parsedSignalQuality.GreaterThan(decimal.NewFromInt(1)) {
		return opts, fmt.Errorf("--min-signal-quality must be between 0 and 1")
	}
	parsedMaxHoldRatio, requireMaxHoldRatio, err := parseOptionalDecimalFlag("max-hold-ratio", *maxHoldRatio)
	if err != nil {
		return opts, err
	}
	if requireMaxHoldRatio && (parsedMaxHoldRatio.IsNegative() || parsedMaxHoldRatio.GreaterThan(decimal.NewFromInt(1))) {
		return opts, fmt.Errorf("--max-hold-ratio must be between 0 and 1")
	}
	parsedPaperNetPnL, requirePaperNetPnL, err := parseOptionalDecimalFlag("min-paper-net-pnl", *minPaperNetPnL)
	if err != nil {
		return opts, err
	}
	parsedPaperAvgNetPnL, requirePaperAvgNetPnL, err := parseOptionalDecimalFlag("min-paper-avg-net-pnl", *minPaperAvgNetPnL)
	if err != nil {
		return opts, err
	}
	parsedPaperProfitFactor, requirePaperProfitFactor, err := parseOptionalNonNegativeDecimalFlag("min-paper-profit-factor", *minPaperProfitFactor)
	if err != nil {
		return opts, err
	}
	parsedPaperDrawdown, requirePaperDrawdown, err := parseOptionalNonNegativeDecimalFlag("max-paper-drawdown", *maxPaperDrawdown)
	if err != nil {
		return opts, err
	}
	parsedPaperDrawdownPct, requirePaperDrawdownPct, err := parseOptionalNonNegativeDecimalFlag("max-paper-drawdown-pct", *maxPaperDrawdownPct)
	if err != nil {
		return opts, err
	}
	parsedReasoningDiagnostics, requireReasoningClean, err := parseOptionalNonNegativeIntFlag("max-reasoning-diagnostics", *maxReasoningDiagnostics)
	if err != nil {
		return opts, err
	}
	opts.Timeout = time.Duration(*timeoutSeconds) * time.Second
	opts.Interval = time.Duration(*intervalMS) * time.Millisecond
	opts.PaperHoldPeriod = time.Duration(*paperHoldPeriodSeconds) * time.Second
	opts.Capital = parsedCapital
	opts.MinSignalQuality = parsedSignalQuality
	opts.MaxHoldRatio = parsedMaxHoldRatio
	opts.RequireMaxHoldRatio = requireMaxHoldRatio
	opts.MinPaperNetPnL = parsedPaperNetPnL
	opts.RequirePaperNetPnL = requirePaperNetPnL
	opts.MinPaperAvgNetPnL = parsedPaperAvgNetPnL
	opts.RequirePaperAvgNetPnL = requirePaperAvgNetPnL
	opts.MinPaperProfitFactor = parsedPaperProfitFactor
	opts.RequirePaperProfitFactor = requirePaperProfitFactor
	opts.MaxPaperDrawdown = parsedPaperDrawdown
	opts.RequirePaperDrawdown = requirePaperDrawdown
	opts.MaxPaperDrawdownPct = parsedPaperDrawdownPct
	opts.RequirePaperDrawdownPct = requirePaperDrawdownPct
	opts.MaxReasoningDiagnostics = parsedReasoningDiagnostics
	opts.RequireReasoningClean = requireReasoningClean
	opts.RequireHealthy = !*allowDegraded
	opts.RequireValid = !*allowInvalidContract
	opts.Provider = ai.NormalizeProviderID(opts.Provider)
	opts.Model = strings.TrimSpace(opts.Model)
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.Exchange = strings.TrimSpace(opts.Exchange)
	if opts.Exchange == "" {
		opts.Exchange = "bitget"
	}
	return opts, nil
}

func parseOptionalNonNegativeIntFlag(flagName string, rawValue string) (int, bool, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, false, fmt.Errorf("--%s value %q: %w", flagName, rawValue, err)
	}
	if value < 0 {
		return 0, false, fmt.Errorf("--%s value %q must be zero or greater", flagName, rawValue)
	}
	return value, true, nil
}

func parseOptionalDecimalFlag(flagName string, rawValue string) (decimal.Decimal, bool, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return decimal.Zero, false, nil
	}
	parsed, err := decimal.NewFromString(rawValue)
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("parse --%s value %q: %w", flagName, rawValue, err)
	}
	return parsed, true, nil
}

func parseOptionalNonNegativeDecimalFlag(flagName string, rawValue string) (decimal.Decimal, bool, error) {
	parsed, required, err := parseOptionalDecimalFlag(flagName, rawValue)
	if err != nil || !required {
		return parsed, required, err
	}
	if parsed.IsNegative() {
		return decimal.Zero, false, fmt.Errorf("--%s value %q must be zero or greater", flagName, rawValue)
	}
	return parsed, true, nil
}

func buildAIProviderProbeNodes(cfg config.AIConfig, opts aiProviderProbeOptions) ([]aiProviderProbeNode, aiProviderProbeResult, error) {
	configuredProviders, err := aiProbeProviderChain(cfg.Provider, opts.Provider)
	result := aiProviderProbeResult{ConfiguredProviders: configuredProviders}
	if err != nil {
		return nil, result, err
	}

	nodes := make([]aiProviderProbeNode, 0, len(configuredProviders))
	for i, provider := range configuredProviders {
		node := resolveAIProviderProbeNode(cfg, opts, provider)
		if i == 0 && opts.Provider == "" && opts.BaseURL != "" {
			node.BaseURL = opts.BaseURL
		}
		if strings.TrimSpace(node.APIKey) == "" && ai.ProviderRequiresAPIKey(provider) {
			result.SkippedProviders = append(result.SkippedProviders, provider+": missing API key")
			continue
		}
		if node.Model == "" {
			result.SkippedProviders = append(result.SkippedProviders, provider+": missing model")
			continue
		}
		if node.BaseURL == "" {
			result.SkippedProviders = append(result.SkippedProviders, provider+": missing base URL")
			continue
		}
		nodes = append(nodes, node)
		result.UsableProviders = append(result.UsableProviders, provider)
	}

	if len(nodes) == 0 {
		return nil, result, fmt.Errorf("no usable AI provider probe nodes configured")
	}
	return nodes, result, nil
}

func aiProbeProviderChain(primary string, override string) ([]string, error) {
	if override != "" {
		if err := validateAIProbeProvider(override); err != nil {
			return nil, err
		}
		return []string{override}, nil
	}

	primary = ai.NormalizeProviderID(primary)
	raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_PROVIDER_CHAIN"))
	parts := []string{}
	if raw != "" {
		parts = strings.Split(raw, ",")
	}
	if primary == "" && len(parts) == 0 {
		return nil, fmt.Errorf("ai provider is not configured; set ai.provider, --provider, or NEURATRADE_AI_PROVIDER_CHAIN")
	}

	seen := map[string]struct{}{}
	chain := make([]string, 0, len(parts)+1)
	if primary != "" {
		if err := validateAIProbeProvider(primary); err != nil {
			return nil, err
		}
		seen[primary] = struct{}{}
		chain = append(chain, primary)
	}
	for _, part := range parts {
		provider := ai.NormalizeProviderID(part)
		if provider == "" {
			continue
		}
		if err := validateAIProbeProvider(provider); err != nil {
			return nil, err
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		chain = append(chain, provider)
	}
	return chain, nil
}

func validateAIProbeProvider(provider string) error {
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	if _, ok := ai.ProviderDefaults(provider); ok {
		return nil
	}
	return fmt.Errorf("unsupported ai provider %q", provider)
}

func resolveAIProviderProbeNode(cfg config.AIConfig, opts aiProviderProbeOptions, provider string) aiProviderProbeNode {
	node := aiProviderProbeNode{Provider: provider}

	if provider == ai.NormalizeProviderID(cfg.Provider) {
		node.APIKey = strings.TrimSpace(cfg.APIKey)
		node.BaseURL = strings.TrimSpace(cfg.BaseURL)
		node.Model = strings.TrimSpace(cfg.Model)
	}
	if opts.Provider == provider {
		if opts.BaseURL != "" {
			node.BaseURL = opts.BaseURL
		}
	}

	for _, envKey := range ai.ProviderAPIKeyEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.APIKey = value
			break
		}
	}
	for _, envKey := range ai.ProviderBaseURLEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.BaseURL = value
			break
		}
	}
	if node.BaseURL == "" {
		if baseURL, ok := ai.ProviderDefaultBaseURL(provider); ok {
			node.BaseURL = baseURL
		}
	}
	for _, envKey := range ai.ProviderModelEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.Model = value
			break
		}
	}
	if node.Model == "" {
		if model, ok := ai.ProviderDefaultModel(provider); ok {
			node.Model = model
		}
	}
	if opts.Model != "" {
		node.Model = opts.Model
	}
	return node
}

func buildAIProviderProbeClient(nodes []aiProviderProbeNode, opts aiProviderProbeOptions) (llm.Client, func()) {
	if len(nodes) == 1 {
		client := newAIProviderProbeNodeClient(nodes[0], opts)
		return client, func() { _ = client.Close() }
	}

	failoverNodes := make([]llm.FailoverNode, 0, len(nodes))
	for _, node := range nodes {
		failoverNodes = append(failoverNodes, llm.FailoverNode{
			Client:       newAIProviderProbeNodeClient(node, opts),
			Provider:     llm.Provider(node.Provider),
			DefaultModel: node.Model,
		})
	}
	client := llm.NewFailoverClient(failoverNodes, opts.FailoverMaxHops)
	return client, func() { _ = client.Close() }
}

func aiProviderProbeOverallTimeout(nodes []aiProviderProbeNode, opts aiProviderProbeOptions) time.Duration {
	attempts := 1
	if len(nodes) > 1 && opts.FailoverMaxHops > 0 {
		attempts = len(nodes)
		if maxAttempts := opts.FailoverMaxHops + 1; maxAttempts < attempts {
			attempts = maxAttempts
		}
	}
	return time.Duration(attempts) * opts.Timeout
}

func aiScalpingDecisionProbeOverallTimeout(nodes []aiProviderProbeNode, providerOpts aiProviderProbeOptions, opts aiScalpingDecisionProbeOptions) time.Duration {
	perCycle := aiProviderProbeOverallTimeout(nodes, providerOpts)
	total := time.Duration(opts.Cycles) * perCycle
	if opts.Cycles > 1 {
		total += time.Duration(opts.Cycles-1) * opts.Interval
	}
	return total
}

func newAIProviderProbeNodeClient(node aiProviderProbeNode, opts aiProviderProbeOptions) llm.Client {
	cfg := llm.ClientConfig{
		Provider:    llm.Provider(node.Provider),
		APIKey:      node.APIKey,
		BaseURL:     node.BaseURL,
		HTTPTimeout: opts.Timeout,
		MaxRetries:  opts.MaxRetries,
	}
	if node.Provider == "mlx" {
		return llm.NewMLXClient(cfg)
	}
	if ai.ProviderUsesAnthropicFormat(node.Provider, node.BaseURL) {
		return llm.NewAnthropicClient(cfg)
	}
	return llm.NewOpenAIClient(cfg)
}

func writeAIProviderProbeFailure(out io.Writer, outputJSON bool, result aiProviderProbeResult, probeErr error) error {
	if writeErr := writeAIProviderProbeResult(out, outputJSON, result); writeErr != nil {
		return errors.Join(probeErr, fmt.Errorf("writing probe result: %w", writeErr))
	}
	return probeErr
}

func writeAIProviderProbeResult(out io.Writer, outputJSON bool, result aiProviderProbeResult) error {
	if outputJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	writeProbeOutput := func(format string, args ...any) error {
		if _, err := fmt.Fprintf(out, format, args...); err != nil {
			return fmt.Errorf("writing probe output: %w", err)
		}
		return nil
	}
	if result.OK {
		if err := writeProbeOutput("AI provider probe succeeded\n"); err != nil {
			return err
		}
		if err := writeProbeOutput("Provider: %s\n", result.Provider); err != nil {
			return err
		}
		if err := writeProbeOutput("Model: %s\n", result.Model); err != nil {
			return err
		}
		if err := writeProbeOutput("Base URL: %s\n", result.BaseURL); err != nil {
			return err
		}
		if err := writeProbeOutput("Latency: %d ms\n", result.LatencyMs); err != nil {
			return err
		}
		if err := writeProbeOutput("Usage: input=%d output=%d total=%d\n", result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.TotalTokens); err != nil {
			return err
		}
		if result.ExpectedContent != "" {
			if err := writeProbeOutput("Expected: %s\n", result.ExpectedContent); err != nil {
				return err
			}
			if err := writeProbeOutput("Response matched: %t\n", result.ResponseMatched); err != nil {
				return err
			}
		}
		if result.ContentPreview != "" {
			if err := writeProbeOutput("Content: %s\n", result.ContentPreview); err != nil {
				return err
			}
		}
		return nil
	}

	if err := writeProbeOutput("AI provider probe failed\n"); err != nil {
		return err
	}
	if result.Error != "" {
		if err := writeProbeOutput("Error: %s\n", result.Error); err != nil {
			return err
		}
	}
	if result.ContentPreview != "" {
		if err := writeProbeOutput("Content: %s\n", result.ContentPreview); err != nil {
			return err
		}
	}
	if len(result.SkippedProviders) > 0 {
		if err := writeProbeOutput("Skipped providers: %s\n", strings.Join(result.SkippedProviders, ", ")); err != nil {
			return err
		}
	}
	return nil
}

func writeAIScalpingDecisionProbeSummary(out io.Writer, outputJSON bool, summary aiScalpingDecisionProbeSummary) error {
	if outputJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	writeProbeOutput := func(format string, args ...any) error {
		if _, err := fmt.Fprintf(out, format, args...); err != nil {
			return fmt.Errorf("writing scalping probe output: %w", err)
		}
		return nil
	}
	if err := writeProbeOutput("AI scalping decision probe completed\n"); err != nil {
		return err
	}
	if err := writeProbeOutput("Cycles: %d/%d\n", summary.CompletedCycles, summary.Cycles); err != nil {
		return err
	}
	if err := writeProbeOutput("Signals: %d\n", summary.TotalSignals); err != nil {
		return err
	}
	if err := writeProbeOutput("Signal quality coverage: %s\n", summary.SignalQualityCoverage.String()); err != nil {
		return err
	}
	if err := writeProbeOutput("Valid contract cycles: %d\n", summary.ValidContractCycles); err != nil {
		return err
	}
	if err := writeProbeOutput("LLM degraded cycles: %d\n", summary.LLMDegradedCycles); err != nil {
		return err
	}
	if err := writeProbeOutput("Actionable cycles: %d hold_ratio=%s\n", summary.ActionableCycles, summary.HoldRatio.String()); err != nil {
		return err
	}
	if err := writeProbeOutput("Paper trades: %d observed=%d wins=%d losses=%d net_pnl=%s fees=%s avg_net_pnl=%s profit_factor=%s unbounded_profit_factor=%t max_drawdown=%s max_drawdown_pct=%s\n", summary.PaperTrades, summary.PaperObservedTrades, summary.PaperWins, summary.PaperLosses, summary.PaperNetPnL.String(), summary.PaperFees.String(), summary.PaperAvgNetPnL.String(), summary.PaperProfitFactor.String(), summary.PaperProfitFactorUnbounded, summary.PaperMaxDrawdown.String(), summary.PaperMaxDrawdownPct.String()); err != nil {
		return err
	}
	if err := writeProbeOutput("Observed paper trades: %d open=%d wins=%d losses=%d net_pnl=%s fees=%s avg_net_pnl=%s profit_factor=%s unbounded_profit_factor=%t max_drawdown=%s max_drawdown_pct=%s\n", summary.ObservedPaperTrades, summary.ObservedPaperOpenPositions, summary.ObservedPaperWins, summary.ObservedPaperLosses, summary.ObservedPaperNetPnL.String(), summary.ObservedPaperFees.String(), summary.ObservedPaperAvgNetPnL.String(), summary.ObservedPaperProfitFactor.String(), summary.ObservedPaperProfitFactorUnbounded, summary.ObservedPaperMaxDrawdown.String(), summary.ObservedPaperMaxDrawdownPct.String()); err != nil {
		return err
	}
	if err := writeProbeOutput("Paper live trial readiness: ready=%t reasons=%v min_closed_trades=%d\n", summary.PaperLiveTrialReadiness.Ready, summary.PaperLiveTrialReadiness.Reasons, summary.PaperLiveTrialReadiness.MinClosedTrades); err != nil {
		return err
	}
	if err := writeProbeOutput("Reasoning diagnostics: cycles=%d count=%d\n", summary.ReasoningDiagnosticCycles, summary.ReasoningDiagnosticCount); err != nil {
		return err
	}
	if err := writeProbeOutput("Actions: %v\n", summary.ActionCounts); err != nil {
		return err
	}
	if err := writeProbeOutput("Providers: %v\n", summary.ProviderCounts); err != nil {
		return err
	}
	if summary.LastResult != nil && summary.LastResult.Decision != nil {
		decision := summary.LastResult.Decision
		if err := writeProbeOutput("Last decision: %s %s confidence=%.4f reason_category=%s\n", decision.Action, decision.Symbol, decision.Confidence, decision.ReasonCategory); err != nil {
			return err
		}
	}
	return nil
}

func aiProviderProbeResponseMatches(content string, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(content), expected)
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		return "..."
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
