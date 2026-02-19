import * as grpc from "@grpc/grpc-js";
import { Effect, Exit, Cause } from "effect";
import {
  CcxtServiceService,
  CcxtServiceServer,
  GetTickerRequest,
  GetTickerResponse,
  GetMarketsRequest,
  GetMarketsResponse,
  GetFundingRatesRequest,
  GetFundingRatesResponse,
  GetOrderBookRequest,
  GetOrderBookResponse,
  GetOHLCVRequest,
  GetOHLCVResponse,
  GetExchangesRequest,
  GetExchangesResponse,
  HealthCheckRequest,
  HealthCheckResponse,
  GetTickersRequest,
  GetTickersResponse,
  GetTradesRequest,
  GetTradesResponse,
} from "./proto/ccxt_service";

// Helper function to safely convert a number to string for financial precision
function toFinancialString(value: number | undefined | null): string {
  if (value === undefined || value === null || isNaN(value)) {
    return "0";
  }
  return value.toString();
}

export class CcxtGrpcServer {
  private exchanges: any;

  constructor(exchanges: any) {
    this.exchanges = exchanges;
  }

  getTicker = (
    call: grpc.ServerUnaryCall<GetTickerRequest, GetTickerResponse>,
    callback: grpc.sendUnaryData<GetTickerResponse>,
  ): void => {
    const { exchange, symbol } = call.request;
    const self = this;

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      if (!self.exchanges[exchange]) {
        yield* _(Effect.fail(new Error("Exchange not supported")));
      }

      const ticker = (yield* _(
        Effect.tryPromise({
          try: () => self.exchanges[exchange].fetchTicker(symbol),
          catch: (error) =>
            new Error(error instanceof Error ? error.message : String(error)),
        }),
      )) as any;

      return {
        exchange,
        symbol,
        ticker: {
          last: toFinancialString(ticker.last),
          high: toFinancialString(ticker.high),
          low: toFinancialString(ticker.low),
          bid: toFinancialString(ticker.bid),
          ask: toFinancialString(ticker.ask),
          baseVolume: toFinancialString(ticker.baseVolume),
          quoteVolume: toFinancialString(ticker.quoteVolume),
        },
        timestamp: new Date().getTime(),
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          const cause = Exit.causeOption(exit);
          if (cause._tag === "Some") {
            const error = Cause.squash(cause.value);
            const msg = error instanceof Error ? error.message : String(error);

            if (msg === "Exchange not supported") {
              callback(
                {
                  code: grpc.status.INVALID_ARGUMENT,
                  details: "Exchange not supported",
                },
                null,
              );
              return;
            }
            callback(null, {
              exchange,
              symbol,
              ticker: undefined,
              timestamp: Date.now(),
              error: msg,
            });
            return;
          }

          callback(null, {
            exchange,
            symbol,
            ticker: undefined,
            timestamp: 0,
            error: "Unknown error",
          });
        }
      },
    });
  };

  getTickers = (
    call: grpc.ServerUnaryCall<GetTickersRequest, GetTickersResponse>,
    callback: grpc.sendUnaryData<GetTickersResponse>,
  ): void => {
    const { exchanges, symbols } = call.request;
    const self = this;
    const exchangesToQuery =
      exchanges.length > 0 ? exchanges : Object.keys(self.exchanges);

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const results = yield* _(
        Effect.forEach(
          exchangesToQuery,
          (exchange) => {
            if (!self.exchanges[exchange]) return Effect.succeed([]);

            return Effect.forEach(
              symbols,
              (symbol) =>
                Effect.tryPromise({
                  try: () => self.exchanges[exchange].fetchTicker(symbol),
                  catch: (err: any) =>
                    new Error(err.message || "Unknown error"),
                }).pipe(
                  Effect.map((ticker: any) => ({
                    exchange,
                    symbol,
                    ticker: {
                      last: toFinancialString(ticker.last),
                      high: toFinancialString(ticker.high),
                      low: toFinancialString(ticker.low),
                      bid: toFinancialString(ticker.bid),
                      ask: toFinancialString(ticker.ask),
                      baseVolume: toFinancialString(ticker.baseVolume),
                      quoteVolume: toFinancialString(ticker.quoteVolume),
                    },
                    timestamp: Date.now(),
                    error: "",
                  })),
                  Effect.catchAll((err) =>
                    Effect.succeed({
                      exchange,
                      symbol,
                      ticker: undefined,
                      timestamp: Date.now(),
                      error: err.message,
                    }),
                  ),
                ),
              { concurrency: 10 }, // Limit concurrent fetches per exchange
            );
          },
          { concurrency: 5 }, // Limit exchanges fetched in parallel
        ),
      );

      const flatResults = results.flat();

      return { tickers: flatResults, error: "" };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, { tickers: [], error: "Internal failure" });
        }
      },
    });
  };

  getTrades = (
    call: grpc.ServerUnaryCall<GetTradesRequest, GetTradesResponse>,
    callback: grpc.sendUnaryData<GetTradesResponse>,
  ): void => {
    const { exchange, symbol, limit } = call.request;
    const self = this;

    if (!self.exchanges[exchange]) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Exchange not supported",
        },
        null,
      );
      return;
    }

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const trades = (yield* _(
        Effect.tryPromise(() =>
          self.exchanges[exchange].fetchTrades(symbol, undefined, limit || 20),
        ),
      )) as any[];

      return {
        exchange,
        symbol,
        trades: trades.map((t: any) => ({
          id: t.id || "",
          timestamp: t.timestamp || Date.now(),
          symbol: t.symbol || symbol,
          side: t.side || "",
          amount: toFinancialString(t.amount),
          price: toFinancialString(t.price),
          cost: toFinancialString(t.cost),
        })),
        timestamp: Date.now(),
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, {
            exchange,
            symbol,
            trades: [],
            timestamp: Date.now(),
            error: "Error fetching trades",
          });
        }
      },
    });
  };

  getMarkets = (
    call: grpc.ServerUnaryCall<GetMarketsRequest, GetMarketsResponse>,
    callback: grpc.sendUnaryData<GetMarketsResponse>,
  ): void => {
    const { exchange } = call.request;
    const self = this;

    if (!self.exchanges[exchange]) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Exchange not supported",
        },
        null,
      );
      return;
    }

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const markets = (yield* _(
        Effect.tryPromise(() => self.exchanges[exchange].loadMarkets()),
      )) as any;
      const symbols = Object.keys(markets);
      return {
        exchange,
        symbols,
        count: symbols.length,
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, {
            exchange,
            symbols: [],
            count: 0,
            error: "Error fetching markets",
          });
        }
      },
    });
  };

  getFundingRates = (
    call: grpc.ServerUnaryCall<GetFundingRatesRequest, GetFundingRatesResponse>,
    callback: grpc.sendUnaryData<GetFundingRatesResponse>,
  ): void => {
    const { exchange, symbols } = call.request;
    const self = this;

    if (!self.exchanges[exchange]) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Exchange not supported",
        },
        null,
      );
      return;
    }

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const ex = self.exchanges[exchange];
      let rates: any[] = [];

      if (symbols && symbols.length > 0) {
        if (ex.has["fetchFundingRates"]) {
          const r = (yield* _(
            Effect.tryPromise(() => ex.fetchFundingRates(symbols)),
          )) as any;
          rates = Object.values(r);
        } else if (ex.has["fetchFundingRate"]) {
          rates = (yield* _(
            Effect.forEach(
              symbols,
              (s) => Effect.tryPromise(() => ex.fetchFundingRate(s)),
              { concurrency: 10 }, // Limit concurrent requests
            ),
          )) as any[];
        }
      } else {
        if (ex.has["fetchFundingRates"]) {
          const r = (yield* _(
            Effect.tryPromise(() => ex.fetchFundingRates()),
          )) as any;
          rates = Object.values(r);
        }
      }

      const mappedRates = rates.map((r: any) => ({
        symbol: r.symbol,
        fundingRate: toFinancialString(r.fundingRate),
        timestamp: r.timestamp || Date.now(),
        nextFundingTime: r.nextFundingDatetime
          ? new Date(r.nextFundingDatetime).getTime()
          : 0,
        markPrice: toFinancialString(r.markPrice),
        indexPrice: toFinancialString(r.indexPrice),
      }));

      return {
        exchange,
        rates: mappedRates,
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, {
            exchange,
            rates: [],
            error: "Error fetching funding rates",
          });
        }
      },
    });
  };

  getOrderBook = (
    call: grpc.ServerUnaryCall<GetOrderBookRequest, GetOrderBookResponse>,
    callback: grpc.sendUnaryData<GetOrderBookResponse>,
  ): void => {
    const { exchange, symbol, limit } = call.request;
    const self = this;
    if (!self.exchanges[exchange]) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Exchange not supported",
        },
        null,
      );
      return;
    }

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const orderbook = (yield* _(
        Effect.tryPromise(() =>
          self.exchanges[exchange].fetchOrderBook(symbol, limit || 20),
        ),
      )) as any;
      return {
        exchange,
        symbol,
        orderbook: {
          bids: orderbook.bids.map((b: any) => ({
            price: toFinancialString(b[0]),
            amount: toFinancialString(b[1]),
          })),
          asks: orderbook.asks.map((a: any) => ({
            price: toFinancialString(a[0]),
            amount: toFinancialString(a[1]),
          })),
          timestamp: orderbook.timestamp || Date.now(),
          nonce: orderbook.nonce || 0,
        },
        timestamp: Date.now(),
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, {
            exchange,
            symbol,
            orderbook: undefined,
            timestamp: Date.now(),
            error: "Error fetching orderbook",
          });
        }
      },
    });
  };

  getOHLCV = (
    call: grpc.ServerUnaryCall<GetOHLCVRequest, GetOHLCVResponse>,
    callback: grpc.sendUnaryData<GetOHLCVResponse>,
  ): void => {
    const { exchange, symbol, timeframe, limit } = call.request;
    const self = this;
    if (!self.exchanges[exchange]) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Exchange not supported",
        },
        null,
      );
      return;
    }

    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const ohlcv = (yield* _(
        Effect.tryPromise(() =>
          self.exchanges[exchange].fetchOHLCV(
            symbol,
            timeframe || "1h",
            undefined,
            limit || 100,
          ),
        ),
      )) as any[];
      return {
        exchange,
        symbol,
        timeframe: timeframe || "1h",
        candles: ohlcv.map((c: any) => ({
          timestamp: c[0],
          open: toFinancialString(c[1]),
          high: toFinancialString(c[2]),
          low: toFinancialString(c[3]),
          close: toFinancialString(c[4]),
          volume: toFinancialString(c[5]),
        })),
        timestamp: Date.now(),
        error: "",
      };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, {
            exchange,
            symbol,
            timeframe: timeframe || "1h",
            candles: [],
            timestamp: Date.now(),
            error: "Error fetching OHLCV",
          });
        }
      },
    });
  };

  getExchanges = (
    call: grpc.ServerUnaryCall<GetExchangesRequest, GetExchangesResponse>,
    callback: grpc.sendUnaryData<GetExchangesResponse>,
  ): void => {
    const self = this;
    // eslint-disable-next-line require-yield
    const program = Effect.gen(function* (_) {
      const exchangeList = Object.keys(self.exchanges).map((id) => ({
        id,
        name: self.exchanges[id].name || id,
        countries: (self.exchanges[id].countries || []).map((country: any) =>
          String(country),
        ),
      }));
      return { exchanges: exchangeList, error: "" };
    });

    Effect.runCallback(program, {
      onExit: (exit) => {
        if (Exit.isSuccess(exit)) {
          callback(null, exit.value);
        } else {
          callback(null, { exchanges: [], error: "Error fetching exchanges" });
        }
      },
    });
  };

  healthCheck = (
    call: grpc.ServerUnaryCall<HealthCheckRequest, HealthCheckResponse>,
    callback: grpc.sendUnaryData<HealthCheckResponse>,
  ): void => {
    callback(null, {
      status: "serving",
      version: "1.0.0",
      service: "ccxt-service",
    });
  };
}

export function startGrpcServer(
  exchanges: any,
  port: number,
  host: string = "127.0.0.1",
) {
  const server = new grpc.Server();
  const service = new CcxtGrpcServer(exchanges);
  server.addService(
    CcxtServiceService,
    service as unknown as CcxtServiceServer,
  );
  const bindAddr = `${host}:${port}`;
  server.bindAsync(
    bindAddr,
    grpc.ServerCredentials.createInsecure(),
    (err, assignedPort) => {
      if (err) {
        console.error(`Failed to bind gRPC server: ${err}`);
        throw err; // Propagate error to caller
      }
      console.log(`🚀 CCXT gRPC Service listening on ${bindAddr}`);
    },
  );
  return server;
}
