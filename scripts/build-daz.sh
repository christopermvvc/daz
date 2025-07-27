#!/bin/bash
# Centralized build script for Daz binary
# This is the ONLY script that should be used to build ./bin/daz

set -e  # Exit on error

# Colors for flashy output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m' # No Color

# Spinner function
spin() {
    local pid=$1
    local delay=0.1
    local spinstr='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    while [ "$(ps a | awk '{print $1}' | grep $pid)" ]; do
        local temp=${spinstr#?}
        printf " [%c]  " "$spinstr"
        local spinstr=$temp${spinstr%"$temp"}
        sleep $delay
        printf "\b\b\b\b\b\b"
    done
    printf "    \b\b\b\b"
}

# Banner
echo ""
echo -e "${PURPLE}╔════════════════════════════════════════╗${NC}"
echo -e "${PURPLE}║${CYAN}        DAZ BUILD SYSTEM v1.0           ${PURPLE}║${NC}"
echo -e "${PURPLE}╚════════════════════════════════════════╝${NC}"
echo ""

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}✗ Error: go.mod not found!${NC}"
    echo -e "${YELLOW}  Please run this script from the project root directory${NC}"
    exit 1
fi

# Display build info
echo -e "${BLUE}🔧 Build Information:${NC}"
echo -e "  ${WHITE}Target:${NC} ./bin/daz"
echo -e "  ${WHITE}Source:${NC} cmd/daz/main.go"
echo -e "  ${WHITE}Time:${NC}   $(date '+%Y-%m-%d %H:%M:%S')"
echo -e "  ${WHITE}User:${NC}   $(whoami)"
echo ""

# Check Go version
echo -e "${BLUE}🔍 Checking Go version...${NC}"
GO_VERSION=$(go version | awk '{print $3}')
echo -e "  ${GREEN}✓${NC} Found ${WHITE}${GO_VERSION}${NC}"
echo ""

# Clean previous build
if [ -f "bin/daz" ]; then
    echo -e "${YELLOW}🗑️  Removing old binary...${NC}"
    rm -f bin/daz
    echo -e "  ${GREEN}✓${NC} Old binary removed"
    echo ""
fi

# Create bin directory if it doesn't exist
mkdir -p bin

# Get dependencies
echo -e "${BLUE}📦 Checking dependencies...${NC}"
(
    go mod download > /dev/null 2>&1
) &
spin $!
echo -e "  ${GREEN}✓${NC} Dependencies ready"
echo ""

# Build with progress
echo -e "${BLUE}🏗️  Building Daz...${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Capture build output
BUILD_OUTPUT=$(mktemp)
BUILD_START=$(date +%s)

(
    go build -v -o bin/daz cmd/daz/main.go 2>&1 | tee "$BUILD_OUTPUT"
) &
BUILD_PID=$!

# Show progress
while kill -0 $BUILD_PID 2> /dev/null; do
    printf "${CYAN}▓${NC}"
    sleep 0.1
done
echo ""

BUILD_END=$(date +%s)
BUILD_TIME=$((BUILD_END - BUILD_START))

# Check if build succeeded
if [ ! -f "bin/daz" ]; then
    echo ""
    echo -e "${RED}✗ Build failed!${NC}"
    echo -e "${YELLOW}Build output:${NC}"
    cat "$BUILD_OUTPUT"
    rm -f "$BUILD_OUTPUT"
    exit 1
fi

rm -f "$BUILD_OUTPUT"

echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Display success and binary info
BINARY_SIZE=$(ls -lh bin/daz | awk '{print $5}')
BINARY_PERMS=$(ls -l bin/daz | awk '{print $1}')

echo -e "${GREEN}✅ BUILD SUCCESSFUL!${NC}"
echo ""
echo -e "${BLUE}📊 Binary Details:${NC}"
echo -e "  ${WHITE}Path:${NC}         ./bin/daz"
echo -e "  ${WHITE}Size:${NC}         ${BINARY_SIZE}"
echo -e "  ${WHITE}Permissions:${NC}  ${BINARY_PERMS}"
echo -e "  ${WHITE}Build Time:${NC}   ${BUILD_TIME}s"
echo ""

# Verify binary is executable
if [ -x "bin/daz" ]; then
    echo -e "${GREEN}✓${NC} Binary is executable"
else
    echo -e "${YELLOW}⚠️  Making binary executable...${NC}"
    chmod +x bin/daz
    echo -e "${GREEN}✓${NC} Binary is now executable"
fi

echo ""
echo -e "${PURPLE}╔════════════════════════════════════════╗${NC}"
echo -e "${PURPLE}║${GREEN}    🎉 DAZ IS READY TO RUN! 🎉          ${PURPLE}║${NC}"
echo -e "${PURPLE}╚════════════════════════════════════════╝${NC}"
echo ""
echo -e "${CYAN}Run with:${NC} ${WHITE}./bin/daz${NC} or ${WHITE}./scripts/run-console.sh${NC}"
echo ""