package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

const (
	BackendURL = "http://localhost:8080"
	APIPath    = "/api/v1"
	agentLogo  = "🤖"
)

type AgentOptions struct {
	ModelID    string
	SessionKey string
	Debug      bool
}

func agentCmd(args []string) {
	opts := parseAgentArgs(args)

	fmt.Printf("%s NeuraTrade Agent Mode\n", logo)
	fmt.Println("  Type your messages or commands. Press Ctrl+C to exit.")
	fmt.Println("Commands:")
	fmt.Println("  :help        - Show available commands")
	fmt.Println("  :status      - Check system status")
	fmt.Println("  :models      - List available AI models")
	fmt.Println("  :budget      - Check budget status")
	fmt.Println("  :health      - Check service health")
	fmt.Println("  :quit/:exit  - Exit agent mode")
	fmt.Println("")

	reader := bufio.NewReader(os.Stdin)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("Exiting agent mode...")
		cancel()
		os.Exit(0)
	}()

	for {
		fmt.Print("You: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, ":") {
			handleAgentCommand(input, opts)
			continue
		}

		response := sendMessage(ctx, input, opts)
		fmt.Printf("%s %s\n\n", agentLogo, response)
	}
}

func parseAgentArgs(args []string) AgentOptions {
	opts := AgentOptions{
		SessionKey: "cli:agent",
		Debug:      false,
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			opts.Debug = true
		case "--model", "-m":
			if i+1 < len(args) {
				opts.ModelID = args[i+1]
				i++
			}
		case "--session", "-s":
			if i+1 < len(args) {
				opts.SessionKey = args[i+1]
				i++
			}
		}
	}

	return opts
}

func handleAgentCommand(input string, opts AgentOptions) {
	cmd := strings.ToLower(strings.TrimPrefix(input, ":"))
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "help":
		printAgentHelp()
	case "quit", "exit":
		fmt.Println("Exiting agent mode...")
		os.Exit(0)
	case "status":
		checkSystemStatus()
	case "models", "aimodels":
		listAIModels()
	case "select":
		if len(parts) < 2 {
			fmt.Println("Usage: :select <model_id>")
			return
		}
		selectAIModel(parts[1], opts)
	case "budget":
		checkBudget()
	case "health":
		checkHealth()
	case "opportunities":
		findArbitrageOpportunities(parts[1:])
	case "wallets", "wallet":
		listWallets()
	case "positions":
		listPositions()
	case "autonomous":
		handleAutonomous(parts[1:])
	case "doctor":
		runDoctor()
	case "clear":
		fmt.Print("\033[2J\033[H")
		fmt.Printf("%s NeuraTrade Agent Mode\n", agentLogo)
	default:
		fmt.Printf("Unknown command: %s\n", parts[0])
		printAgentHelp()
	}
}

func printAgentHelp() {
	fmt.Println("Available Commands:")
	fmt.Println("  :help              - Show this help message")
	fmt.Println("  :status            - Check system status")
	fmt.Println("  :models            - List available AI models")
	fmt.Println("  :select <model>   - Select AI model")
	fmt.Println("  :budget            - Check budget status")
	fmt.Println("  :health            - Check service health")
	fmt.Println("  :opportunities     - Find arbitrage opportunities")
	fmt.Println("  :wallets           - List connected wallets")
	fmt.Println("  :positions         - List open positions")
	fmt.Println("  :autonomous start  - Start autonomous mode")
	fmt.Println("  :autonomous pause  - Pause autonomous mode")
	fmt.Println("  :doctor            - Run system diagnostics")
	fmt.Println("  :clear             - Clear screen")
	fmt.Println("  :quit/:exit        - Exit agent mode")
	fmt.Println("")
	fmt.Println("Natural Language:")
	fmt.Println("  You can also type messages naturally to interact with the AI agent.")
}

func sendMessage(ctx context.Context, message string, opts AgentOptions) string {
	type AIRequest struct {
		Message    string `json:"message"`
		SessionKey string `json:"session_key"`
		ModelID    string `json:"model_id,omitempty"`
	}

	reqBody := AIRequest{
		Message:    message,
		SessionKey: opts.SessionKey,
	}

	if opts.ModelID != "" {
		reqBody.ModelID = opts.ModelID
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Sprintf("Error encoding request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", BackendURL+"/api/ai/chat", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Sprintf("Error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error sending message: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error reading response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Error: HTTP %d - %s", resp.StatusCode, string(body))
	}

	type AIResponse struct {
		Response string `json:"response"`
		Error    string `json:"error,omitempty"`
	}

	var result AIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Sprintf("Error parsing response: %v", err)
	}

	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	return result.Response
}

func checkSystemStatus() {
	resp, err := http.Get(BackendURL + APIPath + "/dashboard")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("%s\n", data)
}

func listAIModels() {
	resp, err := http.Get(BackendURL + APIPath + "/ai/models")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}

	models, ok := result["models"].([]interface{})
	if !ok {
		fmt.Println("No models available")
		return
	}

	fmt.Println("🤖 Available AI Models:")
	providerGroups := make(map[string][]string)
	for _, m := range models {
		model, _ := m.(map[string]interface{})
		provider := model["provider"].(string)
		modelID := model["model_id"].(string)
		providerGroups[provider] = append(providerGroups[provider], modelID)
	}

	for provider, models := range providerGroups {
		fmt.Printf("%s:\n", strings.ToUpper(provider))
		for _, m := range models {
			fmt.Printf("  • %s\n", m)
		}
	}
}

func selectAIModel(modelID string, opts AgentOptions) {
	type SelectRequest struct {
		ModelID    string `json:"model_id"`
		SessionKey string `json:"session_key"`
	}

	reqBody := SelectRequest{
		ModelID:    modelID,
		SessionKey: opts.SessionKey,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(BackendURL+"/api/ai/select", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["success"] == true {
		fmt.Printf("✅ Model selected: %s\n", modelID)
	} else {
		fmt.Printf("❌ Failed to select model: %v\n", result["error"])
	}
}

func checkBudget() {
	resp, err := http.Get(BackendURL + APIPath + "/budget/status")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}

	daily, _ := result["daily"].(map[string]interface{})
	monthly, _ := result["monthly"].(map[string]interface{})

	fmt.Println("💰 Budget Status:")
	if daily != nil {
		fmt.Printf("Daily: $%.2f / $%.2f\n", daily["spent"], daily["limit"])
	}
	if monthly != nil {
		fmt.Printf("Monthly: $%.2f / $%.2f\n", monthly["spent"], monthly["limit"])
	}
}

func checkHealth() {
	resp, err := http.Get(BackendURL + "/health")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	status, _ := result["status"].(string)
	fmt.Printf("System Status: %s\n", status)

	checks, _ := result["checks"].(map[string]interface{})
	for name, check := range checks {
		c, _ := check.(map[string]interface{})
		status, _ := c["status"].(string)
		fmt.Printf("  %s: %s\n", name, status)
	}
}

func findArbitrageOpportunities(args []string) {
	url := BackendURL + APIPath + "/arbitrage/opportunities?min_profit=0.5&limit=10"
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	opportunities, ok := result["opportunities"].([]interface{})
	if !ok || len(opportunities) == 0 {
		fmt.Println("No arbitrage opportunities found")
		return
	}

	fmt.Printf("📊 Found %d opportunities:\n\n", len(opportunities))
	for i, opp := range opportunities {
		o, _ := opp.(map[string]interface{})
		fmt.Printf("%d. %s\n", i+1, o["symbol"])
		fmt.Printf("   Profit: %.2f%%\n", o["profit_percent"])
		fmt.Printf("   From: %s → %s\n", o["buy_exchange"], o["sell_exchange"])
		fmt.Printf("   Price: $%.2f → $%.2f\n\n", o["buy_price"], o["sell_price"])
	}
}

func listWallets() {
	resp, err := http.Get(BackendURL + APIPath + "/wallets")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	wallets, ok := result["wallets"].([]interface{})
	if !ok || len(wallets) == 0 {
		fmt.Println("No wallets connected")
		return
	}

	fmt.Println("💳 Connected Wallets:")
	for _, w := range wallets {
		wallet, _ := w.(map[string]interface{})
		fmt.Printf("• %s (%s)\n", wallet["name"], wallet["exchange"])
		fmt.Printf("  Balance: $%.2f\n\n", wallet["balance"])
	}
}

func listPositions() {
	resp, err := http.Get(BackendURL + APIPath + "/trading/positions")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	positions, ok := result["positions"].([]interface{})
	if !ok || len(positions) == 0 {
		fmt.Println("No open positions")
		return
	}

	fmt.Printf("📈 Open Positions (%d):\n\n", len(positions))
	for _, p := range positions {
		pos, _ := p.(map[string]interface{})
		fmt.Printf("• %s @ %s\n", pos["symbol"], pos["entry_price"])
		fmt.Printf("  P&L: %.2f%% | Size: %s\n\n", pos["pnl_percent"], pos["size"])
	}
}

func handleAutonomous(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: :autonomous <start|pause|status>")
		return
	}

	var endpoint string
	switch args[0] {
	case "start":
		endpoint = APIPath + "/autonomous/begin"
	case "pause":
		endpoint = APIPath + "/autonomous/pause"
	case "status":
		endpoint = APIPath + "/autonomous/quests"
	default:
		fmt.Printf("Unknown action: %s\n", args[0])
		return
	}

	resp, err := http.Post(BackendURL+endpoint, "application/json", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		fmt.Printf("✅ %s\n", strings.Title(args[0]))
	} else {
		fmt.Printf("❌ Failed: %v\n", result["error"])
	}
}

func runDoctor() {
	resp, err := http.Get(BackendURL + APIPath + "/doctor")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("%s\n", data)
}
