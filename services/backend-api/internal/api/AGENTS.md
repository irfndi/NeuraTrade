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

## ANTI-PATTERNS
- Writing DB queries directly in handlers instead of repositories/services.
- Registering new endpoints without matching telemetry/auth middleware placement.
- Mixing admin and public routes in the same subgroup without explicit middleware boundaries.
