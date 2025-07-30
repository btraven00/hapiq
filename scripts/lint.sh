#!/bin/bash
# lint.sh - Run golangci-lint with consistent settings

set -e

# Script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Ensure we're in the project root
cd "${PROJECT_ROOT}"

# Default timeout (can be overridden by passing --timeout=X to this script)
TIMEOUT="5m"

# Check if golangci-lint is installed
if ! command -v golangci-lint &> /dev/null; then
    echo "⚠️ golangci-lint not found. Installing..."
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)/bin" v1.56.2
    echo "✅ golangci-lint installed successfully!"
fi

# Handle version check directly
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
    golangci-lint --version
    exit 0
fi

# Only show running message for normal execution (not for help/version)
if [ "$1" != "--help" ] && [ "$1" != "-h" ]; then
    echo "🔍 Running golangci-lint..."
    echo "📂 Project: $(basename "${PROJECT_ROOT}")"
    echo "🔧 Configuration: .golangci.yml"
fi

# Pass all arguments to golangci-lint
# If no timeout arg provided, use our default
if echo "$*" | grep -q -- "--timeout"; then
    golangci-lint run "$@"
else
    golangci-lint run --timeout="${TIMEOUT}" "$@"
fi

EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ Linting passed successfully!"
else
    echo "❌ Linting failed with exit code ${EXIT_CODE}"
    echo "💡 You can fix some issues automatically with: golangci-lint run --fix"
fi

exit $EXIT_CODE
