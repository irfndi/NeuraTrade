# NeuraTrade Testing Guide

## Overview

This document describes the testing strategy and patterns used in the NeuraTrade project.

## Test Structure

### Unit Tests
- Located alongside source files with `_test.go` suffix
- Test individual functions and methods in isolation
- Use mocks for external dependencies

### Integration Tests
- Located in `test/integration/`
- Test interaction between components
- Use real database (PostgreSQL) and cache (Redis) connections

### E2E Tests
- Located in `test/e2e/`
- Test complete user workflows
- Spin up full application stack

## Running Tests

### Run all tests
```bash
make test
```

### Run tests with coverage
```bash
make test-coverage
```

### Run specific package tests
```bash
cd services/backend-api
go test ./internal/services/...
go test ./internal/api/handlers/...
```

### Run with race detection
```bash
go test -race ./...
```

## Test Coverage

### Current Coverage (as of latest run)
- **High Coverage (80%+)**:
  - observability: 100%
  - metrics: 100%
  - telemetry: 97.3%
  - pkg/interfaces: 96.6%
  - config: 93.3%
  - utils: 92.1%

- **Medium Coverage (60-80%)**:
  - services/workerpool: 88.9%
  - skill: 85.6%
  - services/phase_management: 83.0%
  - services/risk: 78.9%
  - services/jobqueue: 78.5%
  - services/pubsub: 75.8%
  - cache: 66.3%
  - database: 60.2%

- **Lower Coverage (<60%)**:
  - models: 36.0% (struct definitions, minimal logic)
  - ai: 47.9%
  - polymarket: 31.0%

### Coverage Targets
- Critical business logic: 80%+
- Service layer: 70%+
- Models and generated code: 30%+

## Testing Patterns

### Table-Driven Tests
```go
func TestFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"valid case", "input", "output", false},
        {"error case", "bad", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Function(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Mock Usage
```go
// Define mock
type MockService struct {
    mock.Mock
}

func (m *MockService) Method() error {
    args := m.Called()
    return args.Error(0)
}

// Use in test
mockSvc := new(MockService)
mockSvc.On("Method").Return(nil)
```

### Test Isolation
```go
func TestSomething(t *testing.T) {
    // Use temp directory to avoid interference with user config
    tempHome := t.TempDir()
    t.Setenv("HOME", tempHome)
    
    // Test code...
}
```

## Best Practices

1. **Test names should describe behavior**: `TestUserExists/returns_false_when_not_found`
2. **One assertion per test case**: Makes failures clearer
3. **Use subtests**: Group related test cases with `t.Run()`
4. **Mock external dependencies**: Database, Redis, external APIs
5. **Clean up resources**: Use `t.Cleanup()` or `defer`
6. **Parallel tests**: Use `t.Parallel()` for independent tests
7. **Coverage reports**: Run with `-coverprofile` to identify gaps

## CI Integration

Tests run automatically on:
- Push to main, develop, development branches
- Pull requests to main, develop, development

Coverage artifacts are uploaded after each run.

## Writing New Tests

1. Create `*_test.go` file in same package
2. Import testing framework:
   ```go
   import (
       "testing"
       "github.com/stretchr/testify/assert"
       "github.com/stretchr/testify/require"
   )
   ```
3. Follow existing patterns in the codebase
4. Run tests locally before committing
5. Ensure tests pass with race detector: `go test -race`

## Troubleshooting

### Test fails with "no such file or directory"
- Ensure you're running from correct directory
- Use `cd services/backend-api` before running go test

### Test fails due to config.json
- Tests should use `t.TempDir()` and set `HOME` env var
- This prevents interference with user config at `~/.neuratrade/config.json`

### Race conditions
- Run with `-race` flag to detect
- Use mutexes for shared state
- Avoid parallel access to shared resources

### Database connection errors
- Ensure test database is running: `make dev-setup`
- Check DATABASE_URL environment variable
- Use mocks for unit tests
