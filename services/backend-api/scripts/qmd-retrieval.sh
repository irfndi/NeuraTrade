#!/bin/bash
set -euo pipefail

DOCS_PATH="${DOCS_PATH:-$(cd "$(dirname "$0")/../../../docs" && pwd)}"
QMD_INDEX_PATH="${QMD_INDEX_PATH:-$HOME/.qmd/index}"
QMD_ENABLED="${QMD_ENABLED:-0}"
MAX_RESULTS=5

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

check_dependencies() {
    local missing=0
    if ! command -v grep &> /dev/null; then
        log_error "grep is required"
        missing=1
    fi
    if ! command -v rg &> /dev/null; then
        log_warn "ripgrep not found"
    fi
    if [[ "$QMD_ENABLED" == "1" ]] && ! command -v qmd &> /dev/null; then
        log_warn "qmd not found"
        QMD_ENABLED=0
    fi
    return $missing
}

find_markdown_docs() { find "$DOCS_PATH" -type f -name "*.md" 2>/dev/null || echo ""; }
count_docs() { find_markdown_docs | wc -l; }

search_with_grep() {
    local query="$1" start_time end_time
    start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
    if command -v rg &> /dev/null; then
        rg -n --context=3 --max-count=5 --glob='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true
    else
        grep -rn --context=3 --max-count=5 --include='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true
    fi
    end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
    echo "Grep/Rg search time: $((end_time - start_time))ms"
}

search_with_qmd() {
    local query="$1" start_time end_time
    [[ "$QMD_ENABLED" != "1" ]] && return 1
    start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
    [[ ! -d "$QMD_INDEX_PATH" ]] && return 1
    qmd search "$query" --max-results="$MAX_RESULTS" 2>/dev/null || true
    end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
    echo "QMD search time: $((end_time - start_time))ms"
}

search_unified() {
    local query="$1"
    if [[ "$QMD_ENABLED" == "1" ]] && command -v qmd &> /dev/null && search_with_qmd "$query"; then
        return 0
    fi
    log_info "Using grep/rg baseline search..."
    search_with_grep "$query"
}

index_docs() {
    if ! command -v qmd &> /dev/null; then
        log_error "qmd not installed"
        return 1
    fi
    log_info "Indexing from $DOCS_PATH..."
    qmd create neuratrade --path "$DOCS_PATH" 2>/dev/null || qmd add neuratrade "$DOCS_PATH" 2>/dev/null || true
    log_success "Indexing complete"
}

run_benchmark() {
    local test_queries=("database connection" "redis error" "telegram bot" "exchange API" "health check")
    echo "QMD Retrieval Benchmark Report"
    echo "Docs path: $DOCS_PATH, Total docs: $(count_docs), QMD enabled: $QMD_ENABLED"
    
    [[ $(count_docs) -eq 0 ]] && { log_error "No docs found"; return 1; }
    
    echo "Baseline: Grep/Rg Search"
    local total_grep_time=0
    for query in "${test_queries[@]}"; do
        echo "Query: \"$query\""
        local result start_time end_time search_time
        start_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || start_time=$(date +%s000)
        if command -v rg &> /dev/null; then
            result=$(rg -n --context=3 --max-count=5 --glob='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true)
        else
            result=$(grep -rn --context=3 --max-count=5 --include='*.md' "$query" "$DOCS_PATH" 2>/dev/null || true)
        fi
        end_time=$(python3 -c 'import time; print(int(time.time() * 1000))' 2>/dev/null) || end_time=$(date +%s000)
        search_time=$((end_time - start_time))
        total_grep_time=$((total_grep_time + search_time))
        echo "$result"
        echo "Found: $(echo "$result" | grep -c ':' 2>/dev/null || echo "0") matches, Time: ${search_time}ms"
    done
    echo "Average: $((total_grep_time / ${#test_queries[@]}))ms"
    
    if [[ "$QMD_ENABLED" == "1" ]] && command -v qmd &> /dev/null; then
        echo "QMD: $(search_with_qmd "${test_queries[0]}" 2>&1 | tail -1)"
    else
        echo "QMD not available"
    fi
}

check_resource_overhead() {
    echo "Resource Overhead Report"
    local doc_count total_size
    doc_count=$(count_docs)
    total_size=$(find_markdown_docs | xargs du -ch 2>/dev/null | tail -1 | cut -f1 || echo "unknown")
    echo "Files: $doc_count, Size: $total_size"
    [[ -d "$QMD_INDEX_PATH" ]] && echo "QMD Index: $(du -sh "$QMD_INDEX_PATH" 2>/dev/null | cut -f1)" || echo "QMD Index: not created"
}

show_status() {
    echo "QMD Retrieval Status"
    echo "QMD_ENABLED: $QMD_ENABLED, DOCS_PATH: $DOCS_PATH"
    echo -n "grep: "; command -v grep &> /dev/null && echo "ok" || echo "missing"
    echo -n "rg: "; command -v rg &> /dev/null && echo "ok" || echo "missing"
    echo -n "qmd: "; command -v qmd &> /dev/null && echo "ok" || echo "missing"
    echo "Docs: $(count_docs)"
    [[ -d "$QMD_INDEX_PATH" ]] && echo "Index: $(du -sh "$QMD_INDEX_PATH" | cut -f1)" || echo "Index: not created"
}

main() {
    local command="${1:-help}"
    case "$command" in
        search) check_dependencies || exit 1; [[ -z "${2:-}" ]] && { log_error "Usage: $0 search <query>"; exit 1; }; search_unified "$2" ;;
        index) check_dependencies; index_docs ;;
        benchmark) check_dependencies || true; run_benchmark ;;
        status) show_status ;;
        resources) check_resource_overhead ;;
        help|--help|-h) echo "Usage: $0 <command>"; echo "Commands: search, index, benchmark, status, resources, help" ;;
        *) log_error "Unknown: $command"; exit 1 ;;
    esac
}

main "$@"
