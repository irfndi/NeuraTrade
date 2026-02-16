#!/usr/bin/env bash

set -euo pipefail

APP_NAME="neuratrade"
BOOTSTRAP_CMD_NAME="NeuraTrade"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.neuratrade}"
ENV_TARGET="$CONFIG_DIR/.env"
TMP_DIR=""
SKIP_BUILD="${SKIP_BUILD:-false}"
BOOTSTRAP_MODE="none"
BOOTSTRAP_LOCATION=""

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
      *)
        die "unknown argument: $1"
        ;;
    esac
    shift
  done
}

install_binary() {
  local output_bin="$1"

  require_cmd go
  log "building $APP_NAME binary"
  (
    cd "$REPO_ROOT/services/backend-api"
    go build -o "$output_bin" ./cmd/server
  )
}

install_cli_binary() {
  local output_bin="$1"

  require_cmd go
  log "building $APP_NAME-cli binary"
  (
    cd "$REPO_ROOT/cmd/neuratrade-cli"
    go build -o "$output_bin" .
  )
}

create_bootstrap_command() {
  local bin_path="$INSTALL_DIR/$APP_NAME"
  local bootstrap_path="$INSTALL_DIR/$BOOTSTRAP_CMD_NAME"
  local app_name_lower
  local bootstrap_name_lower

  app_name_lower="$(printf '%s' "$APP_NAME" | tr '[:upper:]' '[:lower:]')"
  bootstrap_name_lower="$(printf '%s' "$BOOTSTRAP_CMD_NAME" | tr '[:upper:]' '[:lower:]')"

  if [[ ! -x "$bin_path" ]]; then
    warn "cannot create $BOOTSTRAP_CMD_NAME command because $bin_path is missing"
    return
  fi

  if [[ "$app_name_lower" == "$bootstrap_name_lower" ]]; then
    local bootstrap_alias_path="$CONFIG_DIR/bootstrap-command.sh"
    cat >"$bootstrap_alias_path" <<EOF
alias $BOOTSTRAP_CMD_NAME="$bin_path"
EOF
    BOOTSTRAP_MODE="alias"
    BOOTSTRAP_LOCATION="$bootstrap_alias_path"
    log "installed bootstrap alias file at $bootstrap_alias_path"
    return
  fi

  cat >"$bootstrap_path" <<EOF
#!/usr/bin/env bash
exec "$bin_path" "\$@"
EOF
  chmod 0755 "$bootstrap_path"
  BOOTSTRAP_MODE="binary"
  BOOTSTRAP_LOCATION="$bootstrap_path"
  log "installed bootstrap command at $bootstrap_path"
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
  local bin_path="$INSTALL_DIR/$APP_NAME"
  local binary_installed="$1"
  log "installation complete"
  printf '\n'
  if [[ "$binary_installed" == "true" ]]; then
    printf 'Installed binary: %s\n' "$bin_path"
  else
    printf 'Installed binary: skipped (--skip-build)\n'
  fi
  if [[ "$BOOTSTRAP_MODE" == "binary" ]]; then
    printf 'Bootstrap command: %s\n' "$BOOTSTRAP_LOCATION"
  elif [[ "$BOOTSTRAP_MODE" == "alias" ]]; then
    printf 'Bootstrap alias file: %s\n' "$BOOTSTRAP_LOCATION"
  fi
  
  # Create a shorter alias for the CLI
  local cli_shortcut_path="$CONFIG_DIR/cli-shortcut.sh"
  cat >"$cli_shortcut_path" <<EOF
alias nt="$INSTALL_DIR/${APP_NAME}-cli"
EOF
  log "installed CLI shortcut alias at $cli_shortcut_path"
  printf 'CLI shortcut: nt (alias for neuratrade-cli)\n'
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
  printf '  2) Run: neuratrade gateway start     # Start all services\n'
  printf '     Or:  neuratrade --help             # Show all commands\n'
  if [[ "$BOOTSTRAP_MODE" == "binary" ]]; then
    printf '  3) Backend: %s --help\n' "$BOOTSTRAP_LOCATION"
  elif [[ "$BOOTSTRAP_MODE" == "alias" ]]; then
    printf '  3) Source alias: source %s\n' "$BOOTSTRAP_LOCATION"
    printf '  4) Backend: %s --help\n' "$BOOTSTRAP_CMD_NAME"
  fi
}

main() {
  parse_args "$@"
  detect_repo_root

  mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"
  TMP_DIR="$(mktemp -d)"
  local tmp_bin="$TMP_DIR/$APP_NAME"
  local binary_installed="false"

  if [[ "$SKIP_BUILD" == "true" ]]; then
    warn "skipping binary build and install"
  else
    install_binary "$tmp_bin"
    install -m 0755 "$tmp_bin" "$INSTALL_DIR/$APP_NAME"
    binary_installed="true"
    
    # Also install the CLI binary
    local tmp_cli_bin="$TMP_DIR/${APP_NAME}-cli"
    install_cli_binary "$tmp_cli_bin"
    install -m 0755 "$tmp_cli_bin" "$INSTALL_DIR/${APP_NAME}-cli"
    log "installed ${APP_NAME}-cli binary to $INSTALL_DIR/${APP_NAME}-cli"
  fi

  create_bootstrap_command

  if [[ -f "$ENV_TARGET" ]]; then
    warn "env template already exists at $ENV_TARGET, leaving as-is"
  else
    write_env_template
  fi

  print_next_steps "$binary_installed"
}

main "$@"
