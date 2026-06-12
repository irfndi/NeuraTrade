import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { runTest } from "./exchange.js";

describe("runTest", () => {
  it("returns 1 when credentials are missing", async () => {
    const result = await Effect.runPromise(
      runTest("", "", "BTC/USDT", 0.001).pipe(
        Effect.catchAll(() => Effect.succeed(1)),
      ),
    );
    expect(result).toBe(1);
  });
});
