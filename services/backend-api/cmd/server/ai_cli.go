package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
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

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
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

	command := os.Args[2]
	args := os.Args[3:]

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
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  neuratrade ai models --provider openai")
	fmt.Println("  neuratrade ai search gpt-4")
	fmt.Println("  neuratrade ai show gpt-4-turbo")
	fmt.Println("  neuratrade ai capabilities --tools --vision")
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
	fmt.Fprintln(w, "PROVIDER\tMODEL\tDISPLAY NAME\tTIER\tLATENCY\tTOOLS\tCOST/1M")

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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t$%s\n",
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
	fmt.Fprintln(w, "ID\tNAME\tMODELS\tENV VARS")

	for _, p := range providers {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
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
	fmt.Fprintln(w, "PROVIDER\tMODEL\tDISPLAY NAME")

	for _, m := range matches {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
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
	fmt.Fprintln(w, "PROVIDER\tMODEL\tTIER\tCOST/1M")

	for _, m := range models {
		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		fmt.Fprintf(w, "%s\t%s\t%s\t$%s\n",
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
