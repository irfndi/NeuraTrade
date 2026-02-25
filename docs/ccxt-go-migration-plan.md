# CCXT Service Migration to Go

## Overview
Migrate CCXT service from Bun (TypeScript) to Go to simplify architecture, fix current ticker timeout issues, and enable single-process deployment.

## Related Beads
- **neura-id1r**: Evaluate CCXT Go vs GoCryptoTrader
- **neura-9qf4**: Migrate CCXT Service to Go
- **neura-10zs**: Migrate gRPC Server to Go (optional - we'll skip gRPC)
- **neura-kyva**: Migrate HTTP API Endpoints to Go
- **neura-v7e6**: Fix server startup: HTTP server not starting - hanging during market data collection

## Architecture Change

### Before (Current - Broken)
```
┌─────────────────┐      HTTP/gRPC      ┌──────────────────┐
│  Backend (Go)   │ ──────────────────> │ CCXT (Bun/TS)    │
│   Port: 8080    │                     │   Port: 3001     │
│                 │ <────────────────── │   Port: 50051    │
└─────────────────┘                     └──────────────────┘
                                                │
                                                ▼
                                         ┌──────────────┐
                                         │ Binance API  │
                                         └──────────────┘
```

**Problems:**
- ❌ CCXT service ticker requests hang indefinitely
- ❌ Two services to manage, deploy, monitor
- ❌ Network overhead (HTTP + gRPC)
- ❌ Context switching between Go/Bun
- ❌ Debugging complexity

### After (Target)
```
┌────────────────────────────────────────┐
│     Backend (Go) - Single Process      │
│   Port: 8080                           │
│  ┌──────────────────────────────────┐  │
│  │  CCXT Go Client (internal/ccxt)  │  │
│  └──────────────────────────────────┘  │
└────────────────────────────────────────┘
                  │
                  ▼
         ┌──────────────┐
         │ Binance API  │
         └──────────────┘
```

**Benefits:**
- ✅ Single binary deployment
- ✅ No network calls (direct function calls)
- ✅ Better performance (no serialization overhead)
- ✅ Easier debugging (single process)
- ✅ Fixes current ticker timeout bug
- ✅ Consistent error handling

## Implementation Plan

### Phase 1: Library Evaluation (neura-id1r)
**Decision**: Use **CCXT Go** (not GoCryptoTrader)

**Rationale:**
1. **CCXT Go** (github.com/ccxt/go)
   - Official CCXT Go port
   - Same API as TypeScript version
   - 100+ exchanges supported
   - Active maintenance

2. **GoCryptoTrader** (github.com/thrasher-corp/gocryptotrader)
   - Go-native implementation
   - Fewer exchanges (~30)
   - Different API structure
   - Would require rewriting all CCXT calls

**Winner**: CCXT Go - minimal code changes, same API

### Phase 2: Core Implementation (neura-9qf4)

#### 2.1 Add CCXT Go Dependency
```bash
go get github.com/ccxt/go@latest
```

#### 2.2 Update internal/ccxt Package
Current: HTTP client that calls Bun service
Target: Direct CCXT Go wrapper

**Key Interfaces to Maintain:**
```go
type CCXTService interface {
    FetchTicker(ctx context.Context, exchange, symbol string) (*Ticker, error)
    FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBook, error)
    FetchBalance(ctx context.Context, exchange string) (*Balance, error)
    FetchExchanges() ([]Exchange, error)
    // ... existing methods
}
```

#### 2.3 Remove Bun Service Dependencies
- Remove HTTP client calls to localhost:3001
- Remove gRPC client calls to localhost:50051
- Remove CCXT service URL from config

### Phase 3: API Endpoint Migration (neura-kyva)

#### 3.1 Migrate HTTP Endpoints
Current: `/api/ticker/:exchange/*` in Bun
Target: Direct handler in Go backend

**Endpoints to Migrate:**
- `GET /api/exchanges` - List supported exchanges
- `GET /api/exchanges/supported` - Get enabled exchanges
- `GET /api/ticker/:exchange/:symbol` - Fetch ticker data
- `GET /api/orderbook/:exchange/:symbol` - Fetch order book
- `GET /api/balance/:exchange` - Fetch balance (requires API keys)
- `POST /api/order` - Place order (requires API keys)

#### 3.2 Remove gRPC Server (Skip neura-10zs)
**Decision**: Skip gRPC migration entirely
- Direct function calls are simpler
- No serialization overhead
- No port management needed

### Phase 4: Configuration Cleanup

#### 4.1 Remove CCXT Service Config
```json
// Remove from config.json:
{
  "ccxt": {
    "service_url": "http://localhost:3001",  // ❌ Remove
    "grpc_address": "localhost:50051",       // ❌ Remove
    "timeout": 30
  }
}
```

#### 4.2 Add Exchange API Keys (Optional)
```json
{
  "exchanges": {
    "binance": {
      "api_key": "${BINANCE_API_KEY}",
      "secret": "${BINANCE_SECRET}",
      "testnet": true
    }
  }
}
```

### Phase 5: Testing & Validation

#### 5.1 Unit Tests
- Test CCXT Go wrapper methods
- Test exchange initialization
- Test error handling (timeouts, rate limits)

#### 5.2 Integration Tests
- Test ticker fetching from real exchanges
- Test order book fetching
- Test with testnet credentials

#### 5.3 Performance Tests
- Compare latency: HTTP vs direct calls
- Measure throughput improvement
- Verify no goroutine leaks

### Phase 6: Deployment

#### 6.1 Remove Bun Services from Deployment
- Remove `ccxt-service` from docker-compose
- Remove `telegram-service` (optional, later)
- Single binary deployment

#### 6.2 Update Documentation
- Update README with new architecture
- Update deployment guides
- Update troubleshooting docs

## Code Changes Required

### Files to Modify
1. `services/backend-api/internal/ccxt/ccxt.go` - Core wrapper
2. `services/backend-api/internal/ccxt/client.go` - Client implementation
3. `services/backend-api/internal/api/handlers/market.go` - Market handlers
4. `services/backend-api/internal/config/config.go` - Remove CCXT URLs
5. `services/backend-api/cmd/server/main.go` - Remove CCXT service init

### Files to Delete
1. `services/ccxt-service/` - Entire Bun service
2. `protos/ccxt_service.proto` - gRPC definitions (if removing gRPC)
3. `services/backend-api/proto/` - Generated proto files

### Files to Add
1. `go.mod` - Add github.com/ccxt/go dependency
2. `services/backend-api/internal/ccxt/ccxt_go.go` - CCXT Go wrapper
3. `services/backend-api/internal/ccxt/ccxt_go_test.go` - Tests

## Risk Mitigation

### Risk 1: CCXT Go API Differences
**Mitigation**: Create adapter layer that maintains existing interface

### Risk 2: Exchange Compatibility
**Mitigation**: Test with top 3 exchanges first (Binance, Bybit, OKX)

### Risk 3: Performance Regression
**Mitigation**: Benchmark before/after, ensure latency improvement

### Risk 4: Breaking Changes
**Mitigation**: Maintain backward-compatible interfaces

## Success Criteria

- [ ] All ticker requests complete within 5 seconds
- [ ] No more hanging requests
- [ ] Single binary deployment works
- [ ] All existing tests pass
- [ ] Autonomous trading works end-to-end
- [ ] Latency reduced by >50% (no network calls)
- [ ] Code coverage >80% on new CCXT Go wrapper

## Timeline Estimate

- **Phase 1** (Evaluation): 1-2 hours ✅ DONE
- **Phase 2** (Core Implementation): 4-6 hours
- **Phase 3** (API Migration): 3-4 hours
- **Phase 4** (Config Cleanup): 1-2 hours
- **Phase 5** (Testing): 3-4 hours
- **Phase 6** (Deployment): 1-2 hours

**Total**: 13-20 hours (~2-3 days)

## Next Steps

1. ✅ Create branch: `feature/neura-ccxt-go-migration`
2. ⏳ Add CCXT Go dependency
3. ⏳ Implement CCXT Go wrapper
4. ⏳ Update existing CCXT client to use Go library
5. ⏳ Remove HTTP/gRPC calls to Bun service
6. ⏳ Test with real exchange data
7. ⏳ Remove Bun CCXT service
8. ⏳ Update documentation
9. ⏳ Create PR

---

**Status**: Branch created, ready to start implementation
**Priority**: P0 (blocking autonomous trading)
**Owner**: @irfandi
