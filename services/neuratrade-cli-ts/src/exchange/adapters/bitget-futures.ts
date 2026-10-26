import { Effect, Layer } from "effect";
import { ExchangeError } from "../adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesBalance,
  type FuturesOrderFill,
  type FuturesOrderRequest,
  type FuturesPosition,
} from "../futures-adapter.js";
import {
  BitgetClient,
  type BitgetClientError,
  type BitgetClientImpl,
  type BitgetFuturesOrder,
  type BitgetFuturesOrderRequest,
  type BitgetProductType,
  marginCoinForProductType,
  toBitgetFuturesSymbol,
} from "../../services/bitget-client.js";
import { validateFuturesOrder } from "../../services/bitget-futures-guards.js";
import { validateLiveOrderSafety } from "../../services/bitget-futures-safety.js";
import { Decimal } from "../../utils/money.js";
import { money } from "../../utils/money.js";

function toExchangeError(error: BitgetClientError): ExchangeError {
  switch (error._tag) {
    case "BitgetApiError":
      return new ExchangeError(
        `Bitget API ${error.status} on ${error.endpoint}: ${error.body.slice(0, 200)}`,
        error,
      );
    case "BitgetNetworkError":
      return new ExchangeError(
        `Bitget network error on ${error.endpoint}: ${error.cause}`,
        error,
      );
    case "BitgetRateLimitError":
      return new ExchangeError(
        `Bitget rate limit on ${error.endpoint}: retry after ${error.retryAfterMs}ms`,
        error,
      );
    case "BitgetAuthError":
      return new ExchangeError(`Bitget auth error: ${error.cause}`, error);
    default:
      return new ExchangeError(`Bitget error: ${JSON.stringify(error)}`, error);
  }
}

function toBitgetProductType(
  productType: FuturesOrderRequest["productType"],
): BitgetProductType {
  return productType === "USDT-FUTURES" ? "USDT-FUTURES" : productType;
}

/**
 * Venue statuses in which a Bitget futures order is still working and can
 * fill later. Anything else (filled, canceled, rejected) is terminal.
 */
const BITGET_RESTING_STATUSES = [
  "live",
  "new",
  "partially_filled",
  "partially-filled",
  "not_triggered",
  "untriggered",
];

function bitgetOrderStatusCanStillFill(status: string): boolean {
  const normalized = status.trim().toLowerCase();
  // Empty = not indexed by the venue yet, NOT terminally unfilled. Keep
  // polling; concluding "unfilled" here would cancel a real fill (the same
  // production incident the Bybit adapter documents in pollBybitOrderFill).
  if (normalized === "") return true;
  return BITGET_RESTING_STATUSES.includes(normalized);
}

function bitgetOrderHasFill(order: BitgetFuturesOrder): boolean {
  return (
    money(order.filledSize).greaterThan(0) &&
    money(order.priceAvg).greaterThan(0)
  );
}

export function makeBitgetFuturesAdapter(
  client: BitgetClientImpl,
): FuturesExchangeAdapterService {
  const withError = <A>(effect: Effect.Effect<A, BitgetClientError>) =>
    effect.pipe(Effect.mapError(toExchangeError));

  const runPreTradeGuard = (order: BitgetFuturesOrderRequest) =>
    Effect.gen(function* () {
      const [contracts, balances, ticker] = yield* Effect.all([
        withError(client.getContracts(order.productType)),
        withError(client.getFuturesBalances(order.productType)),
        withError(client.getFuturesTicker(order.symbol, order.productType)),
      ]);
      const { symbol: bsymbol } = toBitgetFuturesSymbol(
        order.symbol,
        order.productType,
      );
      const contract = contracts.find(
        (c) => c.symbol.toUpperCase() === bsymbol.toUpperCase(),
      );
      if (contract === undefined) {
        return yield* Effect.fail(
          new ExchangeError(`contract ${bsymbol} not found`),
        );
      }
      yield* validateFuturesOrder({
        order,
        contract,
        balances,
        lastPrice: ticker.lastPrice,
        leverage: String(order.leverage ?? 1),
      }).pipe(
        Effect.mapError(
          (err) =>
            new ExchangeError(`futures guard rejected: ${err.reason}`, err),
        ),
      );
      return contract;
    });

  // Bitget rejects prices that are not multiples of the instrument's tick
  // (code 45115: "The price you enter should be a multiple of ...") and
  // requires a price parameter even on market orders (code 40020 when
  // absent). Round whatever price the engine provides to the contract's
  // pricePrecision — for market orders the value is only a reference (the
  // fill price comes from the order detail); for limit orders it becomes
  // the actual limit.
  const normalizePrice = (
    price: Decimal | undefined,
    precision: string,
  ): string | undefined => {
    if (price === undefined) return undefined;
    const decimals = Number(precision);
    return price
      .toDecimalPlaces(Number.isFinite(decimals) ? decimals : 0)
      .toString();
  };

  const placeOrder = (request: FuturesOrderRequest) =>
    Effect.gen(function* () {
      const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
        request.symbol,
        toBitgetProductType(request.productType),
      );
      const order: BitgetFuturesOrderRequest = {
        symbol: bsymbol,
        productType,
        side: request.side,
        orderType: request.type,
        size: request.size.toString(),
        marginMode: request.marginMode,
        clientOid: request.clientOid,
        reduceOnly: request.reduceOnly,
        leverage: request.leverage,
      };

      // Pre-trade guards must run inside the adapter so both the direct CLI
      // order path and the live paper-trading path reject unsafe orders before
      // any signed request leaves the process. The guard also returns the
      // contract, whose pricePrecision normalizes limit prices to the
      // instrument tick (45115); market orders drop the price entirely.
      const contract = yield* runPreTradeGuard(order);
      const normalizedPrice = normalizePrice(
        request.price,
        contract.pricePrecision,
      );
      const finalOrder: BitgetFuturesOrderRequest =
        normalizedPrice === undefined
          ? order
          : { ...order, price: normalizedPrice };

      // Account-state safety checks catch reduce-only mismatches, margin-mode
      // conflicts, and leverage mismatches before a signed order is sent.
      const [positions, leverageInfo] = yield* Effect.all([
        withError(
          client.getFuturesPositions(finalOrder.symbol, finalOrder.productType),
        ),
        withError(
          client.getLeverage({
            symbol: finalOrder.symbol,
            productType: finalOrder.productType,
          }),
        ),
      ]);
      yield* validateLiveOrderSafety({
        order: finalOrder,
        positions,
        leverageInfo,
        intendedLeverage: finalOrder.leverage
          ? String(finalOrder.leverage)
          : undefined,
      }).pipe(
        Effect.mapError(
          (err) =>
            new ExchangeError(`futures safety rejected: ${err.reason}`, err),
        ),
      );

      const ack = yield* withError(client.placeFuturesOrder(finalOrder));

      // Bitget's place-order response is an acknowledgement. Query the order
      // detail endpoint to obtain the actual filled size, price and fee before
      // the trading engine records the position. The detail endpoint lags
      // submit by a short window (status=""/qty=0 right after a market fill),
      // so a single read would report a real fill as unfilled, cancel it and
      // leave a live position the engine never recorded. Poll a bounded
      // window, like the Bybit adapter's pollBybitOrderFill.
      const orderRef = {
        symbol: finalOrder.symbol,
        productType: finalOrder.productType,
        orderId: ack.orderId || undefined,
        clientOid: ack.clientOid || undefined,
      };
      const readOrder = () => withError(client.getFuturesOrder(orderRef));
      // Market orders must be confirmed (indexing lag is the whole risk).
      // Limit/reduce-only orders are expected to rest, so they get a shorter
      // grace period for an immediate touch-fill and are then cancelled.
      const pollAttempts =
        finalOrder.orderType === "market" && finalOrder.reduceOnly !== true
          ? 10
          : 3;
      let data = yield* readOrder();
      for (let attempt = 1; attempt < pollAttempts; attempt += 1) {
        if (
          bitgetOrderHasFill(data) ||
          !bitgetOrderStatusCanStillFill(data.status)
        ) {
          break;
        }
        yield* Effect.sleep("250 millis");
        data = yield* readOrder();
      }

      if (!bitgetOrderHasFill(data)) {
        const orderId = data.orderId || ack.orderId;
        const status = String(data.status ?? "");
        const resting = BITGET_RESTING_STATUSES.includes(
          status.trim().toLowerCase(),
        );
        if (resting) {
          // The venue ACCEPTED the order: it is still working and can fill
          // behind the engine's back, and a retry would place a duplicate.
          // Cancel first, then report the failure.
          const cancelFailure = yield* withError(
            client.cancelFuturesOrder({
              symbol: finalOrder.symbol,
              productType: finalOrder.productType,
              orderId,
              clientOid: data.clientOid || undefined,
            }),
          ).pipe(
            Effect.map((): string | undefined => undefined),
            Effect.catch((err) =>
              Effect.succeed<string | undefined>(err.reason),
            ),
          );
          if (cancelFailure !== undefined) {
            // A cancel is rejected when the order filled between the last
            // read and the cancel request (or was already gone). A fill is a
            // success, not a failure — re-read once before reporting.
            const recheck = yield* readOrder().pipe(
              Effect.orElseSucceed(() => null),
            );
            if (recheck !== null && bitgetOrderHasFill(recheck)) {
              data = recheck;
            } else {
              return yield* Effect.fail(
                new ExchangeError(
                  `futures order ${orderId} not filled (status=${status}, filledQty=${data.filledSize}, filledPrice=${data.priceAvg}); CANCEL FAILED (${cancelFailure}) — the order may still rest on the exchange`,
                ),
              );
            }
          } else {
            return yield* Effect.fail(
              new ExchangeError(
                `futures order ${orderId} not filled (status=${status}, filledQty=${data.filledSize}, filledPrice=${data.priceAvg}); order cancelled`,
              ),
            );
          }
        } else {
          // Terminal-unfilled or not-yet-indexed (`""`): nothing to cancel —
          // cancelling an unindexed order is rejected anyway, and the text
          // must not claim the order may still rest when the venue reports a
          // final state.
          return yield* Effect.fail(
            new ExchangeError(
              `futures order ${orderId} not filled (status=${status}, filledQty=${data.filledSize}, filledPrice=${data.priceAvg})`,
            ),
          );
        }
      }

      const filledQty = money(data.filledSize);
      const filledPrice = money(data.priceAvg);
      const fill: FuturesOrderFill = {
        orderId: data.orderId || ack.orderId,
        clientOid: data.clientOid || ack.clientOid,
        symbol: request.symbol,
        side: data.side,
        productType: data.productType,
        marginMode: data.marginMode,
        filledQty,
        filledPrice,
        // Bitget signs the fill fee as a negative debit; the codebase stores
        // fees as non-negative costs (reconciliation + demo-readiness require
        // entryFee >= 0). Normalize the sign at the exchange boundary.
        fee: money(data.fee).abs(),
        timestamp: new Date(),
      };
      return fill;
    });

  const closePosition: FuturesExchangeAdapterService["closePosition"] = (
    request,
  ) =>
    Effect.gen(function* () {
      const positions = yield* withError(
        client.getFuturesPositions(request.symbol, request.productType),
      );
      // In hedge mode there can be both a long and a short for the same
      // symbol. Close the leg that opposes the requested side: a sell close
      // reduces a long, a buy close reduces a short.
      const neededSide = request.side === "sell" ? "long" : "short";
      const existing = positions.find((p) => p.holdSide === neededSide);
      if (!existing) {
        return null;
      }
      const available = money(existing.available);
      const closeSize = request.size.lessThan(available)
        ? request.size
        : available;
      if (closeSize.lessThanOrEqualTo(0)) {
        // A leg with volume but zero available (in-flight fills) is a real
        // divergence: fail instead of masking it as a benign no-op null.
        return yield* Effect.fail(
          new ExchangeError(
            `nothing closable for ${request.productType}:${request.symbol} (${neededSide} leg, available 0)`,
          ),
        );
      }
      return yield* placeOrder({
        symbol: request.symbol,
        side: request.side,
        type: request.price ? "limit" : "market",
        size: closeSize,
        price: request.price,
        productType: request.productType,
        marginMode: request.marginMode,
        leverage: request.leverage,
        reduceOnly: true,
      });
    });

  return {
    placeOrder,
    closePosition,

    getPosition: (symbol, productType) =>
      Effect.gen(function* () {
        const positions = yield* withError(
          client.getFuturesPositions(symbol, productType),
        );
        const activePositions = positions.filter((position) =>
          money(position.total).greaterThan(0),
        );
        if (activePositions.length > 1) {
          return yield* Effect.fail(
            new ExchangeError(
              `multiple active ${productType} positions returned for ${symbol}`,
            ),
          );
        }
        const p = activePositions.at(0);
        if (!p) {
          return null;
        }
        const position: FuturesPosition = {
          symbol,
          side: p.holdSide,
          productType: p.productType,
          marginMode: p.marginMode,
          leverage: Number(p.leverage),
          quantity: money(p.total),
          available: money(p.available),
          entryPrice: money(p.openPrice),
          liquidationPrice:
            p.liquidatedPrice === "" || p.liquidatedPrice === "0"
              ? undefined
              : money(p.liquidatedPrice),
          unrealizedPnl: money(p.unrealizedPL),
          marginCoin: bitgetMarginCoin(p.productType, symbol),
        };
        return position;
      }),

    getBalance: (marginCoin, productType) =>
      Effect.gen(function* () {
        const inferredProductType =
          productType ??
          (marginCoin.toUpperCase() === "USDC"
            ? "USDC-FUTURES"
            : "USDT-FUTURES");
        const balances = yield* withError(
          client.getFuturesBalances(inferredProductType),
        );
        const match = balances.find(
          (b) => b.marginCoin.toUpperCase() === marginCoin.toUpperCase(),
        );
        if (match === undefined) {
          // Returning an all-zero balance here reads as "no funds" and lets
          // the engine size orders against a fabricated account state.
          return yield* Effect.fail(
            new ExchangeError(
              `no ${marginCoin} balance in ${inferredProductType} (returned: ${
                balances.map((b) => b.marginCoin).join(", ") || "none"
              })`,
            ),
          );
        }
        const balance: FuturesBalance = {
          marginCoin,
          available: money(match.available),
          locked: money(match.locked),
          equity: money(match.equity),
          usdtEquity: money(match.usdtEquity),
        };
        return balance;
      }),

    setLeverage: (symbol, productType, marginMode, leverage, holdSide) =>
      Effect.gen(function* () {
        const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
        yield* withError(
          client.setLeverage({
            symbol: bsymbol,
            productType,
            marginMode,
            leverage: String(leverage),
            holdSide,
          }),
        );
      }),

    setMarginMode: (symbol, productType, marginMode) =>
      Effect.gen(function* () {
        const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
        yield* withError(
          client.setMarginMode({ symbol: bsymbol, productType, marginMode }),
        );
      }),

    setPositionMode: (productType, positionMode) =>
      withError(
        client.setPositionMode({
          productType,
          positionMode,
        }),
      ),

    setTradingStop: (request) =>
      Effect.gen(function* () {
        const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
          request.symbol,
          request.productType,
        );
        yield* withError(
          client.setTradingStop({
            symbol: bsymbol,
            productType,
            holdSide: request.side,
            takeProfit: request.takeProfit?.toString(),
            stopLoss: request.stopLoss?.toString(),
          }),
        );
      }),
  };
}

// One canonical margin-coin derivation (failing loud on unexpected COIN-FUTURES
// symbol shapes) shared with the signing client; a per-file regex drift meant
// the adapter could disagree with the endpoint URLs the client signs.
const bitgetMarginCoin = marginCoinForProductType;

export const BitgetFuturesExchangeAdapterLive: Layer.Layer<
  FuturesExchangeAdapterService,
  never,
  BitgetClient
> = Layer.effect(
  FuturesExchangeAdapter,
  Effect.gen(function* () {
    const client = yield* BitgetClient;
    return makeBitgetFuturesAdapter(client);
  }),
);
