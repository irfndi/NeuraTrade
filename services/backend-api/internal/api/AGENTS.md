# API LAYER KNOWLEDGE BASE

## OVERVIEW
`internal/api` owns HTTP route registration and handler composition for backend endpoints.
Keep transport concerns here; push business decisions into `internal/services`.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Register/modify endpoint paths | `routes.go` | Route groups and middleware application live here |
| Market endpoints | `handlers/market.go` | Tickers, orderbook, worker status |
| Arbitrage endpoints | `handlers/arbitrage.go` | Spot + funding routes |
| Analysis endpoints | `handlers/analysis.go` | Indicators and signal endpoints |
| User/auth endpoints | `handlers/user.go` | Register/login/profile |
| Exchange/cache/admin endpoints | `handlers/exchange.go`, `handlers/cache.go` | Admin auth and operational controls |

## ROUTING PATTERN
- Health routes are mounted at root (`/health`, `/ready`, `/live`) with health telemetry middleware.
- Versioned API routes are mounted under `/api/v1` with telemetry middleware.
- Auth-protected routes use `authMiddleware.RequireAuth()`.
- Admin-only operations use `adminMiddleware.RequireAdminAuth()`.

## CONVENTIONS
- Handler constructors follow dependency injection (`NewXHandler(...)`).
- Handlers parse params/inputs and return HTTP responses; service logic stays in domain services.
- Response shape should remain stable across endpoints (status/data/message conventions where used).
- Keep legacy `internal/handlers/*` usage minimal; active path is `internal/api/handlers/*`.

## TESTING
```bash
go test ./internal/api/...
go test ./internal/api/handlers/... -run TestArbitrage
```
- Tests are co-located with handlers (`*_test.go`).
- Use `test_helpers.go` and `testmocks/` for shared setup/mocks.

## BACKLOG (bd CLI)

**Stats:** 173 total | 117 open | 67 blocked | 56 closed | 50 ready

### Ready to Work (No Blockers)
- `neura-7et`: Build prompt builder from skill.md + context
- `neura-r1d`: Progressive disclosure system

### Recently Completed (✓)
- ✓ `neura-myb`: Wallet minimum checks endpoint
- ✓ `neura-3b9`: Priority levels (CRITICAL > HIGH > NORMAL > LOW)
- ✓ `neura-6tk`: Event-driven quest trigger endpoints
- ✓ `neura-2iq`: Analyst agent role API endpoints
- ✓ `neura-fs8`: API key validation endpoint
- ✓ `neura-2n4`: Quest state persistence endpoints
- ✓ `neura-e8u`: Daily loss cap endpoint
- ✓ `neura-cha`: Sum-to-one arbitrage skill.md endpoint
- ✓ `neura-2xe`: Position snapshot tool endpoint
- ✓ `neura-lue`: Readiness endpoints
- ✓ `neura-4eo`: Arbitrage primitives endpoint
- ✓ `neura-1s5`: Risk primitives endpoint
- ✓ `neura-wiz`: Gamma API wrapper endpoints (Polymarket)
- ✓ `neura-94c`: /status budget display endpoint
- ✓ `neura-l2z`: place_order tool endpoint
- ✓ `neura-wz7`: cancel_order tool endpoint
- ✓ `neura-1p0`: /doctor diagnostic handler
- ✓ `neura-4gk`: Quest and monitoring commands
- ✓ `neura-09y`: Wallet management commands
- ✓ `neura-ilw`: /liquidate and /liquidate_all handlers
- ✓ `neura-ik7`: /summary and /performance handlers
- ✓ `neura-hgk`: /begin and /pause handlers
- ✓ `neura-axx`: Action streaming format endpoint

### Order Management Endpoints (Blocked)
- `neura-txu`: Controlled liquidation tool endpoint
- `neura-1wi`: FOK order execution endpoint

### Risk & Monitoring Endpoints (Blocked)
- `neura-duw`: Expose cleanup endpoints

### Quest & Agent Endpoints (Blocked)
- `neura-im9`: Quest progress update endpoints

### Reporting Endpoints (Blocked)
- `neura-fvk`: Fund milestone alert endpoints

## ANTI-PATTERNS
- Writing DB queries directly in handlers instead of repositories/services.
- Registering new endpoints without matching telemetry/auth middleware placement.
- Mixing admin and public routes in the same subgroup without explicit middleware boundaries.
