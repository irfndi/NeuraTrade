import { describe, expect, it } from "bun:test";
import { Cause, Effect, Layer } from "effect";
import * as crypto from "crypto";
import {
  BitgetApiError,
  BitgetAuthError,
  BitgetClient,
  BitgetClientLive,
  BitgetNetworkError,
  BitgetRateLimitError,
  validateCredentials,
} from "./bitget-client.ts";
import { RateLimiterLive } from "./rate-limiter.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function startMock(handler: (req: Request) => Response | Promise<Response>): {
  stop: () => void;
  url: string;
} {
  const server = Bun.serve({ port: 0, fetch: handler });
  return { stop: () => server.stop(), url: `http://localhost:${server.port}` };
}

function json(body: unknown, status = 200): Response {
  return Response.json(body, { status });
}

const testCreds = {
  apiKey: "test-key",
  apiSecret: "test-secret",
  passphrase: "test-pass",
};

function provideClient(baseUrl: string) {
  return Layer.provide(
    BitgetClientLive({ credentials: testCreds, baseUrl }),
    RateLimiterLive({ perSecond: 1000, perMinute: 60_000 }),
  );
}

function provideDemoClient(baseUrl: string) {
  return Layer.provide(
    BitgetClientLive({ credentials: testCreds, baseUrl, isDemo: true }),
    RateLimiterLive({ perSecond: 1000, perMinute: 60_000 }),
  );
}

async function runOk<A>(
  program: Effect.Effect<A, unknown, BitgetClient>,
  baseUrl: string,
): Promise<A> {
  return Effect.runPromise(
    program.pipe(Effect.provide(provideClient(baseUrl))) as Effect.Effect<
      A,
      never
    >,
  );
}

async function runFail<A>(
  program: Effect.Effect<A, unknown, BitgetClient>,
  baseUrl: string,
): Promise<unknown> {
  const exit = await Effect.runPromiseExit(
    program.pipe(Effect.provide(provideClient(baseUrl))) as Effect.Effect<
      A,
      unknown
    >,
  );
  if (exit._tag === "Failure") {
    return Cause.squash(exit.cause);
  }
  throw new Error("Expected failure but got success");
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("BitgetClient", () => {
  describe("validateCredentials", () => {
    it("returns valid credentials when complete", async () => {
      const result = await Effect.runPromise(validateCredentials(testCreds));
      expect(result.apiKey).toBe("test-key");
    });

    it("fails when any field is missing", async () => {
      const exit = await Effect.runPromiseExit(
        validateCredentials({ apiKey: "k", apiSecret: "s" }),
      );
      expect(exit._tag).toBe("Failure");
      if (exit._tag !== "Failure") {
        throw new Error("Expected failure exit");
      }
      const err = Cause.squash(exit.cause) as BitgetAuthError;
      expect(err).toBeInstanceOf(BitgetAuthError);
    });
  });

  describe("getBalances", () => {
    it("returns normalized balances", async () => {
      const mock = startMock((req) => {
        expect(req.headers.get("ACCESS-KEY")).toBe("test-key");
        expect(req.headers.get("ACCESS-PASSPHRASE")).toBe("test-pass");
        expect(req.headers.get("ACCESS-SIGN")).toBeTruthy();
        expect(req.headers.get("ACCESS-TIMESTAMP")).toBeTruthy();
        return json({
          code: "00000",
          data: [
            { coin: "BTC", available: "0.5", frozen: "0.1" },
            { coin: "USDT", available: "1000", frozen: "0" },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getBalances();
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(2);
        expect(result[0].asset).toBe("BTC");
        expect(result[0].available).toBe("0.5");
        expect(result[0].frozen).toBe("0.1");
      } finally {
        mock.stop();
      }
    });

    it("sends PAPTRADING header in demo mode", async () => {
      const mock = startMock((req) => {
        expect(req.headers.get("PAPTRADING")).toBe("1");
        return json({ code: "00000", data: [] });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getBalances();
        });
        await Effect.runPromise(
          program.pipe(
            Effect.provide(provideDemoClient(mock.url)),
          ) as Effect.Effect<unknown[], never>,
        );
      } finally {
        mock.stop();
      }
    });
  });

  describe("getTicker", () => {
    it("returns normalized ticker", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/spot/market/tickers");
        expect(url.searchParams.get("symbol")).toBe("BTCUSDT");
        return json({
          code: "00000",
          data: [
            {
              symbol: "BTCUSDT",
              lastPr: "65000.00",
              bidPr: "64999.50",
              askPr: "65000.50",
              bidSz: "0.5",
              askSz: "0.3",
              baseVolume: "12000",
            },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getTicker("BTC/USDT");
        });
        const result = await runOk(program, mock.url);
        expect(result.symbol).toBe("BTCUSDT");
        expect(result.lastPrice).toBe("65000.00");
        expect(result.bidPrice).toBe("64999.50");
        expect(result.askPrice).toBe("65000.50");
      } finally {
        mock.stop();
      }
    });
  });

  describe("placeOrder", () => {
    it("places a market order with signed body", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/spot/trade/place-order");
        const body = await req.json();
        expect(body.symbol).toBe("BTCUSDT");
        expect(body.side).toBe("buy");
        expect(body.orderType).toBe("market");
        expect(body.size).toBe("0.001");
        expect(body.force).toBeUndefined();
        expect(req.headers.get("ACCESS-KEY")).toBe("test-key");
        return json({
          code: "00000",
          data: {
            orderId: "12345",
            clientOid: body.clientOid ?? "",
            symbol: "BTCUSDT",
            side: "buy",
            orderType: "market",
            status: "live",
            size: "0.001",
            price: "0",
          },
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.placeOrder({
            symbol: "BTC/USDT",
            side: "buy",
            orderType: "market",
            size: "0.001",
            clientOid: "client-oid-1",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result.orderId).toBe("12345");
        expect(result.clientOid).toBe("client-oid-1");
      } finally {
        mock.stop();
      }
    });

    it("includes force=gtc for limit orders", async () => {
      const mock = startMock(async (req) => {
        const body = await req.json();
        expect(body.orderType).toBe("limit");
        expect(body.force).toBe("gtc");
        expect(body.price).toBe("65000");
        return json({
          code: "00000",
          data: {
            orderId: "12346",
            clientOid: body.clientOid ?? "",
            symbol: "BTCUSDT",
            side: "buy",
            orderType: "limit",
            status: "live",
            size: "0.001",
            price: "65000",
          },
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.placeOrder({
            symbol: "BTC/USDT",
            side: "buy",
            orderType: "limit",
            size: "0.001",
            price: "65000",
            clientOid: "client-oid-2",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result.orderId).toBe("12346");
      } finally {
        mock.stop();
      }
    });
  });

  describe("getOrder", () => {
    it("queries order by id", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/spot/trade/orderInfo");
        expect(url.searchParams.get("symbol")).toBe("BTCUSDT");
        expect(url.searchParams.get("orderId")).toBe("12345");
        return json({
          code: "00000",
          data: {
            orderId: "12345",
            symbol: "BTCUSDT",
            side: "buy",
            orderType: "market",
            status: "filled",
            size: "0.001",
            price: "65000",
            accBaseVolume: "0.001",
            accQuoteVolume: "65",
          },
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getOrder({
            symbol: "BTC/USDT",
            orderId: "12345",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result.status).toBe("filled");
        expect(result.filledSize).toBe("0.001");
      } finally {
        mock.stop();
      }
    });
  });

  describe("cancelOrder", () => {
    it("cancels order by clientOid", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const body = await req.json();
        expect(body.symbol).toBe("BTCUSDT");
        expect(body.clientOid).toBe("client-oid-1");
        return json({ code: "00000", data: "success" });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.cancelOrder({
            symbol: "BTC/USDT",
            clientOid: "client-oid-1",
          });
        });
        await runOk(program, mock.url);
      } finally {
        mock.stop();
      }
    });
  });

  describe("futures", () => {
    it("lists contracts", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/market/contracts");
        expect(url.searchParams.get("productType")).toBe("USDT-FUTURES");
        return json({
          code: "00000",
          data: [
            {
              symbol: "BTCUSDT",
              baseCoin: "BTC",
              quoteCoin: "USDT",
              productType: "USDT-FUTURES",
              status: "online",
              pricePrecision: "2",
              quantityPrecision: "3",
              minTradeAmount: "5",
              maxLeverage: "125",
              minLeverage: "1",
            },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getContracts("USDT-FUTURES");
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(1);
        expect(result[0].symbol).toBe("BTCUSDT");
        expect(result[0].maxLeverage).toBe("125");
      } finally {
        mock.stop();
      }
    });

    it("places a futures order with productType", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/order/place-order");
        const body = await req.json();
        expect(body.symbol).toBe("BTCUSDT");
        expect(body.productType).toBe("USDT-FUTURES");
        expect(body.marginCoin).toBe("USDT");
        expect(body.side).toBe("buy");
        expect(body.orderType).toBe("market");
        expect(body.size).toBe("0.01");
        expect(body.marginMode).toBe("crossed");
        return json({
          code: "00000",
          data: {
            orderId: "fut-1",
            symbol: "BTCUSDT",
            productType: "USDT-FUTURES",
            side: "buy",
            orderType: "market",
            status: "live",
            size: "0.01",
            price: "0",
            marginMode: "crossed",
          },
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.placeFuturesOrder({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            side: "buy",
            orderType: "market",
            size: "0.01",
            marginMode: "crossed",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result.orderId).toBe("fut-1");
        expect(result.marginMode).toBe("crossed");
      } finally {
        mock.stop();
      }
    });

    it("sets leverage", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/account/set-leverage");
        const body = await req.json();
        expect(body.symbol).toBe("BTCUSDT");
        expect(body.marginCoin).toBe("USDT");
        expect(body.leverage).toBe("10");
        expect(body.marginMode).toBe("isolated");
        return json({ code: "00000", data: "success" });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.setLeverage({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            marginMode: "isolated",
            leverage: "10",
          });
        });
        await runOk(program, mock.url);
      } finally {
        mock.stop();
      }
    });

    it("gets leverage info", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/account/account");
        expect(url.searchParams.get("symbol")).toBe("BTCUSDT");
        expect(url.searchParams.get("marginCoin")).toBe("USDT");
        return json({
          code: "00000",
          data: {
            marginCoin: "USDT",
            crossedMarginLeverage: 10,
            isolatedLongLever: 20,
            isolatedShortLever: 20,
          },
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getLeverage({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(2);
        expect(result[0].marginMode).toBe("crossed");
        expect(result[0].leverage).toBe("10");
        expect(result[1].marginMode).toBe("isolated");
        expect(result[1].leverage).toBe("20");
      } finally {
        mock.stop();
      }
    });

    it("sets margin mode", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/account/set-margin-mode");
        const body = await req.json();
        expect(body.symbol).toBe("BTCUSDT");
        expect(body.marginCoin).toBe("USDT");
        expect(body.marginMode).toBe("crossed");
        return json({ code: "00000", data: "success" });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.setMarginMode({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            marginMode: "crossed",
          });
        });
        await runOk(program, mock.url);
      } finally {
        mock.stop();
      }
    });

    it("sets position mode", async () => {
      const mock = startMock(async (req) => {
        expect(req.method).toBe("POST");
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/account/set-position-mode");
        const body = await req.json();
        expect(body.posMode).toBe("one_way_mode");
        return json({ code: "00000", data: "success" });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.setPositionMode({
            productType: "USDT-FUTURES",
            positionMode: "one_way",
          });
        });
        await runOk(program, mock.url);
      } finally {
        mock.stop();
      }
    });

    it("fetches futures balances", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/account/accounts");
        return json({
          code: "00000",
          data: [
            {
              marginCoin: "USDT",
              available: "1000",
              locked: "0",
              equity: "1000",
              usdtEquity: "1000",
            },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getFuturesBalances("USDT-FUTURES");
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(1);
        expect(result[0].marginCoin).toBe("USDT");
      } finally {
        mock.stop();
      }
    });

    it("fetches futures positions", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v2/mix/position/single-position");
        return json({
          code: "00000",
          data: [
            {
              positionId: "pos-1",
              symbol: "BTCUSDT",
              productType: "USDT-FUTURES",
              marginMode: "crossed",
              holdSide: "long",
              openPrice: "60000",
              total: "0.01",
              available: "0.01",
              leverage: "10",
              unrealizedPL: "50",
              liquidatedPrice: "54000",
            },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getFuturesPositions(
            "BTC/USDT:USDT",
            "USDT-FUTURES",
          );
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(1);
        expect(result[0].holdSide).toBe("long");
      } finally {
        mock.stop();
      }
    });

    it("resolves marginCoin for COIN-FUTURES positions", async () => {
      let capturedUrl = "";
      const mock = startMock((req) => {
        const url = new URL(req.url);
        capturedUrl = req.url;
        expect(url.pathname).toBe("/api/v2/mix/position/single-position");
        expect(url.searchParams.get("productType")).toBe("COIN-FUTURES");
        return json({
          code: "00000",
          data: [
            {
              positionId: "pos-coin-1",
              symbol: "BTCUSD",
              productType: "COIN-FUTURES",
              marginMode: "crossed",
              holdSide: "long",
              openPrice: "60000",
              total: "1",
              available: "1",
              leverage: "10",
              unrealizedPL: "50",
              liquidatedPrice: "54000",
            },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getFuturesPositions(
            "BTC/USD:BTC",
            "COIN-FUTURES",
          );
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(1);
        expect(result[0].holdSide).toBe("long");
        const finalUrl = new URL(capturedUrl);
        expect(finalUrl.searchParams.get("marginCoin")).toBe("BTC");
      } finally {
        mock.stop();
      }
    });

    it("queries and cancels futures order", async () => {
      const mock = startMock(async (req) => {
        const url = new URL(req.url);
        if (req.method === "GET") {
          expect(url.pathname).toBe("/api/v2/mix/order/detail");
          expect(url.searchParams.get("orderId")).toBe("fut-1");
          return json({
            code: "00000",
            data: {
              orderId: "fut-1",
              symbol: "BTCUSDT",
              productType: "USDT-FUTURES",
              side: "buy",
              orderType: "limit",
              status: "live",
              size: "0.01",
              price: "60000",
              marginMode: "crossed",
            },
          });
        }
        expect(req.method).toBe("POST");
        expect(url.pathname).toBe("/api/v2/mix/order/cancel-order");
        return json({ code: "00000", data: "success" });
      });
      try {
        const getProgram = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getFuturesOrder({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            orderId: "fut-1",
          });
        });
        const getResult = await runOk(getProgram, mock.url);
        expect(getResult.orderId).toBe("fut-1");

        const cancelProgram = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.cancelFuturesOrder({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            orderId: "fut-1",
          });
        });
        await runOk(cancelProgram, mock.url);
      } finally {
        mock.stop();
      }
    });
  });

  describe("error handling", () => {
    it("returns BitgetRateLimitError on 429", async () => {
      const mock = startMock(
        () =>
          new Response("rate limited", {
            status: 429,
            headers: { "Retry-After": "3" },
          }),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getBalances();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BitgetRateLimitError);
      } finally {
        mock.stop();
      }
    });

    it("returns BitgetApiError on non-2xx response", async () => {
      const mock = startMock(() =>
        json({ code: "40001", msg: "Invalid symbol" }, 400),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getTicker("BAD");
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BitgetApiError);
      } finally {
        mock.stop();
      }
    });

    it("returns BitgetApiError on HTTP 200 with non-success business code", async () => {
      const mock = startMock(() =>
        json({ code: "40015", msg: "Invalid API key", data: null }, 200),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getBalances();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BitgetApiError);
        expect((err as BitgetApiError).body).toContain("40015");
      } finally {
        mock.stop();
      }
    });

    it("returns BitgetNetworkError when connection is refused", async () => {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.getBalances();
      });
      const err = await runFail(program, "http://localhost:1");
      expect(err).toBeInstanceOf(BitgetNetworkError);
    });

    it("returns BitgetApiError with non-JSON body on non-2xx response", async () => {
      const mock = startMock(
        () =>
          new Response("Service Unavailable", {
            status: 503,
            headers: { "Content-Type": "text/plain" },
          }),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BitgetClient;
          return yield* client.getTicker("BTCUSDT");
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BitgetApiError);
        expect((err as BitgetApiError).body).toBe("Service Unavailable");
        expect((err as BitgetApiError).status).toBe(503);
      } finally {
        mock.stop();
      }
    });
  });
});
