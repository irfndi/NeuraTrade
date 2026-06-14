import { describe, expect, it } from "bun:test";
import balancesFixture from "../../tests/fixtures/bitget/balances.json";
import tickerFixture from "../../tests/fixtures/bitget/ticker.json";
import orderFixture from "../../tests/fixtures/bitget/order.json";
import futuresContractsFixture from "../../tests/fixtures/bitget/futures-contracts.json";

describe("Bitget fixtures", () => {
  it("balances fixture has expected shape", () => {
    expect(balancesFixture.code).toBe("00000");
    expect(Array.isArray(balancesFixture.data)).toBe(true);
    expect(balancesFixture.data[0].coin).toBe("BTC");
    expect(balancesFixture.data[0].available).toBe("0.5");
  });

  it("ticker fixture has expected shape", () => {
    expect(tickerFixture.code).toBe("00000");
    expect(tickerFixture.data.symbol).toBe("BTCUSDT");
    expect(tickerFixture.data.lastPr).toBe("65000.00");
  });

  it("order fixture has expected shape", () => {
    expect(orderFixture.code).toBe("00000");
    expect(orderFixture.data.orderId).toBe("12345");
    expect(orderFixture.data.side).toBe("buy");
  });

  it("futures contracts fixture has expected shape", () => {
    expect(futuresContractsFixture.code).toBe("00000");
    expect(Array.isArray(futuresContractsFixture.data)).toBe(true);
    expect(futuresContractsFixture.data[0].symbol).toBe("BTCUSDT");
    expect(futuresContractsFixture.data[0].maxLeverage).toBe("125");
  });
});
