#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# run-local.sh — interactive dev runner for hetu
# ─────────────────────────────────────────────────────────────────────────────
# Pattern lifted from /home/dk/Documents/git/nginx-manager-cursor/scripts/avk
# every config item is loaded from env/<ENVIRONMENT>.env, shown to the
# operator with its default in [brackets], prompted for, validated, and only
# then used to start services. With --yes the prompts are silently accepted;
# with --non-interactive a missing required value is a fatal.
#
# Usage:
#   scripts/run-local.sh                    # interactive menu
#   scripts/run-local.sh start [-e dev|uat|prod] [--yes] [--non-interactive]
#   scripts/run-local.sh stop
#   scripts/run-local.sh status
#   scripts/run-local.sh restart            # stop → start with existing settings (no prompts)
#   scripts/run-local.sh logs [analyzer|dashboard|collector|all]
#   scripts/run-local.sh build
#   scripts/run-local.sh publish            # docker build + push to REGISTRY
#   scripts/run-local.sh setup [-e dev|uat|prod]   # writes env/<env>.env
#   scripts/run-local.sh doctor                    # pre-flight checks
#   scripts/run-local.sh lint                      # validate env file only
#   scripts/run-local.sh version
#
# Environment overrides (override the env-file values):
#   ENVIRONMENT, ENV_FILE, PROFILE, BIND_ADDRESS, NEXT_HOSTNAME,
#   DASHBOARD_MODE, ANALYZER_PORT, METRICS_PORT, COLLECTOR_PORT,
#   DASHBOARD_PORT, COLLECTOR_URL, LLM_PROVIDER, LLM_ENDPOINT, LLM_MODEL,
#   LLM_API_KEY, MOCK_INTERVAL, ANALYSIS_INTERVAL, EVICT_INTERVAL,
#   KUBECONFIG, LOG_DIR, REGISTRY
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Colors ────────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
  BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BLUE=''; CYAN=''; BOLD=''; DIM=''; NC=''
fi

info()    { echo -e "${BLUE}[info]${NC}  $*" >&2; }
success() { echo -e "${GREEN}[ok]${NC}    $*" >&2; }
warn()    { echo -e "${YELLOW}[warn]${NC}  $*" >&2; }
error()   { echo -e "${RED}[error]${NC} $*" >&2; }
header()  { echo -e "\n${BOLD}${CYAN}$*${NC}\n" >&2; }
fatal()   { error "$*"; exit 1; }

# ── Globals (defaults; overridden by env file → cli) ─────────────────────────
PROMPT_RESULT=""
YES=0                    # --yes accepts every default silently
NON_INTERACTIVE=0        # --non-interactive: a required prompt aborts
LOG_DIR="${LOG_DIR:-/tmp/hetu-logs}"
mkdir -p "$LOG_DIR"

ANALYZER_PIDFILE="$LOG_DIR/analyzer.pid"
DASHBOARD_PIDFILE="$LOG_DIR/dashboard.pid"
COLLECTOR_PIDFILE="$LOG_DIR/collector.pid"

# ── Prompt helpers (avk pattern) ─────────────────────────────────────────────
# All prompts read from /dev/tty so a piped invocation doesn't accidentally
# consume the next program's stdin. If /dev/tty isn't openable for read (cron,
# subprocess, claude background tasks, etc.) we promote --yes silently so the
# script behaves like a CI run instead of crashing.
TTY_AVAILABLE=1
if ! { : <"/dev/tty"; } 2>/dev/null; then
  TTY_AVAILABLE=0
  YES=1   # implicit non-interactive mode when there's no tty to prompt on
fi

_prompt_input() {
  local label="$1" default="${2:-}"
  local value=""
  : # tty checked at startup
  if (( YES )); then
    PROMPT_RESULT="$default"
    return
  fi
  if (( NON_INTERACTIVE )); then
    [[ -n "$default" ]] || fatal "non-interactive: required value '$label' missing"
    PROMPT_RESULT="$default"
    return
  fi
  echo -en "${DIM}  ${label} [${default}]:${NC} " >&2
  read -r value </dev/tty 2>/dev/null || value=""
  PROMPT_RESULT="${value:-$default}"
}

_prompt_secret() {
  local label="$1" default="${2:-}"
  local value=""
  : # tty checked at startup
  if (( YES )); then PROMPT_RESULT="$default"; return; fi
  if (( NON_INTERACTIVE )); then PROMPT_RESULT="$default"; return; fi
  if [[ -n "$default" ]]; then
    echo -en "${DIM}  ${label} [<existing>]:${NC} " >&2
  else
    echo -en "${DIM}  ${label} (empty for none):${NC} " >&2
  fi
  read -rs value </dev/tty 2>/dev/null || value=""
  echo "" >&2
  PROMPT_RESULT="${value:-$default}"
}

_prompt_select() {
  local label="$1"; shift
  local default="$1"; shift
  local options=("$@")
  : # tty checked at startup
  if (( YES )) || (( NON_INTERACTIVE )); then
    PROMPT_RESULT="$default"
    return
  fi
  echo -e "\n${BOLD}${label}${NC} ${DIM}[default: ${default}]${NC}" >&2
  local i
  for i in "${!options[@]}"; do
    local marker=" "
    [[ "${options[$i]}" == "$default" ]] && marker="*"
    echo -e "  ${CYAN}$((i+1)))${NC}${marker} ${options[$i]}" >&2
  done
  local choice=""
  while true; do
    echo -en "${DIM}  Select [1-${#options[@]}, enter for default]:${NC} " >&2
    read -r choice </dev/tty 2>/dev/null || choice=""
    if [[ -z "$choice" ]]; then
      PROMPT_RESULT="$default"; return
    fi
    if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= ${#options[@]} )); then
      PROMPT_RESULT="${options[$((choice-1))]}"; return
    fi
    error "  Invalid choice. Try again."
  done
}

prompt_confirm() {
  local prompt="$1" default="${2:-N}"
  : # tty checked at startup
  if (( YES )); then
    [[ "$default" =~ ^[Yy]$ ]]
    return
  fi
  if (( NON_INTERACTIVE )); then
    [[ "$default" =~ ^[Yy]$ ]]
    return
  fi
  local hint reply=""
  if [[ "$default" =~ ^[Yy]$ ]]; then hint="Y/n"; else hint="y/N"; fi
  echo -en "${BOLD}  ${prompt} (${hint}):${NC} " >&2
  read -r reply </dev/tty 2>/dev/null || reply=""
  reply="${reply:-$default}"
  [[ "$reply" =~ ^[Yy]$ ]]
}

# ── Validators ───────────────────────────────────────────────────────────────
validate_int() {
  local name="$1" value="$2"
  [[ "$value" =~ ^[0-9]+$ ]] || { error "$name must be an integer (got: '$value')"; return 1; }
}

validate_port() {
  local name="$1" value="$2"
  validate_int "$name" "$value" || return 1
  (( value >= 1 && value <= 65535 )) || { error "$name must be 1..65535 (got: $value)"; return 1; }
}

validate_port_free() {
  # Hard-fail if a port is already in use — protects against silent process collisions.
  local name="$1" value="$2"
  validate_port "$name" "$value" || return 1
  if ss -tlnH "sport = :$value" 2>/dev/null | grep -q .; then
    local owner
    owner="$(ss -tlnpH "sport = :$value" 2>/dev/null | head -1)"
    error "$name :$value is already bound — listener: $owner"
    return 1
  fi
}

validate_url_syntax() {
  local name="$1" value="$2"
  [[ "$value" =~ ^https?:// ]] || { error "$name must start with http:// or https:// (got: '$value')"; return 1; }
}

validate_url_reachable() {
  # Soft-warn: network may be down at start time. Continue but flag it.
  local name="$1" value="$2"
  if ! curl -sI --max-time 5 "$value" >/dev/null 2>&1; then
    warn "$name '$value' is not reachable right now (continuing)"
  fi
}

# normalize_duration — accept a bare integer and append the default
# unit. Operators routinely type "30" instead of "30s"; rejecting them
# with "must be a Go duration" is hostile. We just append the unit.
# Already-suffixed values are left untouched.
#
#   normalize_duration ANALYSIS_INTERVAL s
#   # if value was "30", it's now "30s"
normalize_duration() {
  local var="$1" default_unit="$2"
  local value="${!var}"
  [[ -z "$value" ]] && return 0
  if [[ "$value" =~ ^[0-9]+$ ]]; then
    printf -v "$var" '%s%s' "$value" "$default_unit"
    info "  $var → ${!var} (assumed ${default_unit})"
  fi
}

# normalize_url — auto-prepend http:// when no scheme present.
# Operators routinely type "192.168.1.10:11434" or "myhost.local:8080";
# we should accept that and not bounce them with "must start with http://".
# Variable name is passed by reference so the caller's value is updated.
#
#   normalize_url LLM_ENDPOINT
#   # if LLM_ENDPOINT was "192.168.1.10:11434", it's now "http://192.168.1.10:11434"
normalize_url() {
  local var="$1"
  local value="${!var}"
  [[ -z "$value" ]] && return 0
  if [[ ! "$value" =~ ^https?:// ]]; then
    # Strip any leading "//" some users add by reflex.
    value="${value#//}"
    value="http://$value"
    printf -v "$var" '%s' "$value"
    info "  $var → $value (auto-prepended http://)"
  fi
}

# discover_models — query the LLM endpoint and emit the model list, one
# per line, on stdout. Empty output means discovery wasn't possible
# (network down, wrong endpoint, unsupported provider). Stderr is silent
# so callers can use $(discover_models …) cleanly.
#
#   discover_models <provider> <endpoint> [api_key]
discover_models() {
  local provider="$1" endpoint="$2" api_key="${3:-}"
  local url body
  case "$provider" in
    ollama)
      # Ollama: GET /api/tags → {"models":[{"name":"…"}, ...]}
      url="${endpoint%/}/api/tags"
      body="$(curl -s --max-time 5 "$url" 2>/dev/null || true)"
      ;;
    openai|anthropic|vllm|llamacpp|azure|bedrock)
      # OpenAI-compatible: GET /v1/models → {"data":[{"id":"…"}, ...]}
      # Some endpoints already include /v1 in the path; tolerate both.
      local base="${endpoint%/}"
      [[ "$base" == */v1 ]] || base="$base/v1"
      url="$base/models"
      local hdr=()
      [[ -n "$api_key" ]] && hdr=(-H "Authorization: Bearer $api_key")
      body="$(curl -s --max-time 5 "${hdr[@]}" "$url" 2>/dev/null || true)"
      ;;
    *)
      return 0   # unknown provider, no discovery
      ;;
  esac

  [[ -z "$body" ]] && return 0
  # Parse with python3 (already required by the Next.js dashboard build).
  # Tolerate either schema (Ollama .models[].name OR OpenAI .data[].id);
  # deduplicate so an endpoint that returns the same model under both
  # keys (some llama.cpp builds do) doesn't emit it twice.
  if command -v python3 >/dev/null 2>&1; then
    python3 - <<PY 2>/dev/null
import json, sys
try:
    d = json.loads(${body@Q})
except Exception:
    sys.exit(0)
names, seen = [], set()
def add(n):
    if n and n not in seen:
        seen.add(n); names.append(n)
if isinstance(d, dict):
    for entry in d.get("models", []) or []:
        add(entry.get("name") or entry.get("model"))
    for entry in d.get("data", []) or []:
        add(entry.get("id") or entry.get("name"))
print("\n".join(names))
PY
  fi
}

validate_choice() {
  local name="$1" value="$2"; shift 2
  for opt in "$@"; do
    [[ "$value" == "$opt" ]] && return 0
  done
  error "$name must be one of: $*  (got: '$value')"
  return 1
}

validate_duration() {
  # Go time.ParseDuration: digits + unit (s|m|h), or composite like 1h30m.
  local name="$1" value="$2"
  [[ "$value" =~ ^([0-9]+(ns|us|µs|ms|s|m|h))+$ ]] || {
    error "$name must be a Go duration like 30s, 5m, 1h30m (got: '$value')"
    return 1
  }
}

validate_kubeconfig() {
  # Only invoked when PROFILE=live.
  local kc="$1"
  [[ -f "$kc" ]] || { error "KUBECONFIG '$kc' does not exist"; return 1; }
  if ! kubectl --kubeconfig="$kc" cluster-info --request-timeout=5s >/dev/null 2>&1; then
    error "kubectl cluster-info failed using KUBECONFIG=$kc"
    return 1
  fi
}

# ── Env file loader ──────────────────────────────────────────────────────────
ENVIRONMENT="${ENVIRONMENT:-dev}"
ENV_FILE="${ENV_FILE:-}"

resolve_env_file() {
  if [[ -z "${ENV_FILE:-}" ]]; then
    ENV_FILE="$REPO_ROOT/env/$ENVIRONMENT.env"
  fi
  if [[ ! -f "$ENV_FILE" ]]; then
    warn "Env file not found: $ENV_FILE"
    if [[ "$ENVIRONMENT" == "prod" && -f "$REPO_ROOT/env/prod.env.example" ]]; then
      info "Hint: copy env/prod.env.example to env/prod.env and edit it (env/prod.env is gitignored)."
    fi
    if (( YES )) || (( NON_INTERACTIVE )); then
      fatal "env file required for non-interactive mode"
    fi
    if ! prompt_confirm "Continue without an env file (use built-in defaults)?" "N"; then
      fatal "aborted"
    fi
    ENV_FILE=""
  fi
}

# Vars whose CLI/parent-env value should win over any env-file value.
# Snapshot before sourcing, restore after, so precedence is:
#   CLI env  >  env file  >  built-in default
ENV_FILE_VARS=(
  PROFILE BIND_ADDRESS NEXT_HOSTNAME DASHBOARD_MODE
  ANALYZER_PORT METRICS_PORT DASHBOARD_PORT COLLECTOR_PORT COLLECTOR_METRICS_PORT
  COLLECTOR_URL
  LLM_PROVIDER LLM_ENDPOINT LLM_MODEL LLM_API_KEY
  MOCK_INTERVAL ANALYSIS_INTERVAL EVICT_INTERVAL
  KUBECONFIG
  REGISTRY
)

load_env_file() {
  if [[ -z "$ENV_FILE" ]]; then return; fi
  info "Loading env file: $ENV_FILE"

  # Snapshot which vars were already set by the parent process / CLI.
  # We do this BEFORE sourcing so we can restore them after — env-file
  # values must NEVER override values the operator explicitly set.
  declare -A __preset
  local v
  for v in "${ENV_FILE_VARS[@]}"; do
    if [[ -n "${!v+set}" ]]; then
      __preset[$v]="${!v}"
    fi
  done

  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a

  # Restore parent-set values, overriding whatever the file just set.
  for v in "${!__preset[@]}"; do
    printf -v "$v" '%s' "${__preset[$v]}"
    export "$v"
  done
}

# ── Built-in defaults (used when env file is absent or a key is unset) ───────
apply_defaults() {
  PROFILE="${PROFILE:-mock}"
  BIND_ADDRESS="${BIND_ADDRESS:-0.0.0.0}"
  NEXT_HOSTNAME="${NEXT_HOSTNAME:-0.0.0.0}"
  DASHBOARD_MODE="${DASHBOARD_MODE:-dev}"
  ANALYZER_PORT="${ANALYZER_PORT:-18081}"
  METRICS_PORT="${METRICS_PORT:-19091}"
  COLLECTOR_PORT="${COLLECTOR_PORT:-18080}"
  COLLECTOR_METRICS_PORT="${COLLECTOR_METRICS_PORT:-19090}"
  DASHBOARD_PORT="${DASHBOARD_PORT:-3003}"
  # Sensible default: the local collector on its known port. Operators
  # who run the collector elsewhere (in-cluster service, remote host) can
  # override via env file or CLI.
  COLLECTOR_URL="${COLLECTOR_URL:-http://localhost:${COLLECTOR_PORT}}"
  LLM_PROVIDER="${LLM_PROVIDER:-ollama}"
  LLM_ENDPOINT="${LLM_ENDPOINT:-http://localhost:11434}"
  LLM_MODEL="${LLM_MODEL:-llama3}"
  LLM_API_KEY="${LLM_API_KEY:-}"
  MOCK_INTERVAL="${MOCK_INTERVAL:-20s}"
  ANALYSIS_INTERVAL="${ANALYSIS_INTERVAL:-30s}"
  EVICT_INTERVAL="${EVICT_INTERVAL:-30s}"
  KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
  REGISTRY="${REGISTRY:-}"
}

# ── Interactive collectors ───────────────────────────────────────────────────
prompt_until_valid() {
  # Args: validator_fn, prompt_label, default, [extra args to validator]
  local fn="$1" label="$2" default="$3"; shift 3
  while true; do
    _prompt_input "$label" "$default"
    if "$fn" "$label" "$PROMPT_RESULT" "$@"; then
      eval "$4=\"\$PROMPT_RESULT\"" 2>/dev/null || true
      return 0
    fi
    (( NON_INTERACTIVE )) && fatal "validation failed in non-interactive mode"
    (( YES )) && fatal "validation failed for default value of $label"
  done
}

collect_runtime_config() {
  header "Runtime configuration"

  _prompt_select "PROFILE" "$PROFILE" "live" "mock"
  PROFILE="$PROMPT_RESULT"

  while true; do
    _prompt_input "BIND_ADDRESS (interface for analyzer/collector)" "$BIND_ADDRESS"
    BIND_ADDRESS="$PROMPT_RESULT"
    [[ -n "$BIND_ADDRESS" ]] && break
    error "BIND_ADDRESS may not be empty"
  done

  _prompt_input "NEXT_HOSTNAME (interface for next dev/start)" "$NEXT_HOSTNAME"
  NEXT_HOSTNAME="$PROMPT_RESULT"

  while true; do
    _prompt_select "DASHBOARD_MODE" "$DASHBOARD_MODE" "dev" "prod"
    DASHBOARD_MODE="$PROMPT_RESULT"
    validate_choice DASHBOARD_MODE "$DASHBOARD_MODE" dev prod && break
  done
}

collect_ports() {
  header "Ports"

  for var in ANALYZER_PORT METRICS_PORT DASHBOARD_PORT COLLECTOR_PORT COLLECTOR_METRICS_PORT; do
    while true; do
      _prompt_input "$var" "${!var}"
      printf -v "$var" '%s' "$PROMPT_RESULT"
      validate_port "$var" "${!var}" && break
    done
  done

  # Port-collision check is a separate pass so we can call it again post-edit.
  validate_ports_free
}

validate_ports_free() {
  local var
  for var in ANALYZER_PORT METRICS_PORT DASHBOARD_PORT COLLECTOR_PORT COLLECTOR_METRICS_PORT; do
    validate_port_free "$var" "${!var}" || fatal "port collision — change $var or stop the listener"
  done
}

collect_collector() {
  # Always prompt — even in PROFILE=mock the operator may flip to live
  # later via the dashboard Settings, and we want the value persisted to
  # the env file. In mock mode it's "optional"; in live mode "required".
  header "Collector"
  local label
  if [[ "$PROFILE" == "live" ]]; then
    label="COLLECTOR_URL (required for live profile; scheme optional, e.g. host:8080)"
  else
    label="COLLECTOR_URL (optional in mock; needed when switching to live)"
  fi
  while true; do
    _prompt_input "$label" "$COLLECTOR_URL"
    COLLECTOR_URL="$PROMPT_RESULT"
    if [[ -z "$COLLECTOR_URL" ]]; then
      if [[ "$PROFILE" == "live" ]]; then
        warn "COLLECTOR_URL is empty — analyzer will report 'collector unreachable' until you set one in the dashboard Settings"
      fi
      break
    fi
    normalize_url COLLECTOR_URL
    validate_url_syntax COLLECTOR_URL "$COLLECTOR_URL" && {
      validate_url_reachable COLLECTOR_URL "$COLLECTOR_URL"
      break
    }
  done
}

collect_llm() {
  header "LLM"

  while true; do
    _prompt_select "LLM_PROVIDER" "$LLM_PROVIDER" \
      "ollama" "openai" "anthropic" "vllm" "llamacpp" "azure" "bedrock" "none"
    LLM_PROVIDER="$PROMPT_RESULT"
    validate_choice LLM_PROVIDER "$LLM_PROVIDER" ollama openai anthropic vllm llamacpp azure bedrock none && break
  done

  if [[ "$LLM_PROVIDER" == "none" ]]; then
    info "LLM_PROVIDER=none — skipping endpoint, model, key prompts"
    LLM_ENDPOINT=""; LLM_MODEL=""; LLM_API_KEY=""
    return
  fi

  while true; do
    _prompt_input "LLM_ENDPOINT (scheme optional, e.g. host:11434)" "$LLM_ENDPOINT"
    LLM_ENDPOINT="$PROMPT_RESULT"
    if [[ -z "$LLM_ENDPOINT" ]]; then
      error "LLM_ENDPOINT may not be empty (use provider=none to disable LLM)"
      continue
    fi
    normalize_url LLM_ENDPOINT
    validate_url_syntax LLM_ENDPOINT "$LLM_ENDPOINT" && {
      validate_url_reachable LLM_ENDPOINT "$LLM_ENDPOINT"
      break
    }
  done

  # Ask for the API key BEFORE model discovery so OpenAI-compatible
  # endpoints can authenticate the /v1/models request.
  case "$LLM_PROVIDER" in
    ollama|llamacpp|none)
      info "LLM_PROVIDER=$LLM_PROVIDER — API key not required"
      ;;
    *)
      _prompt_secret "LLM_API_KEY" "$LLM_API_KEY"
      LLM_API_KEY="$PROMPT_RESULT"
      [[ -z "$LLM_API_KEY" ]] && warn "LLM_API_KEY is empty — provider $LLM_PROVIDER will likely 401"
      ;;
  esac

  # Smart Router: query the endpoint for available models and offer them
  # as a picker. Falls back to free-text input if discovery fails (network
  # down, unsupported provider, missing API key for /v1/models, etc.).
  local discovered=""
  if (( ! YES )) && (( ! NON_INTERACTIVE )); then
    info "Discovering models at $LLM_ENDPOINT…"
    discovered="$(discover_models "$LLM_PROVIDER" "$LLM_ENDPOINT" "$LLM_API_KEY")"
  fi

  if [[ -n "$discovered" ]]; then
    # Build the option list from discovered models ONLY — do not mix in
    # a stale env-file LLM_MODEL that the endpoint doesn't actually
    # host. If the configured model is missing, warn and fall back to
    # the first discovered model as the default.
    local options=()
    while IFS= read -r m; do
      [[ -z "$m" ]] && continue
      options+=("$m")
    done <<< "$discovered"

    local default_model=""
    if [[ -n "$LLM_MODEL" ]]; then
      local m
      for m in "${options[@]}"; do
        if [[ "$m" == "$LLM_MODEL" ]]; then
          default_model="$LLM_MODEL"
          break
        fi
      done
    fi
    if [[ -z "$default_model" ]]; then
      default_model="${options[0]:-}"
      if [[ -n "$LLM_MODEL" ]]; then
        warn "Configured LLM_MODEL '$LLM_MODEL' not present at endpoint — defaulting to '$default_model'"
      fi
    fi

    success "Found ${#options[@]} model(s) at endpoint"
    _prompt_select "LLM_MODEL" "$default_model" "${options[@]}"
    LLM_MODEL="$PROMPT_RESULT"
  else
    while true; do
      _prompt_input "LLM_MODEL" "$LLM_MODEL"
      LLM_MODEL="$PROMPT_RESULT"
      [[ -n "$LLM_MODEL" ]] && break
      error "LLM_MODEL may not be empty"
    done
  fi
}

collect_intervals() {
  header "Intervals"
  # All three intervals are short (defaults: 20s, 30s, 30s) so seconds
  # is the natural unit to assume when the operator types a bare number.
  # Suffix-bearing values (5m, 1h30m) still work unchanged.
  for var in ANALYSIS_INTERVAL EVICT_INTERVAL MOCK_INTERVAL; do
    while true; do
      _prompt_input "$var (default unit: seconds; e.g. 30 or 30s or 5m)" "${!var}"
      printf -v "$var" '%s' "$PROMPT_RESULT"
      normalize_duration "$var" s
      validate_duration "$var" "${!var}" && break
    done
  done
}

collect_kubeconfig() {
  if [[ "$PROFILE" != "live" ]]; then return; fi
  header "Kubernetes access (live profile)"
  while true; do
    _prompt_input "KUBECONFIG" "$KUBECONFIG"
    KUBECONFIG="$PROMPT_RESULT"
    if validate_kubeconfig "$KUBECONFIG"; then
      success "kubectl reaches the cluster"
      break
    fi
    (( NON_INTERACTIVE )) && fatal "kubeconfig validation failed"
  done
}

render_summary() {
  header "Summary"
  cat >&2 <<EOF
  ENVIRONMENT      = $ENVIRONMENT
  PROFILE          = $PROFILE
  BIND_ADDRESS     = $BIND_ADDRESS
  NEXT_HOSTNAME    = $NEXT_HOSTNAME
  DASHBOARD_MODE   = $DASHBOARD_MODE
  ANALYZER_PORT    = $ANALYZER_PORT
  METRICS_PORT     = $METRICS_PORT
  DASHBOARD_PORT   = $DASHBOARD_PORT
  COLLECTOR_PORT   = $COLLECTOR_PORT
  COLLECTOR_URL    = ${COLLECTOR_URL:-(empty)}
  LLM_PROVIDER     = $LLM_PROVIDER
  LLM_ENDPOINT     = ${LLM_ENDPOINT:-(none)}
  LLM_MODEL        = ${LLM_MODEL:-(none)}
  LLM_API_KEY      = $([[ -n "$LLM_API_KEY" ]] && echo '<set>' || echo '<empty>')
  ANALYSIS_INTERVAL= $ANALYSIS_INTERVAL
  EVICT_INTERVAL   = $EVICT_INTERVAL
  MOCK_INTERVAL    = $MOCK_INTERVAL
EOF
  if [[ "$PROFILE" == "live" ]]; then
    echo "  KUBECONFIG       = $KUBECONFIG" >&2
  fi
  echo "" >&2
  prompt_confirm "Proceed with these values?" "Y" || fatal "aborted by operator"
}

# ── Service control ──────────────────────────────────────────────────────────
# Returns 0 if any .go file under $1 is newer than $2 (or $2 missing).
# Using `find -newer` catches changes to every source file, not just main.go
# — editing workload.go etc. without this would silently run stale code.
_any_go_newer_than() {
  local src_dir="$1" bin="$2"
  [[ ! -x "$bin" ]] && return 0
  [[ -n "$(find "$src_dir" -name '*.go' -newer "$bin" -print -quit 2>/dev/null)" ]]
}

build_analyzer_if_stale() {
  local bin="$REPO_ROOT/bin/analyzer"
  if _any_go_newer_than "$REPO_ROOT/src/analyzer" "$bin"; then
    info "Building analyzer..."
    (cd "$REPO_ROOT/src/analyzer" && go build -o "$bin" ./...) || fatal "analyzer build failed"
    success "analyzer built"
  fi
}

build_collector_if_stale() {
  local bin="$REPO_ROOT/bin/collector"
  if _any_go_newer_than "$REPO_ROOT/src/collector" "$bin"; then
    info "Building collector..."
    (cd "$REPO_ROOT/src/collector" && go build -o "$bin" ./...) || fatal "collector build failed"
    success "collector built"
  fi
}

# Returns 0 when COLLECTOR_URL points at a local loopback address — which
# means this script is responsible for starting the collector. A non-local
# URL means the operator is pointing at an in-cluster service or remote
# host; we must NOT try to start a collector on their behalf.
collector_is_local() {
  [[ "$COLLECTOR_URL" =~ ^https?://(localhost|127\.0\.0\.1|0\.0\.0\.0)(:|/|$) ]]
}

wait_for_http() {
  local url="$1" name="$2" max="${3:-20}" attempt
  for attempt in $(seq 1 "$max"); do
    if curl -s -o /dev/null --max-time 2 "$url"; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

start_collector() {
  # Only start a local collector in live mode AND when COLLECTOR_URL points
  # at loopback. Mock profile doesn't need one; a remote/in-cluster URL
  # means the operator runs it elsewhere.
  if [[ "$PROFILE" != "live" ]]; then
    info "PROFILE=$PROFILE — skipping collector start (not needed for mock)"
    return 0
  fi
  if ! collector_is_local; then
    info "COLLECTOR_URL=$COLLECTOR_URL is remote — not starting a local collector"
    return 0
  fi

  if [[ -f "$COLLECTOR_PIDFILE" ]] && kill -0 "$(cat "$COLLECTOR_PIDFILE")" 2>/dev/null; then
    fatal "Collector already running (PID $(cat "$COLLECTOR_PIDFILE")). Use 'stop' first."
  fi
  build_collector_if_stale

  info "Starting collector on $BIND_ADDRESS:$COLLECTOR_PORT (metrics $COLLECTOR_METRICS_PORT)..."
  BIND_ADDRESS="$BIND_ADDRESS" \
    HEALTH_PORT="$COLLECTOR_PORT" \
    METRICS_PORT="$COLLECTOR_METRICS_PORT" \
    KUBECONFIG="$KUBECONFIG" \
    CLUSTER_ID="${CLUSTER_ID:-default}" \
    "$REPO_ROOT/bin/collector" > "$LOG_DIR/collector.log" 2>&1 &
  echo $! > "$COLLECTOR_PIDFILE"

  if ! wait_for_http "http://localhost:$COLLECTOR_PORT/healthz" "collector" 30; then
    error "Collector failed to become healthy. Last log lines:"
    tail -20 "$LOG_DIR/collector.log" >&2 || true
    fatal "abort"
  fi
  success "Collector ready (PID $(cat "$COLLECTOR_PIDFILE"))"
}

start_analyzer() {
  if [[ -f "$ANALYZER_PIDFILE" ]] && kill -0 "$(cat "$ANALYZER_PIDFILE")" 2>/dev/null; then
    fatal "Analyzer already running (PID $(cat "$ANALYZER_PIDFILE")). Use 'stop' first."
  fi
  build_analyzer_if_stale

  info "Starting analyzer on $BIND_ADDRESS:$ANALYZER_PORT (profile=$PROFILE, provider=$LLM_PROVIDER)..."
  BIND_ADDRESS="$BIND_ADDRESS" \
    API_PORT="$ANALYZER_PORT" \
    METRICS_PORT="$METRICS_PORT" \
    PROFILE="$PROFILE" \
    LLM_PROVIDER="$LLM_PROVIDER" \
    LLM_ENDPOINT="$LLM_ENDPOINT" \
    LLM_MODEL="$LLM_MODEL" \
    LLM_API_KEY="$LLM_API_KEY" \
    COLLECTOR_URL="$COLLECTOR_URL" \
    MOCK_INTERVAL="$MOCK_INTERVAL" \
    ANALYSIS_INTERVAL="$ANALYSIS_INTERVAL" \
    EVICT_INTERVAL="$EVICT_INTERVAL" \
    KUBECONFIG="$KUBECONFIG" \
    "$REPO_ROOT/bin/analyzer" > "$LOG_DIR/analyzer.log" 2>&1 &
  echo $! > "$ANALYZER_PIDFILE"

  if ! wait_for_http "http://localhost:$ANALYZER_PORT/healthz" "analyzer" 30; then
    error "Analyzer failed to become healthy. Last log lines:"
    tail -20 "$LOG_DIR/analyzer.log" >&2 || true
    fatal "abort"
  fi
  success "Analyzer ready (PID $(cat "$ANALYZER_PIDFILE"))"
}

start_dashboard() {
  if [[ -f "$DASHBOARD_PIDFILE" ]] && kill -0 "$(cat "$DASHBOARD_PIDFILE")" 2>/dev/null; then
    fatal "Dashboard already running (PID $(cat "$DASHBOARD_PIDFILE"))."
  fi
  info "Starting dashboard on $NEXT_HOSTNAME:$DASHBOARD_PORT ($DASHBOARD_MODE mode)..."

  # In prod mode build first (synchronously, no PID racing).
  if [[ "$DASHBOARD_MODE" == "prod" ]]; then
    info "Building dashboard (next build)…"
    (cd "$REPO_ROOT/src/dashboard" && npm run build >/dev/null) || fatal "next build failed"
  fi

  # Run next under setsid so it gets its own process group; we record the
  # group leader PID and tear the whole group down on stop. Without this,
  # `npx next dev` forks node → next-server which gets re-parented to init
  # and survives a plain `kill <wrapper-pid>`.
  local next_cmd
  if [[ "$DASHBOARD_MODE" == "prod" ]]; then
    next_cmd=(npx next start -H "$NEXT_HOSTNAME" -p "$DASHBOARD_PORT")
  else
    next_cmd=(npx next dev --hostname "$NEXT_HOSTNAME" -p "$DASHBOARD_PORT")
  fi

  (
    cd "$REPO_ROOT/src/dashboard"
    export ANALYZER_URL="http://localhost:$ANALYZER_PORT"
    export PORT="$DASHBOARD_PORT"
    export NEXT_HOSTNAME="$NEXT_HOSTNAME"
    export DASHBOARD_MODE="$DASHBOARD_MODE"
    exec setsid "${next_cmd[@]}"
  ) > "$LOG_DIR/dashboard.log" 2>&1 &
  local pid=$!
  echo "$pid" > "$DASHBOARD_PIDFILE"

  if ! wait_for_http "http://localhost:$DASHBOARD_PORT/" "dashboard" 60; then
    error "Dashboard failed to become reachable. Last log lines:"
    tail -20 "$LOG_DIR/dashboard.log" >&2 || true
    fatal "abort"
  fi
  success "Dashboard ready (PID $pid, group leader)"
}

# Kill PID and every descendant (process tree). Used so we don't orphan
# child processes (npx → node → next-server) that have been re-parented
# to init when the immediate child exits.
kill_tree() {
  local pid="$1" sig="${2:-TERM}"
  # Recurse into children FIRST, then kill the parent.
  local children
  children="$(pgrep -P "$pid" 2>/dev/null || true)"
  local c
  for c in $children; do
    kill_tree "$c" "$sig"
  done
  kill -"$sig" "$pid" 2>/dev/null || true
}

stop_pidfile() {
  local pidfile="$1" label="$2"
  if [[ ! -f "$pidfile" ]]; then return; fi
  local pid
  pid="$(cat "$pidfile")"
  rm -f "$pidfile"

  if ! kill -0 "$pid" 2>/dev/null; then
    info "$label PID $pid already gone"
    return
  fi

  info "Stopping $label (PID $pid + descendants)"
  # 1) Try to nuke the whole process group (setsid'd processes — dashboard).
  #    A negative pid in `kill` means "process group". We send SIGTERM, wait,
  #    then SIGKILL anything still alive.
  kill -TERM -- -"$pid" 2>/dev/null || true
  # 2) Belt-and-braces: also walk the process tree (analyzer, which is not
  #    in its own session, has no group of its own).
  kill_tree "$pid" TERM
  sleep 1

  if kill -0 "$pid" 2>/dev/null; then
    warn "$label PID $pid did not exit on TERM, sending KILL"
    kill -KILL -- -"$pid" 2>/dev/null || true
    kill_tree "$pid" KILL
  fi
}

# ── Sub-commands ─────────────────────────────────────────────────────────────
cmd_start() {
  resolve_env_file
  load_env_file
  apply_defaults

  collect_runtime_config
  collect_ports
  collect_collector
  collect_llm
  collect_intervals
  collect_kubeconfig
  render_summary

  start_collector
  start_analyzer
  start_dashboard

  echo "" >&2
  success "Stack ready"
  echo "  dashboard: http://localhost:$DASHBOARD_PORT/" >&2
  echo "  analyzer : http://localhost:$ANALYZER_PORT/" >&2
  if [[ "$PROFILE" == "live" ]] && collector_is_local; then
    echo "  collector: http://localhost:$COLLECTOR_PORT/healthz" >&2
  fi
  echo "  logs     : scripts/run-local.sh logs" >&2
}

cmd_stop() {
  stop_pidfile "$DASHBOARD_PIDFILE" "dashboard"
  stop_pidfile "$ANALYZER_PIDFILE"  "analyzer"
  stop_pidfile "$COLLECTOR_PIDFILE" "collector"
  success "Stopped"
}

cmd_status() {
  resolve_env_file 2>/dev/null || true
  load_env_file 2>/dev/null || true
  apply_defaults

  printf '%-12s %-10s %-8s %-7s %s\n'   "SERVICE" "PIDFILE" "PID" "HTTP" "URL" >&2
  printf '%-12s %-10s %-8s %-7s %s\n'   "-------" "-------" "---" "----" "---" >&2

  for triple in "analyzer:$ANALYZER_PIDFILE:$ANALYZER_PORT:healthz" \
                "dashboard:$DASHBOARD_PIDFILE:$DASHBOARD_PORT:" \
                "collector:$COLLECTOR_PIDFILE:$COLLECTOR_PORT:healthz"; do
    IFS=: read -r name pidfile port probe <<< "$triple"
    local pidfile_state pid http
    if [[ -f "$pidfile" ]]; then
      pidfile_state="$(basename "$pidfile")"
      pid="$(cat "$pidfile")"
      if ! kill -0 "$pid" 2>/dev/null; then
        pid="(stale)"
        http="-"
      else
        http="$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://localhost:$port/$probe" 2>/dev/null || echo "x")"
      fi
    else
      pidfile_state="-"; pid="-"; http="-"
    fi
    printf '%-12s %-10s %-8s %-7s %s\n' "$name" "$pidfile_state" "$pid" "$http" "http://localhost:$port" >&2
  done
}

cmd_restart() {
  cmd_stop
  # Restart re-uses whatever is already in the env file — no re-prompting needed.
  YES=1
  cmd_start "$@"
}

cmd_logs() {
  local target="${1:-all}"
  case "$target" in
    analyzer)  tail -F "$LOG_DIR/analyzer.log" ;;
    dashboard) tail -F "$LOG_DIR/dashboard.log" ;;
    collector) tail -F "$LOG_DIR/collector.log" 2>/dev/null || fatal "no collector log yet" ;;
    all)       tail -F "$LOG_DIR/analyzer.log" "$LOG_DIR/dashboard.log" 2>/dev/null ;;
    *)         fatal "unknown target: $target (use analyzer|dashboard|collector|all)" ;;
  esac
}

cmd_build() {
  build_analyzer_if_stale
  build_collector_if_stale
}

cmd_publish() {
  resolve_env_file
  load_env_file
  apply_defaults

  command -v docker >/dev/null 2>&1 || fatal "docker not found — install Docker to build and push images"

  # Resolve registry — required; prompt if not already set.
  if [[ -z "${REGISTRY:-}" ]]; then
    if (( YES )) || (( NON_INTERACTIVE )); then
      fatal "REGISTRY is not set — add REGISTRY=registry.example.com/myorg to your env file or export it"
    fi
    _prompt_input "REGISTRY (e.g. ghcr.io/myorg or docker.io/myuser)" ""
    REGISTRY="${PROMPT_RESULT%/}"
    [[ -n "$REGISTRY" ]] || fatal "REGISTRY may not be empty"
  fi
  REGISTRY="${REGISTRY%/}"

  # Default tag: short git SHA, falls back to "latest" in a dirty tree.
  local default_tag
  default_tag="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo latest)"
  _prompt_input "IMAGE_TAG" "$default_tag"
  local tag="$PROMPT_RESULT"
  [[ -n "$tag" ]] || tag="$default_tag"

  header "Building and publishing images"
  info "Registry : $REGISTRY"
  info "Tag      : $tag"
  echo "" >&2

  # Each entry: image-suffix | build-context | dockerfile (relative to context)
  # Mirrors the build: sections in docker-compose.yml exactly.
  local -a NAMES=( analyzer     collector               dashboard )
  local -a CONTEXTS=( "$REPO_ROOT" "$REPO_ROOT"         "$REPO_ROOT/src/dashboard" )
  local -a DOCKERFILES=( "src/analyzer/Dockerfile" "src/collector/Dockerfile" "Dockerfile" )

  local failed=0
  local i
  for i in "${!NAMES[@]}"; do
    local name="${NAMES[$i]}"
    local ctx="${CONTEXTS[$i]}"
    local dfile="${DOCKERFILES[$i]}"
    local image="$REGISTRY/hetu-$name"

    info "[$((i+1))/${#NAMES[@]}] Building $image:$tag …"
    if ! docker build \
        --progress=plain \
        --file "$ctx/$dfile" \
        --tag  "$image:$tag" \
        --tag  "$image:latest" \
        "$ctx" 2>&1 | sed 's/^/    /'; then
      error "Build failed: $name"
      failed=$((failed+1))
      continue
    fi
    success "Built   $image:$tag"

    info "Pushing $image:$tag …"
    if ! docker push "$image:$tag" && docker push "$image:latest"; then
      error "Push failed: $name"
      failed=$((failed+1))
      continue
    fi
    success "Pushed  $image:$tag  +  $image:latest"
    echo "" >&2
  done

  if (( failed > 0 )); then
    fatal "$failed image(s) failed — check output above"
  fi
  success "All images published → $REGISTRY  tag=$tag"
}

tool_version() {
  # Best-effort one-line version for a CLI. Different tools use different
  # flags; try the common ones in priority order. Falls back to "(present)".
  local tool="$1"
  case "$tool" in
    go)            go version 2>&1 | head -1 ;;
    kubectl)       kubectl version --client=true 2>&1 | head -1 ;;
    helm)          helm version --short 2>&1 | head -1 ;;
    *)
      "$tool" --version 2>&1 | head -1 \
        || "$tool" -V 2>&1 | head -1 \
        || echo "(present)"
      ;;
  esac
}

cmd_doctor() {
  header "Pre-flight checks"
  local fails=0
  for tool in bash go npx curl ss kubectl helm; do
    if command -v "$tool" >/dev/null 2>&1; then
      success "$tool: $(tool_version "$tool")"
    else
      error "$tool: not found"
      fails=$((fails+1))
    fi
  done

  resolve_env_file 2>/dev/null || true
  if [[ -n "${ENV_FILE:-}" ]]; then
    success "env file: $ENV_FILE"
  else
    warn "env file: none (will use built-in defaults)"
  fi
  load_env_file 2>/dev/null || true
  apply_defaults

  if [[ "$PROFILE" == "live" ]]; then
    if validate_kubeconfig "$KUBECONFIG"; then
      success "kubeconfig: $KUBECONFIG (cluster reachable)"
    else
      fails=$((fails+1))
    fi
  fi

  for var in ANALYZER_PORT METRICS_PORT DASHBOARD_PORT COLLECTOR_PORT; do
    if validate_port_free "$var" "${!var}"; then
      success "$var: ${!var} free"
    else
      fails=$((fails+1))
    fi
  done

  if [[ -x "$REPO_ROOT/bin/analyzer" ]]; then
    success "analyzer binary: $REPO_ROOT/bin/analyzer"
  else
    warn "analyzer binary missing — will be built on start"
  fi

  if [[ -x "$REPO_ROOT/bin/collector" ]]; then
    success "collector binary: $REPO_ROOT/bin/collector"
  else
    warn "collector binary missing — will be built on start (live profile only)"
  fi

  # Optional: Qdrant vector store connectivity
  local qdrant_url="${QDRANT_URL:-}"
  if [[ -n "$qdrant_url" ]]; then
    if curl -sf "${qdrant_url}/health" >/dev/null 2>&1; then
      success "qdrant: reachable at ${qdrant_url} (embed model: ${QDRANT_EMBED_MODEL:-nomic-embed-text})"
    else
      warn "qdrant: QDRANT_URL=${qdrant_url} set but unreachable — semantic search disabled"
    fi
  else
    warn "qdrant: QDRANT_URL not set — semantic similar-incident search disabled (run: docker compose up qdrant -d)"
  fi

  if (( fails > 0 )); then
    error "$fails check(s) failed"
    return 1
  fi
  success "All checks passed"
}

cmd_lint() {
  resolve_env_file
  load_env_file
  apply_defaults

  local fails=0
  validate_choice PROFILE "$PROFILE" live mock || fails=$((fails+1))
  validate_choice DASHBOARD_MODE "$DASHBOARD_MODE" dev prod || fails=$((fails+1))
  validate_choice LLM_PROVIDER "$LLM_PROVIDER" ollama openai anthropic vllm llamacpp azure bedrock none || fails=$((fails+1))

  for var in ANALYZER_PORT METRICS_PORT DASHBOARD_PORT COLLECTOR_PORT; do
    validate_port "$var" "${!var}" || fails=$((fails+1))
  done

  for var in ANALYSIS_INTERVAL EVICT_INTERVAL MOCK_INTERVAL; do
    validate_duration "$var" "${!var}" || fails=$((fails+1))
  done

  if [[ "$PROFILE" == "live" && -z "$COLLECTOR_URL" ]]; then
    warn "PROFILE=live but COLLECTOR_URL is empty — analyzer will report 'collector unreachable'"
  fi

  if [[ "$LLM_PROVIDER" != "none" ]]; then
    [[ -n "$LLM_ENDPOINT" ]] || { error "LLM_ENDPOINT empty for provider=$LLM_PROVIDER"; fails=$((fails+1)); }
    [[ -n "$LLM_MODEL"    ]] || { error "LLM_MODEL empty for provider=$LLM_PROVIDER"; fails=$((fails+1)); }
  fi

  if (( fails > 0 )); then
    fatal "$fails validation error(s) in $ENV_FILE"
  fi
  success "Env file passes lint: $ENV_FILE"
}

cmd_setup() {
  header "Interactive setup wizard"
  resolve_env_file 2>/dev/null || true
  if [[ -z "$ENV_FILE" ]]; then
    ENV_FILE="$REPO_ROOT/env/$ENVIRONMENT.env"
  fi
  if [[ -f "$ENV_FILE" ]]; then
    load_env_file
    if ! prompt_confirm "Overwrite existing $ENV_FILE?" "N"; then
      fatal "aborted"
    fi
  fi
  apply_defaults

  collect_runtime_config
  collect_ports
  collect_collector
  collect_llm
  collect_intervals
  render_summary

  cat > "$ENV_FILE" <<EOF
# Generated by scripts/run-local.sh setup on $(date -u +%Y-%m-%dT%H:%M:%SZ)
ENVIRONMENT=$ENVIRONMENT
PROFILE=$PROFILE
BIND_ADDRESS=$BIND_ADDRESS
NEXT_HOSTNAME=$NEXT_HOSTNAME
DASHBOARD_MODE=$DASHBOARD_MODE

ANALYZER_PORT=$ANALYZER_PORT
METRICS_PORT=$METRICS_PORT
DASHBOARD_PORT=$DASHBOARD_PORT
COLLECTOR_PORT=$COLLECTOR_PORT
COLLECTOR_METRICS_PORT=$COLLECTOR_METRICS_PORT

COLLECTOR_URL=$COLLECTOR_URL

LLM_PROVIDER=$LLM_PROVIDER
LLM_ENDPOINT=$LLM_ENDPOINT
LLM_MODEL=$LLM_MODEL
LLM_API_KEY=$LLM_API_KEY

MOCK_INTERVAL=$MOCK_INTERVAL
ANALYSIS_INTERVAL=$ANALYSIS_INTERVAL
EVICT_INTERVAL=$EVICT_INTERVAL
EOF
  success "Wrote $ENV_FILE"
}

cmd_version() {
  local sha; sha="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  local chart_ver
  chart_ver="$(awk -F': ' '/^version:/{print $2; exit}' "$REPO_ROOT/deploy/helm/hetu/Chart.yaml" 2>/dev/null || echo unknown)"
  echo "hetu: git $sha · chart $chart_ver" >&2
}

usage() {
  cat >&2 <<EOF

${BOLD}scripts/run-local.sh${NC} — interactive dev runner

${BOLD}Usage:${NC}
  scripts/run-local.sh                       # interactive menu
  scripts/run-local.sh <command> [flags]

${BOLD}Commands:${NC}
  ${CYAN}start${NC}    [-e ENV] [--yes] [--non-interactive]   Configure → validate → start
  ${CYAN}stop${NC}                                            Stop services and clean pidfiles
  ${CYAN}status${NC}                                          Tabular status of all services
  ${CYAN}restart${NC}                                         stop → start (reuses existing settings)
  ${CYAN}logs${NC}     [analyzer|dashboard|collector|all]     tail -F selected logs
  ${CYAN}build${NC}                                           Build Go binaries (analyzer + collector)
  ${CYAN}publish${NC}  [--registry URL] [--tag TAG]           docker build + push to REGISTRY
  ${CYAN}setup${NC}    [-e ENV]                               Wizard: write env/<ENV>.env
  ${CYAN}doctor${NC}                                          Pre-flight: tool & env checks
  ${CYAN}lint${NC}                                            Validate env file only
  ${CYAN}version${NC}                                         Print git SHA + chart version
  ${CYAN}help${NC}                                            This message

${BOLD}Flags:${NC}
  ${DIM}-e, --env ENV${NC}        Environment (dev|uat|prod). Defaults to ENVIRONMENT or "dev".
  ${DIM}-y, --yes${NC}            Accept env-file defaults without prompting.
  ${DIM}--non-interactive${NC}    Fail if any required value is missing.
  ${DIM}--env-file PATH${NC}      Override env file path.
  ${DIM}--log-dir PATH${NC}       Override log directory.

EOF
}

# ── Argument parsing ─────────────────────────────────────────────────────────
parse_global_flags() {
  # Mutates: ENVIRONMENT, ENV_FILE, LOG_DIR, YES, NON_INTERACTIVE, then leaves
  # positional args in REMAINING (newline-separated) for the dispatcher.
  REMAINING=()
  while (( $# > 0 )); do
    case "$1" in
      -e|--env)            ENVIRONMENT="$2"; shift 2 ;;
      --env-file)          ENV_FILE="$2"; shift 2 ;;
      --log-dir)           LOG_DIR="$2"; mkdir -p "$LOG_DIR"; shift 2 ;;
      -y|--yes)            YES=1; shift ;;
      --non-interactive)   NON_INTERACTIVE=1; shift ;;
      *)                   REMAINING+=("$1"); shift ;;
    esac
  done
}

main() {
  REMAINING=()
  parse_global_flags "$@"
  set -- "${REMAINING[@]+"${REMAINING[@]}"}"

  local cmd="${1:-}"
  shift 2>/dev/null || true

  if [[ -z "$cmd" ]]; then
    if (( YES )) || (( NON_INTERACTIVE )); then
      usage; fatal "no command given (cannot prompt with --yes/--non-interactive)"
    fi
    _prompt_select "What would you like to do?" "start" \
      "start" "stop" "status" "restart" "logs" "build" "publish" "setup" "doctor" "lint" "version" "help"
    cmd="$PROMPT_RESULT"
  fi

  case "$cmd" in
    start)              cmd_start   "$@" ;;
    stop)               cmd_stop    "$@" ;;
    status)             cmd_status  "$@" ;;
    restart)            cmd_restart "$@" ;;
    logs)               cmd_logs    "$@" ;;
    build)              cmd_build   "$@" ;;
    publish)            cmd_publish "$@" ;;
    setup)              cmd_setup   "$@" ;;
    doctor)             cmd_doctor  "$@" ;;
    lint)               cmd_lint    "$@" ;;
    version|-v|--version) cmd_version ;;
    help|-h|--help)     usage ;;
    *)                  error "Unknown command: $cmd"; usage; exit 1 ;;
  esac
}

main "$@"
