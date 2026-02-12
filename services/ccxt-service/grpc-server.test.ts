import { test, expect, describe } from "bun:test";
import { CcxtGrpcServer } from "./grpc-server";
import * as grpc from "@grpc/grpc-js";

// Mock Exchange Class
class MockExchange {
  id: string;
  name: string;
  countries: string[];
  urls: any;
  has: Record<string, boolean>;

  constructor(id: string) {
    this.id = id;
    this.name = id.charAt(0).toUpperCase() + id.slice(1);
    this.countries = ["US"];
    this.urls = { www: "https://example.com" };
    this.has = {
      fetchTicker: true,
      fetchFundingRates: true,
      fetchOrderBook: true,
      fetchOHLCV: true,
      loadMarkets: true,
      fetchTrades: true,
    };
  }

  async fetchTicker(symbol: string) {
    if (symbol === "INVALID/USDT") {
      throw new Error("Symbol not found");
    }
    return {
      symbol,
      last: 50000,
      high: 51000,
      low: 49000,
      bid: 49900,
      ask: 50100,
      baseVolume: 100,
      quoteVolume: 5000000,
    };
  }

  async fetchFundingRates(symbols?: string[]) {
    return {
      "BTC/USDT": {
        symbol: "BTC/USDT",
        fundingRate: 0.0001,
        timestamp: 1600000000000,
        markPrice: 50000,
        indexPrice: 50005,
      },
      "ETH/USDT": {
        symbol: "ETH/USDT",
        fundingRate: 0.0002,
        timestamp: 1600000000000,
      },
    };
  }

  async loadMarkets() {
    return {
      "BTC/USDT": {},
      "ETH/USDT": {},
    };
  }
}

describe("CcxtGrpcServer", () => {
  const mockExchanges = {
    binance: new MockExchange("binance"),
  };

  const server = new CcxtGrpcServer(mockExchanges);

  test("getTicker returns ticker data for valid symbol", (done) => {
    const call: any = {
      request: {
        exchange: "binance",
        symbol: "BTC/USDT",
      },
    };

    server.getTicker(call, (err, response) => {
      expect(err).toBeNull();
      expect(response).toBeDefined();
      expect(response!.exchange).toBe("binance");
      expect(response!.symbol).toBe("BTC/USDT");
      expect(response!.ticker).toBeDefined();
      expect(response!.ticker!.last).toBe("50000");
      expect(response!.error).toBe("");
      done();
    });
  });

  test("getTicker returns error for unsupported exchange", (done) => {
    const call: any = {
      request: {
        exchange: "unknown",
        symbol: "BTC/USDT",
      },
    };

    server.getTicker(call, (err: any, response: any) => {
      // The implementation calls callback with (error_obj, null) for unsupported exchange
      expect(err).not.toBeNull();
      expect(err.code).toBe(grpc.status.INVALID_ARGUMENT);
      done();
    });
  });

  test("getTicker returns error in response for invalid symbol", (done) => {
    const call: any = {
      request: {
        exchange: "binance",
        symbol: "INVALID/USDT",
      },
    };

    server.getTicker(call, (err, response) => {
      expect(err).toBeNull();
      expect(response).toBeDefined();
      expect(response!.ticker).toBeUndefined();
      expect(response!.error).toContain("Symbol not found");
      done();
    });
  });

  test("getFundingRates returns rates", (done) => {
    const call: any = {
      request: {
        exchange: "binance",
        symbols: [],
      },
    };

    server.getFundingRates(call, (err, response) => {
      expect(err).toBeNull();
      expect(response).toBeDefined();
      expect(response!.rates.length).toBeGreaterThan(0);
      expect(response!.rates[0].symbol).toBe("BTC/USDT");
      expect(response!.rates[0].fundingRate).toBe("0.0001");
      done();
    });
  });

  test("getMarkets returns symbols", (done) => {
    const call: any = {
      request: {
        exchange: "binance",
      },
    };

    server.getMarkets(call, (err, response) => {
      expect(err).toBeNull();
      expect(response).toBeDefined();
      expect(response!.count).toBe(2);
      expect(response!.symbols).toContain("BTC/USDT");
      done();
    });
  });

  test("healthCheck returns serving status", (done) => {
    const call: any = {};
    server.healthCheck(call, (err, response) => {
      expect(err).toBeNull();
      expect(response!.status).toBe("serving");
      expect(response!.service).toBe("ccxt-service");
      done();
    });
  });

  test("getExchanges returns exchange list", (done) => {
    const call: any = {};
    server.getExchanges(call, (err, response) => {
      expect(err).toBeNull();
      expect(response!.exchanges.length).toBe(1);
      expect(response!.exchanges[0].id).toBe("binance");
      done();
    });
  });
});
