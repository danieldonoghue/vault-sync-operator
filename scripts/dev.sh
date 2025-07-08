#!/bin/bash

# Development helper script for vault-sync-operator
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
log_info() {
    echo -e "${BLUE}INFO:${NC} $1"
}

log_success() {
    echo -e "${GREEN}SUCCESS:${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}WARNING:${NC} $1"
}

log_error() {
    echo -e "${RED}ERROR:${NC} $1"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Verify required tools
check_dependencies() {
    log_info "Checking dependencies..."
    
    local missing=0
    
    if ! command_exists go; then
        log_error "Go is not installed"
        missing=1
    else
        log_success "Go $(go version | cut -d' ' -f3) found"
    fi
    
    if ! command_exists docker; then
        log_error "Docker is not installed"
        missing=1
    else
        log_success "Docker $(docker --version | cut -d' ' -f3 | tr -d ',') found"
    fi
    
    if ! command_exists git; then
        log_error "Git is not installed"
        missing=1
    else
        log_success "Git $(git --version | cut -d' ' -f3) found"
    fi
    
    if [ $missing -eq 1 ]; then
        log_error "Missing required dependencies"
        exit 1
    fi
}

# Run tests
run_tests() {
    log_info "Running tests..."
    go test -v -race -coverprofile=coverage.out ./...
    log_success "Tests completed"
}

# Run linting
run_lint() {
    log_info "Running linting..."
    if command_exists golangci-lint; then
        golangci-lint run --timeout=5m
        log_success "Linting completed"
    else
        log_warn "golangci-lint not found, skipping"
        log_info "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    fi
}

# Build binary
build_binary() {
    local version=${1:-"dev"}
    local commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    local date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    
    log_info "Building binary (version: $version, commit: $commit)..."
    
    CGO_ENABLED=0 go build \
        -a \
        -ldflags="-w -s -extldflags '-static' -X main.version=${version} -X main.commit=${commit} -X main.date=${date}" \
        -tags netgo,osusergo \
        -o vault-sync-operator \
        cmd/main.go
    
    log_success "Binary built: vault-sync-operator"
}

# Build Docker image
build_docker() {
    local tag=${1:-"vault-sync-operator:dev"}
    local version=${2:-"dev"}
    local commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    local date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    
    log_info "Building Docker image: $tag"
    
    docker build \
        --build-arg VERSION="$version" \
        --build-arg COMMIT="$commit" \
        --build-arg DATE="$date" \
        -t "$tag" \
        .
    
    log_success "Docker image built: $tag"
}

# Build multi-arch Docker image
build_docker_multiarch() {
    local tag=${1:-"vault-sync-operator:dev"}
    local version=${2:-"dev"}
    local commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    local date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    
    log_info "Building multi-architecture Docker image: $tag"
    
    if ! docker buildx ls | grep -q "vault-sync-builder"; then
        log_info "Creating buildx builder..."
        docker buildx create --name vault-sync-builder --use
    fi
    
    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        --build-arg VERSION="$version" \
        --build-arg COMMIT="$commit" \
        --build-arg DATE="$date" \
        -t "$tag" \
        --load \
        .
    
    log_success "Multi-arch Docker image built: $tag"
}

# Clean build artifacts
clean() {
    log_info "Cleaning build artifacts..."
    
    rm -f vault-sync-operator
    rm -f coverage.out
    
    # Clean Go cache
    go clean -cache
    
    log_success "Clean completed"
}

# Show help
show_help() {
    echo "Development helper script for vault-sync-operator"
    echo ""
    echo "Usage: $0 [command] [options]"
    echo ""
    echo "Commands:"
    echo "  check       Check dependencies"
    echo "  test        Run tests"
    echo "  lint        Run linting"
    echo "  build       Build binary [version]"
    echo "  docker      Build Docker image [tag] [version]"
    echo "  docker-ma   Build multi-arch Docker image [tag] [version]"
    echo "  clean       Clean build artifacts"
    echo "  all         Run check, test, lint, and build"
    echo "  help        Show this help"
    echo ""
    echo "Examples:"
    echo "  $0 build v1.0.0"
    echo "  $0 docker vault-sync-operator:latest v1.0.0"
    echo "  $0 all"
}

# Run all checks and build
run_all() {
    check_dependencies
    run_tests
    run_lint
    build_binary "$1"
}

# Main script logic
case "${1:-help}" in
    check)
        check_dependencies
        ;;
    test)
        run_tests
        ;;
    lint)
        run_lint
        ;;
    build)
        check_dependencies
        build_binary "$2"
        ;;
    docker)
        check_dependencies
        build_docker "$2" "$3"
        ;;
    docker-ma)
        check_dependencies
        build_docker_multiarch "$2" "$3"
        ;;
    clean)
        clean
        ;;
    all)
        run_all "$2"
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        log_error "Unknown command: $1"
        show_help
        exit 1
        ;;
esac
