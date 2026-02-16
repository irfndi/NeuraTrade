package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var (
	version   = "dev"
	gitCommit string
	buildTime string
	goVersion string
)

const logo = "🚀"

func formatVersion() string {
	v := version
	if gitCommit != "" {
		v += fmt.Sprintf(" (git: %s)", gitCommit)
	}
	return v
}

func formatBuildInfo() (build string, goVer string) {
	if buildTime != "" {
		build = buildTime
	}
	goVer = goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	return
}

func printVersion() {
	fmt.Printf("%s NeuraTrade CLI %s\n", logo, formatVersion())
	build, goVer := formatBuildInfo()
	if build != "" {
		fmt.Printf("  Build: %s\n", build)
	}
	if goVer != "" {
		fmt.Printf("  Go: %s\n", goVer)
	}
}

func printHelp() {
	fmt.Printf(`%s NeuraTrade CLI - Manage your trading platform services

Usage: neuratrade <command> [subcommand] [flags]

Commands:
  gateway start    Start all services (Docker mode by default)
  gateway stop     Stop all services
  gateway status   Check service status
  gateway logs     Show service logs
  agent            Start interactive AI agent mode
  version          Show version information
  help             Show this help message
  dev              Start in development mode (testnet for all services)

Gateway Start Options:
  --native         Run services as native processes instead of Docker
  --no-backend     Skip starting backend-api service
  --no-ccxt        Skip starting ccxt-service
  --no-telegram    Skip starting telegram-service
  --detach, -d     Run in detached mode (Docker only)

Environment Variables:
  NEURATRADE_HOME          Base directory for NeuraTrade (default: ~/.neuratrade)
  TELEGRAM_BOT_TOKEN       Telegram bot token
  ADMIN_API_KEY            Admin API key for service authentication
  DATABASE_PASSWORD        PostgreSQL password (required for local mode)

Examples:
  neuratrade gateway start                    # Start all services with Docker
  neuratrade gateway start --native         # Start all services natively
  neuratrade gateway start --no-telegram     # Start without Telegram service
  neuratrade dev                             # Start in dev/testnet mode
  neuratrade gateway stop                    # Stop all services
  neuratrade gateway status                  # Check service health
  neuratrade gateway logs                    # Show all service logs
  neuratrade gateway logs --service backend  # Show backend logs only
`, logo)
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "gateway":
		if len(os.Args) < 3 {
			fmt.Println("Error: gateway requires a subcommand (start, stop, status, logs)")
			fmt.Println("Usage: neuratrade gateway <start|stop|status|logs>")
			os.Exit(1)
		}
		handleGatewayCommand(os.Args[2:])
	case "dev":
		devCmd(os.Args[2:])
	case "agent":
		agentCmd(os.Args[2:])
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func handleGatewayCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("Error: gateway requires a subcommand")
		fmt.Println("Usage: neuratrade gateway <start|stop|status|logs>")
		os.Exit(1)
	}

	subcommand := args[0]
	remainingArgs := args[1:]

	switch subcommand {
	case "start":
		gatewayStartCmd(remainingArgs)
	case "stop":
		gatewayStopCmd(remainingArgs)
	case "status":
		gatewayStatusCmd(remainingArgs)
	case "logs":
		gatewayLogsCmd(remainingArgs)
	default:
		fmt.Printf("Unknown gateway subcommand: %s\n", subcommand)
		fmt.Println("Usage: neuratrade gateway <start|stop|status|logs>")
		os.Exit(1)
	}
}

func getProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	if _, err := os.Stat(filepath.Join(cwd, "docker-compose.yaml")); err == nil {
		return cwd
	}

	parent := filepath.Dir(cwd)
	if _, err := os.Stat(filepath.Join(parent, "docker-compose.yaml")); err == nil {
		return parent
	}

	grandparent := filepath.Dir(parent)
	if _, err := os.Stat(filepath.Join(grandparent, "docker-compose.yaml")); err == nil {
		return grandparent
	}

	return cwd
}
