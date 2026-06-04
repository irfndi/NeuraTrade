# Bitget TP Visibility Investigation

> **Issue:** GH #264 / NeuraTrade-yeso (P0)
> **Status:** Root cause identified — pre-work for PR-4
> **Investigator:** W0-F (ULTRAWORK plan)
> **Date:** 2026-06-04

## Section 1: Root Cause

### Primary Root Cause: Combined→Split Fallback in `SyncPositionProtection`

The TP visibility issue originates in the `SyncPositionProtection` flow at **`bitget_order_executor.go:1268-1274`**. When the combined TP+SL call to `/api/v2/mix/order/place-pos-tpsl` fails, the code falls back to `placePositionTPSLSplit`, which calls the endpoint **twice** — once for TP only, once for SL only.

This creates **two separate plan orders** on Bitget's side. The exchange UI may only display one of them (typically SL), or the second call may overwrite the first.

### Secondary Contributor: `cancelExistingPositionTPSL` Plan Type Mismatch

At **`bitget_order_executor.go:1279-1282`**, the cancel function queries `orders-plan-pending?planType=profit_loss`. However, the plans created by `placePositionTPSL` (line 1383) via `/api/v2/mix/order/place-pos-tpsl` are **position-level plans** (`pos_profit` / `pos_loss`), which may NOT match the `profit_loss` filter. The `orders-plan-pending` endpoint's documented `planType` values include only `profit_plan`, `loss_plan`, and `profit_loss` — NOT `pos_profit` or `pos_loss`.

**Consequence:** After the first successful TP/SL set via `place-pos-tpsl`, subsequent `cancelExistingPositionTPSL` calls silently skip existing position-level plans. These orphaned plans accumulate, causing future combined `place-pos-tpsl` calls to fail (plan limit or conflict), triggering the split fallback, which compounds the issue.

### Tertiary: `place-order` Preset Plans vs Position-Level Plans

The initial order placement at **`bitget_order_executor.go:350-360`** uses `presetStopSurplusPrice` / `presetStopLossPrice` on `/api/v2/mix/order/place-order`. After fill, Bitget auto-creates **order-level plan orders** (`profit_plan` / `loss_plan`). The DynamicProtectionManager (~45s cooldown) subsequently calls `SyncPositionProtection`, which:

1. Cancels the preset-created order-level plans (via `planType=profit_loss` — works here)
2. Attempts to create **position-level plans** via `place-pos-tpsl` (different plan type)
3. If the combined call fails, the split fallback creates two separate position-level plans

This plan-type mismatch between the initial preset (order-level) and the managed update (position-level) creates an inconsistency that compounds over repeated reconciliation cycles.

---

## Section 2: Evidence

### 2.1 Code Paths

#### Path A: Order Placement with TP/SL Presets

| File | Line(s) | Description |
|------|---------|-------------|
| `bitget_order_executor.go` | 350-360 | Sets `presetStopSurplusPrice` / `presetStopSurplusExecutePrice` / `presetStopLossPrice` on `/api/v2/mix/order/place-order` |
| `bitget_order_executor.go` | 397-401 | Calls `verifyFuturesTPSLActive` to check plan orders exist after fill |
| `bitget_order_executor.go` | 409-445 | Queries `/api/v2/mix/order/orders-plan-pending?planType=profit_loss` — only checks `len(orders) > 0`, does NOT verify both TP and SL are present |

#### Path B: Post-Position TP/SL Sync (DynamicProtectionManager)

| File | Line(s) | Description |
|------|---------|-------------|
| `dynamic_protection_manager.go` | 96-195 | `ReconcileOpenPositions` — periodic reconciliation loop |
| `dynamic_protection_manager.go` | 197-274 | `computeTargets` — computes adaptive SL/TP adjustments |
| `bitget_order_executor.go` | 1236-1276 | `SyncPositionProtection` — cancel existing plans → try combined → fall back to split |
| `bitget_order_executor.go` | 1262 | `cancelExistingPositionTPSL` — cancels plans matching `planType=profit_loss` |
| `bitget_order_executor.go` | 1268 | `placePositionTPSL` — combined TP+SL via `/api/v2/mix/order/place-pos-tpsl` |
| `bitget_order_executor.go` | 1269 | `placePositionTPSLSplit` — fallback: two individual calls |
| `bitget_order_executor.go` | 1352-1399 | `placePositionTPSL` — sends `stopSurplusTriggerPrice` + `stopLossTriggerPrice` |
| `bitget_order_executor.go` | 1401-1419 | `placePositionTPSLSplit` — calls `placePositionTPSL` twice (TP first, then SL) |

### 2.2 Log Lines

| Log Pattern | Severity | Location | Meaning |
|-------------|----------|----------|---------|
| `[BITGET-ORDER] Combined TP/SL sync rejected, split fallback applied for %s` | WARN | Line 1273 | Combined `place-pos-tpsl` failed; split fallback activated |
| `[BITGET-ORDER] ⚠️ No active TP/SL plans found for holdSide=%s` | WARN | Line 440 | Verification query returned zero plan orders |
| `[BITGET-ORDER] ✅ Exchange-side TP/SL verified: %d active plan(s) for holdSide=%s` | INFO | Line 444 | Verification found plan(s) — but doesn't confirm both TP+SL exist |
| `[BITGET-ORDER] ✅ Futures order placed: %s %s (size: %s contracts) - OrderID: %s` | INFO | Line 394 | Initial order placed successfully |
| `[PROTECTION] Updating position_id=%q %s %s stop=%s->%s take=%s->%s last=%s` | INFO | `dynamic_protection_manager.go:141` | DynamicProtectionManager computed new targets |
| `[PROTECTION] Failed to sync exchange-side TP/SL for %s: %v` | WARN | `dynamic_protection_manager.go:176` | SyncPositionProtection returned error to reconciler |

### 2.3 Bitget API Doc References

| Endpoint | URL | Purpose |
|----------|-----|---------|
| `POST /api/v2/mix/order/place-order` | [Place Order](https://www.bitget.com/api-doc/contract/trade/Place-Order) | Initial order with `presetStopSurplusPrice`/`presetStopLossPrice` |
| `POST /api/v2/mix/order/place-pos-tpsl` | [Place Position TP/SL](https://www.bitget.com/api-doc/contract/plan/Place-Pos-Tpsl-Order) | Simultaneous position-level TP+SL (our sync endpoint) |
| `POST /api/v2/mix/order/place-tpsl-order` | [Place TP/SL Order](https://www.bitget.com/api-doc/contract/plan/Place-Tpsl-Order) | Order-level TP/SL with `planType`+`size` (NOT currently used) |
| `GET /api/v2/mix/order/orders-plan-pending` | [Pending Trigger Orders](https://www.bitget.com/api-doc/contract/plan/get-orders-plan-pending) | Query pending plan orders with `planType` filter |
| `POST /api/v2/mix/order/cancel-plan-order` | [Cancel Plan Order](https://www.bitget.com/api-doc/contract/plan/Cancel-Plan-Order) | Cancel individual plan orders |
| `POST /api/v2/mix/order/modify-tpsl-order` | [Modify TP/SL Order](https://www.bitget.com/api-doc/contract/plan/Modify-Tpsl-Order) | Modify existing TP/SL plan — may be better than cancel+recreate |

### 2.4 Field Name Verification

The test at **`bitget_order_executor_test.go:1052-1060`** confirms:
- ✅ `presetStopSurplusPrice` is the correct Bitget API field for TP (line 1052)
- ✅ `presetStopLossPrice` is the correct Bitget API field for SL (line 1054)
- ✅ `presetStopLossExecutePrice` is intentionally omitted for market SL execution (line 1056-1057)
- ✅ `presetTakeProfitPrice` is NOT sent — it is an invalid Bitget API field (line 1059-1060)
- The grep across the entire codebase confirms `presetTakeProfitPrice` appears ONLY in the test assertion (zero production usage)

**Conclusion:** The field naming is correct. The root cause is NOT a parameter name mismatch.

---

## Section 3: Recommended Fix

### Primary Path (Recommended): Replace Cancel+Recreate With Modify

**Problem:** The current cancel→recreate approach is destructive and fragile. Every reconciliation cycle tears down and rebuilds TP/SL plans, increasing the chance of failure, orphaned plans, and visibility gaps.

**Solution:** Replace `cancelExistingPositionTPSL` + `placePositionTPSL` with a single `modify-tpsl-order` call:

```
POST /api/v2/mix/order/modify-tpsl-order
```

**Key advantage:** This endpoint modifies existing plan orders in-place without destroying and recreating them. It avoids:
- The race window between cancel and recreate
- Plan type mismatches (existing plans stay as-is, only prices update)
- Orphaned plans from failed recreates

**Implementation sketch:**

```go
// In placePositionTPSL, instead of cancel+create:
// 1. Query existing plan orders
// 2. For each plan order matching the position:
//    - If it's a TP plan (pos_profit/profit_plan): modify trigger price
//    - If it's a SL plan (pos_loss/loss_plan): modify trigger price
//    - If neither exists: create new plan via place-pos-tpsl
```

### Alternative Path A: Fix Split Fallback Semantics

If the combined→split approach is retained (should not be preferred), fix the issues:

1. **Make split fallback atomic**: Cancel both on failure, or use a two-phase approach
2. **Fix `cancelExistingPositionTPSL` query**: Use `planType=pos_profit,pos_loss` or query all plan types to properly find position-level plans
3. **Verify both TP and SL** in `verifyFuturesTPSLActive` rather than just checking count > 0

### Alternative Path B: Switch to Modify API (`modify-tpsl-order`)

This is essentially the primary path. The modify endpoint (`POST /api/v2/mix/order/modify-tpsl-order`) is designed for this exact use case — updating TP/SL trigger prices on existing positions. It:
- Takes `orderId` (existing plan order ID) + new `triggerPrice`
- Returns success/failure without destroying the plan
- Does not create duplicate plans

**Implementation path:**
1. Before modify, check if plan orders exist via `orders-plan-pending` (with proper `planType`)
2. If plans exist → modify them
3. If no plans exist → create them via `place-pos-tpsl`

### Alternative Path C: Remove Position-Level Sync Entirely

If the initial `presetStopSurplusPrice`/`presetStopLossPrice` on `place-order` is sufficient (Bitget auto-creates plan orders after fill), consider **not running `SyncPositionProtection` for positions that already have both TP and SL from the preset**. Only call it when the DynamicProtectionManager determines an adjustment is needed.

---

## Section 4: Adaptive SL/TP Policy

### Current Behavior (`dynamic_protection_manager.go:197-274`)

The `computeTargets` function runs every `UpdateCooldown` (default: 45s) and:

- **SL adjustment:** Moves SL to break-even (entry + `BreakevenBufferPct`) or trailing stop (current price - `TrailingStopPct`), whichever is higher for longs
- **TP adjustment:** Extends TP to current price + `TakeProfitDistancePct` if market is moving favorably
- **Activation threshold:** Only kicks in after `ProfitActivationPct` (default: 0.35%) profit
- **Minimum adjustment:** Skips changes below `MinAdjustmentPct` (default: 0.05%)

### Policy Gaps

| Gap | Impact | Recommendation |
|-----|--------|----------------|
| TP is always extended, never trimmed | In a reversal, TP may be unreachable | Add TP reduction logic when position retraces |
| SL is always moved up (never down) for longs | Safe, but can be too tight | Respect a minimum distance from current price |
| No maximum TP distance | TP could extend to unrealistic levels | Add a cap (e.g., 2x ATR) |
| No cooldown-differentiated behavior | Frequent small adjustments | Adjust cooldown per position (e.g., longer for volatile pairs) |
| No market regime awareness | Same behavior in trend vs chop | Consider ATR or trend strength in adjustment magnitude |

### Recommended Policy

```
SL Rules (priority order):
1. If profit < ActivationPct → keep original SL (no change)
2. If profit > ActivationPct → move SL to break-even + BreakevenBuffer
3. If trailing stop > break-even stop → use trailing stop
4. Never move SL DOWN for longs (or UP for shorts)

TP Rules:
1. If strong uptrend (e.g., price > 20-period EMA + 2 ATR) → extend TP
2. If range-bound → keep TP fixed
3. If position retraces > 50% of max profit → reduce TP to current price + 1 ATR
4. Maximum TP extension: 3x original TP distance

Update Cooldown:
- Normal: 45s (current)
- After SL adjustment: 60s
- After TP extension: 90s (avoid over-optimistic chasing)
- After any failure: 120s (backoff)
```

---

## Section 5: Safeguards

### What Could Go Wrong

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Orphaned plan orders accumulate** | Medium | Fix `cancelExistingPositionTPSL` to use proper plan type filter; add periodic cleanup sweep |
| **Split fallback creates only one trigger** | High | Make split fallback atomic; verify both are created; cancel on partial failure |
| **DynamicProtectionManager overwrites manual UI adjustment** | Low-Medium | Add `manual_override` flag; skip positions updated via UI within cooldown period |
| **Plan type mismatch causes silent failures** | High | Use `/api/v2/mix/order/modify-tpsl-order` instead of cancel+recreate to preserve existing plan types |
| **Race between reconcile and order fill** | Medium | Check position exists and has fill price before syncing protection; skip if entry price is zero |
| **API rate limit on plan operations** | Low | Respect Bitget's 10 req/s limit; add rate limiter to plan operations |
| **Combined call always fails in production** | Medium | Log `Code` and `Msg` from failed response to classify failure reason; add metrics for combined vs split success rate |
| **Partial failure in split leaves inconsistent state** | High | Before split fallback, record desired state; on failure of either leg, attempt rollback of the successful leg |

### Verification Checks for PR-4

1. **Unit tests:** Both combined and split `placePositionTPSL` paths must succeed in test server
2. **Log analysis:** Check for `[BITGET-ORDER] Combined TP/SL sync rejected` in production logs — frequency indicates severity
3. **Plan type audit:** After TP/SL sync, query `orders-plan-pending` with ALL plan type filters to verify both TP and SL exist
4. **Visibility test:** On Bitget testnet, submit combined TP+SL and verify both appear in the UI
5. **Reconciliation durability:** Run 100 consecutive `ReconcileOpenPositions` cycles and verify TP/SL integrity

---

## Appendix: Key File References

| File | Lines | Purpose |
|------|-------|---------|
| `services/backend-api/internal/services/bitget_order_executor.go` | 223-404 | `placeFuturesOrderWithTPSL` — order placement with presets |
| `services/backend-api/internal/services/bitget_order_executor.go` | 409-445 | `verifyFuturesTPSLActive` — post-order verification |
| `services/backend-api/internal/services/bitget_order_executor.go` | 1236-1276 | `SyncPositionProtection` — cancel→create→fallback flow |
| `services/backend-api/internal/services/bitget_order_executor.go` | 1278-1350 | `cancelExistingPositionTPSL` — cancel with `planType=profit_loss` |
| `services/backend-api/internal/services/bitget_order_executor.go` | 1352-1399 | `placePositionTPSL` — combined `/place-pos-tpsl` call |
| `services/backend-api/internal/services/bitget_order_executor.go` | 1401-1419 | `placePositionTPSLSplit` — split fallback |
| `services/backend-api/internal/services/dynamic_protection_manager.go` | 96-195 | `ReconcileOpenPositions` — periodic reconciliation |
| `services/backend-api/internal/services/dynamic_protection_manager.go` | 197-274 | `computeTargets` — adaptive SL/TP target computation |
| `services/backend-api/internal/services/smart_order_executor.go` | 602-619 | `SyncPositionProtection` — delegation to base executor |
| `services/backend-api/internal/services/quest_handlers_integrated.go` | 889-901 | Protection reconciliation call from quest handlers |
| `services/backend-api/internal/services/bitget_order_executor_test.go` | 1040-1061 | Field name validation tests |
| `services/backend-api/internal/services/dynamic_protection_manager_test.go` | 62-342 | Protection manager tests |
