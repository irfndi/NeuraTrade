#!/usr/bin/env bash

set -euo pipefail

CLI_BIN_NAME="neuratrade"
LEGACY_CLI_BIN_NAME="neuratrade-cli"
BACKEND_BIN_NAME="neuratrade-server"
CCXT_BIN_NAME="ccxt-service"
TELEGRAM_BIN_NAME="telegram-service"
BOOTSTRAP_CMD_NAME="NeuraTrade"
AUTOSTART_LABEL="com.neuratrade.gateway"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.neuratrade}"
ENV_TARGET="$CONFIG_DIR/.env"
TMP_DIR=""
SKIP_BUILD="${SKIP_BUILD:-false}"
BOOTSTRAP_MODE="none"
BOOTSTRAP_LOCATION=""
ENABLE_AUTOSTART="${ENABLE_AUTOSTART:-false}"
AUTOSTART_MODE="disabled"
AUTOSTART_LOCATION=""

log() {
  printf '[install] %s\n' "$1"
}

warn() {
  printf '[install][warn] %s\n' "$1" >&2
}

die() {
  printf '[install][error] %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}

trap cleanup EXIT

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
}

detect_repo_root() {
  if [[ -d "$REPO_ROOT/services/backend-api/cmd/server" ]]; then
    return
  fi

  die "run this installer from the NeuraTrade repository root"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --skip-build)
        SKIP_BUILD="true"
        ;;
      --enable-autostart)
        ENABLE_AUTOSTART="true"
        ;;
      --disable-autostart)
        ENABLE_AUTOSTART="false"
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
    shift
  done
}

install_backend_binary() {
  local output_bin="$1"

  require_cmd go
  log "building $BACKEND_BIN_NAME binary"
  (
    cd "$REPO_ROOT/services/backend-api"
    go build -o "$output_bin" ./cmd/server
  )
}

install_cli_binary() {
  local output_bin="$1"

  require_cmd go
  log "building $CLI_BIN_NAME binary"
  (
    cd "$REPO_ROOT/cmd/neuratrade-cli"
    go build -o "$output_bin" .
  )
}

install_ccxt_stub() {
  local output_bin="$1"
  cat >"$output_bin" <<'EOF'
#!/usr/bin/env bash
echo "[CCXT Service] Native CCXT implementation is running within neuratrade-server"
trap "exit 0" SIGTERM SIGINT
while true; do sleep 60; done
EOF
  chmod 0755 "$output_bin"
}

install_telegram_launcher() {
  local output_bin="$1"
  cat >"$output_bin" <<EOF
#!/usr/bin/env bash
set -euo pipefail
export PATH="\$HOME/.bun/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH"
cd "$REPO_ROOT/services/telegram-service"
exec bun run index.ts "\$@"
EOF
  chmod 0755 "$output_bin"
}

install_cli_compat_wrapper() {
  local output_bin="$1"
  cat >"$output_bin" <<EOF
#!/usr/bin/env bash
exec "$INSTALL_DIR/$CLI_BIN_NAME" "\$@"
EOF
  chmod 0755 "$output_bin"
}

create_bootstrap_command() {
  local backend_bin_path="$INSTALL_DIR/$BACKEND_BIN_NAME"
  local bootstrap_path="$INSTALL_DIR/$BOOTSTRAP_CMD_NAME"
  local cli_name_lower
  local bootstrap_name_lower

  cli_name_lower="$(printf '%s' "$CLI_BIN_NAME" | tr '[:upper:]' '[:lower:]')"
  bootstrap_name_lower="$(printf '%s' "$BOOTSTRAP_CMD_NAME" | tr '[:upper:]' '[:lower:]')"

  if [[ ! -x "$backend_bin_path" ]]; then
    warn "cannot create $BOOTSTRAP_CMD_NAME command because $backend_bin_path is missing"
    return
  fi

  if [[ "$cli_name_lower" == "$bootstrap_name_lower" ]]; then
    local bootstrap_alias_path="$CONFIG_DIR/bootstrap-command.sh"
    cat >"$bootstrap_alias_path" <<EOF
alias $BOOTSTRAP_CMD_NAME="$backend_bin_path"
EOF
    BOOTSTRAP_MODE="alias"
    BOOTSTRAP_LOCATION="$bootstrap_alias_path"
    log "installed bootstrap alias file at $bootstrap_alias_path"
    return
  fi

  cat >"$bootstrap_path" <<EOF
#!/usr/bin/env bash
exec "$backend_bin_path" "\$@"
EOF
  chmod 0755 "$bootstrap_path"
  BOOTSTRAP_MODE="binary"
  BOOTSTRAP_LOCATION="$bootstrap_path"
  log "installed bootstrap command at $bootstrap_path"
}

install_launchd_autostart() {
  if [[ "$ENABLE_AUTOSTART" != "true" ]]; then
    AUTOSTART_MODE="disabled"
    return
  fi

  if [[ "$(uname -s)" != "Darwin" ]]; then
    warn "autostart is currently supported only on macOS (launchd)"
    AUTOSTART_MODE="unsupported"
    return
  fi

  require_cmd launchctl

  local launch_agents_dir="$HOME/Library/LaunchAgents"
  local launch_script="$CONFIG_DIR/run-gateway.sh"
  local launch_plist="$launch_agents_dir/$AUTOSTART_LABEL.plist"
  local launch_log_dir="$CONFIG_DIR/logs"

  mkdir -p "$launch_agents_dir" "$launch_log_dir"

  cat >"$launch_script" <<EOF
#!/usr/bin/env bash
set -euo pipefail
export PATH="\$HOME/.bun/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH"
if [[ -f "$ENV_TARGET" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_TARGET"
  set +a
fi
exec "$INSTALL_DIR/$CLI_BIN_NAME" gateway start
EOF
  chmod 0755 "$launch_script"

  cat >"$launch_plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$AUTOSTART_LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$launch_script</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>WorkingDirectory</key>
  <string>$HOME</string>
  <key>StandardOutPath</key>
  <string>$launch_log_dir/launchd-gateway.out.log</string>
  <key>StandardErrorPath</key>
  <string>$launch_log_dir/launchd-gateway.err.log</string>
</dict>
</plist>
EOF

  launchctl bootout "gui/$UID/$AUTOSTART_LABEL" >/dev/null 2>&1 || true
  launchctl unload "$launch_plist" >/dev/null 2>&1 || true

  if launchctl bootstrap "gui/$UID" "$launch_plist" >/dev/null 2>&1; then
    launchctl enable "gui/$UID/$AUTOSTART_LABEL" >/dev/null 2>&1 || true
    launchctl kickstart -k "gui/$UID/$AUTOSTART_LABEL" >/dev/null 2>&1 || true
  else
    launchctl load -w "$launch_plist" >/dev/null 2>&1 || die "failed to load launchd service at $launch_plist"
  fi

  AUTOSTART_MODE="launchd"
  AUTOSTART_LOCATION="$launch_plist"
  log "installed and started launchd autostart service at $launch_plist"
}

write_env_template() {
  local source_env=""
  if [[ -f "$REPO_ROOT/.env.example" ]]; then
    source_env="$REPO_ROOT/.env.example"
  elif [[ -f "$REPO_ROOT/.env.template" ]]; then
    source_env="$REPO_ROOT/.env.template"
  fi

  if [[ -n "$source_env" ]]; then
    cp "$source_env" "$ENV_TARGET"
    log "created env template from $(basename "$source_env") at $ENV_TARGET"
    return
  fi

  cat >"$ENV_TARGET" <<'EOF'
# NeuraTrade local environment template
APP_ENV=development
LOG_LEVEL=info
DATABASE_DRIVER=sqlite
SQLITE_PATH=./data/neuratrade.db
REDIS_URL=redis://localhost:6379
EOF
  log "created default env template at $ENV_TARGET"
}

print_next_steps() {
  local cli_bin_path="$INSTALL_DIR/$CLI_BIN_NAME"
  local backend_bin_path="$INSTALL_DIR/$BACKEND_BIN_NAME"
  local binary_installed="$1"
  log "installation complete"
  printf '\n'
  if [[ "$binary_installed" == "true" ]]; then
    printf 'Installed CLI:      %s\n' "$cli_bin_path"
    printf 'Installed backend:  %s\n' "$backend_bin_path"
    printf 'Installed helper:   %s/%s\n' "$INSTALL_DIR" "$CCXT_BIN_NAME"
    printf 'Installed helper:   %s/%s\n' "$INSTALL_DIR" "$TELEGRAM_BIN_NAME"
    printf 'CLI compatibility:  %s/%s -> %s\n' "$INSTALL_DIR" "$LEGACY_CLI_BIN_NAME" "$CLI_BIN_NAME"
  else
    printf 'Binary install:     skipped (--skip-build)\n'
  fi
  if [[ "$BOOTSTRAP_MODE" == "binary" ]]; then
    printf 'Bootstrap command: %s\n' "$BOOTSTRAP_LOCATION"
  elif [[ "$BOOTSTRAP_MODE" == "alias" ]]; then
    printf 'Bootstrap alias file: %s\n' "$BOOTSTRAP_LOCATION"
  fi
  if [[ "$AUTOSTART_MODE" == "launchd" ]]; then
    printf 'Autostart:          enabled (launchd)\n'
    printf 'LaunchAgent:        %s\n' "$AUTOSTART_LOCATION"
  elif [[ "$AUTOSTART_MODE" == "disabled" ]]; then
    printf 'Autostart:          disabled\n'
  else
    printf 'Autostart:          unsupported on this OS\n'
  fi

  # Create a shorter alias for the CLI
  local cli_shortcut_path="$CONFIG_DIR/cli-shortcut.sh"
  cat >"$cli_shortcut_path" <<EOF
alias nt="$INSTALL_DIR/${CLI_BIN_NAME}"
EOF
  log "installed CLI shortcut alias at $cli_shortcut_path"
  printf 'CLI shortcut: nt (alias for neuratrade)\n'
  printf 'Config directory:  %s\n' "$CONFIG_DIR"
  printf 'Env template:      %s\n' "$ENV_TARGET"
  printf '\n'

  if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    warn "$INSTALL_DIR is not currently in PATH"
    printf 'Add this line to your shell profile:\n'
    printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
    printf '\n'
  fi

  printf 'Next steps:\n'
  printf '  1) Edit %s\n' "$ENV_TARGET"
  if [[ "$AUTOSTART_MODE" == "launchd" ]]; then
    printf '  2) Gateway autostart is active (restarts on login)\n'
    printf '     Check status: launchctl print gui/%s/%s\n' "$UID" "$AUTOSTART_LABEL"
  else
    printf '  2) Run: neuratrade gateway start     # Start all services\n'
  fi
  printf '  3) Run: neuratrade --help            # Show all commands\n'
  if [[ "$BOOTSTRAP_MODE" == "binary" ]]; then
    printf '  4) Backend: %s --help\n' "$BOOTSTRAP_LOCATION"
  elif [[ "$BOOTSTRAP_MODE" == "alias" ]]; then
    printf '  4) Source alias: source %s\n' "$BOOTSTRAP_LOCATION"
    printf '  5) Backend: %s --help\n' "$BOOTSTRAP_CMD_NAME"
  fi
}

main() {
  parse_args "$@"
  detect_repo_root

  mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"
  TMP_DIR="$(mktemp -d)"
  local tmp_backend_bin="$TMP_DIR/$BACKEND_BIN_NAME"
  local tmp_cli_bin="$TMP_DIR/$CLI_BIN_NAME"
  local binary_installed="false"

  if [[ "$SKIP_BUILD" == "true" ]]; then
    warn "skipping binary build and install"
  else
    install_backend_binary "$tmp_backend_bin"
    install_cli_binary "$tmp_cli_bin"
    install -m 0755 "$tmp_backend_bin" "$INSTALL_DIR/$BACKEND_BIN_NAME"
    install -m 0755 "$tmp_cli_bin" "$INSTALL_DIR/$CLI_BIN_NAME"
    install_ccxt_stub "$INSTALL_DIR/$CCXT_BIN_NAME"
    install_telegram_launcher "$INSTALL_DIR/$TELEGRAM_BIN_NAME"
    install_cli_compat_wrapper "$INSTALL_DIR/$LEGACY_CLI_BIN_NAME"
    log "installed $BACKEND_BIN_NAME to $INSTALL_DIR/$BACKEND_BIN_NAME"
    log "installed $CLI_BIN_NAME to $INSTALL_DIR/$CLI_BIN_NAME"
    log "installed CLI compatibility wrapper at $INSTALL_DIR/$LEGACY_CLI_BIN_NAME"
    binary_installed="true"
  fi

  create_bootstrap_command

  if [[ -f "$ENV_TARGET" ]]; then
    warn "env template already exists at $ENV_TARGET, leaving as-is"
  else
    write_env_template
  fi

  install_launchd_autostart

  print_next_steps "$binary_installed"
}

main "$@"
