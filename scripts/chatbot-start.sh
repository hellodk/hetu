#!/bin/bash
# Quick start script for k8s-cluster-health Chatbot
# Usage: ./scripts/chatbot-start.sh [local|k8s|help]

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHATBOT_DIR="$PROJECT_ROOT/src/chatbot"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}🤖 k8s-cluster-health Chatbot Launcher${NC}\n"

# ============================================================================
# Helper Functions
# ============================================================================

show_help() {
    cat <<EOF
Usage: ./scripts/chatbot-start.sh [MODE]

Modes:
  local   - Start locally with Docker Compose
  k8s     - Deploy to Kubernetes cluster
  help    - Show this message

Examples:
  ./scripts/chatbot-start.sh local      # Start with Docker Compose
  ./scripts/chatbot-start.sh k8s        # Deploy to K8s

Prerequisites:
  - Local model server at 192.168.1.19:8080 (Ollama, vLLM, etc.)
  - Docker (for local mode)
  - kubectl + kubeconfig (for k8s mode)
EOF
}

check_model_server() {
    echo -e "${BLUE}Checking model server at 192.168.1.19:8080...${NC}"
    if curl -s http://192.168.1.19:8080/models > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Model server is running${NC}\n"
        return 0
    else
        echo -e "${RED}✗ Model server not found at http://192.168.1.19:8080${NC}"
        echo "Please ensure your model server is running:"
        echo "  - Ollama: docker run -d -p 8080:11434 ollama/ollama && ollama pull mistral"
        echo "  - vLLM: python -m vllm.entrypoints.openai.api_server --model mistral --port 8080"
        return 1
    fi
}

check_docker() {
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}✗ Docker is not installed${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ Docker is installed${NC}"
    return 0
}

check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        echo -e "${RED}✗ kubectl is not installed${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ kubectl is installed${NC}"

    if ! kubectl cluster-info > /dev/null 2>&1; then
        echo -e "${RED}✗ Cannot connect to Kubernetes cluster${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ Connected to Kubernetes cluster${NC}"
    return 0
}

copy_config() {
    if [ ! -f "$CHATBOT_DIR/config/chatbot-models.yaml" ]; then
        echo -e "${BLUE}Creating configuration file...${NC}"
        cp "$CHATBOT_DIR/config/chatbot-models.example.yaml" "$CHATBOT_DIR/config/chatbot-models.yaml"
        echo -e "${GREEN}✓ Config created at $CHATBOT_DIR/config/chatbot-models.yaml${NC}\n"
    fi
}

# ============================================================================
# Start in Local Mode (Docker Compose)
# ============================================================================

start_local() {
    echo -e "${BLUE}Starting chatbot locally with Docker Compose...${NC}\n"

    # Check prerequisites
    check_model_server || return 1
    check_docker || return 1
    copy_config

    # Start services
    echo -e "${BLUE}Starting Docker Compose...${NC}"
    docker-compose -f "$PROJECT_ROOT/docker-compose.chatbot.yml" up -d

    # Wait for services to be ready
    echo -e "${BLUE}Waiting for services to be ready...${NC}"
    sleep 5

    # Test the server
    if curl -s http://localhost:8000/health > /dev/null; then
        echo -e "${GREEN}✓ Chatbot API is running at http://localhost:8000${NC}\n"
    else
        echo -e "${RED}✗ Chatbot API failed to start${NC}"
        docker-compose -f "$PROJECT_ROOT/docker-compose.chatbot.yml" logs chatbot-api
        return 1
    fi

    # Generate a test token
    TEST_TOKEN="dev-$(date +%s)"

    echo -e "${GREEN}✓ Chatbot is ready!${NC}\n"
    echo -e "${BLUE}Quick commands:${NC}"
    echo "  # Check health"
    echo "  curl http://localhost:8000/health"
    echo ""
    echo "  # Start interactive CLI"
    echo "  export TOKEN='$TEST_TOKEN'"
    echo "  python $CHATBOT_DIR/cli.py --token \$TOKEN"
    echo ""
    echo "  # View logs"
    echo "  docker-compose -f $PROJECT_ROOT/docker-compose.chatbot.yml logs -f chatbot-api"
    echo ""
    echo "  # Stop services"
    echo "  docker-compose -f $PROJECT_ROOT/docker-compose.chatbot.yml down"
}

# ============================================================================
# Start in Kubernetes Mode
# ============================================================================

start_k8s() {
    echo -e "${BLUE}Deploying chatbot to Kubernetes...${NC}\n"

    # Check prerequisites
    check_model_server || return 1
    check_kubectl || return 1
    copy_config

    # Deploy
    echo -e "${BLUE}Applying Kubernetes manifests...${NC}"
    kubectl apply -f "$PROJECT_ROOT/deploy/k8s/chatbot-deployment.yaml"

    # Wait for deployment to be ready
    echo -e "${BLUE}Waiting for chatbot pods to be ready...${NC}"
    kubectl rollout status deployment/chatbot-api -n chatbot --timeout=60s

    echo -e "${GREEN}✓ Chatbot deployed to Kubernetes${NC}\n"

    # Get service info
    echo -e "${BLUE}Service Information:${NC}"
    kubectl get svc -n chatbot

    echo -e "\n${BLUE}Quick commands:${NC}"
    echo "  # Port-forward to localhost"
    echo "  kubectl port-forward -n chatbot svc/chatbot-api 8000:8000"
    echo ""
    echo "  # View logs"
    echo "  kubectl logs -n chatbot deployment/chatbot-api -f"
    echo ""
    echo "  # Access from cluster"
    echo "  export API_URL='http://chatbot-api.chatbot.svc.cluster.local:8000'"
    echo "  export TOKEN='your-token'"
    echo "  python $CHATBOT_DIR/cli.py --server \$API_URL --token \$TOKEN"
    echo ""
    echo "  # Check status"
    echo "  kubectl get pods -n chatbot"
}

# ============================================================================
# Main
# ============================================================================

MODE="${1:-help}"

case "$MODE" in
    local)
        start_local
        ;;
    k8s|kubernetes)
        start_k8s
        ;;
    help)
        show_help
        ;;
    *)
        echo -e "${RED}Unknown mode: $MODE${NC}"
        show_help
        exit 1
        ;;
esac

echo ""
