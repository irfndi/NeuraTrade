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
API layer roadmap tracked via `bd` (~25 items):

### Ready to Work
- `neura-wiz`: Gamma API wrapper endpoints (market discovery)
- `neura-2iq`: Analyst agent role API endpoints

### Order Management Endpoints
- `neura-l2z`: place_order tool endpoint
- `neura-wz7`: cancel_order tool endpoint
- `neura-2xe`: Position snapshot tool endpoint
- `neura-txu`: Controlled liquidation tool endpoint
- `neura-1wi`: FOK order execution endpoint

### Wallet & Account Endpoints
- `neura-09y`: Wallet management commands
- `neura-myb`: Wallet minimum checks endpoint
- `neura-94c`: /status budget display endpoint

### Risk & Monitoring Endpoints
- `neura-nh5`: Risk event notification endpoints
- `neura-1s5`: Expose risk primitives via API
- `neura-4eo`: Expose arbitrage primitives via API
- `neura-lue`: Expose readiness endpoints
- `neura-duw`: Expose cleanup endpoints
- `neura-1p0`: /doctor diagnostic handler

### Quest & Agent Endpoints
- `neura-4gk`: Quest and monitoring commands
- `neura-im9`: Quest progress update endpoints
- `neura-6tk`: Event-driven quest trigger endpoints
- `neura-bxg`: One-time auth code generation endpoint

### Reporting Endpoints
- `neura-ik7`: /summary and /performance handlers
- `neura-ilw`: /liquidate and /liquidate_all handlers
- `neura-hgk`: /begin and /pause handlers
- `neura-fvk`: Fund milestone alert endpoints
- `neura-axx`: Action streaming format endpoint

## ANTI-PATTERNS
- Writing DB queries directly in handlers instead of repositories/services.
- Registering new endpoints without matching telemetry/auth middleware placement.
- Mixing admin and public routes in the same subgroup without explicit middleware boundaries.
