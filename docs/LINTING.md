# Guide to Fixing Linting Issues in Hapiq

This document provides guidance on fixing the common linting issues found in the Hapiq codebase.

## Running the Linter

To run the linter:

```bash
# Standard linting
make lint

# Auto-fix the issues that can be fixed automatically
make lint-fix

# Format code with gofmt
make fmt

# Run all code quality checks
make check
```

## Common Linting Issues and How to Fix Them

### 1. Package Comments Missing (`ST1000`)

**Issue**: Files need a package comment describing the package's purpose.

**Fix**: Add a package-level comment at the top of your file:

```go
// Package cmd provides the command-line interface for the Hapiq application.
package cmd
```

### 2. Formatting Issues (`goimports`, `gofmt`)

**Fix**: Run `make fmt` or:

```bash
gofmt -w file.go
goimports -w file.go
```

### 3. Print Statements (`forbidigo`)

**Issue**: Direct use of `fmt.Print`, `fmt.Printf`, `fmt.Println` is discouraged.

**Fix**: Replace with structured logging or implement a dedicated logger:

```go
// Replace this:
fmt.Printf("Downloading from %s: %s\n", sourceType, id)

// With this:
log.Infof("Downloading from %s: %s", sourceType, id)
```

### 4. Error Return Values Not Checked (`errcheck`)

**Issue**: Return values from functions like `Close()`, `Flush()`, etc. aren't checked.

**Fix**: 
```go
// Replace this:
file.Close()

// With this:
if err := file.Close(); err != nil {
    log.Warnf("Failed to close file: %v", err)
}

// Or for defer statements:
defer func() {
    if err := file.Close(); err != nil {
        log.Warnf("Failed to close file: %v", err)
    }
}()
```

### 5. Unused Parameters (`unused-parameter`, `revive`)

**Issue**: Function parameters aren't used in the function body.

**Fix**: Rename unused parameters to `_` to explicitly mark them as unused:

```go
// Replace this:
func runCheck(cmd *cobra.Command, args []string) error {

// With this:
func runCheck(_ *cobra.Command, args []string) error {
```

### 6. High Cognitive Complexity (`gocognit`)

**Issue**: Functions are too complex and hard to understand.

**Fix**: Break down complex functions into smaller, more focused functions:

```go
// Instead of one large function:
func complexFunction() {
    // 50 lines of code
}

// Break it down:
func complexFunction() {
    doFirstPart()
    doSecondPart()
    doThirdPart()
}

func doFirstPart() {
    // 15 lines of code
}

func doSecondPart() {
    // 20 lines of code
}

func doThirdPart() {
    // 15 lines of code
}
```

### 7. Init Functions (`gochecknoinits`)

**Issue**: `init()` functions can cause unexpected side effects and initialization order issues.

**Fix**: Replace with explicit initialization functions:

```go
// Replace this:
func init() {
    // initialize something
}

// With this:
func InitializeMyComponent() {
    // initialize something
}

// Then call it explicitly at the appropriate time
```

### 8. Comments Not Ending in Periods (`godot`)

**Fix**: Add periods to the end of comments:

```go
// This comment needs a period
// This comment has a period.
```

### 9. Code Style Issues (`wsl`)

**Issue**: Whitespace and structure issues like improper cuddling of statements.

**Fix**: Follow the whitespace guidelines:
- Don't cuddle if statements with non-related statements
- Keep consistent line breaks between blocks
- Structure code with appropriate spacing

```go
// Bad:
a := getA()
if a > 5 {
    // ...
}

// Good:
a := getA()

if a > 5 {
    // ...
}
```

### 10. Security Issues (`gosec`)

**Issue**: Potential security problems.

**Fix**: Follow secure coding practices:

```go
// Instead of:
if err := os.MkdirAll(outputDir, 0755); err != nil {

// Use more restrictive permissions:
if err := os.MkdirAll(outputDir, 0750); err != nil {
```

### 11. Index Boundary Issues (`gocritic`)

**Issue**: Potential index out of bounds issues.

**Fix**: Check if an index is valid before using it:

```go
// Instead of:
cleaned = cleaned[:bracketIdx]

// Check for -1 (not found):
if bracketIdx > 0 {
    cleaned = cleaned[:bracketIdx]
}
```

## Creating a Custom Linting Configuration

If some linting rules are too strict or not applicable to your project, you can customize the linting configuration in `.golangci.yml`. For example:

1. To disable a specific linter completely:
   ```yaml
   linters:
     disable:
       - forbidigo  # Disable the forbidigo linter
   ```

2. To exclude specific linting issues:
   ```yaml
   issues:
     exclude:
       - "exported function .* should have comment"
   ```

3. To configure a specific linter:
   ```yaml
   linters-settings:
     gocognit:
       min-complexity: 25  # Increase complexity threshold
   ```

## Recommended Approach for Large Codebases

1. **Fix one category at a time**: Start with the easiest fixes like formatting and comments
2. **Use linter directives sparingly**: For legitimate exceptions
3. **Automated fixes first**: Run `make lint-fix` first, then tackle manual fixes
4. **Fix by directory**: Work through one package at a time

## Best Practices for Keeping Code Lint-Free

1. Run linting as part of your pre-commit workflow
2. Set up linting in your IDE for real-time feedback
3. Run `make check` before submitting PRs
4. Regularly update your linting configuration as the project evolves