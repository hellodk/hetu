#!/bin/bash
#
# Semantic Version Bump Script
# Usage: ./scripts/bump-version.sh [major|minor|patch] [--dry-run]
#
# Examples:
#   ./scripts/bump-version.sh patch        # 4.1.0 -> 4.1.1
#   ./scripts/bump-version.sh minor        # 4.1.0 -> 4.2.0
#   ./scripts/bump-version.sh major        # 4.1.0 -> 5.0.0
#   ./scripts/bump-version.sh patch --dry-run  # Show what would happen
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
VERSION_FILE="$ROOT_DIR/VERSION"
CHANGELOG_FILE="$ROOT_DIR/CHANGELOG.md"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

DRY_RUN=false
BUMP_TYPE="${1:-patch}"

# Parse arguments
for arg in "$@"; do
    case $arg in
        --dry-run)
            DRY_RUN=true
            ;;
        major|minor|patch)
            BUMP_TYPE="$arg"
            ;;
        -h|--help)
            echo "Usage: $0 [major|minor|patch] [--dry-run]"
            echo ""
            echo "Semantic versioning bump script"
            echo ""
            echo "Arguments:"
            echo "  major     Bump major version (breaking changes)"
            echo "  minor     Bump minor version (new features)"
            echo "  patch     Bump patch version (bug fixes)"
            echo "  --dry-run Show what would happen without making changes"
            exit 0
            ;;
    esac
done

# Read current version
if [[ ! -f "$VERSION_FILE" ]]; then
    echo -e "${RED}ERROR: VERSION file not found at $VERSION_FILE${NC}"
    exit 1
fi

CURRENT_VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')

# Validate version format
if [[ ! "$CURRENT_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo -e "${RED}ERROR: Invalid version format: $CURRENT_VERSION${NC}"
    echo "Expected format: MAJOR.MINOR.PATCH (e.g., 1.2.3)"
    exit 1
fi

# Parse version components
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"

# Calculate new version
case "$BUMP_TYPE" in
    major)
        NEW_MAJOR=$((MAJOR + 1))
        NEW_MINOR=0
        NEW_PATCH=0
        ;;
    minor)
        NEW_MAJOR=$MAJOR
        NEW_MINOR=$((MINOR + 1))
        NEW_PATCH=0
        ;;
    patch)
        NEW_MAJOR=$MAJOR
        NEW_MINOR=$MINOR
        NEW_PATCH=$((PATCH + 1))
        ;;
    *)
        echo -e "${RED}ERROR: Invalid bump type: $BUMP_TYPE${NC}"
        echo "Valid options: major, minor, patch"
        exit 1
        ;;
esac

NEW_VERSION="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║              Semantic Version Bump                            ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Bump type:       ${YELLOW}$BUMP_TYPE${NC}"
echo -e "  Current version: ${YELLOW}$CURRENT_VERSION${NC}"
echo -e "  New version:     ${GREEN}$NEW_VERSION${NC}"
echo ""

if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${YELLOW}[DRY RUN] No changes will be made${NC}"
    echo ""
    echo "Files that would be updated:"
    echo "  - $VERSION_FILE"
    echo "  - $CHANGELOG_FILE"
    echo "  - $ROOT_DIR/deploy.sh"
    echo "  - $ROOT_DIR/manifests/full-stack/cluster-intel.yaml"
    echo "  - $ROOT_DIR/manifests/simple/deployment.yaml"
    exit 0
fi

# Confirm
read -p "Proceed with version bump? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Aborted${NC}"
    exit 0
fi

echo -e "${BLUE}[1/5] Updating VERSION file...${NC}"
echo "$NEW_VERSION" > "$VERSION_FILE"

echo -e "${BLUE}[2/5] Updating CHANGELOG.md...${NC}"
DATE=$(date +%Y-%m-%d)
TEMP_CHANGELOG=$(mktemp)

# Create new changelog entry
cat > "$TEMP_CHANGELOG" << EOF
# Changelog

All notable changes to K8s Cluster Intelligence Engine will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [${NEW_VERSION}] - ${DATE}

### Changed
- Version bump from ${CURRENT_VERSION} to ${NEW_VERSION}

EOF

# Append existing changelog (skip header if exists)
if [[ -f "$CHANGELOG_FILE" ]]; then
    # Skip the first few header lines and append the rest
    tail -n +8 "$CHANGELOG_FILE" >> "$TEMP_CHANGELOG" 2>/dev/null || true
fi

mv "$TEMP_CHANGELOG" "$CHANGELOG_FILE"

echo -e "${BLUE}[3/5] Updating deploy.sh...${NC}"
sed -i "s/# Version: .*/# Version: $NEW_VERSION/" "$ROOT_DIR/deploy.sh"
sed -i "s/Engine v[0-9]*\.[0-9]*/Engine v${NEW_MAJOR}.${NEW_MINOR}/" "$ROOT_DIR/deploy.sh"

echo -e "${BLUE}[4/5] Updating manifests...${NC}"
# Update full-stack manifest
if [[ -f "$ROOT_DIR/manifests/full-stack/cluster-intel.yaml" ]]; then
    sed -i "s/version: \"[0-9]*\.[0-9]*\.[0-9]*\"/version: \"$NEW_VERSION\"/" "$ROOT_DIR/manifests/full-stack/cluster-intel.yaml"
    sed -i "s/app.kubernetes.io\/version: \"[0-9]*\.[0-9]*\.[0-9]*\"/app.kubernetes.io\/version: \"$NEW_VERSION\"/" "$ROOT_DIR/manifests/full-stack/cluster-intel.yaml"
fi

# Update simple manifest
if [[ -f "$ROOT_DIR/manifests/simple/deployment.yaml" ]]; then
    sed -i "s/version: \"[0-9]*\.[0-9]*\.[0-9]*\"/version: \"$NEW_VERSION\"/" "$ROOT_DIR/manifests/simple/deployment.yaml"
    sed -i "s/app.kubernetes.io\/version: \"[0-9]*\.[0-9]*\.[0-9]*\"/app.kubernetes.io\/version: \"$NEW_VERSION\"/" "$ROOT_DIR/manifests/simple/deployment.yaml"
fi

echo -e "${BLUE}[5/5] Creating git tag...${NC}"
if command -v git &> /dev/null && [[ -d "$ROOT_DIR/.git" ]]; then
    git add "$VERSION_FILE" "$CHANGELOG_FILE" "$ROOT_DIR/deploy.sh" "$ROOT_DIR/manifests/" 2>/dev/null || true
    echo -e "${GREEN}Files staged for commit${NC}"
    echo ""
    echo "To complete the release:"
    echo "  git commit -m \"chore: bump version to $NEW_VERSION\""
    echo "  git tag -a v$NEW_VERSION -m \"Release v$NEW_VERSION\""
    echo "  git push origin main --tags"
else
    echo -e "${YELLOW}Git not available or not a git repository${NC}"
fi

echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  Version bumped successfully: $CURRENT_VERSION → $NEW_VERSION${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
