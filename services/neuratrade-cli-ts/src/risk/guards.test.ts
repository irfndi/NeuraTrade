import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import {
  defaultRiskLimits,
  makeRiskGuard,
  type RiskContext,
} from "./guards.js";

function baseContext(overrides: Partial<RiskContext> = {}): RiskContext {
  return {
    isLive: false,
    capital: 10_000,
    peakCapital: 10_000,
    startOfDayCapital: 10_000,
    dailyRealizedPnl: 0,
    tradesTodayCount: 0,
    positionValue: 1_000,
    symbol: "BTC/USDT",
    side: "buy",
    ...overrides,
  };
}

describe("makeRiskGuard", () => {
  it("allows paper trades with permissive defaults", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(false));
    const result = await Effect.runPromise(
      guard.check(baseContext({ positionValue: 10_000 })),
    );
    expect(result).toBeUndefined();
  });

  it("blocks live trades when live trading is disabled", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(false));
    const error = await Effect.runPromise(
      guard.check(baseContext({ isLive: true })).pipe(Effect.flip),
    );
    expect(
      error.violations.some((v) => v.includes("live trading is disabled")),
    ).toBe(true);
  });

  it("blocks when capital is below the minimum", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard.check(baseContext({ isLive: true, capital: 50 })).pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("capital"))).toBe(true);
  });

  it("blocks when drawdown exceeds the max", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(
          baseContext({ isLive: true, capital: 9_400, peakCapital: 10_000 }),
        )
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("drawdown"))).toBe(true);
  });

  it("blocks when daily loss exceeds the max", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            dailyRealizedPnl: -300,
            startOfDayCapital: 10_000,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("daily loss"))).toBe(true);
  });

  it("blocks when position size exceeds the max", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(baseContext({ isLive: true, positionValue: 2_000 }))
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("position size"))).toBe(
      true,
    );
  });

  it("blocks when daily trade limit is reached", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(baseContext({ isLive: true, tradesTodayCount: 10 }))
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("trades today"))).toBe(true);
  });

  it("blocks symbols not in the allowed list", async () => {
    const guard = makeRiskGuard({
      ...defaultRiskLimits(true),
      allowedSymbols: ["ETH/USDT"],
    });
    const error = await Effect.runPromise(
      guard.check(baseContext({ isLive: true })).pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("allowed list"))).toBe(true);
  });

  it("aggregates multiple violations", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            capital: 50,
            positionValue: 5_000,
            tradesTodayCount: 15,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(error.violations.length).toBeGreaterThanOrEqual(3);
  });

  it("blocks leverage above the maximum", async () => {
    const guard = makeRiskGuard({
      ...defaultRiskLimits(true),
      maxLeverage: 5,
    });
    const error = await Effect.runPromise(
      guard
        .check(baseContext({ isLive: true, leverage: 10 }))
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("leverage"))).toBe(true);
  });

  it("blocks disallowed futures product types", async () => {
    const guard = makeRiskGuard({
      ...defaultRiskLimits(true),
      allowedProductTypes: ["USDT-FUTURES"],
    });
    const error = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            productType: "COIN-FUTURES",
          }),
        )
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("product type"))).toBe(true);
  });

  it("blocks when the minimum orderable position exceeds the cap even at leverage", async () => {
    // Fail-closed at the guard level: an unorderable minimum (e.g. 0.0001 BTC
    // = $6.48 at $64,795) whose margin cannot fit the 10% live cap should be
    // blocked locally (RISK BLOCKED) instead of reaching the exchange.
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            minOrderableNotional: 15_000,
            leverage: 10,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(error.violations.some((v) => v.includes("minimum orderable position"))).toBe(true);
  });

  it("allows a minimum orderable position that fits the cap at leverage", async () => {
    // 5,000 USDT floor at 10x = 500 USDT margin = 5% of capital <= 10% cap.
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const result = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          minOrderableNotional: 5_000,
          leverage: 10,
        }),
      ),
    );
    expect(result).toBeUndefined();
  });

  it("caps the position by margin (positionValue / leverage), not raw notional", async () => {
    // A $10k notional position on $10k capital at 10x = $1k margin = 10% of
    // capital, exactly at the 10% live cap -> allowed. Pre-leverage semantics
    // compared raw notional (100%) and would have blocked every leveraged
    // trade; the $10 challenge account relies on the margin semantics.
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const result = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          positionValue: 10_000,
          leverage: 10,
        }),
      ),
    );
    expect(result).toBeUndefined();
  });

  it("allows drawdown exactly at the max and blocks just over", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const atLimit = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          capital: 9_500,
          peakCapital: 10_000,
          positionValue: 900,
        }),
      ),
    );
    expect(atLimit).toBeUndefined();
    const overLimit = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            productType: "USDT-FUTURES",
            capital: 9_499.99,
            peakCapital: 10_000,
            positionValue: 900,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(
      overLimit.violations.some((v) => v.includes("drawdown")),
    ).toBe(true);
  });

  it("allows daily loss exactly at the max and blocks just over", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const atLimit = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          dailyRealizedPnl: -200,
          startOfDayCapital: 10_000,
        }),
      ),
    );
    expect(atLimit).toBeUndefined();
    const overLimit = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            productType: "USDT-FUTURES",
            dailyRealizedPnl: -200.01,
            startOfDayCapital: 10_000,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(
      overLimit.violations.some((v) => v.includes("daily loss")),
    ).toBe(true);
  });

  it("allows position size exactly at the cap and blocks just over", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const atLimit = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          positionValue: 1_000,
        }),
      ),
    );
    expect(atLimit).toBeUndefined();
    const overLimit = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            productType: "USDT-FUTURES",
            positionValue: 1_000.01,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(
      overLimit.violations.some((v) => v.includes("position size")),
    ).toBe(true);
  });

  it("allows a minimum orderable floor exactly at the cap and blocks just over", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const atLimit = await Effect.runPromise(
      guard.check(
        baseContext({
          isLive: true,
          productType: "USDT-FUTURES",
          minOrderableNotional: 1_000,
        }),
      ),
    );
    expect(atLimit).toBeUndefined();
    const overLimit = await Effect.runPromise(
      guard
        .check(
          baseContext({
            isLive: true,
            productType: "USDT-FUTURES",
            minOrderableNotional: 1_000.01,
          }),
        )
        .pipe(Effect.flip),
    );
    expect(
      overLimit.violations.some((v) => v.includes("minimum orderable position")),
    ).toBe(true);
  });

  it("blocks an unknown product type when an allowlist is configured", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    const error = await Effect.runPromise(
      guard
        .check(baseContext({ isLive: true })) // productType omitted
        .pipe(Effect.flip),
    );
    expect(
      error.violations.some((v) => v.includes("product type unknown")),
    ).toBe(true);
  });

  it("treats non-positive start-of-day capital as a violation (fail closed)", async () => {
    const guard = makeRiskGuard(defaultRiskLimits(true));
    for (const startOfDayCapital of [0, -100]) {
      const error = await Effect.runPromise(
        guard
          .check(
            baseContext({
              isLive: true,
              productType: "USDT-FUTURES",
              startOfDayCapital,
            }),
          )
          .pipe(Effect.flip),
      );
      expect(
        error.violations.some((v) => v.includes("start-of-day capital")),
      ).toBe(true);
    }
  });
});
