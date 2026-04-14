#!/usr/bin/env bash
# run-local.sh — Start analyzer + dashboard locally for development.
#
# The dashboard proxies /api/v1/* to the analyzer via a Next.js route
# handler that reads ANALYZER_URL from the server-side env (NOT
# NEXT_PUBLIC_ANALYZER_URL — that variable is only usable from client
# code). Getting the name wrong silently falls back to the K8s DNS
# default and every proxied request 502s. This script sets them
# correctly so that doesn't recur.
#
# Usage:
#   scripts/run-local.sh [start|stop|status|logs]
#
# Defaults:
#   analyzer  -> :18081 (mock profile; connects to Ollama at $OLLAMA_HOST)
#   dashboard -> :3003 (next dev; proxies /api/v1/* to analyzer)
#
# Env overrides:
#   ANALYZER_PORT, DASHBOARD_PORT, PROFILE, LLM_ENDPOINT, LLM_MODEL, LOG_DIR

set -euo pipefail

ANALYZER_PORT="${ANALYZER_PORT:-18081}"
METRICS_PORT="${METRICS_PORT:-19091}"
DASHBOARD_PORT="${DASHBOARD_PORT:-3003}"
PROFILE="${PROFILE:-mock}"
LLM_ENDPOINT="${LLM_ENDPOINT:-http://100.89.50.27:11434}"
LLM_MODEL="${LLM_MODEL:-qwen2.5:7b-instruct}"
MOCK_INTERVAL="${MOCK_INTERVAL:-20s}"
ANALYSIS_INTERVAL="${ANALYSIS_INTERVAL:-30s}"
EVICT_INTERVAL="${EVICT_INTERVAL:-30s}"

LOG_DIR="${LOG_DIR:-/tmp/cluster-intel-logs}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ANALYZER_PIDFILE="$LOG_DIR/analyzer.pid"
DASHBOARD_PIDFILE="$LOG_DIR/dashboard.pid"

mkdir -p "$LOG_DIR"

cmd_build() {
  echo "Building analyzer binary..."
  (cd "$REPO_ROOT/src/analyzer" && go build -o "$REPO_ROOT/bin/analyzer" ./...)
}

cmd_start() {
  if [[ -f "$ANALYZER_PIDFILE" ]] && kill -0 "$(cat "$ANALYZER_PIDFILE")" 2>/dev/null; then
    echo "Analyzer already running (PID $(cat "$ANALYZER_PIDFILE")). Use 'stop' first."
    return 1
  fi

  if [[ ! -x "$REPO_ROOT/bin/analyzer" ]] || [[ "$REPO_ROOT/src/analyzer/main.go" -nt "$REPO_ROOT/bin/analyzer" ]]; then
    cmd_build
  fi

  echo "Starting analyzer on :$ANALYZER_PORT (profile=$PROFILE, llm=$LLM_MODEL)..."
  API_PORT="$ANALYZER_PORT" \
    METRICS_PORT="$METRICS_PORT" \
    PROFILE="$PROFILE" \
    LLM_BACKEND=ollama \
    LLM_ENDPOINT="$LLM_ENDPOINT" \
    LLM_MODEL="$LLM_MODEL" \
    MOCK_INTERVAL="$MOCK_INTERVAL" \
    ANALYSIS_INTERVAL="$ANALYSIS_INTERVAL" \
    EVICT_INTERVAL="$EVICT_INTERVAL" \
    "$REPO_ROOT/bin/analyzer" > "$LOG_DIR/analyzer.log" 2>&1 &
  echo $! > "$ANALYZER_PIDFILE"

  # Wait for analyzer to be ready
  for _ in {1..20}; do
    if curl -s -o /dev/null "http://localhost:$ANALYZER_PORT/healthz"; then
      break
    fi
    sleep 0.5
  done
  if ! curl -s -o /dev/null "http://localhost:$ANALYZER_PORT/healthz"; then
    echo "Analyzer failed to start. Logs:"
    tail -20 "$LOG_DIR/analyzer.log"
    return 1
  fi
  echo "Analyzer ready (PID $(cat "$ANALYZER_PIDFILE"))."

  echo "Starting dashboard on :$DASHBOARD_PORT (proxying to analyzer)..."
  # IMPORTANT: the Next.js proxy route handler reads ANALYZER_URL from
  # server-side env. NEXT_PUBLIC_ANALYZER_URL would only work client-side.
  # See src/dashboard/app/api/v1/[...path]/route.ts for the lookup.
  (
    cd "$REPO_ROOT/src/dashboard" && \
    ANALYZER_URL="http://localhost:$ANALYZER_PORT" \
    PORT="$DASHBOARD_PORT" \
    exec npx next dev -p "$DASHBOARD_PORT"
  ) > "$LOG_DIR/dashboard.log" 2>&1 &
  echo $! > "$DASHBOARD_PIDFILE"

  for _ in {1..40}; do
    if curl -s -o /dev/null "http://localhost:$DASHBOARD_PORT/"; then
      break
    fi
    sleep 0.5
  done
  if ! curl -s -o /dev/null -w "%{http_code}" "http://localhost:$DASHBOARD_PORT/api/v1/health" | grep -q 200; then
    echo "WARN: dashboard proxy not returning 200 on /api/v1/health"
    tail -20 "$LOG_DIR/dashboard.log"
  fi
  echo "Dashboard ready (PID $(cat "$DASHBOARD_PIDFILE"))."
  echo ""
  echo "  dashboard: http://localhost:$DASHBOARD_PORT/"
  echo "  analyzer:  http://localhost:$ANALYZER_PORT/"
  echo "  logs:      tail -f $LOG_DIR/{analyzer,dashboard}.log"
}

cmd_stop() {
  for f in "$ANALYZER_PIDFILE" "$DASHBOARD_PIDFILE"; do
    if [[ -f "$f" ]]; then
      pid="$(cat "$f")"
      if kill -0 "$pid" 2>/dev/null; then
        echo "Stopping PID $pid ($f)..."
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
      fi
      rm -f "$f"
    fi
  done
  echo "Stopped."
}

cmd_status() {
  for pair in "analyzer:$ANALYZER_PIDFILE:$ANALYZER_PORT:healthz" "dashboard:$DASHBOARD_PIDFILE:$DASHBOARD_PORT:"; do
    IFS=: read -r name pidfile port probe <<< "$pair"
    if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
      code="$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$port/$probe" || echo fail)"
      echo "$name: running (PID $(cat "$pidfile")), http $code"
    else
      echo "$name: not running"
    fi
  done
}

cmd_logs() {
  tail -F "$LOG_DIR/analyzer.log" "$LOG_DIR/dashboard.log"
}

case "${1:-start}" in
  start)  cmd_start  ;;
  stop)   cmd_stop   ;;
  status) cmd_status ;;
  logs)   cmd_logs   ;;
  build)  cmd_build  ;;
  *)
    echo "usage: $0 [start|stop|status|logs|build]"
    exit 1
    ;;
esac
