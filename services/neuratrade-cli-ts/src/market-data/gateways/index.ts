import { Effect, Layer } from "effect";
import type { MarketDataGatewayService } from "../gateway.js";
import { MarketDataError, MarketDataGateway } from "../gateway.js";
import * as Binance from "./binance.js";
import * as Bitget from "./bitget.js";

/**
 * Composite gateway that routes by exchange name.
 *
 * Currently supports:
 *   - binance
 *   - bitget (spot)
 *   - bitget-futures
 *
 * Additional exchanges can be added here without changing consumers.
 */
export const MarketDataGatewayLive = Layer.succeed(MarketDataGateway, {
  fetchTick: (exchange, symbol) =>
    dispatch(exchange, "fetchTick", () => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchTick(symbol, "spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchTick(symbol, "futures");
      }
      return Binance.fetchTick(symbol);
    }),

  fetchOHLCV: (exchange, symbol, timeframe, limit, startTime) =>
    dispatch(exchange, "fetchOHLCV", () => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchOHLCV(symbol, timeframe, limit, startTime, "spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchOHLCV(
          symbol,
          timeframe,
          limit,
          startTime,
          "futures",
        );
      }
      return Binance.fetchOHLCV(symbol, timeframe, limit, startTime);
    }),

  fetchOrderBook: (exchange, symbol, limit) =>
    dispatch(exchange, "fetchOrderBook", () => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchOrderBook(symbol, limit, "spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchOrderBook(symbol, limit, "futures");
      }
      return Binance.fetchOrderBook(symbol, limit);
    }),

  fetchSymbols: (exchange) =>
    dispatch(exchange, "fetchSymbols", () => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchSymbols("spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchSymbols("futures");
      }
      return Binance.fetchSymbols();
    }),

  fetch24hrVolumes: (exchange) =>
    dispatch(exchange, "fetch24hrVolumes", () => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetch24hrVolumes("spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetch24hrVolumes("futures");
      }
      return Binance.fetch24hrVolumes();
    }),

  fetchFundingRates: (exchange, symbol, startTime, endTime, limit) =>
    dispatch(exchange, "fetchFundingRates", () => {
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchFundingRates(symbol, startTime, endTime, limit);
      }
      return Binance.fetchFundingRates(symbol, startTime, endTime, limit);
    }),
} satisfies MarketDataGatewayService);

function dispatch<A>(
  exchange: string,
  operation: string,
  run: () => Effect.Effect<A, MarketDataError, never>,
): Effect.Effect<A, MarketDataError, never> {
  const supported = ["binance", "bitget", "bitget-futures"];
  if (!supported.includes(exchange.toLowerCase())) {
    return Effect.fail(
      new MarketDataError(
        `Exchange "${exchange}" is not supported by the market-data gateway (operation: ${operation})`,
      ),
    );
  }
  return run();
}
