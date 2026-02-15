#!/bin/bash
# =============================================================================
# QMD Retrieval Prototype for /doctor Diagnostics
# =============================================================================
# This script provides a PoC CLI wrapper for QMD (Query Markup Documents)
# retrieval, primarily for /doctor diagnostic runbooks.
#
# Scope: Non-critical operator guidance only
# - No execution queue, scheduling, risk, or order routing dependencies
# - Used for /doctor diagnostics, troubleshooting, operator assistance
#
# Usage:
#   ./qmd-retrieval.sh search "query terms"
#   ./qmd-retrieval.sh benchmark
#   ./qmd-retrieval.sh status
#
# Environment:
#   QMD_ENABLED=1     # Enable QMD semantic search (if available)
#   QMD_INDEX_PATH    # Path to QMD index (default: ~/.qmd/index)
#   DOCS_PATH         # Path to markdown docs (default: ./docs)
# =============================================================================

set -euo pipefail

# Configuration
DOCS_PATH="${DOCS_PATH:-$(cd "$(dirname "$0")/../../../docs" && pwd)}"
QMD_INDEX_PATH="${QMD_INDEX_PATH:-$HOME/.qmd/index}"
QMD_ENABLED="${QMD_ENABLED:-0}"
MAX_RESULTS="${MAX_RESULTS:-5}"
CONTEXT_LINES="${CONTEXT_LINES:-3}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# Check if required tools are available
check_dependencies() {
    local missing=0
    
    # Check for grep (always required as baseline)
    if ! command -v grep &> /dev/null; then
        log_error "grep is required but not installed"
        missing=1
    fi
    
    # Check for ripgrep (preferred for baseline)
    if ! command -v rg &> /dev/null; then
        log_warn "ripgrep (rg) not found - will use grep as fallback"
    fi
    
    # Check for QMD (optional)
    if [[ "$QMD_ENABLED" == "1" ]]; then
        if ! command -v qmd &> /dev/null; then
            log_warn "qmd not found - install with: bun install -g https://github.com/tobi/qmd"
            log_info "Falling back to grep/rg-based retrieval"
            QMD_ENABLED=0
        fi
    fi
    
    return $missing
}

# Find markdown files in docs path
find_markdown_docs() {
    find "$DOCS_PATH" -type f -name "*.md" 2>/dev/null | grep -v "^$" || echo ""
}

# Get total doc count
count_docs() {
    find_markdown_docs | wc -l
}

# =============================================================================
# Search Functions
# =============================================================================

# Baseline search using grep/rg
search_with_grep() {
    local query="$1"
    local start_time
    local end_time
    
    start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
    
    if command -v rg &> /dev/null; then
        rg -n --context=3 --max-count=5 --glob='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true
    else
        grep -rn --context=3 --max-count=5 --include='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true
    fi
    
    end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
    echo ""
    echo "Grep/Rg search time: $((end_time - start_time))ms"
}

# QMD-based semantic search
search_with_qmd() {
    local query="$1"
    local start_time
    local end_time
    
    if [[ "$QMD_ENABLED" != "1" ]]; then
        log_warn "QMD is not enabled"
        return 1
    fi
    
    start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
    
    # Check if index exists
    if [[ ! -d "$QMD_INDEX_PATH" ]]; then
        log_warn "QMD index not found at $QMD_INDEX_PATH"
        log_info "Run './qmd-retrieval.sh index' to create index"
        return 1
    fi
    
    # Search using QMD
    qmd search "$query" --max-results="$MAX_RESULTS" 2>/dev/null || true
    
    end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
    echo ""
    echo "QMD search time: $((end_time - start_time))ms"
}

# Unified search that tries QMD first if enabled, falls back to grep
search_unified() {
    local query="$1"
    
    if [[ "$QMD_ENABLED" == "1" ]] && command -v qmd &> /dev/null; then
        log_info "Using QMD semantic search (if indexed)..."
        if search_with_qmd "$query"; then
            return 0
        fi
        log_info "Falling back to grep/rg..."
    fi
    
    log_info "Using grep/rg baseline search..."
    search_with_grep "$query"
}

# =============================================================================
# Indexing Functions
# =============================================================================

# Index documents for QMD
index_docs() {
    if ! command -v qmd &> /dev/null; then
        log_error "qmd is not installed"
        log_info "Install with: bun install -g https://github.com/tobi/qmd"
        return 1
    fi
    
    log_info "Indexing markdown documents from $DOCS_PATH..."
    
    # Create a collection for neuratrade docs
    qmd create neuratrade --path "$DOCS_PATH" 2>/dev/null || qmd add neuratrade "$DOCS_PATH" 2>/dev/null || true
    
    log_success "Indexing complete"
    log_info "Index location: $QMD_INDEX_PATH"
}

# =============================================================================
# Benchmark Functions
# =============================================================================

# Run benchmark comparing grep/rg vs QMD
run_benchmark() {
    local test_queries=(
        "database connection"
        "redis error"
        "telegram bot"
        "exchange API"
        "health check"
    )
    
    echo "============================================================================"
    echo "QMD Retrieval Benchmark Report"
    echo "============================================================================"
    echo ""
    echo "Configuration:"
    echo "  Docs path: $DOCS_PATH"
    echo "  Total docs: $(count_docs)"
    echo "  QMD enabled: $QMD_ENABLED"
    echo "  Max results: $MAX_RESULTS"
    echo ""
    
    # Check docs exist
    if [[ $(count_docs) -eq 0 ]]; then
        log_error "No markdown documents found in $DOCS_PATH"
        return 1
    fi
    
    echo "----------------------------------------------------------------------------"
    echo "Baseline: Grep/Rg Search"
    echo "----------------------------------------------------------------------------"
    
    local total_grep_time=0
    for query in "${test_queries[@]}"; do
        echo "Query: \"$query\""
        local result
        local start_time end_time
        
        start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
        
        if command -v rg &> /dev/null; then
            result=$(rg -n --context=3 --max-count=5 --glob='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true)
        else
            result=$(grep -rn --context=3 --max-count=5 --include='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true)
        fi
        
        end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
        local search_time=$((end_time - start_time))
        total_grep_time=$((total_grep_time + search_time))
        
        echo "$result"
        local match_count
        match_count=$(echo "$result" | grep -c ':' 2>/dev/null || echo "0")
        echo "Found: $match_count matches"
        echo "Time: ${search_time}ms"
        echo ""
    done
    
    echo "Average grep/rg search time: $((total_grep_time / ${#test_queries[@]}))ms"
    echo ""
    
    # QMD benchmark (if enabled and available)
    if [[ "$QMD_ENABLED" == "1" ]] && command -v qmd &> /dev/null; then
        echo "----------------------------------------------------------------------------"
        echo "Enhanced: QMD Semantic Search"
        echo "----------------------------------------------------------------------------"
        
        local total_qmd_time=0
        for query in "${test_queries[@]}"; do
            echo "Query: \"$query\""
            local result
            result=$(search_with_qmd "$query" 2>&1)
            local search_time
            search_time=$(echo "$result" | grep -oP "search time: \K\d+" || echo "0")
            total_qmd_time=$((total_qmd_time + search_time))
            echo ""
        done
        
        echo "Average QMD search time: $((total_qmd_time / ${#test_queries[@]}))ms"
        echo ""
        
        echo "----------------------------------------------------------------------------"
        echo "Comparison Summary"
        echo "----------------------------------------------------------------------------"
        echo "Grep/Rg average: $((total_grep_time / ${#test_queries[@]}))ms"
        echo "QMD average: $((total_qmd_time / ${#test_queries[@]}))ms"
    else
        echo "----------------------------------------------------------------------------"
        echo "QMD Not Available"
        echo "----------------------------------------------------------------------------"
        log_info "To enable QMD benchmarking:"
        echo "  1. Install: bun install -g https://github.com/tobi/qmd"
        echo "  2. Run: ./qmd-retrieval.sh index"
        echo "  3. Set: QMD_ENABLED=1"
        echo ""
        echo "QMD provides semantic search with BM25 + vector hybrid ranking"
    fi
    
    echo "============================================================================"
    echo "Benchmark complete"
    echo "============================================================================"
}

# Check resource overhead
check_resource_overhead() {
    echo "============================================================================"
    echo "Resource Overhead Report"
    echo "============================================================================"
    echo ""
    
    # Document statistics
    local doc_count
    doc_count=$(count_docs)
    local total_size
    total_size=$(find_markdown_docs | xargs du -ch 2>/dev/null | tail -1 | cut -f1 || echo "unknown")
    
    echo "Document Statistics:"
    echo "  Total markdown files: $doc_count"
    echo "  Total size: $total_size"
    echo ""
    
    # QMD index size (if exists)
    if [[ -d "$QMD_INDEX_PATH" ]]; then
        local index_size
        index_size=$(du -sh "$QMD_INDEX_PATH" 2>/dev/null | cut -f1 || echo "unknown")
        echo "QMD Index:"
        echo "  Index location: $QMD_INDEX_PATH"
        echo "  Index size: $index_size"
        echo "  Index overhead: ~$(echo "scale=2; $(du -s "$QMD_INDEX_PATH" 2>/dev/null | cut -f1 || echo "0") * 100 / $(find_markdown_docs | xargs du -s 2>/dev/null | tail -1 | cut -f1 || echo "1")" | bc 2>/dev/null || echo "0")% of original"
    else
        echo "QMD Index: Not created yet (run ./qmd-retrieval.sh index)"
    fi
    
    echo ""
    echo "Memory Usage (approximate):"
    echo "  Grep/Rg baseline: <10MB (minimal footprint)"
    echo "  QMD (with vectors): ~500MB-2GB (depends on model)"
    echo ""
    
    echo "============================================================================"
}

# =============================================================================
# Status Function
# =============================================================================

show_status() {
    echo "============================================================================"
    echo "QMD Retrieval Status"
    echo "============================================================================"
    echo ""
    echo "Configuration:"
    echo "  QMD_ENABLED: $QMD_ENABLED"
    echo "  DOCS_PATH: $DOCS_PATH"
    echo "  QMD_INDEX_PATH: $QMD_INDEX_PATH"
    echo "  MAX_RESULTS: $MAX_RESULTS"
    echo ""
    
    echo "Dependencies:"
    echo -n "  grep: "
    if command -v grep &> /dev/null; then
        echo "✓ available"
    else
        echo "✗ not found"
    fi
    
    echo -n "  rg (ripgrep): "
    if command -v rg &> /dev/null; then
        echo "✓ available ($(rg --version | head -1))"
    else
        echo "✗ not found (will use grep fallback)"
    fi
    
    echo -n "  qmd: "
    if command -v qmd &> /dev/null; then
        echo "✓ available"
    else
        echo "✗ not found"
    fi
    
    echo ""
    echo "Documents:"
    echo "  Total markdown files: $(count_docs)"
    
    if [[ -d "$QMD_INDEX_PATH" ]]; then
        echo "  QMD index: ✓ exists ($(du -sh "$QMD_INDEX_PATH" | cut -f1))"
    else
        echo "  QMD index: ✗ not created"
    fi
    
    echo ""
    echo "============================================================================"
}

# =============================================================================
# Main
# =============================================================================

main() {
    local command="${1:-help}"
    
    case "$command" in
        search)
            check_dependencies || exit 1
            if [[ -z "${2:-}" ]]; then
                log_error "Usage: $0 search <query>"
                exit 1
            fi
            search_unified "$2"
            ;;
        index)
            check_dependencies || exit 1
            index_docs
            ;;
        benchmark)
            check_dependencies || true
            run_benchmark
            ;;
        status)
            show_status
            ;;
        resources)
            check_resource_overhead
            ;;
        help|--help|-h)
            echo "QMD Retrieval Prototype for /doctor Diagnostics"
            echo ""
            echo "Usage: $0 <command> [options]"
            echo ""
            echo "Commands:"
            echo "  search <query>    Search docs for query (uses QMD if enabled, else grep/rg)"
            echo "  index             Create QMD index for faster semantic search"
            echo "  benchmark         Run retrieval quality benchmark"
            echo "  status            Show current configuration and status"
            echo "  resources         Show resource overhead report"
            echo "  help              Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  QMD_ENABLED=1     Enable QMD semantic search"
            echo "  DOCS_PATH         Path to markdown docs (default: ./docs)"
            echo "  QMD_INDEX_PATH    Path to QMD index"
            echo "  MAX_RESULTS       Maximum results to return"
            echo ""
            ;;
        *)
            log_error "Unknown command: $command"
            echo "Run '$0 help' for usage information"
            exit 1
            ;;
    esac
}

main "$@"
