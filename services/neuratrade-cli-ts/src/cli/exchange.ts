import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { ExchangeAdapter } from "../exchange/adapter.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";

const apiKeyOption = Options.text("api-key").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API key (or set BINANCE_API_KEY env)"),
);

const apiSecretOption = Options.text("api-secret").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API secret (or set BINANCE_API_SECRET env)"),
);

const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Symbol for the round-trip test order"),
);

const quantityOption = Options.float("quantity").pipe(
  Options.withDefault(0.001),
  Options.withDescription("Quantity to buy and sell on testnet"),
);

function makeLayer() {
  return Layer.mergeAll(BunServices.layer, MarketDataGatewayLive);
}

export const exchangeTestCommand = Command.make(
  "test",
  {
    apiKey: apiKeyOption,
    apiSecret: apiSecretOption,
    symbol: symbolOption,
    quantity: quantityOption,
  },
  (args) =>
    Effect.gen(function* () {
      const apiKey = args.apiKey || process.env.BINANCE_API_KEY || "";
      const apiSecret = args.apiSecret || process.env.BINANCE_API_SECRET || "";

      const adapterLayer = BinanceLiveExchangeAdapterLive({
        apiKey,
        apiSecret,
        live: false, // testnet only
      });
      const layers = Layer.mergeAll(makeLayer(), adapterLayer);

      const exitCode = yield* runTest(
        apiKey,
        apiSecret,
        args.symbol,
        args.quantity,
      ).pipe(
        Effect.provide(layers),
        Effect.tap((report) => Console.log(report)),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `exchange test failed: ${"reason" in err ? err.reason : String(err)}`,
            );
            return 1;
          }),
        ),
      );

      return exitCode;
    }),
).pipe(
  Command.withDescription(
    "Validate live Binance adapter with a testnet order round-trip",
  ),
);

export function runTest(
  apiKey: string,
  apiSecret: string,
  symbol: string,
  quantity: number,
) {
  return Effect.gen(function* () {
    if (!apiKey || !apiSecret) {
      yield* Console.error(
        "Binance API key and secret are required (testnet default).",
      );
      return 1;
    }

    const adapter = yield* ExchangeAdapter;

    yield* Console.log("Loading testnet account balance...");
    const usdtBefore = yield* adapter.getBalance("USDT");
    yield* Console.log(
      `USDT balance: free=${usdtBefore.free.toFixed(2)} locked=${usdtBefore.locked.toFixed(2)}`,
    );

    yield* Console.log(`Buying ${quantity} ${symbol} on testnet...`);
    const buyFill = yield* adapter.placeOrder({
      symbol,
      side: "buy",
      type: "market",
      quantity,
    });
    yield* Console.log(
      `BUY filled: orderId=${buyFill.orderId} price=${buyFill.filledPrice.toFixed(2)} qty=${buyFill.filledQty.toFixed(6)} fee=${buyFill.fee.toFixed(6)}`,
    );

    yield* Console.log(`Selling ${quantity} ${symbol} on testnet...`);
    const sellFill = yield* adapter.placeOrder({
      symbol,
      side: "sell",
      type: "market",
      quantity: buyFill.filledQty,
    });
    yield* Console.log(
      `SELL filled: orderId=${sellFill.orderId} price=${sellFill.filledPrice.toFixed(2)} qty=${sellFill.filledQty.toFixed(6)} fee=${sellFill.fee.toFixed(6)}`,
    );

    const usdtAfter = yield* adapter.getBalance("USDT");
    yield* Console.log(
      `USDT balance after: free=${usdtAfter.free.toFixed(2)} locked=${usdtAfter.locked.toFixed(2)}`,
    );

    return `Testnet round-trip complete for ${symbol}.`;
  });
}

export const exchangeCommand = Command.make("exchange", {}, () =>
  Console.log("Exchange commands. Use 'exchange test --help' for details."),
).pipe(
  Command.withDescription("Exchange adapter operations"),
  Command.withSubcommands([exchangeTestCommand]),
);
