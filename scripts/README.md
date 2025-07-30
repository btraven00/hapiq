# Hapiq Development Scripts

This directory contains utility scripts for the Hapiq project development workflow.

## Available Scripts

### `lint.sh`

A wrapper script for running `golangci-lint` with consistent settings across all development environments.

#### Features:

- Automatically installs `golangci-lint` v1.56.2+ if not available
- Uses project's `.golangci.yml` configuration
- Provides helpful output formatting
- Ensures consistent timeout settings
- Can be used both locally and in CI environments

#### Usage:

```bash
# Basic usage
./scripts/lint.sh

# With custom path
./scripts/lint.sh ./pkg/...

# With custom options
./scripts/lint.sh --fix
./scripts/lint.sh --timeout=10m
./scripts/lint.sh --disable=errcheck,gosimple

# Check version
./scripts/lint.sh --version
```

## Integration with Makefile

These scripts are integrated with the project's Makefile. You can use:

```bash
# Run linter
make lint

# Install linter
make install-lint
```

## Guidelines for Adding New Scripts

When adding new scripts to this directory:

1. Make the script executable (`chmod +x scripts/your-script.sh`)
2. Add appropriate documentation in this README
3. Include script usage information at the top of the script file
4. Consider integrating with the Makefile if appropriate
5. Use a consistent style (error handling, exit codes, help text)

## CI Integration

These scripts are designed to work seamlessly with the project's GitHub Actions workflows. The linting script in particular is synchronized with the linter version used in CI to ensure consistent results between local development and the CI environment.