# Contributing to Hapiq

Thank you for your interest in contributing to Hapiq! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Contributing Workflow](#contributing-workflow)
- [Coding Standards](#coding-standards)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [Submitting Changes](#submitting-changes)
- [Review Process](#review-process)
- [Release Process](#release-process)

## Code of Conduct

By participating in this project, you agree to abide by our code of conduct:

- **Be respectful**: Treat everyone with respect and kindness
- **Be inclusive**: Welcome contributors from all backgrounds
- **Be constructive**: Provide helpful feedback and criticism
- **Be collaborative**: Work together towards common goals
- **Be patient**: Remember that everyone is learning

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git
- Make (optional but recommended)
- golangci-lint (for code quality checks)

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/hapiq.git
   cd hapiq
   ```
3. Add the upstream repository:
   ```bash
   git remote add upstream https://github.com/btraven00/hapiq.git
   ```

## Development Setup

### Initial Setup

```bash
# Install dependencies
make deps

# Set up development environment
make setup

# Build the project
make build

# Run tests to ensure everything works
make test
```

### Development Tools

We recommend installing these tools for the best development experience:

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Install goimports
go install golang.org/x/tools/cmd/goimports@latest

# Install gotests (for generating test stubs)
go install github.com/cweill/gotests/gotests@latest
```

## Contributing Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-number-description
```

### Branch Naming Conventions

- `feature/description` - New features
- `fix/issue-description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test improvements

### 2. Make Changes

- Write clean, readable code following our [coding standards](#coding-standards)
- Add tests for new functionality
- Update documentation as needed
- Ensure all tests pass

### 3. Commit Changes

We use conventional commits for clear commit messages:

```bash
git commit -m "feat: add support for new repository type"
git commit -m "fix: handle timeout errors gracefully"
git commit -m "docs: update API documentation"
git commit -m "test: add integration tests for DOI validation"
```

#### Commit Message Format

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Adding or updating tests
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `chore`: Maintenance tasks

## Coding Standards

### Go Code Style

We follow standard Go conventions with some additional guidelines:

#### General Guidelines

- **Follow `gofmt`**: All code must be formatted with `gofmt`
- **Use `goimports`**: Organize imports properly
- **Write godoc comments**: Document all public APIs
- **Keep functions small**: Aim for functions under 50 lines
- **Use meaningful names**: Variables and functions should be self-documenting

#### Specific Rules

```go
// Good: Clear, descriptive function names
func ValidateZenodoURL(url string) (*ValidationResult, error) {
    // ...
}

// Good: Error handling
result, err := someOperation()
if err != nil {
    return nil, fmt.Errorf("failed to perform operation: %w", err)
}

// Good: Context usage for timeouts
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// Good: Constants for magic numbers
const (
    DefaultTimeout = 30 * time.Second
    MaxRetries     = 3
)
```

#### Error Handling

- Always handle errors explicitly
- Use `fmt.Errorf` with `%w` verb for error wrapping
- Return meaningful error messages
- Don't ignore errors (use `_ = err` if you must)

### Functional Programming Principles

Hapiq follows functional programming paradigms where appropriate:

#### Immutability

```go
// Good: Return new instances instead of modifying
func (v ValidationResult) WithError(err error) ValidationResult {
    newResult := v
    newResult.Error = err.Error()
    newResult.Valid = false
    return newResult
}
```

#### Pure Functions

```go
// Good: Pure function with no side effects
func CalculateLikelihoodScore(resultType string, httpStatus int, contentType string) float64 {
    // Function logic that doesn't modify external state
}
```

#### Composition

```go
// Good: Composable validation pipeline
func ValidateIdentifier(input string) ValidationResult {
    return Pipeline(input).
        Normalize().
        ValidateFormat().
        ClassifyType().
        Result()
}
```

## Testing Guidelines

### Test Organization

- Unit tests in `*_test.go` files alongside source code
- Integration tests in `test/` directory
- Use table-driven tests for multiple test cases
- Test both success and error cases

### Test Structure

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected ExpectedType
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    "valid-input",
            expected: ExpectedResult{},
            wantErr:  false,
        },
        {
            name:    "invalid input",
            input:   "invalid-input",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := FunctionName(tt.input)
            
            if (err != nil) != tt.wantErr {
                t.Errorf("FunctionName() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            
            if !tt.wantErr && !reflect.DeepEqual(result, tt.expected) {
                t.Errorf("FunctionName() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Test Coverage

- Aim for >80% test coverage
- Test edge cases and error conditions
- Use `make coverage` to generate coverage reports
- Write benchmarks for performance-critical code

### Integration Tests

```go
func TestIntegration_FeatureName(t *testing.T) {
    // Use testify/suite for complex integration tests
    // Create mock servers for external dependencies
    // Test end-to-end functionality
}
```

## Documentation

### Code Documentation

- Write godoc comments for all public functions and types
- Include examples in documentation when helpful
- Document complex algorithms and business logic

```go
// ValidateURL validates a URL and determines its repository type.
// It returns a ValidationResult containing the validation status,
// repository type, and confidence score.
//
// Example:
//   result := ValidateURL("https://zenodo.org/record/123456")
//   if result.Valid {
//       fmt.Printf("Repository type: %s", result.Type)
//   }
func ValidateURL(url string) ValidationResult {
    // ...
}
```

### README Updates

- Update README.md if adding new features
- Include usage examples for new functionality
- Update installation instructions if needed

### Architecture Documentation

- Update ARCHITECTURE.md for significant changes
- Document design decisions and trade-offs
- Keep architectural diagrams current

## Submitting Changes

### Before Submitting

Run these checks before submitting your pull request:

```bash
# Format code
make fmt

# Run linter
make lint

# Run all tests
make test

# Check for security issues
make security

# Run integration tests
make integration-test
```

### Pull Request Process

1. **Update your branch**:
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. **Push your changes**:
   ```bash
   git push origin your-branch-name
   ```

3. **Create a Pull Request**:
   - Use a clear, descriptive title
   - Reference any related issues
   - Provide a detailed description of changes
   - Include testing information

### Pull Request Template

```markdown
## Description
Brief description of changes and motivation.

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] No breaking changes (or clearly documented)
```

## Review Process

### What We Look For

- **Correctness**: Does the code work as intended?
- **Design**: Is the code well-designed and fits the architecture?
- **Functionality**: Does it solve the stated problem?
- **Complexity**: Is the code as simple as possible?
- **Tests**: Are there appropriate tests?
- **Naming**: Are names clear and meaningful?
- **Comments**: Are comments useful and necessary?
- **Documentation**: Is public API documented?

### Review Timeline

- Initial review within 2-3 business days
- Follow-up reviews within 1-2 business days
- Urgent fixes reviewed within 24 hours

### Addressing Feedback

- Address all review comments
- Ask questions if feedback is unclear
- Make requested changes in new commits
- Squash commits before merge if requested

## Release Process

### Versioning

We use Semantic Versioning (SemVer):

- **MAJOR**: Incompatible API changes
- **MINOR**: New functionality (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

### Release Checklist

1. Update version in relevant files
2. Update CHANGELOG.md
3. Create release notes
4. Tag the release
5. Update documentation
6. Announce the release

## Getting Help

### Communication Channels

- **GitHub Issues**: Bug reports and feature requests
- **GitHub Discussions**: General questions and ideas
- **Pull Request Comments**: Code-specific discussions

### Asking Questions

When asking for help:

1. Search existing issues and discussions first
2. Provide clear reproduction steps for bugs
3. Include relevant code snippets
4. Specify your environment (OS, Go version, etc.)

### Reporting Bugs

Use this template for bug reports:

```markdown
## Bug Description
Clear description of the bug.

## Steps to Reproduce
1. Step one
2. Step two
3. Step three

## Expected Behavior
What should happen.

## Actual Behavior
What actually happens.

## Environment
- OS: [e.g., Linux, macOS, Windows]
- Go version: [e.g., 1.21.0]
- Hapiq version: [e.g., v0.1.0]

## Additional Context
Any other relevant information.
```

## Recognition

Contributors will be recognized in:

- CONTRIBUTORS.md file
- Release notes for significant contributions
- GitHub contributor graphs

Thank you for contributing to Hapiq! Your efforts help make scientific dataset discovery more accessible and reliable.