import { beforeEach, describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import {
  BitgetClient,
  type BitgetClientImpl,
  type BitgetFuturesPosition,
} from "../../services/bitget-client.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../futures-adapter.js";
import { makeBitgetFuturesAdapter } from "./bitget-futures.js";
import { money } from "../../utils/money.js";

let calls: string[] = [];

function makeStubClient(): BitgetClientImpl {
  calls = [];
  return {
    getBalances: () => Effect.succeed([]),
    getInstruments: () => Effect.succeed([]),
    getTicker: () =>
      Effect.succeed({
        symbol: "BTCUSDT",
        lastPrice: "70000",
        bidPrice: "69999",
        askPrice: "70001",
        bidQty: "1",
        askQty: "1",
        volume24h: "1000000",
      }),
    placeOrder: () =>
      Effect.succeed({
        orderId: "spot-1",
        clientOid: "",
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "market",
        status: "filled",
        size: "0.1",
        price: "70000",
        filledSize: "0.1",
        filledAmount: "7000",
        fee: "4.2",
      }),
    getOrder: () =>
      Effect.succeed({
        orderId: "spot-1",
        clientOid: "",
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "market",
        status: "filled",
        size: "0.1",
        price: "70000",
        filledSize: "0.1",
        filledAmount: "7000",
        fee: "4.2",
      }),
    cancelOrder: () => Effect.void,
    getContracts: () =>
      Effect.succeed([
        {
          symbol: "BTCUSDT",
          baseCoin: "BTC",
          quoteCoin: "USDT",
          productType: "USDT-FUTURES" as const,
          status: "online",
          symbolStatus: "online",
          pricePrecision: "2",
          quantityPrecision: "4",
          minTradeAmount: "5",
          minTradeNum: "0.0001",
          minTradeUSDT: "5",
          maxLeverage: "125",
          minLeverage: "1",
          takerFeeRate: "0.0006",
          makerFeeRate: "0.0002",
        },
      ]),
    getFuturesTicker: () =>
      Effect.succeed({
        symbol: "BTCUSDT",
        lastPrice: "70000",
        bidPrice: "69999",
        askPrice: "70001",
        bidQty: "1",
        askQty: "1",
        volume24h: "1000000",
      }),
    getFuturesBalances: () =>
      Effect.succeed([
        {
          marginCoin: "USDT",
          available: "5000",
          locked: "1000",
          equity: "6000",
          usdtEquity: "6000",
        },
      ]),
    getFuturesPositions: () =>
      Effect.succeed([
        {
          positionId: "pos-1",
          symbol: "BTCUSDT",
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          holdSide: "long",
          openPrice: "66000",
          total: "0.5",
          available: "0.5",
          leverage: "10",
          unrealizedPL: "200",
          liquidatedPrice: "60000",
        },
      ]),
    setLeverage: (args) =>
      Effect.sync(() => {
        calls.push(`setLeverage:${args.symbol}:${args.leverage}`);
      }),
    getLeverage: () => Effect.succeed([]),
    setMarginMode: (args) =>
      Effect.sync(() => {
        calls.push(`setMarginMode:${args.symbol}:${args.marginMode}`);
      }),
    setPositionMode: (args) =>
      Effect.sync(() => {
        calls.push(`setPositionMode:${args.productType}:${args.positionMode}`);
      }),
    placeFuturesOrder: (order) =>
      Effect.sync(() => {
        calls.push(
          `placeFuturesOrder:${order.symbol}:${order.side}:${order.reduceOnly ?? "no"}`,
        );
        return {
          orderId: "fut-1",
          clientOid: order.clientOid ?? "",
          symbol: order.symbol,
          productType: order.productType,
          side: order.side,
          orderType: order.orderType,
          status: "filled",
          size: order.size,
          price: order.price ?? "70000",
          priceAvg: order.price ?? "70000",
          filledSize: order.size,
          filledAmount: String(
            Number(order.size) * Number(order.price ?? "70000"),
          ),
          fee: "1",
          marginMode: order.marginMode ?? "crossed",
        };
      }),
    getFuturesOrder: () =>
      Effect.succeed({
        orderId: "fut-1",
        clientOid: "",
        symbol: "BTCUSDT",
        productType: "USDT-FUTURES",
        side: "buy",
        orderType: "market",
        status: "filled",
        size: "0.1",
        price: "70000",
        priceAvg: "70000",
        filledSize: "0.1",
        filledAmount: "7000",
        fee: "1",
        marginMode: "crossed",
      }),
    cancelFuturesOrder: () => Effect.void,
  };
}

let testLayer: Layer.Layer<FuturesExchangeAdapterService>;

beforeEach(() => {
  const adapter = makeBitgetFuturesAdapter(makeStubClient());
  testLayer = Layer.succeed(FuturesExchangeAdapter, adapter);
});

function run<T>(effect: Effect.Effect<T, unknown, unknown>): Promise<T> {
  return Effect.runPromise(
    effect.pipe(Effect.provide(testLayer)) as Effect.Effect<T, unknown>,
  );
}

describe("BitgetFuturesExchangeAdapter", () => {
  it("places a market order", async () => {
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: money(0.1),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.orderId).toBe("fut-1");
    expect(fill.symbol).toBe("BTC/USDT:USDT");
    expect(fill.side).toBe("buy");
    expect(fill.filledQty.toNumber()).toBe(0.1);
    expect(calls).toContain("placeFuturesOrder:BTCUSDT:buy:no");
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
  });

  it("fails closed when multiple active position legs are returned", async () => {
    const activePositions: ReadonlyArray<BitgetFuturesPosition> = [
      {
        positionId: "long-1",
        symbol: "BTCUSDT",
        productType: "USDT-FUTURES",
        marginMode: "crossed",
        holdSide: "long",
        openPrice: "66000",
        total: "0.5",
        available: "0.5",
        leverage: "10",
        unrealizedPL: "200",
        liquidatedPrice: "60000",
      },
      {
        positionId: "short-1",
        symbol: "BTCUSDT",
        productType: "USDT-FUTURES",
        marginMode: "crossed",
        holdSide: "short",
        openPrice: "66000",
        total: "0.5",
        available: "0.5",
        leverage: "10",
        unrealizedPL: "200",
        liquidatedPrice: "60000",
      },
    ];
    const client: BitgetClientImpl = {
      ...makeStubClient(),
      getFuturesPositions: () => Effect.succeed(activePositions),
    };
    const adapterLayer = Layer.succeed(
      FuturesExchangeAdapter,
      makeBitgetFuturesAdapter(client),
    );
    const exit = await Effect.runPromiseExit(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }).pipe(Effect.provide(adapterLayer)),
    );

    expect(exit._tag).toBe("Failure");
  });

  it("reads a balance", async () => {
    const balance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getBalance("USDT");
      }),
    );

    expect(balance.available.toNumber()).toBe(5000);
    expect(balance.equity.toNumber()).toBe(6000);
  });

  it("closes a position with a reduce-only order", async () => {
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
    expect(calls).toContain("placeFuturesOrder:BTCUSDT:sell:true");
  });

  it("configures leverage, margin mode and position mode", async () => {
    await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        yield* adapter.setLeverage(
          "BTC/USDT:USDT",
          "USDT-FUTURES",
          "crossed",
          5,
        );
        yield* adapter.setMarginMode(
          "BTC/USDT:USDT",
          "USDT-FUTURES",
          "crossed",
        );
        yield* adapter.setPositionMode("USDT-FUTURES", "one_way");
      }),
    );

    expect(calls).toContain("setLeverage:BTCUSDT:5");
    expect(calls).toContain("setMarginMode:BTCUSDT:crossed");
    expect(calls).toContain("setPositionMode:USDT-FUTURES:one_way");
  });
});
