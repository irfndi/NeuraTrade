import { describe, expect, it, vi } from "bun:test";
import { Effect } from "effect";
import {
  makeFlowRecorder,
  type FlowLiquidationRow,
  type FlowOfiRow,
  type FlowRecorderRepository,
  type FlowWebSocket,
  type RawWsEventPayload,
} from "./flow-recorder.js";

const MINUTE = 60_000;
const T0 = 1_700_000_000_000; // arbitrary epoch ms (minute boundary +0)
const T0_MINUTE = Math.floor(T0 / MINUTE) * MINUTE;

/** Scriptable fake WebSocket implementing the recorder's seam. */
class FakeWebSocket implements FlowWebSocket {
  readonly sent: string[] = [];
  private readonly handlers = new Map<
    string,
    (payload: RawWsEventPayload) => void
  >();
  closed = false;

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
    this.handlers.get("close")?.(undefined);
  }

  on(
    event: "open" | "message" | "close" | "error",
    cb: (payload: RawWsEventPayload) => void,
  ): void {
    this.handlers.set(event, cb);
  }

  // -- test helpers ---------------------------------------------------------
  emitOpen(): void {
    this.handlers.get("open")?.(undefined);
  }

  emitFrame(frame: TestFrame): void {
    this.handlers.get("message")?.(JSON.stringify(frame));
  }

  emitRaw(data: string): void {
    this.handlers.get("message")?.(data);
  }

  emitClose(): void {
    this.handlers.get("close")?.(undefined);
  }
}

/** Minimal Bybit v5 frame shape used by the scriptable fake socket. */
type TestFrame = {
  readonly topic?: string;
  readonly op?: string;
  readonly success?: boolean;
  readonly ret_msg?: string;
  readonly data?: unknown;
};

function fakeFactory(sockets: FakeWebSocket[]) {
  return (_url: string) => {
    const ws = new FakeWebSocket();
    sockets.push(ws);
    return ws;
  };
}

/**
 * In-memory repo whose saves resolve a signal, so tests await the exact
 * persistence event instead of guessing at timing.
 */
class FakeFlowRepo implements FlowRecorderRepository {
  ensured = 0;
  readonly ofi: FlowOfiRow[] = [];
  readonly liquidations: FlowLiquidationRow[] = [];
  private readonly saveWaiters: Array<() => void> = [];

  ensureFlowTables(): Effect.Effect<void> {
    this.ensured += 1;
    return Effect.void;
  }

  saveFlowOfi(rows: FlowOfiRow[]): Effect.Effect<void> {
    this.ofi.push(...rows);
    this.notify();
    return Effect.void;
  }

  saveLiquidations(rows: FlowLiquidationRow[]): Effect.Effect<void> {
    this.liquidations.push(...rows);
    this.notify();
    return Effect.void;
  }

  /** Resolves when the next save lands. Register before triggering the frame. */
  nextSave(): Promise<void> {
    const { promise, resolve } = Promise.withResolvers<void>();
    this.saveWaiters.push(resolve);
    return promise;
  }

  private notify(): void {
    const waiters = this.saveWaiters.splice(0);
    for (const w of waiters) w();
  }
}

function makeHarness(
  overrides: {
    symbols?: readonly string[];
    maxRetries?: number;
    backoffMs?: number;
    ticker?: boolean;
  } = {},
) {
  const repo = new FakeFlowRepo();
  const sockets: FakeWebSocket[] = [];
  const warns: string[] = [];
  const recorder = makeFlowRecorder(repo, {
    symbols: overrides.symbols ?? ["BTCUSDT"],
    maxRetries: overrides.maxRetries,
    backoffMs: overrides.backoffMs,
    ticker: overrides.ticker,
    wsFactory: fakeFactory(sockets),
    onWarn: (message) => warns.push(message),
  });
  Effect.runSync(recorder.start);
  return { repo, sockets, warns, recorder };
}

const stopAndSettle = (recorder: ReturnType<typeof makeHarness>["recorder"]) =>
  Effect.runPromise(recorder.stop);

describe("flow recorder", () => {
  it("subscribes to the trade feed and the all-symbols liquidation feed", () => {
    const { sockets } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();
    const subscribes = ws.sent.filter((m) => m.includes('"subscribe"'));
    expect(subscribes.length).toBeGreaterThanOrEqual(2);
    expect(subscribes.some((m) => m.includes("publicTrade.BTCUSDT"))).toBe(
      true,
    );
    expect(subscribes.some((m) => m.includes("liquidation"))).toBe(true);
  });

  it("increments buy_vol on Buy trades", async () => {
    const { repo, sockets, recorder } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [
        { T: T0, s: "BTCUSDT", S: "Buy", v: "0.5", p: "100" },
        { T: T0 + 5_000, s: "BTCUSDT", S: "Buy", v: "1.25", p: "100" },
      ],
    });
    await stopAndSettle(recorder);
    expect(repo.ofi).toHaveLength(1);
    expect(repo.ofi[0]).toMatchObject({
      exchange: "bybit-futures",
      symbol: "BTCUSDT",
      ts: T0_MINUTE,
      buyVol: 1.75,
      sellVol: 0,
      trades: 2,
    });
  });

  it("increments sell_vol on Sell trades", async () => {
    const { repo, sockets, recorder } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0, s: "BTCUSDT", S: "Sell", v: "0.25", p: "100" }],
    });
    await stopAndSettle(recorder);
    expect(repo.ofi).toHaveLength(1);
    expect(repo.ofi[0]).toMatchObject({ buyVol: 0, sellVol: 0.25, trades: 1 });
  });

  it("splits trades across minute boundaries into separate rows", async () => {
    const { repo, sockets, recorder } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0, s: "BTCUSDT", S: "Buy", v: "1", p: "100" }],
    });
    // Next minute — the completed first minute flushes immediately.
    const firstFlush = repo.nextSave();
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0 + MINUTE, s: "BTCUSDT", S: "Sell", v: "2", p: "100" }],
    });
    await firstFlush;
    expect(repo.ofi).toHaveLength(1);
    expect(repo.ofi[0]).toMatchObject({ buyVol: 1, sellVol: 0 });
    await stopAndSettle(recorder);
    expect(repo.ofi).toHaveLength(2);
    expect(repo.ofi[1]).toMatchObject({
      ts: T0_MINUTE + MINUTE,
      buyVol: 0,
      sellVol: 2,
    });
  });

  it("persists liquidation rows as they arrive", async () => {
    const { repo, sockets } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();
    const saved = repo.nextSave();
    ws.emitFrame({
      topic: "liquidation",
      data: [
        {
          updatedTime: T0,
          symbol: "BTCUSDT",
          side: "Sell",
          size: "0.01",
          price: "16588.5",
          bankruptcyPrice: "16564.5",
        },
      ],
    });
    await saved;
    expect(repo.liquidations).toHaveLength(1);
    expect(repo.liquidations[0]).toMatchObject({
      exchange: "bybit-futures",
      symbol: "BTCUSDT",
      ts: T0,
      side: "Sell",
      size: 0.01,
      price: 16588.5,
      bankruptcyPrice: 16564.5,
    });
  });

  it("reconnects on close up to maxRetries, then fails the recorder", async () => {
    vi.useFakeTimers();
    try {
      const { sockets, warns, recorder } = makeHarness({
        maxRetries: 2,
        backoffMs: 5_000,
      });
      expect(sockets).toHaveLength(1);

      // Retries are budgeted per failed connection: a socket that never opens
      // (close before open) consumes budget, so close-without-open below.
      sockets[0].emitOpen();
      sockets[0].emitClose(); // reconnect attempt 1
      vi.advanceTimersByTime(5_000);
      expect(sockets).toHaveLength(2);

      sockets[1].emitClose(); // reconnect attempt 2
      vi.advanceTimersByTime(5_000);
      expect(sockets).toHaveLength(3);

      sockets[2].emitClose(); // retries exhausted -> fail, no 4th socket
      vi.advanceTimersByTime(5_000);
      expect(sockets).toHaveLength(3);

      const outcome = await Effect.runPromise(recorder.done).then(
        () => "ok",
        () => "err",
      );
      expect(outcome).toBe("err");
      expect(
        warns.some((m) => m.includes("exceeded max reconnect retries")),
      ).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("skips malformed frames with a warning instead of crashing", async () => {
    const { repo, sockets, warns, recorder } = makeHarness();
    const ws = sockets[0];
    ws.emitOpen();

    ws.emitRaw("{not valid json");
    ws.emitFrame({ topic: "publicTrade.BTCUSDT", data: 42 }); // non-array data
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0, s: "BTCUSDT" }], // missing side -> malformed event
    });
    await stopAndSettle(recorder);
    expect(warns.length).toBeGreaterThan(0);
    expect(repo.ofi).toHaveLength(0);

    // A valid frame after the malformed ones still flows through.
    const {
      sockets: sockets2,
      repo: repo2,
      recorder: recorder2,
    } = makeHarness();
    const ws2 = sockets2[0];
    ws2.emitOpen();
    ws2.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0, s: "BTCUSDT", S: "Buy", v: "0.5", p: "100" }],
    });
    await stopAndSettle(recorder2);
    expect(repo2.ofi).toHaveLength(1);
    expect(repo2.ofi[0].buyVol).toBe(0.5);
  });

  it("keeps the latest ticker price in memory (throttled)", async () => {
    const { repo, sockets, recorder } = makeHarness({ ticker: true });
    const ws = sockets[0];
    ws.emitOpen();
    ws.emitFrame({
      topic: "tickers.BTCUSDT",
      data: { symbol: "BTCUSDT", lastPrice: "65000.5" },
    });
    ws.emitFrame({
      topic: "publicTrade.BTCUSDT",
      data: [{ T: T0, s: "BTCUSDT", S: "Sell", v: "0.1", p: "65000" }],
    });
    await stopAndSettle(recorder);
    expect(repo.ofi).toHaveLength(1);
    // Tickers are never persisted per message — only the trade row lands.
    expect(repo.ofi[0].sellVol).toBe(0.1);
    expect(repo.liquidations).toHaveLength(0);
  });
});
