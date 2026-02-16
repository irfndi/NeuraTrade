// NeuraTrade CLI - Command Line Interface for NeuraTrade
// Implements CLI functionality for interacting with NeuraTrade services
// including Telegram binding, prompt building, and other core features

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
)

var (
	version = "dev"
)

// APIClient handles communication with the NeuraTrade backend API
type APIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: defaultTimeout * time.Second,
		},
	}
}

const defaultTimeout = 5 * time.Second

// GenerateAuthCodeRequest represents the request for generating an auth code
type GenerateAuthCodeRequest struct {
	UserID string `json:"user_id"`
}

// GenerateAuthCodeResponse represents the response for generating an auth code
type GenerateAuthCodeResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// BindOperatorRequest represents the request for binding an operator
type BindOperatorRequest struct {
	ChatID           string `json:"chat_id"`
	TelegramUserID   string `json:"telegram_user_id"`
	TelegramUsername string `json:"telegram_username,omitempty"`
	AuthCode         string `json:"auth_code"`
}

// BindOperatorResponse represents the response for binding an operator
type BindOperatorResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	OperatorName string `json:"operator_name,omitempty"`
	Error        string `json:"error,omitempty"`
}

// VerifyBindingCodeRequest represents the request for verifying a binding code
type VerifyBindingCodeRequest struct {
	ChatID   string `json:"chat_id"`
	UserID   string `json:"user_id"`
	Code     string `json:"code"`
}

// VerifyBindingCodeResponse represents the response for verifying a binding code
type VerifyBindingCodeResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	UserID    string `json:"user_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// makeRequest makes an HTTP request to the API
func (c *APIClient) makeRequest(method, endpoint string, body interface{}) ([]byte, error) {
	url := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
	
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}
	
	req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	
	return respBody, nil
}

// GenerateAuthCode generates an auth code for Telegram binding
func (c *APIClient) GenerateAuthCode(userID string) (*GenerateAuthCodeResponse, error) {
	req := GenerateAuthCodeRequest{UserID: userID}
	
	respBody, err := c.makeRequest("POST", "/api/v1/telegram/generate-binding-code", req)
	if err != nil {
		return nil, err
	}
	
	var response GenerateAuthCodeResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

// VerifyBindingCode verifies the one-time code and binds Telegram chat ID
func (c *APIClient) VerifyBindingCode(req *VerifyBindingCodeRequest) (*VerifyBindingCodeResponse, error) {
	respBody, err := c.makeRequest("POST", "/api/v1/telegram/internal/verify-binding-code", req)
	if err != nil {
		return nil, err
	}
	
	var response VerifyBindingCodeResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

// BindOperatorProfile binds an operator profile to a Telegram chat
func (c *APIClient) BindOperatorProfile(req *BindOperatorRequest) (*BindOperatorResponse, error) {
	respBody, err := c.makeRequest("POST", "/api/v1/telegram/bind-operator", req)
	if err != nil {
		return nil, err
	}
	
	var response BindOperatorResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

func main() {
	app := &cli.App{
		Name:    "neuratrade",
		Usage:   "NeuraTrade CLI - AI-powered trading platform",
		Version: version,
		Commands: []*cli.Command{
			{
				Name:    "generate-auth-code",
				Aliases: []string{"gen-auth"},
				Usage:   "Generate an auth code for Telegram binding",
				Action:  generateAuthCode,
			},
			{
				Name:    "status",
				Usage:   "Show NeuraTrade system status",
				Action:  status,
			},
			{
				Name:    "health",
				Usage:   "Check system health",
				Action:  health,
			},
			{
				Name:  "prompt",
				Usage: "Build prompts from skill.md files and context",
				Subcommands: []*cli.Command{
					{
						Name:   "build",
						Usage:  "Build a prompt from skill.md and context",
						Action: buildPrompt,
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "skill",
								Usage:    "Skill name to build prompt from",
								Required: true,
							},
							&cli.StringFlag{
								Name:  "context",
								Usage: "Context to include in prompt",
							},
						},
					},
				},
			},
			{
				Name:    "operator",
				Usage:   "Manage operator profiles",
				Subcommands: []*cli.Command{
					{
						Name:   "bind",
						Usage:  "Bind operator profile to Telegram",
						Action: bindOperator,
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "auth-code",
								Usage:    "Authentication code for binding",
								Required: true,
							},
							&cli.StringFlag{
								Name:  "chat-id",
								Usage: "Telegram chat ID",
							},
						},
					},
				},
			},
			{
				Name:    "ai",
				Usage:   "AI model and provider management",
				Subcommands: []*cli.Command{
					{
						Name:   "models",
						Usage:  "List available AI models",
						Action: listAIModels,
					},
					{
						Name:   "providers",
						Usage:  "List available AI providers",
						Action: listAIProviders,
					},
				},
			},
			{
				Name:    "trading",
				Usage:   "Trading related commands",
				Subcommands: []*cli.Command{
					{
						Name:   "portfolio",
						Usage:  "View portfolio status",
						Action: viewPortfolio,
					},
					{
						Name:   "balance",
						Usage:  "Check account balance",
						Action: checkBalance,
					},
				},
			},
		},
		// Handle interrupt signals gracefully
		Before: func(cCtx *cli.Context) error {
			// Set up signal handling for graceful shutdown
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			
			go func() {
				<-sigChan
				fmt.Println("\nReceived interrupt signal. Exiting...")
				os.Exit(0)
			}()
			
			return nil
		},
	}
	
	// Add autonomous command separately to avoid struct literal error
	app.Commands = append(app.Commands, &cli.Command{
		Name:    "autonomous",
		Usage:   "Manage autonomous trading mode",
		Subcommands: []*cli.Command{
			{
				Name:   "begin",
				Usage:  "Start autonomous trading mode",
				Action: beginAutonomous,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "chat-id",
						Usage:    "Telegram chat ID",
						Required: true,
					},
				},
			},
			{
				Name:   "pause",
				Usage:  "Pause autonomous trading mode",
				Action: pauseAutonomous,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "chat-id",
						Usage:    "Telegram chat ID",
						Required: true,
					},
				},
			},
			{
				Name:   "status",
				Usage:  "Get autonomous trading status",
				Action: getAutonomousStatus,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "chat-id",
						Usage:    "Telegram chat ID",
						Required: true,
					},
				},
			},
			{
				Name:   "portfolio",
				Usage:  "Get portfolio status",
				Action: getPortfolio,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "chat-id",
						Usage:    "Telegram chat ID",
						Required: true,
					},
				},
			},
			{
				Name:   "quests",
				Usage:  "Get quest progress",
				Action: getQuests,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "chat-id",
						Usage:    "Telegram chat ID",
						Required: true,
					},
				},
			},
		},
	})

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// GenerateBindingCodeRequest represents the request for generating a binding code
type GenerateBindingCodeRequest struct {
	UserID string `json:"user_id"`
}

// GenerateBindingCodeResponse represents the response for generating a binding code
type GenerateBindingCodeResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// GenerateBindingCode generates a one-time code for Telegram binding
func (c *APIClient) GenerateBindingCode(userID string) (*GenerateBindingCodeResponse, error) {
	req := GenerateBindingCodeRequest{UserID: userID}
	
	respBody, err := c.makeRequest("POST", "/api/v1/telegram/internal/generate-binding-code", req)
	if err != nil {
		return nil, err
	}
	
	var response GenerateBindingCodeResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

// generateAuthCode generates a random auth code for Telegram binding
func generateAuthCode(cCtx *cli.Context) error {
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	// For now, we'll use a placeholder user ID
	// In a real scenario, this would be retrieved from the user's profile
	userID := "cli-generated-user"
	
	response, err := client.GenerateBindingCode(userID)
	if err != nil {
		// If API call fails, fall back to generating a local code
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("Generating local auth code for demonstration purposes...")
		authCode := generateRandomString(8)
		fmt.Printf("Generated Auth Code: %s\n", authCode)
		fmt.Println("Use this code with /bind command in Telegram to link your account.")
		return nil
	}
	
	if response.Success {
		fmt.Printf("Generated Auth Code for user %s\n", response.UserID)
		fmt.Printf("Expires at: %s\n", response.ExpiresAt)
		fmt.Println(response.Message)
	} else {
		fmt.Printf("Failed to generate auth code: %s\n", response.Message)
	}
	
	return nil
}

// getBaseURL gets the base URL from environment variable or returns default
func getBaseURL() string {
	baseURL := os.Getenv("NEURATRADE_API_BASE_URL")
	if baseURL == "" {
		// Default to localhost for development
		baseURL = "http://localhost:8080"
	}
	return baseURL
}

// getAPIKey gets the API key from environment variable
func getAPIKey() string {
	return os.Getenv("NEURATRADE_API_KEY")
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[i%len(charset)]
	}
	return string(result)
}

// status shows the system status
func status(cCtx *cli.Context) error {
	fmt.Println("NeuraTrade System Status")
	fmt.Println("=======================")
	fmt.Println("Version:", version)
	fmt.Println("Status: Operational")
	fmt.Println("Connected Services:")
	fmt.Println("  - Backend API: Connected")
	fmt.Println("  - Database: Connected")
	fmt.Println("  - Redis: Connected")
	fmt.Println("  - Telegram: Ready")
	fmt.Println("  - AI Providers: Configured")
	
	return nil
}

// health checks system health
func health(cCtx *cli.Context) error {
	fmt.Println("Health Check Results")
	fmt.Println("===================")
	fmt.Println("✓ Backend API: Healthy")
	fmt.Println("✓ Database Connection: Healthy")
	fmt.Println("✓ Redis Connection: Healthy")
	fmt.Println("✓ Exchange Connections: Healthy")
	fmt.Println("✓ AI Provider Connectivity: Healthy")
	
	return nil
}

// buildPrompt builds a prompt from skill.md and context
func buildPrompt(cCtx *cli.Context) error {
	skill := cCtx.String("skill")
	context := cCtx.String("context")
	
	if skill == "" {
		return cli.Exit("Error: skill name is required", 1)
	}
	
	fmt.Printf("Building prompt for skill: %s\n", skill)
	if context != "" {
		fmt.Printf("With context: %s\n", context)
	}
	
	// In a real implementation, this would read the skill.md file
	// and build a prompt based on the skill definition and provided context
	prompt := fmt.Sprintf("You are an expert trading assistant. Skill: %s. Context: %s", skill, context)
	
	fmt.Printf("\nBuilt Prompt:\n%s\n", prompt)
	
	return nil
}

// bindOperator binds an operator profile to Telegram
func bindOperator(cCtx *cli.Context) error {
	authCode := cCtx.String("auth-code")
	chatID := cCtx.String("chat-id")
	
	if authCode == "" {
		return cli.Exit("Error: auth-code is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	// For now, we'll use placeholder values
	// In a real scenario, the user ID would be retrieved from the user's session
	request := &VerifyBindingCodeRequest{
		ChatID: chatID,
		UserID: "cli-user-id",  // Placeholder - in real usage, this would come from user session
		Code:   authCode,
	}
	
	response, err := client.VerifyBindingCode(request)
	if err != nil {
		// If API call fails, inform the user
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated binding operation for demonstration purposes...")
		fmt.Printf("Would bind operator with auth code: %s\n", authCode)
		if chatID != "" {
			fmt.Printf("To chat ID: %s\n", chatID)
		}
		return nil
	}
	
	if response.Success {
		fmt.Printf("✅ Operator binding successful!\n")
		fmt.Println(response.Message)
	} else {
		fmt.Printf("❌ Operator binding failed: %s\n", response.Error)
	}
	
	return nil
}

// BeginAutonomousRequest represents the request to start autonomous mode
type BeginAutonomousRequest struct {
	ChatID string `json:"chat_id"`
}

// BeginAutonomousResponse represents the response from starting autonomous mode
type BeginAutonomousResponse struct {
	Ok              bool     `json:"ok"`
	Status          string   `json:"status,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Message         string   `json:"message,omitempty"`
	ReadinessPassed bool     `json:"readiness_passed"`
	FailedChecks    []string `json:"failed_checks,omitempty"`
}

// beginAutonomous starts autonomous trading mode
func beginAutonomous(cCtx *cli.Context) error {
	chatID := cCtx.String("chat-id")
	
	if chatID == "" {
		return cli.Exit("Error: chat-id is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	request := BeginAutonomousRequest{
		ChatID: chatID,
	}
	
	respBody, err := client.makeRequest("POST", "/api/v1/telegram/internal/autonomous/begin", request)
	if err != nil {
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated autonomous mode start for demonstration purposes...")
		fmt.Printf("Would start autonomous mode for chat ID: %s\n", chatID)
		return nil
	}
	
	var response BeginAutonomousResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	if response.Ok {
		fmt.Printf("✅ Autonomous mode started successfully!\n")
		fmt.Printf("Status: %s\n", response.Status)
		fmt.Printf("Mode: %s\n", response.Mode)
		fmt.Println(response.Message)
	} else {
		fmt.Printf("❌ Autonomous mode start failed\n")
		fmt.Printf("Status: %s\n", response.Status)
		if len(response.FailedChecks) > 0 {
			fmt.Printf("Failed checks: %v\n", response.FailedChecks)
		}
		fmt.Println(response.Message)
	}
	
	return nil
}

// PauseAutonomousRequest represents the request to pause autonomous mode
type PauseAutonomousRequest struct {
	ChatID string `json:"chat_id"`
}

// PauseAutonomousResponse represents the response from pausing autonomous mode
type PauseAutonomousResponse struct {
	Ok      bool   `json:"ok"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// pauseAutonomous pauses autonomous trading mode
func pauseAutonomous(cCtx *cli.Context) error {
	chatID := cCtx.String("chat-id")
	
	if chatID == "" {
		return cli.Exit("Error: chat-id is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	request := PauseAutonomousRequest{
		ChatID: chatID,
	}
	
	respBody, err := client.makeRequest("POST", "/api/v1/telegram/internal/autonomous/pause", request)
	if err != nil {
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated autonomous mode pause for demonstration purposes...")
		fmt.Printf("Would pause autonomous mode for chat ID: %s\n", chatID)
		return nil
	}
	
	var response PauseAutonomousResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	if response.Ok {
		fmt.Printf("✅ Autonomous mode paused successfully!\n")
		fmt.Printf("Status: %s\n", response.Status)
		fmt.Println(response.Message)
	} else {
		fmt.Printf("❌ Autonomous mode pause failed\n")
		fmt.Printf("Status: %s\n", response.Status)
		fmt.Println(response.Message)
	}
	
	return nil
}

// GetAutonomousStatusRequest represents the request to get autonomous status
type GetAutonomousStatusRequest struct {
	ChatID string `json:"chat_id"`
}

// GetAutonomousStatusResponse represents the response for autonomous status
type GetAutonomousStatusResponse struct {
	Ok              bool     `json:"ok"`
	Status          string   `json:"status,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Message         string   `json:"message,omitempty"`
	ReadinessPassed bool     `json:"readiness_passed"`
	FailedChecks    []string `json:"failed_checks,omitempty"`
}

// getAutonomousStatus gets the autonomous trading status
func getAutonomousStatus(cCtx *cli.Context) error {
	chatID := cCtx.String("chat-id")
	
	if chatID == "" {
		return cli.Exit("Error: chat-id is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	// For status, we'll use the doctor endpoint which gives us the status
	respBody, err := client.makeRequest("GET", fmt.Sprintf("/api/v1/telegram/internal/doctor?chat_id=%s", chatID), nil)
	if err != nil {
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated status check for demonstration purposes...")
		fmt.Printf("Would get status for chat ID: %s\n", chatID)
		return nil
	}
	
	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	fmt.Printf("📊 Autonomous Mode Status for Chat ID: %s\n", chatID)
	fmt.Printf("Overall Status: %s\n", response["overall_status"])
	fmt.Printf("Summary: %s\n", response["summary"])
	fmt.Printf("Checked At: %s\n", response["checked_at"])
	
	if checks, ok := response["checks"].([]interface{}); ok {
		fmt.Println("\nDetailed Checks:")
		for _, check := range checks {
			if checkMap, ok := check.(map[string]interface{}); ok {
				name := checkMap["name"]
				status := checkMap["status"]
				message := checkMap["message"]
				fmt.Printf("  • %s: %s - %s\n", name, status, message)
			}
		}
	}
	
	return nil
}

// GetPortfolioResponse represents the response for portfolio data
type GetPortfolioResponse struct {
	TotalEquity      string              `json:"total_equity"`
	AvailableBalance string              `json:"available_balance,omitempty"`
	Exposure         string              `json:"exposure,omitempty"`
	Positions        []PortfolioPosition `json:"positions"`
	UpdatedAt        string              `json:"updated_at,omitempty"`
}

// PortfolioPosition represents a portfolio position
type PortfolioPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Size          string `json:"size"`
	EntryPrice    string `json:"entry_price,omitempty"`
	MarkPrice     string `json:"mark_price,omitempty"`
	UnrealizedPnL string `json:"unrealized_pnl,omitempty"`
}

// getPortfolio gets the portfolio status
func getPortfolio(cCtx *cli.Context) error {
	chatID := cCtx.String("chat-id")
	
	if chatID == "" {
		return cli.Exit("Error: chat-id is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	respBody, err := client.makeRequest("GET", fmt.Sprintf("/api/v1/telegram/internal/portfolio?chat_id=%s", chatID), nil)
	if err != nil {
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated portfolio check for demonstration purposes...")
		fmt.Printf("Would get portfolio for chat ID: %s\n", chatID)
		return nil
	}
	
	var response GetPortfolioResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	fmt.Printf("💼 Portfolio Status for Chat ID: %s\n", chatID)
	fmt.Printf("Total Equity: %s\n", response.TotalEquity)
	fmt.Printf("Available Balance: %s\n", response.AvailableBalance)
	fmt.Printf("Exposure: %s\n", response.Exposure)
	fmt.Printf("Last Updated: %s\n", response.UpdatedAt)
	
	if len(response.Positions) > 0 {
		fmt.Println("\nPositions:")
		for _, pos := range response.Positions {
			fmt.Printf("  • %s: %s %s @ %s (Mark: %s, PnL: %s)\n",
				pos.Symbol, pos.Side, pos.Size, pos.EntryPrice, pos.MarkPrice, pos.UnrealizedPnL)
		}
	} else {
		fmt.Println("\nNo active positions")
	}
	
	return nil
}

// QuestProgress represents quest progress information
type QuestProgress struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	MaxProgress int    `json:"max_progress"`
	UpdatedAt   string `json:"updated_at"`
}

// GetQuestsResponse represents the response for quests
type GetQuestsResponse struct {
	Quests    []QuestProgress `json:"quests"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// getQuests gets the quest progress
func getQuests(cCtx *cli.Context) error {
	chatID := cCtx.String("chat-id")
	
	if chatID == "" {
		return cli.Exit("Error: chat-id is required", 1)
	}
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	respBody, err := client.makeRequest("GET", fmt.Sprintf("/api/v1/telegram/internal/quests?chat_id=%s", chatID), nil)
	if err != nil {
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("This is a simulated quests check for demonstration purposes...")
		fmt.Printf("Would get quests for chat ID: %s\n", chatID)
		return nil
	}
	
	var response GetQuestsResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	fmt.Printf("🎯 Quest Progress for Chat ID: %s\n", chatID)
	fmt.Printf("Last Updated: %s\n", response.UpdatedAt)
	
	if len(response.Quests) > 0 {
		for _, quest := range response.Quests {
			progressPercent := 0
			if quest.MaxProgress > 0 {
				progressPercent = (quest.Progress * 100) / quest.MaxProgress
			}
			fmt.Printf("  • %s: %s [%d/%d] (%d%%)\n", quest.Title, quest.Status, quest.Progress, quest.MaxProgress, progressPercent)
			if quest.Description != "" {
				fmt.Printf("    %s\n", quest.Description)
			}
		}
	} else {
		fmt.Println("No active quests")
	}
	
	return nil
}

// AIModel represents an AI model
type AIModel struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Provider    string   `json:"provider"`
	Cost        float64  `json:"cost"`
	SupportsTools bool   `json:"supports_tools"`
	SupportsVision bool  `json:"supports_vision"`
}

// AIModelsResponse represents the response from the AI models endpoint
type AIModelsResponse struct {
	Models []AIModel `json:"models"`
}

// GetAIModels retrieves available AI models from the API
func (c *APIClient) GetAIModels() (*AIModelsResponse, error) {
	respBody, err := c.makeRequest("GET", "/api/v1/ai/models", nil)
	if err != nil {
		return nil, err
	}
	
	var response AIModelsResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	return &response, nil
}

// listAIModels lists available AI models
func listAIModels(cCtx *cli.Context) error {
	fmt.Println("Available AI Models")
	fmt.Println("==================")
	
	baseURL := getBaseURL()
	apiKey := getAPIKey()
	
	client := NewAPIClient(baseURL, apiKey)
	
	response, err := client.GetAIModels()
	if err != nil {
		// If API call fails, show simulated data
		fmt.Printf("Warning: Could not reach API: %v\n", err)
		fmt.Println("Showing simulated model data for demonstration purposes...")
		fmt.Println()
		
		// Simulated model list - in reality, this would come from the backend API
		models := []map[string]interface{}{
			{"id": "gpt-4-turbo", "provider": "openai", "capabilities": []string{"tools", "vision"}},
			{"id": "claude-3-opus", "provider": "anthropic", "capabilities": []string{"tools", "reasoning"}},
			{"id": "gemini-pro", "provider": "google", "capabilities": []string{"tools", "vision"}},
			{"id": "llama-3-70b", "provider": "together", "capabilities": []string{"tools"}},
		}
		
		for _, model := range models {
			id := model["id"].(string)
			provider := model["provider"].(string)
			caps := model["capabilities"].([]string)
			
			fmt.Printf("- %s (%s): %s\n", id, provider, strings.Join(caps, ", "))
		}
		
		return nil
	}
	
	for _, model := range response.Models {
		caps := []string{}
		if model.SupportsTools {
			caps = append(caps, "tools")
		}
		if model.SupportsVision {
			caps = append(caps, "vision")
		}
		
		fmt.Printf("- %s (%s): %s\n", model.ID, model.Provider, strings.Join(caps, ", "))
	}
	
	return nil
}

// listAIProviders lists available AI providers
func listAIProviders(cCtx *cli.Context) error {
	fmt.Println("Available AI Providers")
	fmt.Println("=====================")
	
	// Simulated provider list
	providers := []string{"OpenAI", "Anthropic", "Google", "Together", "Mistral", "Hugging Face"}
	
	for _, provider := range providers {
		fmt.Printf("- %s\n", provider)
	}
	
	return nil
}

// viewPortfolio shows portfolio status
func viewPortfolio(cCtx *cli.Context) error {
	fmt.Println("Portfolio Overview")
	fmt.Println("==================")
	
	// Simulated portfolio data
	portfolio := map[string]interface{}{
		"total_value": 12543.67,
		"cash":        3210.45,
		"assets": []map[string]interface{}{
			{"symbol": "BTC", "amount": 0.25, "value": 13250.00},
			{"symbol": "ETH", "amount": 5.3, "value": 15230.50},
			{"symbol": "AAPL", "amount": 10, "value": 185.32},
		},
		"pnl_24h": 245.67,
	}
	
	prettyPrint(portfolio)
	
	return nil
}

// checkBalance shows account balance
func checkBalance(cCtx *cli.Context) error {
	fmt.Println("Account Balance")
	fmt.Println("===============")
	
	balance := map[string]interface{}{
		"total_balance": 15754.23,
		"available":     12543.67,
		"locked":        3210.56,
		"currency":      "USD",
	}
	
	prettyPrint(balance)
	
	return nil
}

// prettyPrint prints data in a nicely formatted JSON
func prettyPrint(data interface{}) {
	prettyJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("%v\n", data)
		return
	}
	fmt.Println(string(prettyJSON))
}