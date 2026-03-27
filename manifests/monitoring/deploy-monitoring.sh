#!/bin/bash
#===============================================================================
# Ollama LLM Monitoring Stack - Deployment Script
#===============================================================================
# This script deploys the complete LLM monitoring stack including:
#   - Ollama metrics collection (ServiceMonitor)
#   - Grafana dashboards (in 'llm' folder)
#   - PrometheusRules for alerting
#   - Tempo for distributed tracing
#   - OpenTelemetry Collector
#   - Slack alert configuration
#
# Usage:
#   ./deploy-monitoring.sh deploy [options]    Deploy the stack
#   ./deploy-monitoring.sh status              Check deployment status
#   ./deploy-monitoring.sh uninstall           Remove all components
#   ./deploy-monitoring.sh help                Show this help
#
# Examples:
#   ./deploy-monitoring.sh deploy --ollama-ip 192.168.1.10
#   ./deploy-monitoring.sh deploy --ollama-ip 192.168.1.10 --slack-webhook https://hooks.slack.com/...
#   ./deploy-monitoring.sh status
#   ./deploy-monitoring.sh uninstall
#===============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAMESPACE="${MONITORING_NAMESPACE:-monitoring}"
OLLAMA_NAMESPACE="${OLLAMA_NAMESPACE:-ai-system}"
OLLAMA_IP="${OLLAMA_IP:-100.89.50.27}"
COMMAND="${1:-deploy}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${CYAN}[STEP]${NC} $1"; }

print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════════╗"
    echo "║       Ollama Monitoring Stack Deployment                      ║"
    echo "╠═══════════════════════════════════════════════════════════════╣"
    echo "║  Components:                                                  ║"
    echo "║  • Ollama ServiceMonitor & Metrics                            ║"
    echo "║  • Grafana Dashboards (Ollama + LLM Performance)              ║"
    echo "║  • PrometheusRules (Alerts)                                   ║"
    echo "║  • Tempo (Distributed Tracing)                                ║"
    echo "║  • OpenTelemetry Collector                                    ║"
    echo "║  • AlertManager Slack Configuration                           ║"
    echo "╚═══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi
    
    # Check cluster connection
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check if monitoring namespace exists
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_warn "Namespace $NAMESPACE does not exist, creating..."
        kubectl create namespace "$NAMESPACE"
    fi
    
    # Check if Prometheus Operator CRDs exist
    if ! kubectl get crd servicemonitors.monitoring.coreos.com &> /dev/null; then
        log_warn "Prometheus Operator CRDs not found. ServiceMonitor/PrometheusRule may not work."
        log_info "Install Prometheus Operator first: https://prometheus-operator.dev/"
    fi
    
    log_success "Prerequisites check passed"
}

create_ai_namespace() {
    log_info "Creating AI system namespace..."
    
    if ! kubectl get namespace "$OLLAMA_NAMESPACE" &> /dev/null; then
        kubectl create namespace "$OLLAMA_NAMESPACE"
        log_success "Created namespace $OLLAMA_NAMESPACE"
    else
        log_info "Namespace $OLLAMA_NAMESPACE already exists"
    fi
}

update_ollama_ip() {
    log_info "Configuring Ollama endpoint: $OLLAMA_IP"
    
    # Update the servicemonitor with the correct IP
    sed -i "s/192.168.1.10/$OLLAMA_IP/g" "$SCRIPT_DIR/ollama/servicemonitor.yaml" 2>/dev/null || \
    sed -i '' "s/192.168.1.10/$OLLAMA_IP/g" "$SCRIPT_DIR/ollama/servicemonitor.yaml"
    
    log_success "Updated Ollama IP to $OLLAMA_IP"
}

deploy_grafana_folders() {
    log_info "Deploying Grafana folder configuration..."
    
    # Deploy Grafana folder provisioning (creates 'llm' folder)
    kubectl apply -f "$SCRIPT_DIR/grafana/folder-provisioning.yaml"
    log_success "Deployed Grafana LLM folder configuration"
    
    # Deploy LLM Traces Dashboard
    kubectl apply -f "$SCRIPT_DIR/grafana/llm-traces-dashboard.yaml"
    log_success "Deployed LLM Traces Dashboard"
}

deploy_ollama_monitoring() {
    log_info "Deploying Ollama monitoring components..."
    
    # Deploy Ollama ServiceMonitor and Service
    kubectl apply -f "$SCRIPT_DIR/ollama/servicemonitor.yaml"
    log_success "Deployed Ollama ServiceMonitor"
    
    # Deploy Ollama Grafana Dashboard (into 'llm' folder)
    kubectl apply -f "$SCRIPT_DIR/ollama/grafana-dashboard.yaml"
    log_success "Deployed Ollama Grafana Dashboard to 'llm' folder"
}

deploy_alerts() {
    log_info "Deploying alert rules..."
    
    # Deploy PrometheusRules
    kubectl apply -f "$SCRIPT_DIR/alerts/ollama-alerts.yaml"
    log_success "Deployed Ollama alert rules"
    
    # Deploy AlertManager config template
    kubectl apply -f "$SCRIPT_DIR/alerts/alertmanager-config.yaml"
    log_success "Deployed AlertManager configuration template"
}

deploy_tempo() {
    log_info "Deploying Tempo for distributed tracing..."
    
    # Deploy Tempo
    kubectl apply -f "$SCRIPT_DIR/tempo/tempo-deployment.yaml"
    log_success "Deployed Tempo"
    
    # Deploy Grafana datasource for Tempo
    kubectl apply -f "$SCRIPT_DIR/tempo/grafana-datasource.yaml"
    log_success "Deployed Tempo Grafana datasource"
    
    # Deploy OTEL Collector
    kubectl apply -f "$SCRIPT_DIR/tempo/otel-collector.yaml"
    log_success "Deployed OpenTelemetry Collector"
}

configure_slack_webhook() {
    log_info "Configuring Slack webhook..."
    
    if [ -z "${SLACK_WEBHOOK_URL:-}" ]; then
        log_warn "SLACK_WEBHOOK_URL not set. Skipping Slack configuration."
        log_info "To configure Slack alerts later, run:"
        echo ""
        echo "  kubectl create secret generic alertmanager-slack-config \\"
        echo "    --from-literal=slack-webhook-url='YOUR_WEBHOOK_URL' \\"
        echo "    -n $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -"
        echo ""
        return
    fi
    
    # Create/update the secret with the webhook URL
    kubectl create secret generic alertmanager-slack-config \
        --from-literal=slack-webhook-url="$SLACK_WEBHOOK_URL" \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    log_success "Configured Slack webhook"
}

wait_for_deployments() {
    log_info "Waiting for deployments to be ready..."
    
    local deployments=("tempo" "otel-collector")
    
    for deploy in "${deployments[@]}"; do
        if kubectl get deployment "$deploy" -n "$NAMESPACE" &> /dev/null; then
            log_info "Waiting for $deploy..."
            kubectl rollout status deployment/"$deploy" -n "$NAMESPACE" --timeout=120s || true
        fi
    done
    
    log_success "All deployments ready"
}

print_next_steps() {
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                   Deployment Complete!                        ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BLUE}Next Steps:${NC}"
    echo ""
    echo "1. Access Grafana dashboards:"
    echo "   kubectl port-forward svc/grafana 3000:80 -n $NAMESPACE"
    echo "   Open: http://localhost:3000/d/ollama-monitoring"
    echo ""
    echo "2. Configure Slack Webhook (if not done):"
    echo "   - Go to https://api.slack.com/apps"
    echo "   - Create a new app or use existing"
    echo "   - Enable 'Incoming Webhooks'"
    echo "   - Create webhook for your channel"
    echo "   - Run: export SLACK_WEBHOOK_URL='https://hooks.slack.com/...'"
    echo "   - Re-run this script or manually create the secret"
    echo ""
    echo "3. View traces in Tempo:"
    echo "   kubectl port-forward svc/tempo 3200:3200 -n $NAMESPACE"
    echo "   Open Grafana → Explore → Select Tempo datasource"
    echo ""
    echo "4. Verify Ollama metrics are being scraped:"
    echo "   kubectl port-forward svc/prometheus 9090:9090 -n $NAMESPACE"
    echo "   Query: up{job=\"ollama\"}"
    echo ""
    echo "5. Update your application to use the instrumented LLM client:"
    echo "   - Go: Import and use LLMClient from llm_metrics.go"
    echo "   - Python: from llm_metrics import create_instrumented_llm_client"
    echo ""
    echo -e "${YELLOW}Environment Variables for Applications:${NC}"
    echo "   OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.$NAMESPACE.svc:4317"
    echo "   LLM_ENDPOINT=http://ollama.$OLLAMA_NAMESPACE.svc:11434"
    echo ""
}

#===============================================================================
# Command: status - Check deployment status
#===============================================================================
check_status() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║             LLM Monitoring Stack Status                       ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    
    log_step "Checking namespaces..."
    echo "  monitoring:  $(kubectl get ns monitoring -o jsonpath='{.status.phase}' 2>/dev/null || echo 'Not Found')"
    echo "  ai-system:   $(kubectl get ns ai-system -o jsonpath='{.status.phase}' 2>/dev/null || echo 'Not Found')"
    echo ""
    
    log_step "Checking deployments in monitoring namespace..."
    kubectl get deployments -n "$NAMESPACE" -o wide 2>/dev/null || echo "  No deployments found"
    echo ""
    
    log_step "Checking services..."
    kubectl get svc -n "$NAMESPACE" -l 'app in (tempo,otel-collector,ollama)' 2>/dev/null || echo "  No services found"
    echo ""
    
    log_step "Checking ServiceMonitors..."
    kubectl get servicemonitors -n "$NAMESPACE" 2>/dev/null || echo "  No ServiceMonitors found (Prometheus Operator may not be installed)"
    echo ""
    
    log_step "Checking PrometheusRules..."
    kubectl get prometheusrules -n "$NAMESPACE" 2>/dev/null || echo "  No PrometheusRules found"
    echo ""
    
    log_step "Checking Grafana ConfigMaps (dashboards)..."
    kubectl get configmaps -n "$NAMESPACE" -l grafana_dashboard=1 2>/dev/null || echo "  No dashboard ConfigMaps found"
    echo ""
    
    log_step "Checking Ollama connectivity..."
    if kubectl get endpoints ollama -n "$OLLAMA_NAMESPACE" &>/dev/null; then
        local ollama_ip=$(kubectl get endpoints ollama -n "$OLLAMA_NAMESPACE" -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null)
        echo "  Ollama endpoint: $ollama_ip:11434"
    else
        echo "  Ollama endpoint not configured"
    fi
    echo ""
    
    log_step "Pod status..."
    kubectl get pods -n "$NAMESPACE" -l 'app in (tempo,otel-collector)' 2>/dev/null || echo "  No pods found"
    echo ""
}

#===============================================================================
# Command: uninstall - Remove all components
#===============================================================================
uninstall_stack() {
    echo ""
    echo -e "${RED}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║           Uninstalling LLM Monitoring Stack                   ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    
    log_warn "This will remove all LLM monitoring components from namespace: $NAMESPACE"
    read -p "Are you sure? (y/N): " confirm
    if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
        log_info "Aborted"
        exit 0
    fi
    
    log_step "Removing LLM Grafana dashboards..."
    kubectl delete configmap grafana-dashboard-ollama grafana-dashboard-llm-traces -n "$NAMESPACE" --ignore-not-found
    
    log_step "Removing Grafana folder config..."
    kubectl delete -f "$SCRIPT_DIR/grafana/" --ignore-not-found 2>/dev/null || true
    
    log_step "Removing Ollama monitoring..."
    kubectl delete -f "$SCRIPT_DIR/ollama/" --ignore-not-found 2>/dev/null || true
    
    log_step "Removing alerts..."
    kubectl delete -f "$SCRIPT_DIR/alerts/" --ignore-not-found 2>/dev/null || true
    
    log_step "Removing Tempo and OTEL Collector..."
    kubectl delete -f "$SCRIPT_DIR/tempo/" --ignore-not-found 2>/dev/null || true
    
    log_step "Removing Slack webhook secret..."
    kubectl delete secret alertmanager-slack-config -n "$NAMESPACE" --ignore-not-found 2>/dev/null || true
    
    log_step "Removing Ollama service/endpoints from ai-system..."
    kubectl delete svc ollama ollama-external -n "$OLLAMA_NAMESPACE" --ignore-not-found 2>/dev/null || true
    kubectl delete endpoints ollama -n "$OLLAMA_NAMESPACE" --ignore-not-found 2>/dev/null || true
    
    log_success "Uninstall complete"
}

#===============================================================================
# Command: help - Show usage
#===============================================================================
show_help() {
    echo ""
    echo -e "${BLUE}Ollama LLM Monitoring Stack - Deployment Script${NC}"
    echo ""
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  deploy      Deploy the complete monitoring stack"
    echo "  status      Check deployment status"
    echo "  uninstall   Remove all monitoring components"
    echo "  help        Show this help message"
    echo ""
    echo "Options (for deploy command):"
    echo "  --ollama-ip <IP>       Ollama server IP address (default: 192.168.1.10)"
    echo "  --slack-webhook <URL>  Slack webhook URL for alerts"
    echo "  --namespace <ns>       Monitoring namespace (default: monitoring)"
    echo ""
    echo "Examples:"
    echo "  $0 deploy --ollama-ip 192.168.1.10"
    echo "  $0 deploy --ollama-ip 10.0.0.50 --slack-webhook https://hooks.slack.com/services/xxx"
    echo "  $0 status"
    echo "  $0 uninstall"
    echo ""
    echo "Environment Variables:"
    echo "  MONITORING_NAMESPACE   Override monitoring namespace"
    echo "  OLLAMA_NAMESPACE       Override Ollama namespace (default: ai-system)"
    echo "  OLLAMA_IP              Override Ollama IP"
    echo "  SLACK_WEBHOOK_URL      Slack webhook URL"
    echo ""
}

#===============================================================================
# Main execution
#===============================================================================
main() {
    # Handle commands
    case "${1:-deploy}" in
        deploy)
            shift || true
            ;;
        status)
            check_status
            exit 0
            ;;
        uninstall)
            uninstall_stack
            exit 0
            ;;
        help|--help|-h)
            show_help
            exit 0
            ;;
        *)
            # Check if first arg is an option (starts with --)
            if [[ "${1:-}" == --* ]]; then
                : # Continue to deploy with options
            else
                log_error "Unknown command: $1"
                echo "Run '$0 help' for usage"
                exit 1
            fi
            ;;
    esac
    
    print_banner
    
    # Parse deploy options
    while [[ $# -gt 0 ]]; do
        case $1 in
            --ollama-ip)
                OLLAMA_IP="$2"
                shift 2
                ;;
            --slack-webhook)
                SLACK_WEBHOOK_URL="$2"
                shift 2
                ;;
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                echo "Run '$0 help' for usage"
                exit 1
                ;;
        esac
    done
    
    log_info "Deploying with configuration:"
    echo "  Monitoring Namespace: $NAMESPACE"
    echo "  Ollama Namespace:     $OLLAMA_NAMESPACE"
    echo "  Ollama IP:            $OLLAMA_IP"
    echo "  Slack Webhook:        ${SLACK_WEBHOOK_URL:-(not set)}"
    echo ""
    
    check_prerequisites
    create_ai_namespace
    update_ollama_ip
    deploy_grafana_folders
    deploy_ollama_monitoring
    deploy_alerts
    deploy_tempo
    configure_slack_webhook
    wait_for_deployments
    print_next_steps
}

main "$@"
