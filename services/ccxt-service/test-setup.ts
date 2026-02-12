import { mock } from "bun:test";

// Ensure ADMIN_API_KEY is present and long enough before importing the app
if (!process.env.ADMIN_API_KEY || process.env.ADMIN_API_KEY.length < 32) {
  process.env.ADMIN_API_KEY =
    "test-admin-key-1234567890123456789012345678901234";
}

// Prevent tracing side-effects in tests
// mock.module("./tracing", () => ({}));

// Mock CCXT to avoid network and control exchanges
mock.module("ccxt", () => {
  class MockBinance {
    id = "binance";
    name = "Binance";
    countries = ["JP"];
    urls = { www: "https://binance.com" };
    has: Record<string, boolean> = {
      fetchFundingRates: true,
      fetchFundingRate: true,
      fetchTicker: true,
      fetchOrderBook: true,
      fetchOHLCV: true,
    };
    markets: Record<string, any> = { "BTC/USDT": {}, "ETH/USDT": {} };
    constructor(public config?: any) {}

    async fetchTicker(symbol: string) {
      // Simulate invalid symbol error using a proper Error subclass pattern
      // that mimics CCXT's BadSymbol error behavior
      if (!this.markets[symbol]) {
        class BadSymbol extends Error {
          constructor(message: string) {
            super(message);
            this.name = "BadSymbol";
          }
        }
        throw new BadSymbol(`binance does not have market symbol ${symbol}`);
      }
      return { symbol, last: 50000, baseVolume: 1000, bid: 49990, ask: 50010 };
    }

    async loadMarkets() {
      return this.markets;
    }

    async fetchOrderBook(symbol: string, limit = 20) {
      const bids = Array.from({ length: Math.min(limit, 5) }, (_, i) => [
        49990 - i,
        1 + i,
      ]);
      const asks = Array.from({ length: Math.min(limit, 5) }, (_, i) => [
        50010 + i,
        1 + i,
      ]);
      return { bids, asks, timestamp: Date.now(), nonce: undefined } as any;
    }

    async fetchOHLCV(
      symbol: string,
      timeframe = "1h",
      since?: number,
      limit = 5,
    ) {
      const now = Date.now();
      const base = since ?? now - limit * 3600_000;
      const ohlcv = Array.from({ length: limit }, (_, i) => {
        const t = base + i * 3600_000;
        const o = 50000 + i;
        const h = o + 10;
        const l = o - 10;
        const c = o + 5;
        const v = 100 + i;
        return [t, o, h, l, c, v];
      });
      return ohlcv as any;
    }

    async fetchFundingRate(symbol: string) {
      const now = Date.now();
      return {
        symbol,
        fundingRate: 0.0001,
        fundingTimestamp: now,
        nextFundingDatetime: new Date(now + 60 * 60 * 1000).toISOString(),
        markPrice: 50000,
        indexPrice: 50005,
        timestamp: now,
      };
    }

    async fetchFundingRates(symbols?: string[]) {
      const all = ["BTC/USDT", "ETH/USDT"];
      const selected = symbols && symbols.length ? symbols : all;
      const out: Record<string, any> = {};
      for (const s of selected) {
        out[s] = await this.fetchFundingRate(s);
      }
      return out;
    }
  }

  const ccxtMockDefault: any = {
    exchanges: ["binance"],
    binance: MockBinance,
  };

  return {
    default: ccxtMockDefault,
    // Provide named export to satisfy tests that do `import("ccxt")` and access `.exchanges`
    exchanges: ccxtMockDefault.exchanges,
  };
});

export {};
