# Autofix.ci Setup Guide

This repository now includes autofix.ci integration for automatic code formatting and linting fixes.

## What is Autofix.ci?

Autofix.ci is a GitHub App that automatically fixes code style issues, linting errors, and formatting problems in your pull requests. It runs on every PR and commits fixes directly to the branch.

## How to Enable

### 1. Install the GitHub App

Visit **https://autofix.ci/setup** and install the Autofix.ci GitHub App for this repository.

### 2. Configuration

The repository already includes the necessary configuration files:

- **`.autofix.ci.yml`** - Main configuration for autofix.ci
- **`.github/workflows/autofix.yml`** - GitHub Actions workflow for auto-formatting
- **`.golangci.yml`** - Linting configuration for Go code

### 3. What Gets Fixed Automatically

Autofix.ci will automatically fix:

#### Go Code
- Import formatting (via goimports)
- Code formatting (via gofmt)
- Some linting issues (via golangci-lint --fix)

#### TypeScript/JavaScript (CCXT service)
- Code formatting (via prettier)
- Linting issues (via oxlint --fix)

#### Shell Scripts
- Formatting (via shfmt)

#### YAML Files
- Formatting (via prettier)

### 4. How It Works

1. You create or update a pull request
2. The autofix workflow runs automatically
3. If any formatting or fixable linting issues are found, autofix commits the fixes
4. The fixes are pushed to your PR branch
5. CI validation runs on the fixed code

### 5. Manual Fixes

You can also run the fixers manually:

```bash
# Fix Go code
make fmt
goimports -w .
golangci-lint run --fix

# Fix TypeScript/JavaScript
cd ccxt-service
bunx prettier --write .
bunx oxlint --fix .

# Fix shell scripts
shfmt -w -i 2 -ci -bn $(find . -name "*.sh" -not -path "./node_modules/*" -not -path "./.git/*")
```

## CI/CD Pipeline

### Validation Workflow

The `.github/workflows/validation.yml` workflow runs on every push and PR:

1. **Setup** - Installs Go 1.25.5, Bun 1.3.3, and golangci-lint v2.7.2
2. **Dependencies** - Downloads Go modules and installs Bun packages
3. **Formatting Check** - Verifies code is properly formatted
4. **CI Checks** - Runs linting, tests, and builds

### Autofix Workflow

The `.github/workflows/autofix.yml` workflow runs on every PR:

1. **Formats** all code automatically
2. **Commits** fixes to the PR branch
3. **Triggers** validation workflow on the fixed code

## Linting Configuration

The `.golangci.yml` configuration focuses on critical issues:

- **Enabled Linters:**
  - `errcheck` - Checks for unchecked errors
  - `govet` - Examines suspicious constructs
  - `ineffassign` - Detects unused variable assignments
  - `staticcheck` - Advanced static analysis
  - `unused` - Checks for unused code
  - `misspell` - Finds spelling errors
  - `copyloopvar` - Detects loop variable issues

- **Formatters:**
  - `gofmt` - Go code formatting
  - `goimports` - Import statement formatting

### Test Files

Test files are excluded from strict linting to allow for test-specific patterns.

## Troubleshooting

### Autofix not running?

1. Check that the GitHub App is installed at https://autofix.ci/setup
2. Ensure your PR is from a branch in the same repository (not a fork)
3. Check the Actions tab for workflow run status

### Linting fails locally?

```bash
# Update golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ./bin v2.7.2

# Run linting
./bin/golangci-lint run --timeout=5m
```

### Format check fails?

```bash
# Fix formatting
make fmt
goimports -w .

# Verify
make fmt-check
```

## Benefits

✅ Consistent code style across the repository  
✅ Automatic fixes save developer time  
✅ Reduced back-and-forth in code reviews  
✅ Catches common errors early  
✅ Maintains high code quality standards  

## Support

For issues with:
- **Autofix.ci** - Visit https://autofix.ci
- **Golangci-lint** - Visit https://golangci-lint.run
- **CI/CD Pipeline** - Check `.github/workflows/` files
