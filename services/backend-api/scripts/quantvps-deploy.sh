#!/bin/bash
#
# QuantVPS Deployment Script for NeuraTrade
# Automates VPS provisioning and deployment on QuantVPS infrastructure
#

set -euo pipefail

# Configuration
QUANTVPS_API_URL="${QUANTVPS_API_URL:-https://api.quantvps.com/v1}"
QUANTVPS_API_KEY="${QUANTVPS_API_KEY:-}"
SERVER_NAME="${SERVER_NAME:-neuratrade-$(date +%s)}"
SERVER_PLAN="${SERVER_PLAN:-standard}"
SERVER_REGION="${SERVER_REGION:-us-east}"
SERVER_OS="${SERVER_OS:-ubuntu-22.04}"
SSH_KEY_PATH="${SSH_KEY_PATH:-$HOME/.ssh/id_rsa.pub}"
DEPLOYMENT_ENV="${DEPLOYMENT_ENV:-production}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
  echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
  echo -e "${RED}[ERROR]${NC} $1"
}

check_requirements() {
  log_info "Checking requirements..."

  if [[ -z "$QUANTVPS_API_KEY" ]]; then
    log_error "QUANTVPS_API_KEY environment variable is required"
    exit 1
  fi

  if ! command -v curl &>/dev/null; then
    log_error "curl is required but not installed"
    exit 1
  fi

  if [[ ! -f "$SSH_KEY_PATH" ]]; then
    log_error "SSH key not found at $SSH_KEY_PATH"
    log_info "Generate one with: ssh-keygen -t rsa -b 4096"
    exit 1
  fi

  log_info "All requirements satisfied"
}

provision_server() {
  log_info "Provisioning QuantVPS server: $SERVER_NAME"

  local ssh_key
  ssh_key=$(cat "$SSH_KEY_PATH")

  local response
  response=$(curl -s -X POST "$QUANTVPS_API_URL/servers" \
    -H "Authorization: Bearer $QUANTVPS_API_KEY" \
    -H "Content-Type: application/json" \
    -d "{
            \"name\": \"$SERVER_NAME\",
            \"plan\": \"$SERVER_PLAN\",
            \"region\": \"$SERVER_REGION\",
            \"os\": \"$SERVER_OS\",
            \"ssh_key\": \"$ssh_key\",
            \"tags\": [\"neuratrade\", \"$DEPLOYMENT_ENV\"]
        }" 2>/dev/null || echo "{}")

  local server_id
  server_id=$(echo "$response" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

  if [[ -z "$server_id" ]]; then
    log_error "Failed to provision server. Response: $response"
    exit 1
  fi

  echo "$server_id"
}

wait_for_server() {
  local server_id=$1
  log_info "Waiting for server $server_id to be ready..."

  local max_attempts=30
  local attempt=1

  while [[ $attempt -le $max_attempts ]]; do
    local response
    response=$(curl -s -X GET "$QUANTVPS_API_URL/servers/$server_id" \
      -H "Authorization: Bearer $QUANTVPS_API_KEY" 2>/dev/null || echo "{}")

    local status
    status=$(echo "$response" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)

    if [[ "$status" == "active" ]]; then
      log_info "Server is ready!"
      return 0
    fi

    log_info "Server status: $status (attempt $attempt/$max_attempts)"
    sleep 10
    ((attempt++))
  done

  log_error "Server failed to become ready within timeout"
  exit 1
}

get_server_ip() {
  local server_id=$1

  local response
  response=$(curl -s -X GET "$QUANTVPS_API_URL/servers/$server_id" \
    -H "Authorization: Bearer $QUANTVPS_API_KEY" 2>/dev/null || echo "{}")

  local ip
  ip=$(echo "$response" | grep -o '"ip":"[^"]*"' | cut -d'"' -f4)

  echo "$ip"
}

deploy_application() {
  local server_ip=$1
  log_info "Deploying NeuraTrade to $server_ip..."

  # Create deployment directory
  ssh -o StrictHostKeyChecking=no "root@$server_ip" "mkdir -p /opt/neuratrade"

  # Copy deployment files
  rsync -avz --exclude '.git' --exclude 'node_modules' \
    -e "ssh -o StrictHostKeyChecking=no" \
    ../../ "root@$server_ip:/opt/neuratrade/"

  # Run deployment script on server
  ssh -o StrictHostKeyChecking=no "root@$server_ip" "cd /opt/neuratrade && bash scripts/quantvps-setup.sh"

  log_info "Application deployed successfully"
}

setup_monitoring() {
  local server_ip=$1
  log_info "Setting up monitoring for $server_ip..."

  # Install monitoring agents
  ssh -o StrictHostKeyChecking=no "root@$server_ip" "
        # Install node_exporter for Prometheus
        wget -q https://github.com/prometheus/node_exporter/releases/download/v1.6.1/node_exporter-1.6.1.linux-amd64.tar.gz
        tar xzf node_exporter-1.6.1.linux-amd64.tar.gz
        mv node_exporter-1.6.1.linux-amd64/node_exporter /usr/local/bin/
        rm -rf node_exporter-1.6.1.linux-amd64*
        
        # Create systemd service
        cat > /etc/systemd/system/node_exporter.service << 'EOF'
[Unit]
Description=Node Exporter
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter
Restart=always

[Install]
WantedBy=multi-user.target
EOF
        
        systemctl daemon-reload
        systemctl enable node_exporter
        systemctl start node_exporter
        
        # Install log rotation
        cat > /etc/logrotate.d/neuratrade << 'EOF'
/opt/neuratrade/logs/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0644 root root
}
EOF
    "

  log_info "Monitoring setup complete"
}

cleanup() {
  log_info "Cleaning up local resources..."
  # Add any local cleanup if needed
}

show_usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Deploy NeuraTrade to QuantVPS

Options:
    -n, --name NAME         Server name (default: neuratrade-<timestamp>)
    -p, --plan PLAN         Server plan (default: standard)
    -r, --region REGION     Server region (default: us-east)
    -o, --os OS             Operating system (default: ubuntu-22.04)
    -e, --env ENV           Deployment environment (default: production)
    -k, --key PATH          SSH key path (default: ~/.ssh/id_rsa.pub)
    -h, --help              Show this help message

Environment Variables:
    QUANTVPS_API_KEY        Required: Your QuantVPS API key
    QUANTVPS_API_URL        Optional: API endpoint URL

Examples:
    # Deploy with defaults
    QUANTVPS_API_KEY=xxx $0

    # Deploy with custom name and plan
    QUANTVPS_API_KEY=xxx $0 -n my-server -p premium -r eu-west

EOF
}

main() {
  # Parse arguments
  while [[ $# -gt 0 ]]; do
    case $1 in
      -n | --name)
        SERVER_NAME="$2"
        shift 2
        ;;
      -p | --plan)
        SERVER_PLAN="$2"
        shift 2
        ;;
      -r | --region)
        SERVER_REGION="$2"
        shift 2
        ;;
      -o | --os)
        SERVER_OS="$2"
        shift 2
        ;;
      -e | --env)
        DEPLOYMENT_ENV="$2"
        shift 2
        ;;
      -k | --key)
        SSH_KEY_PATH="$2"
        shift 2
        ;;
      -h | --help)
        show_usage
        exit 0
        ;;
      *)
        log_error "Unknown option: $1"
        show_usage
        exit 1
        ;;
    esac
  done

  trap cleanup EXIT

  log_info "Starting NeuraTrade deployment to QuantVPS"
  log_info "Server name: $SERVER_NAME"
  log_info "Plan: $SERVER_PLAN"
  log_info "Region: $SERVER_REGION"
  log_info "Environment: $DEPLOYMENT_ENV"

  check_requirements

  local server_id
  server_id=$(provision_server)
  log_info "Server provisioned with ID: $server_id"

  wait_for_server "$server_id"

  local server_ip
  server_ip=$(get_server_ip "$server_id")
  log_info "Server IP: $server_ip"

  # Save deployment info
  cat >"quantvps-deployment-${SERVER_NAME}.json" <<EOF
{
    "server_id": "$server_id",
    "server_name": "$SERVER_NAME",
    "server_ip": "$server_ip",
    "region": "$SERVER_REGION",
    "plan": "$SERVER_PLAN",
    "environment": "$DEPLOYMENT_ENV",
    "deployed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

  deploy_application "$server_ip"
  setup_monitoring "$server_ip"

  log_info "Deployment complete!"
  log_info "Server IP: $server_ip"
  log_info "Deployment info saved to: quantvps-deployment-${SERVER_NAME}.json"
}

main "$@"
