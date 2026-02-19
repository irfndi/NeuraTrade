# Contributing to GoFlux

Thank you for your interest in contributing to GoFlux! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment for everyone.

## How to Contribute

### Reporting Bugs

Before submitting a bug report:

1. Check the [issue tracker](https://github.com/irfndi/goflux/issues) to see if the issue already exists
2. If not, create a new issue with:
   - A clear, descriptive title
   - Steps to reproduce the bug
   - Expected vs actual behavior
   - Go version and OS information
   - Code samples if applicable

### Suggesting Features

We welcome feature suggestions! Please:

1. Check existing issues and the [BEADS.md](./BEADS.md) roadmap
2. Open a new issue with the `enhancement` label
3. Describe the feature and its use case
4. Include code examples if helpful

### Pull Requests

1. **Fork** the repository
2. **Create a branch** from `main`:
   ```sh
   git checkout -b feature/my-feature
   ```
3. **Make your changes** following our coding standards
4. **Add tests** for new functionality
5. **Run tests** to ensure everything passes:
   ```sh
   make test
   ```
6. **Run linting**:
   ```sh
   make lint
   ```
7. **Commit** with clear messages:
   ```sh
   git commit -m "feat: add new indicator X"
   ```
8. **Push** and create a Pull Request

## Development Setup

### Prerequisites

- Go 1.21 or later
- Make

### Getting Started

```sh
# Clone your fork
git clone https://github.com/irfndi/goflux.git
cd goflux

# Install development tools
make bootstrap

# Run tests
make test

# Run linting
make lint
```

## Coding Standards

### Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` and `goimports` for formatting
- Keep functions small and focused
- Write descriptive variable and function names

### Documentation

- All exported functions, types, and constants must have doc comments
- Use complete sentences starting with the function/type name
- Include examples where helpful

```go
// NewSMAIndicator creates a Simple Moving Average indicator with the given
// window size. It calculates the average of the last n values of the
// underlying indicator.
func NewSMAIndicator(indicator Indicator, window int) Indicator {
    // ...
}
```

### Testing

- Write tests for all new functionality
- Use table-driven tests where appropriate
- Aim for high test coverage
- Test edge cases and error conditions

```go
func TestSMAIndicator(t *testing.T) {
    tests := []struct {
        name     string
        window   int
        expected string
    }{
        {"basic case", 5, "10.00"},
        {"single value", 1, "15.00"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `test:` - Adding or updating tests
- `refactor:` - Code refactoring
- `chore:` - Maintenance tasks

Examples:
```
feat: add Hull Moving Average indicator
fix: correct EMA calculation for edge case
docs: update README with new examples
test: add benchmark for MACD indicator
```

## Adding New Indicators

When adding a new indicator:

1. Create the indicator file: `indicator_<name>.go`
2. Create the test file: `indicator_<name>_test.go`
3. Follow the existing patterns in the codebase
4. Implement the `Indicator` interface
5. Add comprehensive tests
6. Update documentation in README.md
7. Add to BEADS.md if it's a planned indicator

### Indicator Template

```go
package goflux

import "github.com/irfndi/goflux/pkg/decimal"

// MyIndicator is a description of what this indicator does.
type myIndicator struct {
    indicator Indicator
    window    int
}

// NewMyIndicator creates a new MyIndicator with the specified window.
func NewMyIndicator(indicator Indicator, window int) Indicator {
    return &myIndicator{
        indicator: indicator,
        window:    window,
    }
}

// Calculate returns the indicator value at the given index.
func (mi *myIndicator) Calculate(index int) decimal.Decimal {
    // Implementation
}
```

## Questions?

If you have questions, feel free to:

- Open a [Discussion](https://github.com/irfndi/goflux/discussions)
- Check existing issues for similar questions

Thank you for contributing to GoFlux!
