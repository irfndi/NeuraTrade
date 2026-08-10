import { beforeEach, describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import type { MarketDataGatewayService } from "../../market-data/gateway.js";
import {
  type BybitClientImpl,
  type BybitOrderRequest,
  makeBybitFuturesAdapter,
} from "./bybit-futures.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../futures-adapter.js";
import { money } from "../../utils/money.js";

const GATEWAY_PRICE = 40000;

let calls: string[] = [];
let lastOrder: BybitOrderRequest | undefined;

function makeStubClient(): BybitClientImpl {
  calls = [];
  lastOrder = undefined;
  return {
    getContract: () =>
      Effect.succeed({
        symbol: "BTCUSDT",
        status: "Trading",
        minOrderQty: "0.0001",
        qtyStep: "0.0001",
        minOrderAmt: "5",
        tickSize: "0.1",
        maxLeverage: "100",
      }),
    getBalance: () =>
      Effect.succeed([
        {
          coin: "USDT",
          equity: "6000",
          walletBalance: "5000",
          availableToWithdraw: "4000",
          usdValue: "6000",
        },
      ]),
    getPositions: () =>
      Effect.succeed([
        {
          symbol: "BTCUSDT",
          side: "Buy",
          size: "0.5",
          avgPrice: "66000",
          unrealisedPnl: "200",
          liqPrice: "60000",
          leverage: "10",
          tradeMode: 0,
          positionIdx: 0,
        },
      ]),
    placeOrder: (order) =>
      Effect.sync(() => {
        lastOrder = order;
        calls.push(
          `placeOrder:${order.symbol}:${order.side}:${order.orderType}:${order.qty}:${order.reduceOnly ?? "no"}`,
        );
        return { orderId: "bybit-1", clientOrderId: "link-1" };
      }),
    getOrder: () =>
      Effect.succeed({
        orderId: "bybit-1",
        clientOrderId: "link-1",
        symbol: lastOrder?.symbol ?? "BTCUSDT",
        side: lastOrder?.side ?? "Buy",
        orderType: lastOrder?.orderType ?? "Market",
        orderStatus: "Filled",
        qty: lastOrder?.qty ?? "0",
        price: lastOrder?.price ?? "0",
        avgPrice: lastOrder?.price ?? String(GATEWAY_PRICE),
        cumExecQty: lastOrder?.qty ?? "0",
        cumExecFee: "0.5",
      }),
    setLeverage: (args) =>
      Effect.sync(() => {
        calls.push(`setLeverage:${args.symbol}:${args.buyLeverage}`);
      }),
    setMarginMode: () => Effect.void,
    setPositionMode: () => Effect.void,
  };
}

const stubGateway: MarketDataGatewayService = {
  fetchTick: (_exchange, symbol) =>
    Effect.succeed({
      exchange: "bybit-futures",
      symbol,
      price: GATEWAY_PRICE,
      volume: 0,
      timestamp: new Date(),
    }),
  fetchOHLCV: () => Effect.die("not used in bybit adapter test"),
  fetchOrderBook: () => Effect.die("not used in bybit adapter test"),
  fetchSymbols: () => Effect.die("not used in bybit adapter test"),
  fetchDemoSymbols: () => Effect.die("not used in bybit adapter test"),
  fetch24hrVolumes: () => Effect.die("not used in bybit adapter test"),
  fetchFundingRates: () => Effect.die("not used in bybit adapter test"),
};

let testLayer: Layer.Layer<FuturesExchangeAdapterService>;

beforeEach(() => {
  const adapter = makeBybitFuturesAdapter(makeStubClient(), stubGateway);
  testLayer = Layer.succeed(FuturesExchangeAdapter, adapter);
});

function run<T>(effect: Effect.Effect<T, unknown, unknown>): Promise<T> {
  return Effect.runPromise(
    effect.pipe(Effect.provide(testLayer)) as Effect.Effect<T, unknown>,
  );
}

describe("BybitFuturesExchangeAdapter", () => {
  it("sizes a sub-floor market order UP to the contract floor (qty round-up)", async () => {
    // floor = max(minOrderAmt=5, minQty 0.0001 * 40000 = 4) = 5 USDT. A 0.0001
    // BTC order is 4 USDT notional — below the floor — so the qty is raised to
    // ceil(5/40000 / 0.0001) * 0.0001 = 0.0002 and leverage lifted to
    // min(requested 10, ceil(5/4)=2, max 100) = 2 so the floor's margin fits.
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: money(0.0001),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.orderId).toBe("bybit-1");
    expect(fill.symbol).toBe("BTC/USDT:USDT");
    expect(fill.side).toBe("buy");
    expect(fill.filledQty.toNumber()).toBe(0.0002);
    expect(calls).toContain("placeOrder:BTCUSDT:Buy:Market:0.0002:no");
    expect(calls).toContain("setLeverage:BTCUSDT:2");
  });

  it("rounds an off-step qty up to the qty step", async () => {
    // 0.00013 BTC * 40000 = 5.2 USDT >= floor 5, but 0.00013 is not a multiple
    // of the 0.0001 step — rounded UP to 0.0002.
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: money(0.00013),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.filledQty.toNumber()).toBe(0.0002);
    expect(calls).toContain("placeOrder:BTCUSDT:Buy:Market:0.0002:no");
  });

  it("rejects a below-min qty with the orderability error", async () => {
    const outcome = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter
          .placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: money(0.00005),
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 10,
          })
          .pipe(
            Effect.map((fill) => ({ ok: true as const, fill })),
            Effect.catch((err) =>
              Effect.succeed({ ok: false as const, reason: err.reason }),
            ),
          );
      }),
    );

    expect(outcome.ok).toBe(false);
    if (!outcome.ok) {
      expect(outcome.reason).toContain("futures guard rejected");
      expect(outcome.reason).toContain("below min order qty");
      expect(outcome.reason).toContain("0.00005");
    }
    // No order reached the exchange.
    expect(calls.join("\n")).not.toContain("placeOrder:BTCUSDT");
  });

  it("reads a balance from the wallet response", async () => {
    const balance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getBalance("USDT");
      }),
    );

    expect(balance.marginCoin).toBe("USDT");
    expect(balance.available.toNumber()).toBe(4000);
    expect(balance.locked.toNumber()).toBe(1000);
    expect(balance.equity.toNumber()).toBe(6000);
    expect(balance.usdtEquity.toNumber()).toBe(6000);
  });

  it("reads a position", async () => {
    const position = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }),
    );

    expect(position).not.toBeNull();
    expect(position?.side).toBe("long");
    expect(position?.quantity.toNumber()).toBe(0.5);
    expect(position?.leverage).toBe(10);
    expect(position?.marginMode).toBe("crossed");
  });

  it("closes a position with a reduce-only order on the opposing side", async () => {
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.closePosition({
          symbol: "BTC/USDT:USDT",
          side: "sell",
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
          size: money(0.5),
        });
      }),
    );

    expect(fill).not.toBeNull();
    // Closing a long (Buy position) with a sell: the order flips to Sell and
    // must be reduce-only.
    expect(calls).toContain("placeOrder:BTCUSDT:Sell:Market:0.5:true");
  });

  it("rounds limit prices to the instrument tick", async () => {
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "limit",
          size: money(0.0001),
          price: money(66055.303),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill).not.toBeNull();
    const order = calls.find((c) => c.startsWith("placeOrder:"));
    expect(order).toContain("0.0001:no");
    // The price is embedded in the body via the fake's lastOrder.
    expect(lastOrder?.price).toBe("66055.3");
  });
});
