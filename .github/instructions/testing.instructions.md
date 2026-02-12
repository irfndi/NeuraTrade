---
applyTo: "**/*_test.go,test/**/*"
---

# Testing Instructions

## Test Organization

- Unit tests: Place beside source files as `*_test.go`
- Integration tests: Place in `test/integration/`
- Mocks: Place in `internal/testutil` or `test/testmocks`

## Testing Patterns

### Table-Driven Tests

Use table-driven tests for comprehensive coverage:

```go
func TestFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected OutputType
        wantErr  bool
    }{
        {"case 1", input1, expected1, false},
        {"case 2", input2, expected2, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Function(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Financial Calculation Tests

- Test precision edge cases
- Test rounding behavior
- Test overflow scenarios
- Use `decimal.Decimal` comparisons

## Coverage Requirements

- Minimum 80% global coverage gate in CI (strict)
- Target 85%+ coverage on touched packages
- Focus on edge cases and error paths
- Test concurrent access patterns
- Every task should define unit + integration + e2e/smoke scope (or explicit N/A with reason)

## Test Commands

```bash
make test                    # Run all tests
make test-coverage           # Run with coverage report
make coverage-check          # Strict coverage threshold gate (default 80%)
go test -v ./internal/...    # Run specific package tests
go test -race ./...          # Run with race detection
```

## Mocking Guidelines

- Use interface-based mocking
- Prefer `pgxmock` for database mocks
- Use `miniredis` for Redis mocks
- Keep mocks simple and focused
