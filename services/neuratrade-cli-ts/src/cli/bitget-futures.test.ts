import { describe, expect, it } from "bun:test";
import { Cause, Effect } from "effect";
import { handleErr } from "./bitget-futures.js";

async function runFailingEffect(
  effect: Effect.Effect<never, Error>,
): Promise<Error> {
  const exit = await Effect.runPromiseExit(
    effect as Effect.Effect<never, unknown>,
  );
  if (exit._tag === "Failure") {
    return Cause.squash(exit.cause) as Error;
  }
  throw new Error("Expected failure but got success");
}

describe("handleErr", () => {
  it("preserves standard Error message (not just '{}')", async () => {
    const err = await runFailingEffect(
      handleErr(new Error("connection refused")),
    );
    expect(err.message).toContain("connection refused");
    expect(err.message).not.toBe("Error: {}");
  });

  it("formats _tag-bearing objects with tag + serialized body", async () => {
    const err = await runFailingEffect(
      handleErr({ _tag: "BitgetApiError", status: 429, body: "rate limit" }),
    );
    expect(err.message).toContain("BitgetApiError");
    expect(err.message).toContain("rate limit");
  });

  it("converts plain strings to error message", async () => {
    const err = await runFailingEffect(handleErr("plain string error"));
    expect(err.message).toContain("plain string error");
  });
});
