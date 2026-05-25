#!/usr/bin/env bash
# prepare-env.sh — Create or update .env with auto-generated secrets.
# Safe to run multiple times: only fills in missing values, never overwrites existing ones.
#
# Usage:  ./prepare-env.sh

set -euo pipefail

ENV_FILE=".env"

# --- helpers ---

gen_hex() { openssl rand -hex "$1" 2>/dev/null || head -c "$1" /dev/urandom | xxd -p | tr -d '\n'; }

# Read current value from .env (KEY=VALUE format, no export prefix).
get_env_val() {
  local key="$1"
  if [ -f "$ENV_FILE" ]; then
    grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d'=' -f2-
  fi
}

strip_env_quotes() {
  local val="${1:-}"
  val="${val%$'\r'}"
  if [[ "$val" == \"*\" && "$val" == *\" ]]; then
    val="${val:1:${#val}-2}"
  elif [[ "$val" == \'*\' && "$val" == *\' ]]; then
    val="${val:1:${#val}-2}"
  fi
  printf '%s' "$val"
}

# Set a key in .env. Appends if missing, replaces if empty.
set_env_val() {
  local key="$1" val="$2"
  if [ ! -f "$ENV_FILE" ]; then
    echo "${key}=${val}" >> "$ENV_FILE"
  elif grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
    # Key exists — only replace if current value is empty
    local current
    current="$(get_env_val "$key")"
    if [ -z "$current" ]; then
      if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
      else
        sed -i "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
      fi
    fi
  else
    echo "${key}=${val}" >> "$ENV_FILE"
  fi
}

upsert_env_val() {
  local key="$1" val="$2"
  if [ ! -f "$ENV_FILE" ]; then
    echo "${key}=${val}" >> "$ENV_FILE"
  elif grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
    else
      sed -i "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
    fi
  else
    echo "${key}=${val}" >> "$ENV_FILE"
  fi
}

compose_path_separator() {
  case "$(uname -s 2>/dev/null || echo "")" in
    MINGW*|MSYS*|CYGWIN*) printf ';' ;;
    *) printf ':' ;;
  esac
}

compose_has_item() {
  local needle="$1"
  shift
  local item
  for item in "$@"; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

ensure_compose_file() {
  local sep existing normalized part f
  sep="$(compose_path_separator)"
  existing="$(strip_env_quotes "$(get_env_val COMPOSE_FILE)")"

  # Existing values may come from Linux, Windows, or old prepare-compose.sh runs.
  normalized="${existing//;/$sep}"
  if [ "$sep" = ";" ]; then
    normalized="${normalized//:/$sep}"
  fi

  local compose_files=()
  if [ -n "$normalized" ]; then
    local old_ifs="$IFS"
    IFS="$sep"
    for part in $normalized; do
      [ -n "$part" ] && compose_files+=("$part")
    done
    IFS="$old_ifs"
  fi

  local required=()
  [ -f "docker-compose.yml" ] && required+=("docker-compose.yml")
  [ -f "docker-compose.postgres.yml" ] && required+=("docker-compose.postgres.yml")
  for f in docker-compose.*-mcp.yml; do
    [ -f "$f" ] && required+=("$f")
  done

  for f in "${required[@]}"; do
    if ! compose_has_item "$f" "${compose_files[@]}"; then
      compose_files+=("$f")
    fi
  done

  if [ "${#compose_files[@]}" -gt 0 ]; then
    local joined=""
    for f in "${compose_files[@]}"; do
      joined="${joined}${joined:+$sep}${f}"
    done
    upsert_env_val "COMPOSE_FILE" "$joined"
    echo "  [updated]   COMPOSE_FILE=$joined"
  fi
}

# --- main ---

echo ""
echo "=== GoClaw Environment Preparation ==="
echo ""

# 1. Create .env from .env.example if it doesn't exist
if [ ! -f "$ENV_FILE" ]; then
  if [ -f ".env.example" ]; then
    # Strip 'export ' prefix for Docker Compose compatibility
    sed 's/^export //' .env.example > "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    echo "  [created]   .env from .env.example"
  else
    touch "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    echo "  [created]   .env (empty)"
  fi
else
  echo "  [exists]    .env"
fi

# 2. Auto-generate GOCLAW_ENCRYPTION_KEY if missing
current_enc="$(get_env_val GOCLAW_ENCRYPTION_KEY)"
if [ -z "$current_enc" ]; then
  new_key="$(gen_hex 32)"
  set_env_val "GOCLAW_ENCRYPTION_KEY" "$new_key"
  echo "  [generated] GOCLAW_ENCRYPTION_KEY"
else
  echo "  [exists]    GOCLAW_ENCRYPTION_KEY"
fi

# 3. Auto-generate GOCLAW_GATEWAY_TOKEN if missing
current_tok="$(get_env_val GOCLAW_GATEWAY_TOKEN)"
if [ -z "$current_tok" ]; then
  new_tok="$(gen_hex 16)"
  set_env_val "GOCLAW_GATEWAY_TOKEN" "$new_tok"
  echo "  [generated] GOCLAW_GATEWAY_TOKEN"
else
  echo "  [exists]    GOCLAW_GATEWAY_TOKEN"
fi

# 4. Ensure Docker Compose includes core files and bundled local MCP overlays.
ensure_compose_file

echo ""
echo "=== Done ==="
echo ""
echo "  Run: make up"
echo ""
echo "  Web dashboard: http://localhost:18790"
echo "  With separate nginx: make up WITH_WEB_NGINX=1 → http://localhost:3000"
echo ""
