import { describe, expect, it } from "bun:test";
import {
  parseRealMoneyReadinessArgs,
  runRealMoneyReadiness,
} from "./real-money-readiness.js";

describe("real-money-readiness CLI contract", () => {
  it("parses the default candidate and explicit candidate overrides", () => {
    const defaults = parseRealMoneyReadinessArgs([]);
    const explicit = parseRealMoneyReadinessArgs([
      "--exchange",
      "bitget-futures",
      "--symbol",
      "BTC/USDT:USDT",
      "--timeframe",
      "15m",
    ]);

    expect(defaults).toEqual({
      kind: "ok",
      args: {
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "15m",
      },
    });
    expect(explicit).toEqual(defaults);
  });

  it("rejects unknown, missing, and test-only production arguments", () => {
    expect(parseRealMoneyReadinessArgs(["--unknown", "value"])).toEqual({
      kind: "error",
      message: "unknown option: --unknown",
    });
    expect(parseRealMoneyReadinessArgs(["--symbol"])).toEqual({
      kind: "error",
      message: "missing value for --symbol",
    });
    expect(parseRealMoneyReadinessArgs(["--parity-fixture", "golden"])).toEqual(
      {
        kind: "error",
        message: "--parity-fixture is test-runner-only",
      },
    );
  });

  it("returns ERROR/2 when the requested database is absent", () => {
    const result = runRealMoneyReadiness([], {
      home: "/tmp/neuratrade-readiness-unit-database-does-not-exist",
    });

    expect(result.exitCode).toBe(2);
    expect(result.report.status).toBe("ERROR");
    expect(result.report.errors).toHaveLength(1);
  });
});
