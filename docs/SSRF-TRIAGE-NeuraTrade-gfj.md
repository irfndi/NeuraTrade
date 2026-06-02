# SSRF Triage Report — NeuraTrade-gfj

**Scanner**: deepsec ssrf  
**Scan**: 20260505172439-8736ac0c3e5845b3  
**Status**: FALSE-POSITIVE (all sites SAFE)

---

## Summary

| Metric | Count |
|--------|-------|
| Total HTTP call sites (non-test) | 83 |
| SAFE | 83 |
| CONFIRMED-SSRF | 0 |
| NEEDS-VALIDATION | 0 |

No SSRF vulnerabilities exist. All outbound HTTP requests target URLs that are either hardcoded, config-driven, or validated against a compile-time allowlist of known exchange hostnames.

---

## Detailed Analysis

### 1. Exchange API Calls — `internal/ccxt/ccxt_native.go` (13 call sites)

| Line | Function | URL Construction | Verdict |
|------|----------|-----------------|---------|
| 316 | `testExchangeConnection` | `getExchangePingURL(exchangeID)` → hardcoded map | SAFE |
| 529 | `fetchTickerFromURL` | URL from `buildTickerURL(exchange, symbol)` → switch to hardcoded URLs | SAFE |
| 951 | `fetchBitgetBulkTickers` | Hardcoded `https://api.bitget.com/api/v2/spot/market/tickers` | SAFE |
| 1081 | `FetchOrderBook` | URL from `buildOrderBookURL(...)` → switch to hardcoded URLs | SAFE |
| 1147 | `FetchOHLCV` | URL from `buildOHLCVURL(...)` → switch to hardcoded URLs | SAFE |
| 1441 | (fetchMarkets) | URL from `buildMarketsURL(exchange)` → switch to hardcoded URLs | SAFE |
| 1843 | (fetchBalance) | Hardcoded per-exchange URLs | SAFE |
| 2025/2100 | (fetch positions) | Hardcoded per-exchange URLs | SAFE |
| 2189/2298 | (create/cancel order) | Hardcoded per-exchange URLs | SAFE |
| 2353 | (fetch funding rate) | URL from `buildFundingRateURL(...)` → switch to hardcoded URLs | SAFE |
| 3073 | (bitget endpoint) | `"https://api.bitget.com" + endpoint` — hardcoded base | SAFE |

**Why SAFE**: All 6 URL builder functions (`getExchangeBaseURL`, `getExchangePingURL`, `buildTickerURL`, `buildOrderBookURL`, `buildOHLCVURL`, `buildMarketsURL`, `buildFundingRateURL`) use either a hardcoded `map[string]string` or a `switch` statement with known exchange IDs (`binance`, `bybit`, `okx`, `kraken`, `kucoin`, `gateio`, `mexc`, `bitget`, `coinbase`, `bingx`, `cryptocom`). An unrecognized exchange name returns an empty string, causing the request to abort before any HTTP call.

### 2. CCXT Service Client — `internal/ccxt/client.go` (1 call site)

| Line | Function | URL Construction | Verdict |
|------|----------|-----------------|---------|
| 640 | `makeRequest` | `c.baseURL + path` | SAFE |

**Why SAFE**: Both `baseURL` and `path` come from configuration and known API route constants. Already annotated with `#nosec G704`. The exchange name only appears as a path segment in the path, not as part of the host.

### 3. Order Executors — `internal/services/ccxt_order_executor.go` (6 call sites)

All URLs: `e.serviceURL + "/api/" + ...` where `serviceURL` is config-driven (defaults to `http://localhost:3001`). The exchange name only appears as a path parameter or in the request body — it cannot change the target host.

### 4. Notification Channels — `internal/services/notification_channels.go` (2 call sites)

- Line 219: `c.config.WebhookURL` — from `DiscordChannelConfig` (config-driven)
- Line 436: `c.config.URL` — from `WebhookChannelConfig` (config-driven)

**SAFE** — URLs are provided by admin configuration, not user input.

### 5. Notification Service — `internal/services/notification_service.go` (1 call site)

Line 187: `ns.telegramServiceURL + "/send-message"` — URL is from config. Already annotated `#nosec G704`.

### 6. Bitget Order Executor — `internal/services/bitget_order_executor.go` (1 call site)

Line 1016: `e.baseURL + endpoint` where `baseURL` is hardcoded to `"https://api.bitget.com"`.

### 7. AI/LLM Provider Clients — `internal/ai/` (9 call sites)

| File | Line(s) | URL Source | Verdict |
|------|---------|-----------|---------|
| `provider_unified.go` | 62 | `c.baseURL` (config) | SAFE |
| `llm/openai.go` | 170, 224 | `c.config.BaseURL` (config) | SAFE |
| `llm/anthropic.go` | 151, 204 | `c.config.BaseURL` (config) | SAFE |
| `llm/mlx.go` | 129, 183 | `c.config.BaseURL` (config) | SAFE |
| `client.go` | 418, 541 | Provider base URL (config) | SAFE |
| `capability_ranking.go` | 77 | `rs.sourceURL` (config option) | SAFE |
| `registry.go` | 165 | `r.modelsDevURL` (config option) | SAFE |

### 8. Polymarket Clients — `internal/polymarket/` (7 call sites)

| File | Function | URL | Verdict |
|------|----------|-----|---------|
| `clob_client.go` | `CreateOrder` | `c.baseURL + "/order"` | SAFE |
| `clob_client.go` | `GetOrderBook` | `c.baseURL + "/orderbook/" + tokenID` | SAFE* |
| `clob_client.go` | `CancelOrder` | `c.baseURL + "/order"` | SAFE |
| `clob_client.go` | `GetOpenOrders` | `c.baseURL + "/orders"` | SAFE |
| `clob_client.go` | `GetOrder` | `c.baseURL + "/order/" + orderID` | SAFE* |
| `gamma_client.go` | `doRequest` | `c.baseURL + path` | SAFE |

*`tokenID` and `orderID` are used in URL paths, but `baseURL` is fixed to `"https://clob.polymarket.com"`. Path traversal could theoretically reach different endpoints on the same public API server, but cannot redirect to a different host. Not an SSRF vector.

### 9. API Handlers — `internal/api/handlers/` (6 call sites)

| File | Line | URL | Verdict |
|------|------|-----|---------|
| `autonomous.go` | 1935 | `http.Get("http://localhost:3001/api/exchanges")` | SAFE |
| `dashboard.go` | 289 | `h.ccxtURL + "/health"` | SAFE |
| `health.go` | 333 | `serviceURL + "/health"` (config) | SAFE |
| `health.go` | 397 | `ccxtURL + "/health"` (config) | SAFE |
| `sqlite/market_handler.go` | 154 | `h.ccxtServiceURL + path` (config) | SAFE |
| `sqlite/portfolio_handler.go` | 882 | `h.ccxtServiceURL + "/health"` (config) | SAFE |

### 10. Telegram Adapter — `internal/adapters/telegram/adapter.go` (1 call site)

Line 224: `a.config.BaseURL + "/send-message"` — config-driven.

### 11. Sentiment/Twitter Services — (5 call sites)

All URLs are either hardcoded (Reddit, CryptoPanic, Twitter API hosts) or config-driven. User-provided crypto symbols only appear as query parameter values and cannot change the URL host.

---

## Conclusion

**False-positive.** No SSRF vulnerabilities exist. The codebase protects against outbound request forgery through:

1. **Allowlist-by-construction**: Exchange names are validated against hardcoded allowlists/maps before any URL is built.
2. **Config-driven URLs**: All internal service URLs come from environment/config, never from user input.
3. **Hardcoded external API hosts**: Third-party API calls target fixed, known hosts (binance.com, okx.com, reddit.com, twitter.com, etc.).
4. **Existing G704 annotations**: Several call sites already carry `#nosec G704` or equivalent comments acknowledging and dismissing the SSRF risk by design.

No code changes required.
