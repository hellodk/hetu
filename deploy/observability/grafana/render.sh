#!/usr/bin/env bash
# Render datasources/*.yaml from datasources/*.yaml.tmpl using contract.yaml.
# Usage:  ./render.sh [path/to/contract.yaml]
# Then:   kubectl apply -k .
#
# Only the contract's named tokens are substituted; Grafana's own ${__value.raw}
# and ${__tags} are deliberately left untouched.
set -euo pipefail
cd "$(dirname "$0")"
CONTRACT="${1:-contract.yaml}"

[ -f "$CONTRACT" ] || { echo "contract not found: $CONTRACT" >&2; exit 1; }

# Minimal scalar reader for our fixed-structure contract (no yq dependency).
val() {
  grep -E "^[[:space:]]*$1:" "$CONTRACT" | head -1 \
    | sed -E "s/^[^:]*:[[:space:]]*//; s/[[:space:]]*#.*$//; s/^['\"]//; s/['\"]$//; s/[[:space:]]*$//"
}

MONITORING_NAMESPACE="$(val monitoringNamespace)"
PROMETHEUS_URL="$(val prometheusUrl)"
LOKI_URL="$(val lokiUrl)"
TEMPO_URL="$(val tempoUrl)"
PROMETHEUS_UID="$(val prometheus)"
LOKI_UID="$(val loki)"
TEMPO_UID="$(val tempo)"

for v in MONITORING_NAMESPACE PROMETHEUS_URL LOKI_URL TEMPO_URL PROMETHEUS_UID LOKI_UID TEMPO_UID; do
  [ -n "${!v}" ] || { echo "missing value for $v in $CONTRACT" >&2; exit 1; }
done

subst() {
  sed \
    -e "s|\${MONITORING_NAMESPACE}|${MONITORING_NAMESPACE}|g" \
    -e "s|\${PROMETHEUS_URL}|${PROMETHEUS_URL}|g" \
    -e "s|\${LOKI_URL}|${LOKI_URL}|g" \
    -e "s|\${TEMPO_URL}|${TEMPO_URL}|g" \
    -e "s|\${PROMETHEUS_UID}|${PROMETHEUS_UID}|g" \
    -e "s|\${LOKI_UID}|${LOKI_UID}|g" \
    -e "s|\${TEMPO_UID}|${TEMPO_UID}|g"
}

for name in prometheus loki tempo; do
  subst < "datasources/${name}.yaml.tmpl" > "datasources/${name}.yaml"
  echo "rendered datasources/${name}.yaml"
done

echo "done. namespace=${MONITORING_NAMESPACE}  loki=${LOKI_URL}"
echo "next: kubectl apply -k ."
