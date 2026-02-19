.PHONY: bootstrap clean test lint bench commit release test-with-coverage view-coverage fmt check

# Find all Go files
files := $(shell find . -name "*.go" | grep -v vendor)

# Default target
all: fmt test lint

# Install development tools
bootstrap:
	go install -v golang.org/x/tools/cmd/goimports@latest
	go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Format code
fmt:
	goimports -local github.com/irfndi/goflux -w $(files)
	go fmt ./...

# Clean and format
clean: fmt

# Run tests
test:
	go test -v ./...

# Run linting with golangci-lint
lint:
	golangci-lint run ./...

# Run benchmarks
bench: clean
	go test -bench=. -benchmem ./...

# Run tests with coverage
test-with-coverage:
	go test -race -cover -covermode=atomic -coverprofile=coverage.txt ./...

# View coverage in browser
view-coverage: test-with-coverage
	go tool cover -html=coverage.txt

# Check if code is ready for commit
check: fmt test lint
	@echo "All checks passed!"

# Build examples
build-examples:
	go build -v ./example/...

# Tidy dependencies
tidy:
	go mod tidy

# Update dependencies
update-deps:
	go get -u ./...
	go mod tidy

# Verify dependencies
verify:
	go mod verify

# Generate (if needed in future)
generate:
	go generate ./...

# Help target
help:
	@echo "Available targets:"
	@echo "  all              - Format, test, and lint (default)"
	@echo "  bootstrap        - Install development tools"
	@echo "  fmt              - Format code with goimports"
	@echo "  test             - Run all tests"
	@echo "  lint             - Run golangci-lint"
	@echo "  bench            - Run benchmarks"
	@echo "  test-with-coverage - Run tests with coverage report"
	@echo "  view-coverage    - View coverage report in browser"
	@echo "  check            - Run all checks (fmt, test, lint)"
	@echo "  tidy             - Tidy go.mod"
	@echo "  update-deps      - Update all dependencies"
	@echo "  verify           - Verify dependencies"
	@echo "  help             - Show this help"
