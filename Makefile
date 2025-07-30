# Hapiq Makefile
# Common development tasks for the hapiq CLI tool

.PHONY: help build test clean install run fmt vet lint lint-fix install-lint deps tidy check coverage benchmark

# Default target
help: ## Show this help message
	@echo 'Usage: make <target>'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build the binary
build: ## Build the hapiq binary
	@echo "Building hapiq..."
	go build -o bin/hapiq -ldflags="-s -w" .

# Build for multiple platforms
build-all: ## Build for multiple platforms
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build -o bin/hapiq-linux-amd64 -ldflags="-s -w" .
	GOOS=darwin GOARCH=amd64 go build -o bin/hapiq-darwin-amd64 -ldflags="-s -w" .
	GOOS=darwin GOARCH=arm64 go build -o bin/hapiq-darwin-arm64 -ldflags="-s -w" .
	GOOS=windows GOARCH=amd64 go build -o bin/hapiq-windows-amd64.exe -ldflags="-s -w" .

# Install the binary to GOPATH/bin
install: ## Install hapiq to GOPATH/bin
	@echo "Installing hapiq..."
	go install -ldflags="-s -w" .

# Run tests
test: ## Run all tests
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run benchmarks
benchmark: ## Run benchmark tests
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Clean build artifacts
clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html

# Format code
fmt: ## Format Go code
	@echo "Formatting code..."
	go fmt ./...

# Vet code
vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

# Run golangci-lint (requires golangci-lint to be installed)
# Usage: make lint [ARGS="--fix"]
lint: ## Run golangci-lint (pass ARGS="--fix" to auto-fix issues)
	@echo "Running golangci-lint..."
	@if [ -f scripts/lint.sh ]; then \
		./scripts/lint.sh $(ARGS); \
	else \
		@if command -v golangci-lint >/dev/null 2>&1; then \
			golangci-lint run --timeout=5m $(ARGS); \
		else \
			echo "golangci-lint not found. Install it with 'make install-lint'"; \
		fi \
	fi

# Install golangci-lint
install-lint: ## Install golangci-lint
	@echo "Installing golangci-lint..."
	@if [ -f scripts/lint.sh ]; then \
		./scripts/lint.sh --help >/dev/null 2>&1; \
	else \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v1.56.2; \
	fi

# Shortcut to run linter with fix flag
lint-fix: ## Run golangci-lint with auto-fix enabled
	@$(MAKE) lint ARGS="--fix"

# Download dependencies
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download

# Tidy up dependencies
tidy: ## Tidy up go.mod
	@echo "Tidying up dependencies..."
	go mod tidy

# Run all checks (fmt, vet, lint, test)
check: fmt vet lint test ## Run all code quality checks

# Quick development run
run: build ## Build and run with example
	@echo "Running hapiq with example..."
	./bin/hapiq check https://zenodo.org/record/123456

# Development setup
setup: deps install-lint ## Setup development environment
	@echo "Setting up development environment..."
	@echo "Development setup complete!"

# Release preparation
release-check: clean check build-all ## Prepare for release (run all checks and build all platforms)
	@echo "Release preparation complete!"

# Docker build (if Dockerfile exists)
docker-build: ## Build Docker image
	@if [ -f Dockerfile ]; then \
		echo "Building Docker image..."; \
		docker build -t hapiq:latest .; \
	else \
		echo "Dockerfile not found"; \
	fi

# Show project info
info: ## Show project information
	@echo "Project: Hapiq"
	@echo "Description: CLI tool for extracting and inspecting dataset links from scientific papers"
	@echo "Go version: $(shell go version)"
	@echo "Module: $(shell go list -m)"
	@echo "Dependencies:"
	@go list -m all | grep -v "$(shell go list -m)" | head -10
