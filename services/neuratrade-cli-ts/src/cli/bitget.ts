/**
 * `neuratrade bitget` commands — real-money Bitget exchange operations.
 *
 * All commands require BITGET_API_KEY, BITGET_API_SECRET and BITGET_PASSPHRASE
 * to be set in the environment. Use BITGET_USE_SANDBOX=true to target demo
 * trading endpoints when available.
 */
import { Command, Options } from "@effect/cli";
import { Console, Effect } from "effect";
import {
  BitgetClient,
  BitgetClientError,
  toBitgetSymbol,
} from "../services/bitget-client.ts";
import {
  BitgetConfig,
  requireBitgetCredentials,
} from "../services/bitget-config.ts";
import { validateOrder } from "../services/bitget-guards.ts";
import { bitgetFuturesCommand } from "./bitget-futures.ts";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const symbolOption = Options.text("symbol").pipe(
  Options.withDescription("Trading pair, e.g. BTC/USDT"),
);

const orderIdOption = Options.text("order-id").pipe(
  Options.withDescription("Bitget order ID"),
  Options.withDefault(""),
);

const clientOidOption = Options.text("client-oid").pipe(
  Options.withDescription("Client-supplied order ID"),
  Options.withDefault(""),
);

const sideOption = Options.text("side").pipe(
  Options.withDescription("Order side: buy or sell"),
);

const typeOption = Options.text("type").pipe(
  Options.withDescription("Order type: market or limit"),
  Options.withDefault("market"),
);

const sizeOption = Options.text("size").pipe(
  Options.withDescription(
    "Order size (base quantity for sell/limit; quote for market buy)",
  ),
);

const priceOption = Options.text("price").pipe(
  Options.withDescription("Limit price (required for limit orders)"),
  Options.withDefault(""),
);

const dryRunOption = Options.boolean("dry-run").pipe(
  Options.withDescription("Validate the order but do not send it to Bitget"),
  Options.withDefault(false),
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function catchBitget<A>(
  program: Effect.Effect<A, BitgetClientError, BitgetClient>,
): Effect.Effect<void, never, BitgetClient> {
  return program.pipe(
    Effect.tap((value) => Console.log(JSON.stringify(value, null, 2))),
    Effect.catchAll((err) =>
      Console.log(`❌ Bitget API error: ${err._tag}: ${JSON.stringify(err)}`),
    ),
    Effect.map(() => undefined),
  );
}

function requireOneOf(
  orderId: string,
  clientOid: string,
): Effect.Effect<string, never> {
  return Effect.sync(() => {
    if (orderId.trim() === "" && clientOid.trim() === "") {
      throw new Error("Specify --order-id or --client-oid");
    }
    return orderId.trim() !== "" ? orderId.trim() : clientOid.trim();
  });
}

// ---------------------------------------------------------------------------
// balance
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// verify
// ---------------------------------------------------------------------------

const verifyCommand = Command.make("verify", {}, () =>
  Effect.gen(function* () {
    const config = yield* BitgetConfig;
    const creds = yield* requireBitgetCredentials(config);
    const client = yield* BitgetClient;
    const balances = yield* client.getBalances();
    const mode = config.useSandbox ? "demo (PAPTRADING)" : "live";
    yield* Console.log(`✅ Bitget ${mode} credentials verified`);
    yield* Console.log(
      `   key:      ${creds.apiKey.slice(0, 4)}...${creds.apiKey.slice(-4)}`,
    );
    yield* Console.log(`   balances: ${balances.length} assets`);
    for (const b of balances.slice(0, 5)) {
      yield* Console.log(`     ${b.asset}: available=${b.available}`);
    }
    if (balances.length > 5) {
      yield* Console.log(`     ... and ${balances.length - 5} more`);
    }
  }).pipe(
    Effect.catchAll((err: unknown) => {
      const details =
        err && typeof err === "object"
          ? `${"_tag" in err ? String(err._tag) : ""}: ${JSON.stringify(err)}`
          : String(err);
      return Console.log(`❌ verify failed: ${details}`).pipe(
        Effect.flatMap(() => Effect.fail(new Error(details))),
      );
    }),
  ),
).pipe(Command.withDescription("Verify Bitget credentials and read balances"));

// ---------------------------------------------------------------------------
// balance
// ---------------------------------------------------------------------------

const balanceCommand = Command.make("balance", {}, () =>
  Effect.gen(function* () {
    const config = yield* BitgetConfig;
    yield* requireBitgetCredentials(config);
    const client = yield* BitgetClient;
    const balances = yield* client.getBalances();
    yield* Console.log("Bitget balances:");
    for (const b of balances) {
      yield* Console.log(
        `  ${b.asset}: available=${b.available} frozen=${b.frozen}`,
      );
    }
  }).pipe(
    Effect.catchAll((err: unknown) => {
      const message = err instanceof Error ? err.message : String(err);
      return Console.log(`❌ balance failed: ${message}`).pipe(
        Effect.flatMap(() => Effect.fail(new Error(message))),
      );
    }),
  ),
).pipe(Command.withDescription("Show Bitget spot account balances"));

// ---------------------------------------------------------------------------
// ticker
// ---------------------------------------------------------------------------

const tickerCommand = Command.make(
  "ticker",
  { symbol: symbolOption },
  ({ symbol }) =>
    Effect.gen(function* () {
      const client = yield* BitgetClient;
      const ticker = yield* client.getTicker(symbol);
      yield* Console.log(`Bitget ticker ${ticker.symbol}:`);
      yield* Console.log(`  last:  ${ticker.lastPrice}`);
      yield* Console.log(`  bid:   ${ticker.bidPrice} x ${ticker.bidQty}`);
      yield* Console.log(`  ask:   ${ticker.askPrice} x ${ticker.askQty}`);
      yield* Console.log(`  vol24: ${ticker.volume24h}`);
    }).pipe(
      Effect.catchAll((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        return Console.log(`❌ ticker failed: ${message}`).pipe(
          Effect.flatMap(() => Effect.fail(new Error(message))),
        );
      }),
    ),
).pipe(Command.withDescription("Fetch Bitget ticker for a symbol"));

// ---------------------------------------------------------------------------
// instruments
// ---------------------------------------------------------------------------

const instrumentsCommand = Command.make("instruments", {}, () =>
  Effect.gen(function* () {
    const client = yield* BitgetClient;
    const instruments = yield* client.getInstruments();
    yield* Console.log(`Bitget spot instruments: ${instruments.length}`);
    for (const i of instruments.slice(0, 20)) {
      yield* Console.log(
        `  ${i.symbol} (${i.baseCoin}/${i.quoteCoin}) min=${i.minTradeAmount} precision=${i.quantityPrecision}`,
      );
    }
    if (instruments.length > 20) {
      yield* Console.log(`  ... and ${instruments.length - 20} more`);
    }
  }).pipe(
    Effect.catchAll((err: unknown) => {
      const message = err instanceof Error ? err.message : String(err);
      return Console.log(`❌ instruments failed: ${message}`).pipe(
        Effect.flatMap(() => Effect.fail(new Error(message))),
      );
    }),
  ),
).pipe(Command.withDescription("List Bitget spot trading instruments"));

// ---------------------------------------------------------------------------
// order place
// ---------------------------------------------------------------------------

const orderPlaceCommand = Command.make(
  "place",
  {
    symbol: symbolOption,
    side: sideOption,
    type: typeOption,
    size: sizeOption,
    price: priceOption,
    clientOid: clientOidOption,
    dryRun: dryRunOption,
  },
  ({ symbol, side, type, size, price, clientOid, dryRun }) =>
    Effect.gen(function* () {
      const config = yield* BitgetConfig;
      yield* requireBitgetCredentials(config);
      if (side !== "buy" && side !== "sell") {
        return yield* Effect.fail(new Error(`invalid side: ${side}`));
      }
      if (type !== "market" && type !== "limit") {
        return yield* Effect.fail(new Error(`invalid type: ${type}`));
      }
      if (type === "limit" && price.trim() === "") {
        return yield* Effect.fail(
          new Error("--price is required for limit orders"),
        );
      }
      const client = yield* BitgetClient;

      const orderInput: import("../services/bitget-client.ts").BitgetOrderRequest =
        {
          symbol,
          side: side as "buy" | "sell",
          orderType: type as "market" | "limit",
          size,
          price: price.trim() || undefined,
          clientOid: clientOid.trim() || undefined,
        };

      if (dryRun) {
        const [instruments, balances, ticker] = yield* Effect.all([
          client.getInstruments(),
          client.getBalances(),
          client.getTicker(symbol),
        ]);
        const bsymbol = toBitgetSymbol(symbol);
        const instrument = instruments.find((i) => i.symbol === bsymbol);
        if (instrument === undefined) {
          return yield* Effect.fail(
            new Error(`symbol ${symbol} not found in Bitget instruments`),
          );
        }
        const normalized = yield* validateOrder(
          { order: orderInput, instrument, balances },
          ticker.lastPrice,
        );
        yield* Console.log("🔍 DRY RUN — order would be:");
        yield* Console.log(`  symbol:    ${normalized.symbol}`);
        yield* Console.log(`  side:      ${normalized.side}`);
        yield* Console.log(`  type:      ${normalized.orderType}`);
        yield* Console.log(`  size:      ${normalized.size}`);
        if (normalized.price) {
          yield* Console.log(`  price:     ${normalized.price}`);
        }
        yield* Console.log(
          `  mode:      ${config.useSandbox ? "demo" : "live"}`,
        );
        return;
      }

      const order = yield* client.placeOrder(orderInput);
      yield* Console.log(`✅ Order placed: ${order.orderId}`);
      yield* Console.log(`  clientOid: ${order.clientOid}`);
      yield* Console.log(`  symbol:    ${order.symbol}`);
      yield* Console.log(`  side:      ${order.side}`);
      yield* Console.log(`  status:    ${order.status}`);
    }).pipe(
      Effect.catchAll((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        return Console.log(`❌ place order failed: ${message}`).pipe(
          Effect.flatMap(() => Effect.fail(new Error(message))),
        );
      }),
    ),
).pipe(Command.withDescription("Place a Bitget spot order"));

// ---------------------------------------------------------------------------
// order status
// ---------------------------------------------------------------------------

const orderStatusCommand = Command.make(
  "status",
  {
    symbol: symbolOption,
    orderId: orderIdOption,
    clientOid: clientOidOption,
  },
  ({ symbol, orderId, clientOid }) =>
    Effect.gen(function* () {
      yield* requireOneOf(orderId, clientOid);
      const client = yield* BitgetClient;
      const order = yield* client.getOrder({
        symbol,
        orderId: orderId.trim() || undefined,
        clientOid: clientOid.trim() || undefined,
      });
      yield* Console.log(`Order ${order.orderId}:`);
      yield* Console.log(`  status: ${order.status}`);
      yield* Console.log(`  side:   ${order.side}`);
      yield* Console.log(`  size:   ${order.size}`);
      yield* Console.log(`  price:  ${order.price}`);
      yield* Console.log(
        `  filled: ${order.filledSize} / ${order.filledAmount}`,
      );
    }).pipe(
      Effect.catchAll((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        return Console.log(`❌ order status failed: ${message}`).pipe(
          Effect.flatMap(() => Effect.fail(new Error(message))),
        );
      }),
    ),
).pipe(Command.withDescription("Query a Bitget order status"));

// ---------------------------------------------------------------------------
// order cancel
// ---------------------------------------------------------------------------

const orderCancelCommand = Command.make(
  "cancel",
  {
    symbol: symbolOption,
    orderId: orderIdOption,
    clientOid: clientOidOption,
  },
  ({ symbol, orderId, clientOid }) =>
    Effect.gen(function* () {
      yield* requireOneOf(orderId, clientOid);
      const client = yield* BitgetClient;
      yield* client.cancelOrder({
        symbol,
        orderId: orderId.trim() || undefined,
        clientOid: clientOid.trim() || undefined,
      });
      yield* Console.log("✅ Order cancel request sent");
    }).pipe(
      Effect.catchAll((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        return Console.log(`❌ cancel order failed: ${message}`).pipe(
          Effect.flatMap(() => Effect.fail(new Error(message))),
        );
      }),
    ),
).pipe(Command.withDescription("Cancel a Bitget order"));

// ---------------------------------------------------------------------------
// order namespace
// ---------------------------------------------------------------------------

const orderCommand = Command.make("order", {}, () =>
  Console.log("Usage: neuratrade bitget order <place|status|cancel>"),
).pipe(
  Command.withDescription("Bitget order management"),
  Command.withSubcommands([
    orderPlaceCommand,
    orderStatusCommand,
    orderCancelCommand,
  ]),
);

// ---------------------------------------------------------------------------
// Public export
// ---------------------------------------------------------------------------

export const bitgetCommand = Command.make("bitget", {}, () =>
  Console.log(
    "Usage: neuratrade bitget <verify|balance|ticker|instruments|order|futures>",
  ),
).pipe(
  Command.withDescription("Real-money Bitget exchange operations"),
  Command.withSubcommands([
    verifyCommand,
    balanceCommand,
    tickerCommand,
    instrumentsCommand,
    orderCommand,
    bitgetFuturesCommand,
  ]),
);
