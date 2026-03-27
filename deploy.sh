#!/bin/bash
#
# K8s Cluster Intelligence Engine - Full Stack Deployment
# Version: 6.0.0
#
# This script deploys the complete security and compliance platform:
# - Trivy Operator (vulnerability scanning)
# - Cluster Intelligence Engine (analysis & dashboard)
# - All required RBAC and storage
#
# Usage: ./deploy.sh [OPTIONS]
#
# Options:
#   --namespace NAME      Deploy to specific namespace (default: cluster-intel)
#   --skip-trivy          Skip Trivy Operator installation
#   --skip-checks         Skip pre-deploy validation checks
#   --prometheus URL      Set Prometheus server URL
#   --webhook URL         Set alert webhook URL (Slack, Discord, etc.)
#   --uninstall           Remove all components
#   --version             Show version information
#   --help                Show this help message
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration defaults
NAMESPACE="utilities"
SKIP_TRIVY=false
SKIP_CHECKS=false
PROMETHEUS_URL=""
ALERT_WEBHOOK=""
UNINSTALL=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Read version from VERSION file
VERSION_FILE="$SCRIPT_DIR/VERSION"
if [[ -f "$VERSION_FILE" ]]; then
    VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
else
    VERSION="unknown"
fi

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --skip-trivy)
            SKIP_TRIVY=true
            shift
            ;;
        --skip-checks)
            SKIP_CHECKS=true
            shift
            ;;
        --prometheus)
            PROMETHEUS_URL="$2"
            shift 2
            ;;
        --webhook)
            ALERT_WEBHOOK="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --version)
            echo "K8s Cluster Intelligence Engine v$VERSION"
            exit 0
            ;;
        --help)
            head -30 "$0" | tail -20
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Run pre-deploy checks unless skipped
if [[ "$SKIP_CHECKS" != "true" && "$UNINSTALL" != "true" ]]; then
    if [[ -x "$SCRIPT_DIR/scripts/pre-deploy-check.sh" ]]; then
        echo -e "${BLUE}Running pre-deploy validation...${NC}"
        if ! "$SCRIPT_DIR/scripts/pre-deploy-check.sh"; then
            echo -e "${RED}Pre-deploy checks failed. Use --skip-checks to bypass.${NC}"
            exit 1
        fi
        echo ""
    fi
fi

# Banner
echo -e "${CYAN}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║         K8s Cluster Intelligence Engine v$VERSION                  ║"
echo "║         Full-Stack Security & Compliance Platform             ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# Check prerequisites
check_prerequisites() {
    echo -e "${BLUE}[1/6] Checking prerequisites...${NC}"
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        echo -e "${RED}Error: kubectl is not installed${NC}"
        exit 1
    fi
    echo -e "  ${GREEN}✓${NC} kubectl found"
    
    # Check cluster connection
    if ! kubectl cluster-info &> /dev/null; then
        echo -e "${RED}Error: Cannot connect to Kubernetes cluster${NC}"
        exit 1
    fi
    echo -e "  ${GREEN}✓${NC} Connected to cluster"
    
    # Check helm (optional, for Trivy)
    if ! command -v helm &> /dev/null; then
        echo -e "  ${YELLOW}⚠${NC} Helm not found - will skip Trivy Operator installation"
        SKIP_TRIVY=true
    else
        echo -e "  ${GREEN}✓${NC} Helm found"
    fi
    
    # Get cluster info
    CLUSTER_NAME=$(kubectl config current-context 2>/dev/null || echo "unknown")
    NODE_COUNT=$(kubectl get nodes --no-headers 2>/dev/null | wc -l)
    echo -e "  ${GREEN}✓${NC} Cluster: ${CLUSTER_NAME} (${NODE_COUNT} nodes)"
}

# Uninstall function
uninstall() {
    echo -e "${YELLOW}Uninstalling Cluster Intelligence...${NC}"
    
    # Delete main resources
    kubectl delete -k "${SCRIPT_DIR}/manifests/" --ignore-not-found 2>/dev/null || true
    
    # Delete namespace
    kubectl delete namespace "${NAMESPACE}" --ignore-not-found 2>/dev/null || true
    
    # Optionally remove Trivy
    if helm list -n trivy-system 2>/dev/null | grep -q trivy-operator; then
        read -p "Remove Trivy Operator? [y/N] " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            helm uninstall trivy-operator -n trivy-system 2>/dev/null || true
            kubectl delete namespace trivy-system --ignore-not-found 2>/dev/null || true
        fi
    fi
    
    echo -e "${GREEN}Uninstallation complete${NC}"
    exit 0
}

# Install Trivy Operator
install_trivy() {
    if [ "$SKIP_TRIVY" = true ]; then
        echo -e "${YELLOW}[2/6] Skipping Trivy Operator installation${NC}"
        return
    fi
    
    echo -e "${BLUE}[2/6] Installing Trivy Operator...${NC}"
    
    # Check if already installed
    if kubectl get namespace trivy-system &> /dev/null; then
        if kubectl get pods -n trivy-system -l app.kubernetes.io/name=trivy-operator --no-headers 2>/dev/null | grep -q Running; then
            echo -e "  ${GREEN}✓${NC} Trivy Operator already installed and running"
            return
        fi
    fi
    
    # Add Aqua helm repo
    echo -e "  Adding Aqua Security Helm repository..."
    helm repo add aqua https://aquasecurity.github.io/helm-charts/ 2>/dev/null || true
    helm repo update aqua 2>/dev/null || true
    
    # Install Trivy Operator
    echo -e "  Installing Trivy Operator..."
    helm upgrade --install trivy-operator aqua/trivy-operator \
        --namespace trivy-system \
        --create-namespace \
        --set trivy.ignoreUnfixed=true \
        --set operator.scanJobsConcurrentLimit=3 \
        --set operator.vulnerabilityScannerEnabled=true \
        --set operator.configAuditScannerEnabled=true \
        --set operator.rbacAssessmentScannerEnabled=false \
        --wait \
        --timeout 5m 2>/dev/null || {
            echo -e "  ${YELLOW}⚠${NC} Trivy installation failed, continuing without it"
            return
        }
    
    echo -e "  ${GREEN}✓${NC} Trivy Operator installed"
}

# Create namespace
create_namespace() {
    echo -e "${BLUE}[3/6] Creating namespace...${NC}"
    
    if kubectl get namespace "${NAMESPACE}" &> /dev/null; then
        echo -e "  ${GREEN}✓${NC} Namespace '${NAMESPACE}' already exists"
    else
        kubectl create namespace "${NAMESPACE}"
        echo -e "  ${GREEN}✓${NC} Created namespace '${NAMESPACE}'"
    fi
}

# Update manifest with configuration
prepare_manifests() {
    echo -e "${BLUE}[4/6] Preparing manifests...${NC}"
    
    MANIFEST_DIR="${SCRIPT_DIR}/manifests"
    cd "${MANIFEST_DIR}"
    
    # Update namespace in manifest if different
    if [ "${NAMESPACE}" != "cluster-intel" ]; then
        echo -e "  Updating namespace to '${NAMESPACE}'..."
        kustomize edit set namespace "${NAMESPACE}"
    fi
    
    # Update Prometheus URL if provided
    if [ -n "${PROMETHEUS_URL}" ]; then
        echo -e "  Setting Prometheus URL..."
        # We need a patch for this as it's not a standard field kustomize can set
        cat <<EOF > prometheus-patch.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-intel-config
data:
  config.yaml: |
    cluster_id: "default"
    analysis_interval: 60
    history_retention_days: 30
    theme: "light"
    
    prometheus:
      enabled: true
      url: "${PROMETHEUS_URL}"
      
    alerts:
      enabled: false
      threshold: 50
      webhooks: []
    
    scoring:
      weights:
        reliability: 0.25
        security: 0.40
        cost: 0.15
        architecture: 0.20
EOF
        kustomize edit add patch prometheus-patch.yaml
    fi
    
    # Note: ALERT_WEBHOOK wasn't supported robustly before, we are dropping support 
    # for inline string replacement to remain fully idempotent with kustomize.
    
    echo -e "  ${GREEN}✓${NC} Manifests prepared"
}

# Deploy Cluster Intelligence
deploy_cluster_intel() {
    echo -e "${BLUE}[5/6] Deploying Cluster Intelligence...${NC}"
    
    kubectl apply -k "${SCRIPT_DIR}/manifests/"
    
    # Wait for deployment
    echo -e "  Waiting for deployment to be ready..."
    kubectl rollout status deployment/cluster-intel -n "${NAMESPACE}" --timeout=180s
    
    echo -e "  ${GREEN}✓${NC} Cluster Intelligence deployed"
}

# Setup access
setup_access() {
    echo -e "${BLUE}[6/6] Setting up access...${NC}"
    
    # Get pod name
    POD_NAME=$(kubectl get pods -n "${NAMESPACE}" -l app=cluster-intel -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    
    # Check if pod is ready
    kubectl wait --for=condition=ready pod/"${POD_NAME}" -n "${NAMESPACE}" --timeout=120s 2>/dev/null || true
    
    echo ""
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  Deployment Complete!${NC}"
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${CYAN}Access the dashboard:${NC}"
    echo ""
    echo -e "    ${YELLOW}Option 1: Port Forward (recommended for testing)${NC}"
    echo -e "    kubectl port-forward svc/cluster-intel 8080:8080 -n ${NAMESPACE}"
    echo -e "    Then open: ${BLUE}http://localhost:8080${NC}"
    echo ""
    echo -e "    ${YELLOW}Option 2: NodePort Service${NC}"
    echo -e "    kubectl patch svc cluster-intel -n ${NAMESPACE} -p '{\"spec\":{\"type\":\"NodePort\"}}'"
    echo ""
    echo -e "    ${YELLOW}Option 3: Ingress (requires ingress controller)${NC}"
    echo -e "    kubectl apply -f - <<EOF"
    echo -e "    apiVersion: networking.k8s.io/v1"
    echo -e "    kind: Ingress"
    echo -e "    metadata:"
    echo -e "      name: cluster-intel"
    echo -e "      namespace: ${NAMESPACE}"
    echo -e "    spec:"
    echo -e "      rules:"
    echo -e "      - host: cluster-intel.example.com"
    echo -e "        http:"
    echo -e "          paths:"
    echo -e "          - path: /"
    echo -e "            pathType: Prefix"
    echo -e "            backend:"
    echo -e "              service:"
    echo -e "                name: cluster-intel"
    echo -e "                port:"
    echo -e "                  number: 8080"
    echo -e "    EOF"
    echo ""
    echo -e "  ${CYAN}Useful commands:${NC}"
    echo ""
    echo -e "    Check status:   kubectl get pods -n ${NAMESPACE}"
    echo -e "    View logs:      kubectl logs -f -n ${NAMESPACE} -l app=cluster-intel"
    echo -e "    Get health:     kubectl exec -n ${NAMESPACE} deploy/cluster-intel -- curl -s localhost:8080/api/v1/health | jq '.scores'"
    echo ""
    echo -e "  ${CYAN}Trivy Operator:${NC}"
    if kubectl get pods -n trivy-system 2>/dev/null | grep -q Running; then
        echo -e "    Status: ${GREEN}Running${NC}"
        echo -e "    Check vulnerabilities: kubectl get vulnerabilityreports -A"
    else
        echo -e "    Status: ${YELLOW}Not installed${NC}"
        echo -e "    Install: helm install trivy-operator aqua/trivy-operator -n trivy-system --create-namespace"
    fi
    echo ""
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
}

# Auto port-forward helper
start_port_forward() {
    echo ""
    read -p "Start port-forward now? [Y/n] " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        echo -e "${BLUE}Starting port-forward...${NC}"
        echo -e "Dashboard will be available at: ${GREEN}http://localhost:8080${NC}"
        echo -e "Press Ctrl+C to stop"
        echo ""
        kubectl port-forward svc/cluster-intel 8080:8080 -n "${NAMESPACE}"
    fi
}

# Main execution
main() {
    if [ "$UNINSTALL" = true ]; then
        uninstall
    fi
    
    check_prerequisites
    install_trivy
    create_namespace
    prepare_manifests
    deploy_cluster_intel
    setup_access
    start_port_forward
}

# Run main
main
