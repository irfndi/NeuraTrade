/**
 * `neuratrade bitget futures` commands — Bitget USDT-M / coin-M futures.
 *
 * Supports leverage, margin mode, position mode, order placement and position
 * queries. All commands respect BITGET_USE_SANDBOX for demo trading.
 */
import { Command, Options } from "./kit/kit.ts";
import { appendFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { Console, Effect, Option, Result } from "effect";
import {
  BitgetClient,
  type BitgetMarginMode,
  type BitgetOrderSide,
  type BitgetOrderType,
  type BitgetPositionMode,
  type BitgetProductType,
  toBitgetFuturesSymbol,
} from "../services/bitget-client.ts";
import {
  BitgetConfig,
  requireBitgetCredentials,
} from "../services/bitget-config.ts";
import { validateFuturesOrder } from "../services/bitget-futures-guards.ts";
import { validateLiveOrderSafety } from "../services/bitget-futures-safety.ts";
import type { ExchangeError } from "../exchange/adapter.ts";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const symbolOption = Options.text("symbol").pipe(
  Options.withDescription("Futures pair, e.g. BTC/USDT:USDT"),
  Options.withDefault("BTC/USDT:USDT"),
);

const productTypeOption = Options.text("product-type").pipe(
  Options.withDescription("USDT-FUTURES, COIN-FUTURES or USDC-FUTURES"),
  Options.withDefault("USDT-FUTURES"),
);

// Optional on purpose: `leverage` with no --leverage READS the current
// leverage (see the command description). Marking it required made that
// documented read mode unreachable.
const leverageOption = Options.text("leverage").pipe(
  Options.withDescription("Leverage value, e.g. 10 (omit to read current)"),
  Options.withDefault(""),
);

const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDescription("isolated or crossed"),
  Options.withDefault("crossed"),
);

const positionModeOption = Options.text("position-mode").pipe(
  Options.withDescription("one_way or hedge_mode"),
);

const sideOption = Options.text("side").pipe(
  Options.withDescription("buy or sell"),
);

const typeOption = Options.text("type").pipe(
  Options.withDescription("market or limit"),
  Options.withDefault("market"),
);

const sizeOption = Options.text("size").pipe(
  Options.withDescription("Order size in contracts"),
);

const priceOption = Options.text("price").pipe(
  Options.withDescription("Limit price (required for limit orders)"),
  Options.withDefault(""),
);

const orderIdOption = Options.text("order-id").pipe(
  Options.withDescription("Bitget order ID"),
  Options.withDefault(""),
);

const clientOidOption = Options.text("client-oid").pipe(
  Options.withDescription("Client-supplied order ID"),
  Options.withDefault(""),
);

const dryRunOption = Options.boolean("dry-run").pipe(
  Options.withDescription("Validate but do not send the order"),
  Options.withDefault(false),
);

const forceOption = Options.boolean("force").pipe(
  Options.withDescription("Bypass live-order safety checks"),
  Options.withDefault(false),
);

const reduceOnlyOption = Options.boolean("reduce-only").pipe(
  Options.withDescription("Reduce-only flag for closing positions"),
  Options.withDefault(false),
);

const orderLeverageOption = Options.text("leverage").pipe(
  Options.withDescription(
    "Explicit leverage intent for margin/safety checks (defaults to account leverage)",
  ),
  Options.optional,
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function parseProductType(raw: string): BitgetProductType {
  const upper = raw.toUpperCase();
  if (
    upper === "USDT-FUTURES" ||
    upper === "COIN-FUTURES" ||
    upper === "USDC-FUTURES"
  ) {
    return upper;
  }
  throw new Error(
    `invalid product-type: ${raw} (expected USDT-FUTURES, COIN-FUTURES, or USDC-FUTURES)`,
  );
}

function parseMarginMode(raw: string): BitgetMarginMode {
  const lower = raw.toLowerCase();
  if (lower === "isolated" || lower === "crossed") {
    return lower;
  }
  throw new Error(`invalid margin-mode: ${raw} (expected isolated or crossed)`);
}

function parsePositionMode(raw: string): BitgetPositionMode {
  const lower = raw.toLowerCase();
  if (lower === "hedge_mode" || lower === "one_way") {
    return lower;
  }
  throw new Error(
    `invalid position-mode: ${raw} (expected hedge_mode or one_way)`,
  );
}

/** A tag-bearing, non-`Error` failure artifact surfaced by futures commands. */
interface TaggedErrorCarrier {
  readonly _tag: string;
  readonly status?: number;
  readonly body?: string;
  readonly reason?: string;
}

export function handleErr(
  err: Error | string | ExchangeError | TaggedErrorCarrier,
): Effect.Effect<never, Error> {
  // ExchangeError carries its message in .reason; Error.message is empty
  // there, which made every adapter rejection print as an empty failure.
  const details =
    err instanceof Error
      ? "reason" in err &&
        err.reason !== undefined &&
        String(err.reason).length > 0
        ? String(err.reason)
        : err.message
      : err instanceof Object
        ? "_tag" in err
          ? `${String(err._tag)}: ${JSON.stringify(err)}`
          : JSON.stringify(err)
        : String(err);
  return Console.log(`❌ futures command failed: ${details}`).pipe(
    Effect.flatMap(() => Effect.fail(new Error(details))),
  );
}

/** Durable audit trail for every --force safety bypass. */
function appendForceOrderAudit(record: {
  readonly timestamp: string;
  readonly mode: "live" | "demo";
  readonly symbol: string;
  readonly productType: string;
  readonly side: string;
  readonly orderType: string;
  readonly size: string;
  readonly rejectedReason: string;
  readonly forceReason: string;
  readonly clientOid?: string;
}): void {
  // Same resolution contract as src/services/path.ts: NEURATRADE_HOME env,
  // then ~/.neuratrade, with a leading `~` expanded before joining.
  const raw =
    process.env.NEURATRADE_HOME && process.env.NEURATRADE_HOME.length > 0
      ? process.env.NEURATRADE_HOME
      : "~/.neuratrade";
  const home = raw.startsWith("~") ? join(homedir(), raw.slice(1)) : raw;
  mkdirSync(join(home, "audit"), { recursive: true });
  appendFileSync(
    join(home, "audit", "forced-live-orders.log"),
    `${JSON.stringify(record)}\n`,
  );
}

/** clientOid-safe slug for the bypass reason (Bitget caps clientOid). */
function forceReasonSlug(reason: string): string {
  const slug = reason
    .replace(/[^a-zA-Z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 12);
  return slug === "" ? "noreason" : slug;
}

/** First validation error for raw order CLI args, if any. */
function orderArgError(
  side: string,
  type: string,
  price: string,
): Error | undefined {
  if (side !== "buy" && side !== "sell") {
    return new Error(`invalid side: ${side}`);
  }
  if (type !== "market" && type !== "limit") {
    return new Error(`invalid type: ${type}`);
  }
  if (type === "limit" && price.trim() === "") {
    return new Error("--price is required for limit orders");
  }
  return undefined;
}

// ---------------------------------------------------------------------------
// contracts
// ---------------------------------------------------------------------------

const contractsCommand = Command.make(
  "contracts",
  { productType: productTypeOption },
  ({ productType }) =>
    Effect.gen(function* () {
      const client = yield* BitgetClient;
      const contracts = yield* client.getContracts(
        parseProductType(productType),
      );
      yield* Console.log(`Bitget futures contracts: ${contracts.length}`);
      for (const c of contracts.slice(0, 20)) {
        yield* Console.log(
          `  ${c.symbol} [${c.productType}] leverage ${c.minLeverage}-${c.maxLeverage}x min=${c.minTradeAmount}`,
        );
      }
      if (contracts.length > 20) {
        yield* Console.log(`  ... and ${contracts.length - 20} more`);
      }
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("List Bitget futures contracts"));

// ---------------------------------------------------------------------------
// ticker
// ---------------------------------------------------------------------------

const tickerCommand = Command.make(
  "ticker",
  { symbol: symbolOption, productType: productTypeOption },
  ({ symbol, productType }) =>
    Effect.gen(function* () {
      const client = yield* BitgetClient;
      const ticker = yield* client.getFuturesTicker(
        symbol,
        parseProductType(productType),
      );
      yield* Console.log(`Bitget futures ticker ${ticker.symbol}:`);
      yield* Console.log(`  last: ${ticker.lastPrice}`);
      yield* Console.log(`  bid:  ${ticker.bidPrice} x ${ticker.bidQty}`);
      yield* Console.log(`  ask:  ${ticker.askPrice} x ${ticker.askQty}`);
      if (ticker.fundingRate !== undefined) {
        yield* Console.log(`  funding: ${ticker.fundingRate}`);
      }
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Fetch Bitget futures ticker"));

// ---------------------------------------------------------------------------
// balance
// ---------------------------------------------------------------------------

const balanceCommand = Command.make(
  "balance",
  { productType: productTypeOption },
  ({ productType }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const client = yield* BitgetClient;
      const balances = yield* client.getFuturesBalances(
        parseProductType(productType),
      );
      yield* Console.log("Bitget futures balances:");
      for (const b of balances) {
        yield* Console.log(
          `  ${b.marginCoin}: available=${b.available} equity=${b.equity} usdtEq=${b.usdtEquity}`,
        );
      }
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Show Bitget futures account balances"));

// ---------------------------------------------------------------------------
// positions
// ---------------------------------------------------------------------------

const positionsCommand = Command.make(
  "positions",
  { symbol: symbolOption, productType: productTypeOption },
  ({ symbol, productType }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const client = yield* BitgetClient;
      const positions = yield* client.getFuturesPositions(
        symbol,
        parseProductType(productType),
      );
      if (positions.length === 0) {
        yield* Console.log("No open futures positions");
        return;
      }
      yield* Console.log(`Open futures positions: ${positions.length}`);
      for (const p of positions) {
        yield* Console.log(
          `  ${p.holdSide} ${p.total} ${p.symbol} @ ${p.openPrice} lev=${p.leverage} margin=${p.marginMode} liq=${p.liquidatedPrice}`,
        );
      }
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Show open Bitget futures positions"));

// ---------------------------------------------------------------------------
// leverage
// ---------------------------------------------------------------------------

const leverageCommand = Command.make(
  "leverage",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    marginMode: marginModeOption,
    leverage: leverageOption,
  },
  ({ symbol, productType, marginMode, leverage }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const client = yield* BitgetClient;
      const pt = parseProductType(productType);
      const mm = parseMarginMode(marginMode);
      if (leverage.trim() === "") {
        const info = yield* client.getLeverage({ symbol, productType: pt });
        yield* Console.log(`Leverage info for ${symbol}:`);
        for (const i of info) {
          yield* Console.log(
            `  ${i.marginMode}: ${i.leverage} (min ${i.minLeverage} - max ${i.maxLeverage})`,
          );
        }
      } else {
        yield* client.setLeverage({
          symbol,
          productType: pt,
          marginMode: mm,
          leverage: leverage.trim(),
        });
        yield* Console.log(
          `✅ Leverage set to ${leverage}x (${mm}) for ${symbol}`,
        );
      }
    }).pipe(Effect.catch(handleErr)),
).pipe(
  Command.withDescription(
    "Get or set leverage (omit --leverage to read current)",
  ),
);

// ---------------------------------------------------------------------------
// margin-mode
// ---------------------------------------------------------------------------

const marginModeCommand = Command.make(
  "margin-mode",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    marginMode: marginModeOption,
  },
  ({ symbol, productType, marginMode }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const client = yield* BitgetClient;
      yield* client.setMarginMode({
        symbol,
        productType: parseProductType(productType),
        marginMode: parseMarginMode(marginMode),
      });
      yield* Console.log(`✅ Margin mode set to ${marginMode} for ${symbol}`);
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Set futures margin mode"));

// ---------------------------------------------------------------------------
// position-mode
// ---------------------------------------------------------------------------

const positionModeCommand = Command.make(
  "position-mode",
  { productType: productTypeOption, positionMode: positionModeOption },
  ({ productType, positionMode }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const client = yield* BitgetClient;
      yield* client.setPositionMode({
        productType: parseProductType(productType),
        positionMode: parsePositionMode(positionMode),
      });
      yield* Console.log(
        `✅ Position mode set to ${positionMode} for ${productType}`,
      );
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Set futures position mode"));

// ---------------------------------------------------------------------------
// order place
// ---------------------------------------------------------------------------

const forceReasonOption = Options.text("force-reason").pipe(
  Options.withDescription(
    "Required with --force: why the rejected safety check may be bypassed; " +
      "recorded in the audit log and embedded in the order's clientOid",
  ),
  Options.withDefault(""),
);

const orderPlaceCommand = Command.make(
  "place",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    side: sideOption,
    type: typeOption,
    size: sizeOption,
    price: priceOption,
    marginMode: marginModeOption,
    clientOid: clientOidOption,
    dryRun: dryRunOption,
    force: forceOption,
    forceReason: forceReasonOption,
    reduceOnly: reduceOnlyOption,
    leverage: orderLeverageOption,
  },
  (args) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      const argError = orderArgError(args.side, args.type, args.price);
      if (argError !== undefined) {
        return yield* Effect.fail(argError);
      }
      const client = yield* BitgetClient;
      const pt = parseProductType(args.productType);
      const orderInput = {
        symbol: args.symbol,
        productType: pt,
        side: args.side as BitgetOrderSide,
        orderType: args.type as BitgetOrderType,
        size: args.size,
        price: args.price.trim() || undefined,
        marginMode: parseMarginMode(args.marginMode),
        clientOid: args.clientOid.trim() || undefined,
        reduceOnly: args.reduceOnly,
      };

      const [contracts, balances, ticker, positions, leverageInfo] =
        yield* Effect.all([
          client.getContracts(pt),
          client.getFuturesBalances(pt),
          client.getFuturesTicker(args.symbol, pt),
          client.getFuturesPositions(args.symbol, pt),
          client.getLeverage({ symbol: args.symbol, productType: pt }),
        ]);
      const { symbol: bsymbol } = toBitgetFuturesSymbol(args.symbol, pt);
      const contract = contracts.find((c) => c.symbol === bsymbol);
      if (contract === undefined) {
        return yield* Effect.fail(
          new Error(`contract ${args.symbol} not found`),
        );
      }

      const intendedLeverage = Option.getOrUndefined(args.leverage);
      const accountLeverage = leverageInfo.find(
        (l) => l.marginMode === orderInput.marginMode,
      )?.leverage;
      const effectiveLeverage = intendedLeverage ?? accountLeverage ?? "1";

      const safetyCheck = yield* validateLiveOrderSafety({
        order: orderInput,
        positions,
        leverageInfo,
        intendedLeverage,
      }).pipe(Effect.result);
      if (Result.isFailure(safetyCheck)) {
        const rejectedReason = safetyCheck.failure.reason;
        if (!args.force) {
          return yield* Effect.fail(
            new Error(`safety check failed: ${rejectedReason}`),
          );
        }
        // @codebuff fix 082: --force alone no longer silently places a
        // real-money leveraged order the safety layer REJECTED. Bypassing
        // requires (1) --force-reason, (2) on a live order an extra env
        // opt-in, (3) a durable audit record, and (4) the reason stamped
        // into the order's clientOid so the order itself is traceable.
        const forceReason = args.forceReason.trim();
        if (forceReason === "") {
          return yield* Effect.fail(
            new Error(
              "--force requires --force-reason <why the safety gate may be bypassed>; the reason is recorded in the audit log and embedded in the order's clientOid",
            ),
          );
        }
        if (
          !config.useSandbox &&
          process.env.NEURATRADE_ALLOW_LIVE_OVERRIDE !== "1"
        ) {
          return yield* Effect.fail(
            new Error(
              "--force on a LIVE order additionally requires NEURATRADE_ALLOW_LIVE_OVERRIDE=1 (demo orders pass with --force-reason alone)",
            ),
          );
        }
        if (!orderInput.clientOid) {
          orderInput.clientOid = `forced-${forceReasonSlug(forceReason)}-${Date.now().toString(36)}`;
        }
        const auditResult = yield* Effect.result(
          Effect.sync(() =>
            appendForceOrderAudit({
              timestamp: new Date().toISOString(),
              mode: config.useSandbox ? "demo" : "live",
              symbol: orderInput.symbol,
              productType: orderInput.productType,
              side: orderInput.side,
              orderType: orderInput.orderType,
              size: orderInput.size,
              rejectedReason,
              forceReason,
              clientOid: orderInput.clientOid,
            }),
          ),
        );
        if (Result.isFailure(auditResult)) {
          yield* Console.error(
            `⚠️  could not write the --force audit record (${String(auditResult.failure)}) — bypass reason: ${forceReason}`,
          );
        }
        yield* Console.log(
          `⚠️  safety check bypassed: ${rejectedReason} (reason: ${forceReason}${orderInput.clientOid ? `, clientOid: ${orderInput.clientOid}` : ""})`,
        );
      }

      // Pre-trade futures guards must run in both dry-run and live paths so a
      // live futures order is never sent without notional/margin/contract checks.
      const guard = yield* validateFuturesOrder({
        order: orderInput,
        contract,
        balances,
        lastPrice: ticker.lastPrice,
        leverage: effectiveLeverage,
      });

      if (args.dryRun) {
        yield* Console.log("🔍 DRY RUN — futures order would be:");
        yield* Console.log(`  symbol:         ${bsymbol}`);
        yield* Console.log(`  productType:    ${orderInput.productType}`);
        yield* Console.log(`  side:           ${orderInput.side}`);
        yield* Console.log(`  type:           ${orderInput.orderType}`);
        yield* Console.log(`  size:           ${orderInput.size}`);
        if (orderInput.price) {
          yield* Console.log(`  price:          ${orderInput.price}`);
        }
        yield* Console.log(`  marginMode:     ${orderInput.marginMode}`);
        yield* Console.log(`  leverage:       ${effectiveLeverage}x`);
        yield* Console.log(`  notional:       ${guard.notional}`);
        yield* Console.log(`  marginRequired: ${guard.marginRequired}`);
        yield* Console.log(`  reduceOnly:     ${orderInput.reduceOnly}`);
        if (positions.length > 0) {
          yield* Console.log(
            `  openPositions:  ${positions.map((p) => `${p.holdSide} ${p.total}`).join(", ")}`,
          );
        }
        yield* Console.log(
          `  leverageInfo:   ${leverageInfo.map((l) => `${l.marginMode} ${l.leverage}x`).join(", ")}`,
        );
        yield* Console.log(
          `  mode:           ${config.useSandbox ? "demo" : "live"}`,
        );
        return;
      }
      const order = yield* client.placeFuturesOrder(orderInput);
      yield* Console.log(`✅ Futures order placed: ${order.orderId}`);
      yield* Console.log(`  symbol:    ${order.symbol}`);
      yield* Console.log(`  side:      ${order.side}`);
      yield* Console.log(`  status:    ${order.status}`);
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Place a Bitget futures order"));

// ---------------------------------------------------------------------------
// order status
// ---------------------------------------------------------------------------

const orderStatusCommand = Command.make(
  "status",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    orderId: orderIdOption,
    clientOid: clientOidOption,
  },
  ({ symbol, productType, orderId, clientOid }) =>
    Effect.gen(function* () {
      if (orderId.trim() === "" && clientOid.trim() === "") {
        return yield* Effect.fail(
          new Error("Specify --order-id or --client-oid"),
        );
      }
      const client = yield* BitgetClient;
      const order = yield* client.getFuturesOrder({
        symbol,
        productType: parseProductType(productType),
        orderId: orderId.trim() || undefined,
        clientOid: clientOid.trim() || undefined,
      });
      yield* Console.log(`Futures order ${order.orderId}:`);
      yield* Console.log(`  status: ${order.status}`);
      yield* Console.log(`  side:   ${order.side}`);
      yield* Console.log(`  size:   ${order.size}`);
      yield* Console.log(`  price:  ${order.price}`);
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Query a Bitget futures order"));

// ---------------------------------------------------------------------------
// order cancel
// ---------------------------------------------------------------------------

const orderCancelCommand = Command.make(
  "cancel",
  {
    symbol: symbolOption,
    productType: productTypeOption,
    orderId: orderIdOption,
    clientOid: clientOidOption,
  },
  ({ symbol, productType, orderId, clientOid }) =>
    Effect.gen(function* () {
      if (orderId.trim() === "" && clientOid.trim() === "") {
        return yield* Effect.fail(
          new Error("Specify --order-id or --client-oid"),
        );
      }
      const client = yield* BitgetClient;
      yield* client.cancelFuturesOrder({
        symbol,
        productType: parseProductType(productType),
        orderId: orderId.trim() || undefined,
        clientOid: clientOid.trim() || undefined,
      });
      yield* Console.log("✅ Futures order cancel request sent");
    }).pipe(Effect.catch(handleErr)),
).pipe(Command.withDescription("Cancel a Bitget futures order"));

// ---------------------------------------------------------------------------
// order namespace
// ---------------------------------------------------------------------------

const orderCommand = Command.make("order", {}, () =>
  Console.log("Usage: neuratrade bitget futures order <place|status|cancel>"),
).pipe(
  Command.withDescription("Bitget futures order management"),
  Command.withSubcommands([
    orderPlaceCommand,
    orderStatusCommand,
    orderCancelCommand,
  ]),
);

// ---------------------------------------------------------------------------
// Public export
// ---------------------------------------------------------------------------

export const bitgetFuturesCommand = Command.make("futures", {}, () =>
  Console.log(
    "Usage: neuratrade bitget futures <contracts|ticker|balance|positions|leverage|margin-mode|position-mode|order>",
  ),
).pipe(
  Command.withDescription("Bitget futures (perpetual) operations"),
  Command.withSubcommands([
    contractsCommand,
    tickerCommand,
    balanceCommand,
    positionsCommand,
    leverageCommand,
    marginModeCommand,
    positionModeCommand,
    orderCommand,
  ]),
);
