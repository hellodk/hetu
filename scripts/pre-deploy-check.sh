#!/bin/bash
#
# Pre-Deploy Validation Script
# Runs comprehensive checks before deployment
#
# Usage: ./scripts/pre-deploy-check.sh [--fix]
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
VERSION_FILE="$ROOT_DIR/VERSION"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

ERRORS=0
WARNINGS=0
FIX_MODE=false

[[ "$1" == "--fix" ]] && FIX_MODE=true

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║              Pre-Deploy Validation Checks                     ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""

check_pass() {
    echo -e "  ${GREEN}✓${NC} $1"
}

check_fail() {
    echo -e "  ${RED}✗${NC} $1"
    ((ERRORS++))
}

check_warn() {
    echo -e "  ${YELLOW}!${NC} $1"
    ((WARNINGS++))
}

# ═══════════════════════════════════════════════════════════════
# 1. VERSION FILE CHECKS
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[1/7] Checking VERSION file...${NC}"

if [[ -f "$VERSION_FILE" ]]; then
    VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
    if [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        check_pass "VERSION file exists with valid format: $VERSION"
    else
        check_fail "VERSION file has invalid format: $VERSION"
    fi
else
    check_fail "VERSION file not found"
fi

# ═══════════════════════════════════════════════════════════════
# 2. MANIFEST VALIDATION
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[2/7] Validating Kubernetes manifests...${NC}"

for manifest in "$ROOT_DIR/manifests/full-stack/cluster-intel.yaml" "$ROOT_DIR/manifests/simple/deployment.yaml"; do
    if [[ -f "$manifest" ]]; then
        # Check YAML syntax
        if command -v python3 &> /dev/null; then
            if python3 -c "import yaml; yaml.safe_load_all(open('$manifest'))" 2>/dev/null; then
                check_pass "$(basename $manifest): Valid YAML syntax"
            else
                check_fail "$(basename $manifest): Invalid YAML syntax"
            fi
        elif command -v kubectl &> /dev/null; then
            if kubectl apply --dry-run=client -f "$manifest" &>/dev/null; then
                check_pass "$(basename $manifest): Valid Kubernetes manifest"
            else
                check_warn "$(basename $manifest): Could not validate (kubectl dry-run failed)"
            fi
        else
            check_warn "$(basename $manifest): Skipped (no validator available)"
        fi
    else
        check_fail "$(basename $manifest): File not found"
    fi
done

# ═══════════════════════════════════════════════════════════════
# 3. PYTHON SYNTAX CHECK
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[3/7] Checking embedded Python syntax...${NC}"

if command -v python3 &> /dev/null; then
    # Extract Python code from manifests and check syntax
    for manifest in "$ROOT_DIR/manifests/full-stack/cluster-intel.yaml"; do
        if [[ -f "$manifest" ]]; then
            # Extract app.py content and check syntax
            TEMP_PY=$(mktemp --suffix=.py)
            grep -A 10000 "app.py: |" "$manifest" | tail -n +2 | sed '/^[^ ]/q' | head -n -1 | sed 's/^    //' > "$TEMP_PY" 2>/dev/null || true
            if [[ -s "$TEMP_PY" ]]; then
                if python3 -m py_compile "$TEMP_PY" 2>/dev/null; then
                    check_pass "Embedded Python: Valid syntax"
                else
                    check_fail "Embedded Python: Syntax error detected"
                fi
            fi
            rm -f "$TEMP_PY"
        fi
    done
else
    check_warn "Python3 not available for syntax check"
fi

# ═══════════════════════════════════════════════════════════════
# 4. REQUIRED FILES CHECK
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[4/7] Checking required files...${NC}"

REQUIRED_FILES=(
    "VERSION"
    "README.md"
    "CHANGELOG.md"
    "deploy.sh"
    "manifests/full-stack/cluster-intel.yaml"
    "manifests/simple/deployment.yaml"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [[ -f "$ROOT_DIR/$file" ]]; then
        check_pass "$file exists"
    else
        check_fail "$file missing"
    fi
done

# ═══════════════════════════════════════════════════════════════
# 5. SCRIPT PERMISSIONS
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[5/7] Checking script permissions...${NC}"

SCRIPTS=(
    "deploy.sh"
    "scripts/bump-version.sh"
    "scripts/pre-deploy-check.sh"
)

for script in "${SCRIPTS[@]}"; do
    if [[ -f "$ROOT_DIR/$script" ]]; then
        if [[ -x "$ROOT_DIR/$script" ]]; then
            check_pass "$script is executable"
        else
            if [[ "$FIX_MODE" == "true" ]]; then
                chmod +x "$ROOT_DIR/$script"
                check_pass "$script made executable (fixed)"
            else
                check_warn "$script is not executable (use --fix to correct)"
            fi
        fi
    fi
done

# ═══════════════════════════════════════════════════════════════
# 6. KUBECTL CONNECTION
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[6/7] Checking Kubernetes connectivity...${NC}"

if command -v kubectl &> /dev/null; then
    if kubectl cluster-info &>/dev/null; then
        CLUSTER=$(kubectl config current-context 2>/dev/null || echo "unknown")
        check_pass "Connected to cluster: $CLUSTER"
        
        # Check RBAC permissions
        if kubectl auth can-i create deployments --namespace=cluster-intel &>/dev/null; then
            check_pass "Have deployment permissions"
        else
            check_warn "May not have deployment permissions in cluster-intel namespace"
        fi
    else
        check_warn "Not connected to a Kubernetes cluster"
    fi
else
    check_fail "kubectl not installed"
fi

# ═══════════════════════════════════════════════════════════════
# 7. VERSION CONSISTENCY
# ═══════════════════════════════════════════════════════════════
echo -e "${BLUE}[7/7] Checking version consistency...${NC}"

if [[ -n "$VERSION" ]]; then
    # Check deploy.sh version
    DEPLOY_VERSION=$(grep "# Version:" "$ROOT_DIR/deploy.sh" | head -1 | awk '{print $3}')
    if [[ "$DEPLOY_VERSION" == "$VERSION" ]]; then
        check_pass "deploy.sh version matches: $DEPLOY_VERSION"
    else
        check_warn "deploy.sh version mismatch: $DEPLOY_VERSION vs $VERSION"
    fi
    
    # Check CHANGELOG has entry for current version
    if grep -q "\[$VERSION\]" "$ROOT_DIR/CHANGELOG.md" 2>/dev/null; then
        check_pass "CHANGELOG.md has entry for v$VERSION"
    else
        check_warn "CHANGELOG.md missing entry for v$VERSION"
    fi
fi

# ═══════════════════════════════════════════════════════════════
# SUMMARY
# ═══════════════════════════════════════════════════════════════
echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
echo -e "  ${GREEN}Passed:${NC}   $((7 * 3 - ERRORS - WARNINGS)) checks"
echo -e "  ${YELLOW}Warnings:${NC} $WARNINGS"
echo -e "  ${RED}Errors:${NC}   $ERRORS"
echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"

if [[ $ERRORS -gt 0 ]]; then
    echo ""
    echo -e "${RED}Pre-deploy validation FAILED with $ERRORS error(s)${NC}"
    echo "Please fix the errors before deploying."
    exit 1
elif [[ $WARNINGS -gt 0 ]]; then
    echo ""
    echo -e "${YELLOW}Pre-deploy validation passed with $WARNINGS warning(s)${NC}"
    echo "Review warnings before proceeding."
    exit 0
else
    echo ""
    echo -e "${GREEN}Pre-deploy validation PASSED${NC}"
    echo "Ready to deploy!"
    exit 0
fi
