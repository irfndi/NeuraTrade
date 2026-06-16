import { Effect, Layer } from "effect";
import type { MarketDataGatewayService } from "../gateway.js";
import { MarketDataError, MarketDataGateway } from "../gateway.js";
import * as Binance from "./binance.js";

/**
 * Composite gateway that routes by exchange name.
 *
 * Currently supports:
 *   - binance
 *
 * Additional exchanges can be added here without changing consumers.
 */
export const MarketDataGatewayLive = Layer.succeed(MarketDataGateway, {
  fetchTick: (exchange, symbol) =>
    dispatch(exchange, "fetchTick", () => Binance.fetchTick(symbol)),

  fetchOHLCV: (exchange, symbol, timeframe, limit, startTime) =>
    dispatch(exchange, "fetchOHLCV", () =>
      Binance.fetchOHLCV(symbol, timeframe, limit, startTime),
    ),

  fetchOrderBook: (exchange, symbol, limit) =>
    dispatch(exchange, "fetchOrderBook", () =>
      Binance.fetchOrderBook(symbol, limit),
    ),

  fetchSymbols: (exchange) =>
    dispatch(exchange, "fetchSymbols", () => Binance.fetchSymbols()),

  fetch24hrVolumes: (exchange) =>
    dispatch(exchange, "fetch24hrVolumes", () => Binance.fetch24hrVolumes()),
} satisfies MarketDataGatewayService);

function dispatch<A>(
  exchange: string,
  operation: string,
  run: () => Effect.Effect<A, MarketDataError, never>,
): Effect.Effect<A, MarketDataError, never> {
  if (exchange.toLowerCase() !== "binance") {
    return Effect.fail(
      new MarketDataError(
        `Exchange "${exchange}" is not supported by the market-data gateway (operation: ${operation})`,
      ),
    );
  }
  return run();
}
