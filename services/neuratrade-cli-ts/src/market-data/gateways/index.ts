import { Effect, Layer } from "effect";
import type { MarketDataGatewayService } from "../gateway.js";
import { MarketDataError, MarketDataGateway } from "../gateway.js";
import * as Binance from "./binance.js";
import * as Bitget from "./bitget.js";
import * as Bybit from "./bybit.js";

/**
 * Composite gateway that routes by exchange name.
 *
 * Currently supports:
 *   - binance
 *   - bitget (spot)
 *   - bitget-futures
 *   - bybit-futures (testnet)
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
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchTick(symbol);
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
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchOHLCV(symbol, timeframe, limit, startTime);
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
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchOrderBook(symbol, limit);
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
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchSymbols();
      }
      return Binance.fetchSymbols();
    }),

  fetchDemoSymbols: (exchange) =>
    dispatch(exchange, "fetchDemoSymbols", () => {
      // Bitget futures has a simulated (PAPTRADING) environment; Bybit
      // testnet IS the demo (same tradeable list). The other gateways have
      // no demo concept and report their full list (a no-op filter for the
      // universe bound).
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchDemoSymbols();
      }
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchDemoSymbols();
      }
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchSymbols("spot");
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
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetch24hrVolumes();
      }
      return Binance.fetch24hrVolumes();
    }),

  fetchFundingRates: (exchange, symbol, startTime, endTime, limit) =>
    dispatch(exchange, "fetchFundingRates", () => {
      if (exchange.toLowerCase() === "bitget") {
        // Spot has no funding rates; never fall through to another venue.
        return Effect.fail(
          new MarketDataError("Funding rates not supported for bitget spot"),
        );
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchFundingRates(symbol, startTime, endTime, limit);
      }
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchFundingRates(symbol, startTime, endTime, limit);
      }
      return Binance.fetchFundingRates(symbol, startTime, endTime, limit);
    }),
} satisfies MarketDataGatewayService);

function dispatch<A>(
  exchange: string,
  operation: string,
  run: () => Effect.Effect<A, MarketDataError, never>,
): Effect.Effect<A, MarketDataError, never> {
  const supported = ["binance", "bitget", "bitget-futures", "bybit-futures"];
  if (!supported.includes(exchange.toLowerCase())) {
    return Effect.fail(
      new MarketDataError(
        `Exchange "${exchange}" is not supported by the market-data gateway (operation: ${operation})`,
      ),
    );
  }
  return run();
}
