/**
 * `neuratrade scalp trade` commands — Model B manual trading with exchange-side
 * TP/SL.
 *
 * Unlike the poll-based flow-trade engine, these commands attach take-profit
 * and stop-loss orders ON THE EXCHANGE via `setTradingStop`, so the position
 * is managed by the exchange even if this CLI disconnects. The operator picks
 * entries; the exchange enforces exits.
 *
 * Backed by the Bybit linear (USDT-perp) futures adapter (the active live
 * engine). All commands respect BYBIT_USE_TESTNET for demo trading.
 */
import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, Layer, Option } from "effect";
import { PathLive } from "../services/path.js";
import { ConfigLive } from "../services/config.js";
import { SqliteClient, SqliteClientLiveRaw } from "../services/sqlite.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { MarketDataGatewayRepositoryLive } from "../market-data/gateway-repository.js";
import { MarketDataRepositorySQLiteLive } from "../market-data/repository.js";
import {
  BybitClientLiveConfig,
  BybitFuturesExchangeAdapterLive,
} from "../exchange/adapters/bybit-futures.js";
import { BybitConfigLive } from "../services/bybit-config.js";
import { RateLimiterLive } from "../services/rate-limiter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesMarginMode,
  type FuturesProductType,
  type SetTradingStopRequest,
} from "../exchange/futures-adapter.js";
import { ExchangeError } from "../exchange/adapter.js";
import { money, type Money } from "../utils/money.js";
import type { Database } from "bun:sqlite";

/** Error union for the trade commands: parse errors (Error) or exchange errors. */
type TradeError = Error | ExchangeError;

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const symbolOption = Options.text("symbol").pipe(
  Options.withDescription("Futures pair, e.g. BTC/USDT:USDT"),
);

const sideOption = Options.text("side").pipe(
  Options.withDescription("Position side: long or short"),
);

const sizeOption = Options.text("size").pipe(
  Options.withDescription("Position size in base units, e.g. 0.01"),
);

const leverageOption = Options.integer("leverage").pipe(
  Options.withDescription("Leverage, e.g. 10"),
  Options.withDefault(1),
);

const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDescription("isolated or crossed"),
  Options.withDefault("crossed"),
);

const productTypeOption = Options.text("product-type").pipe(
  Options.withDescription("USDT-FUTURES, COIN-FUTURES or USDC-FUTURES"),
  Options.withDefault("USDT-FUTURES"),
);

/** Absolute take-profit trigger price. */
const takeProfitOption = Options.text("take-profit").pipe(
  Options.withDescription("Absolute take-profit trigger price, e.g. 70000"),
  Options.withDefault(""),
);

/** Absolute stop-loss trigger price. */
const stopLossOption = Options.text("stop-loss").pipe(
  Options.withDescription("Absolute stop-loss trigger price, e.g. 65000"),
  Options.withDefault(""),
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function parseProductType(value: string): FuturesProductType {
  if (value === "COIN-FUTURES") return "COIN-FUTURES";
  if (value === "USDC-FUTURES") return "USDC-FUTURES";
  return "USDT-FUTURES";
}

function parseMarginMode(value: string): FuturesMarginMode {
  return value === "isolated" ? "isolated" : "crossed";
}

function parseSide(value: string): "long" | "short" {
  if (value !== "long" && value !== "short") {
    throw new Error(`invalid side: ${value} (expected long or short)`);
  }
  return value;
}

function parsePrice(value: string, label: string): Option.Option<Money> {
  if (value.trim() === "") return Option.none();
  const parsed = money(value.trim());
  if (parsed.lessThanOrEqualTo(0)) {
    throw new Error(`${label} must be positive`);
  }
  return Option.some(parsed);
}

/**
 * Layer that provides SqliteClient (scoped) for the trade commands. Repos and
 * the bybit adapter are built from the resolved `db` inside each command's
 * Effect, since the market-data repository needs a concrete `bun:sqlite`
 * Database handle.
 */
function makeTradeDbLayer(home?: string) {
  const base = Layer.mergeAll(BunServices.layer, PathLive(home));
  const config = Layer.provide(ConfigLive(home), base);
  return Layer.provide(SqliteClientLiveRaw, Layer.merge(base, config));
}

/**
 * Build the adapter layer graph from a concrete SQLite handle: market-data
 * repo -> market-data gateway -> bybit client -> bybit futures adapter.
 */
function makeAdapterLayer(db: Database) {
  const base = Layer.mergeAll(BunServices.layer, MarketDataGatewayLive);
  const repoLayer = MarketDataRepositorySQLiteLive(db);
  const marketDataLayer = Layer.provide(MarketDataGatewayRepositoryLive, repoLayer);
  const bybitConfig = Layer.provide(BybitConfigLive, base);
  const bybitClient = Layer.provide(
    BybitClientLiveConfig,
    Layer.merge(bybitConfig, RateLimiterLive()),
  );
  const adapter = BybitFuturesExchangeAdapterLive.pipe(
    Layer.provide(bybitClient),
    Layer.provide(marketDataLayer),
  );
  return Layer.mergeAll(marketDataLayer, adapter);
}

/** Resolve the SQLite handle and run a body against the bybit adapter layers. */
function runWithLayers(
  home: string | undefined,
  body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService>,
): Effect.Effect<void, TradeError, SqliteClient> {
  return Effect.gen(function* () {
    const sqlite = yield* SqliteClient;
    const adapterLayers = makeAdapterLayer(sqlite.database);
    yield* body.pipe(
      Effect.provide(adapterLayers),
      Effect.tapError((err) =>
        Console.error(
          `trade failed: ${"reason" in err ? (err as { reason: unknown }).reason : String(err)}`,
        ),
      ),
    );
  });
}

/** Wrap a command body with the full layer graph. */
function wrapTrade(
  body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService>,
): Effect.Effect<void, TradeError, never> {
  return runWithLayers(process.env.NEURATRADE_HOME, body).pipe(
    Effect.provide(makeTradeDbLayer(process.env.NEURATRADE_HOME)),
  );
}

// ---------------------------------------------------------------------------
// trade open — open a position and attach exchange-side TP/SL
// ---------------------------------------------------------------------------

const tradeOpenCommand = Command.make(
  "open",
  {
    symbol: symbolOption,
    side: sideOption,
    size: sizeOption,
    leverage: leverageOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    takeProfit: takeProfitOption,
    stopLoss: stopLossOption,
  },
  (args) => {
    const body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService> =
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        const side = parseSide(args.side);
        if (args.size.trim() === "") {
          return yield* Effect.fail(new Error("--size is required"));
        }
        const size = money(args.size.trim());
        if (size.lessThanOrEqualTo(0)) {
          return yield* Effect.fail(new Error("--size must be positive"));
        }
        const productType = parseProductType(args.productType);
        const marginMode = parseMarginMode(args.marginMode);
        const tp = parsePrice(args.takeProfit, "--take-profit");
        const sl = parsePrice(args.stopLoss, "--stop-loss");
        const orderSide = side === "long" ? "buy" : "sell";

        const fill = yield* adapter.placeOrder({
          symbol: args.symbol,
          side: orderSide,
          type: "market",
          size,
          productType,
          marginMode,
          leverage: args.leverage,
        });

        yield* Console.log(
          `✅ Opened ${side} ${args.symbol} (${fill.filledQty} @ ${fill.filledPrice})`,
        );

        if (Option.isSome(tp) || Option.isSome(sl)) {
          if (adapter.setTradingStop === undefined) {
            yield* Console.log(
              "⚠️  adapter has no setTradingStop; position is open without exchange-side TP/SL",
            );
          } else {
            const tpsl: SetTradingStopRequest = {
              symbol: args.symbol,
              productType,
              marginMode,
              side,
            };
            const tpslWithTp: SetTradingStopRequest =
              Option.isSome(tp) ? { ...tpsl, takeProfit: tp.value } : tpsl;
            const tpslWithSl: SetTradingStopRequest = Option.isSome(sl)
              ? { ...tpslWithTp, stopLoss: sl.value }
              : tpslWithTp;
            yield* adapter.setTradingStop(tpslWithSl);
            yield* Console.log(
              `🛡️  Attached TP/SL (takeProfit=${Option.isSome(tp) ? tp.value.toString() : "none"}, stopLoss=${Option.isSome(sl) ? sl.value.toString() : "none"})`,
            );
          }
        } else {
          yield* Console.log(
            "ℹ️  No --take-profit/--stop-loss given; position managed manually",
          );
        }
      });
    return wrapTrade(body);
  },
).pipe(
  Command.withDescription(
    "Open a futures position and attach exchange-side take-profit/stop-loss",
  ),
);

// ---------------------------------------------------------------------------
// trade tpsl — set/update TP/SL on an existing position
// ---------------------------------------------------------------------------

const tradeTpslCommand = Command.make(
  "tpsl",
  {
    symbol: symbolOption,
    side: sideOption,
    productType: productTypeOption,
    marginMode: marginModeOption,
    takeProfit: takeProfitOption,
    stopLoss: stopLossOption,
  },
  (args) => {
    const body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService> =
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        if (adapter.setTradingStop === undefined) {
          return yield* Effect.fail(
            new Error("this adapter has no setTradingStop support"),
          );
        }
        const side = parseSide(args.side);
        const tp = parsePrice(args.takeProfit, "--take-profit");
        const sl = parsePrice(args.stopLoss, "--stop-loss");
        if (Option.isNone(tp) && Option.isNone(sl)) {
          return yield* Effect.fail(
            new Error("provide --take-profit and/or --stop-loss"),
          );
        }
        const tpsl: SetTradingStopRequest = {
          symbol: args.symbol,
          productType: parseProductType(args.productType),
          marginMode: parseMarginMode(args.marginMode),
          side,
        };
        const tpslWithTp: SetTradingStopRequest =
          Option.isSome(tp) ? { ...tpsl, takeProfit: tp.value } : tpsl;
        const tpslWithSl: SetTradingStopRequest = Option.isSome(sl)
          ? { ...tpslWithTp, stopLoss: sl.value }
          : tpslWithTp;
        yield* adapter.setTradingStop(tpslWithSl);
        yield* Console.log(
          `🛡️  Updated TP/SL for ${side} ${args.symbol} (takeProfit=${Option.isSome(tp) ? tp.value.toString() : "unchanged"}, stopLoss=${Option.isSome(sl) ? sl.value.toString() : "unchanged"})`,
        );
      });
    return wrapTrade(body);
  },
).pipe(
  Command.withDescription(
    "Set or update exchange-side TP/SL on an open position",
  ),
);

// ---------------------------------------------------------------------------
// trade status — show the open position
// ---------------------------------------------------------------------------

const tradeStatusCommand = Command.make(
  "status",
  {
    symbol: symbolOption,
    productType: productTypeOption,
  },
  (args) => {
    const body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService> =
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        const position = yield* adapter.getPosition(
          args.symbol,
          parseProductType(args.productType),
        );
        if (position === null) {
          yield* Console.log(`No open position for ${args.symbol}`);
          return;
        }
        yield* Console.log(`Position ${args.symbol}:`);
        yield* Console.log(`  side:        ${position.side}`);
        yield* Console.log(`  quantity:    ${position.quantity}`);
        yield* Console.log(`  entryPrice:  ${position.entryPrice}`);
        yield* Console.log(`  leverage:    ${position.leverage}x`);
        yield* Console.log(`  marginMode:  ${position.marginMode}`);
        yield* Console.log(`  avail:       ${position.available}`);
        if (position.liquidationPrice) {
          yield* Console.log(`  liqPrice:    ${position.liquidationPrice}`);
        }
        if (position.unrealizedPnl) {
          yield* Console.log(`  unrealizedPnl: ${position.unrealizedPnl}`);
        }
      });
    return wrapTrade(body);
  },
).pipe(Command.withDescription("Show the current open futures position"));

// ---------------------------------------------------------------------------
// trade close — close the position with a market order
// ---------------------------------------------------------------------------

const tradeCloseCommand = Command.make(
  "close",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    marginMode: marginModeOption,
    leverage: leverageOption,
  },
  (args) => {
    const body: Effect.Effect<void, TradeError, FuturesExchangeAdapterService> =
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        const position = yield* adapter.getPosition(
          args.symbol,
          parseProductType(args.productType),
        );
        if (position === null) {
          yield* Console.log(`No open position for ${args.symbol}`);
          return;
        }
        const productType = parseProductType(args.productType);
        const marginMode = parseMarginMode(args.marginMode);
        const closeSide = position.side === "long" ? "sell" : "buy";
        const fill = yield* adapter.closePosition({
          symbol: args.symbol,
          side: closeSide,
          productType,
          marginMode,
          leverage: args.leverage,
          size: position.quantity,
        });
        if (fill === null) {
          yield* Console.log(`No position to close for ${args.symbol}`);
          return;
        }
        yield* Console.log(
          `✅ Closed ${position.side} ${args.symbol} (${fill.filledQty} @ ${fill.filledPrice})`,
        );
      });
    return wrapTrade(body);
  },
).pipe(Command.withDescription("Close the open futures position (market order)"));

// ---------------------------------------------------------------------------
// trade namespace
// ---------------------------------------------------------------------------

export const tradeCommand = Command.make("trade", {}, () =>
  Console.log("Usage: neuratrade scalp trade <open|tpsl|status|close>"),
).pipe(
  Command.withDescription("Model B manual trading with exchange-side TP/SL"),
  Command.withSubcommands([
    tradeOpenCommand,
    tradeTpslCommand,
    tradeStatusCommand,
    tradeCloseCommand,
  ]),
);

// export for testing
export {
  tradeOpenCommand,
  tradeTpslCommand,
  tradeStatusCommand,
  tradeCloseCommand,
};