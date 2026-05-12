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
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  neuratrade ai models --provider openai")
	fmt.Println("  neuratrade ai search gpt-4")
	fmt.Println("  neuratrade ai show gpt-4-turbo")
	fmt.Println("  neuratrade ai capabilities --tools --vision")
	fmt.Println("  neuratrade ai probe --provider zai --json")
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
		if node.APIKey != "" {
			break
		}
		node.APIKey = strings.TrimSpace(os.Getenv(envKey))
	}
	for _, envKey := range ai.ProviderBaseURLEnvVars(provider) {
		if node.BaseURL != "" {
			break
		}
		node.BaseURL = strings.TrimSpace(os.Getenv(envKey))
	}
	if node.BaseURL == "" {
		if baseURL, ok := ai.ProviderDefaultBaseURL(provider); ok {
			node.BaseURL = baseURL
		}
	}
	for _, envKey := range ai.ProviderModelEnvVars(provider) {
		if node.Model != "" {
			break
		}
		node.Model = strings.TrimSpace(os.Getenv(envKey))
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
