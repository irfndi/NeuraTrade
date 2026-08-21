/**
 * Live Flow Ignition recorder — Bybit v5 linear public WebSocket feed.
 *
 * Subscribes to:
 *  - `publicTrade.<SYMBOL>` for each configured symbol (default: top-100 from
 *    `selectFlowUniverse`, else a liquid fallback set)
 *  - `liquidation` (all symbols) — best-effort: some endpoints reject the
 *    topic (observed on this mainnet linear endpoint), in which case the
 *    recorder warns and keeps streaming trades/tickers
 *  - `tickers.<SYMBOL>` (optional; throttled — only the latest price is kept
 *    in memory and surfaced on aggregate logs, never persisted per message)
 *
 * Trades are bucketed into 1-minute `flow_ofi_1m` rows (buy/sell volume +
 * trade count) and persisted on minute rollover and on close. Liquidations
 * are persisted as they arrive. An app-level ping keeps the connection alive
 * (Bybit drops idle sockets); on an unexpected close the socket reconnects
 * with a fixed backoff up to `maxRetries`, then the recorder fails. State is
 * drained on every fresh connection so a restart never carries stale buckets.
 *
 * Public market data only — no credentials.
 */
import { Data, Effect } from "effect";
import * as S from "effect/Schema";
import {
  fetchTickers,
  fetchInstruments,
} from "../market-data/gateways/bybit.js";
import {
  DEFAULT_FLOW_UNIVERSE_TOP_N,
  selectFlowUniverse,
} from "./flow-universe.js";

export const FLOW_EXCHANGE = "bybit-futures";

export const FLOW_WS_URL = "wss://stream.bybit.com/v5/public/linear";

/** Fallback symbol set — used until `selectFlowUniverse` exists. */
export const DEFAULT_FLOW_SYMBOLS = [
  "BTCUSDT",
  "ETHUSDT",
  "SOLUSDT",
  "XRPUSDT",
  "DOGEUSDT",
] as const;

// ---------------------------------------------------------------------------
// Repository contract (matches the AgentFlowData flow-repository contract)
// ---------------------------------------------------------------------------

export interface FlowOfiRow {
  readonly exchange: string;
  readonly symbol: string;
  /** Minute-start epoch ms (floor(ts/60000)*60000). */
  readonly ts: number;
  readonly buyVol: number;
  readonly sellVol: number;
  readonly trades: number;
}

export interface FlowLiquidationRow {
  readonly exchange: string;
  readonly symbol: string;
  readonly ts: number;
  readonly side: string;
  readonly size: number;
  readonly price: number;
  readonly bankruptcyPrice: number;
}

export interface FlowRecorderRepository {
  readonly ensureFlowTables: () => Effect.Effect<void, unknown, never>;
  readonly saveFlowOfi: (
    rows: FlowOfiRow[],
  ) => Effect.Effect<void, unknown, never>;
  readonly saveLiquidations: (
    rows: FlowLiquidationRow[],
  ) => Effect.Effect<void, unknown, never>;
}

export class FlowRecorderError extends Data.TaggedError("FlowRecorderError")<{
  readonly reason: string;
  readonly cause?: unknown;
}> {}

// ---------------------------------------------------------------------------
// WebSocket seam — Bun's native WebSocket under the hood, fake in tests
// ---------------------------------------------------------------------------

export type RawWsEventPayload =
  | string
  | ArrayBuffer
  | Uint8Array
  | Blob
  | undefined;

export interface FlowWebSocket {
  send(data: string): void;
  close(): void;
  on(
    event: "open" | "message" | "close" | "error",
    cb: (payload: RawWsEventPayload) => void,
  ): void;
}

export type FlowWebSocketFactory = (url: string) => FlowWebSocket;

function defaultWsFactory(url: string): FlowWebSocket {
  const ws = new WebSocket(url);
  return {
    send: (data) => ws.send(data),
    close: () => ws.close(),
    on: (event, cb) => {
      switch (event) {
        case "open":
          ws.onopen = () => cb(undefined);
          break;
        case "message":
          ws.onmessage = (ev) => cb(ev.data);
          break;
        case "close":
          ws.onclose = () => cb(undefined);
          break;
        case "error":
          ws.onerror = () => cb(undefined);
          break;
      }
    },
  };
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export interface FlowRecorderOpts {
  readonly symbols?: readonly string[];
  readonly ticker?: boolean;
  readonly exchange?: string;
  readonly baseUrl?: string;
  readonly wsFactory?: FlowWebSocketFactory;
  readonly pingIntervalMs?: number;
  readonly maxRetries?: number;
  readonly backoffMs?: number;
  readonly aggregateIntervalMs?: number;
  /** Called with every persisted flow_ofi_1m batch (minute rollover / close). */
  readonly onFlush?: (rows: readonly FlowOfiRow[]) => void;
  /** Called periodically with in-memory partial buckets + latest tick prices. */
  readonly onAggregate?: (
    rows: readonly FlowOfiRow[],
    prices: ReadonlyMap<string, number>,
  ) => void;
  readonly onWarn?: (message: string) => void;
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

/** Bybit v5 numeric fields arrive as either a JSON number or a decimal string. */
const NumberLike = S.Union([S.Number, S.String]);

const TradeWireSchema = S.Struct({
  s: S.String,
  S: S.Literals(["Buy", "Sell"]),
  v: NumberLike,
  T: NumberLike,
});
type TradeWire = typeof TradeWireSchema.Type;

const TickerWireSchema = S.Struct({
  symbol: S.optional(S.String),
  lastPrice: NumberLike,
});
type TickerWire = typeof TickerWireSchema.Type;

/**
 * Documented Bybit v5 liquidation shape uses symbol/side/size/price/
 * bankruptcyPrice/updatedTime; the compact s/S/v/p/bp/T form is accepted as a
 * fallback. Both are decoded into one wire contract.
 */
const LiquidationWireSchema = S.Struct({
  symbol: S.optional(S.String),
  side: S.optional(S.String),
  size: S.optional(NumberLike),
  price: S.optional(NumberLike),
  bankruptcyPrice: S.optional(NumberLike),
  updatedTime: S.optional(NumberLike),
  s: S.optional(S.String),
  S: S.optional(S.String),
  v: S.optional(NumberLike),
  p: S.optional(NumberLike),
  bp: S.optional(NumberLike),
  T: S.optional(NumberLike),
});
type LiquidationWire = typeof LiquidationWireSchema.Type;

/** Minimal Bybit v5 frame envelope; `data` stays opaque until topic dispatch. */
const FrameWireSchema = S.Struct({
  topic: S.optional(S.String),
  op: S.optional(S.String),
  success: S.optional(S.Boolean),
  ret_msg: S.optional(S.String),
  data: S.optional(S.Unknown),
});

function toWireSymbol(symbol: string): string {
  return symbol.replace("/", "").split(":")[0].toUpperCase();
}

/** Coerce a Bybit numeric wire value (number or decimal string) to a number. */
function asNum(value: number | string | undefined): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : Number.NaN;
}

/** Volumes are coin amounts; 4 dp kills float noise without losing significance. */
function round4(n: number): number {
  return Math.round(n * 10_000) / 10_000;
}

function chunk<T>(items: readonly T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}

function describeError<T>(err: T): string {
  if (err instanceof Error) return err.message;
  if (err === null || err === undefined) return String(err);
  const e = err as {
    readonly message?: unknown;
    readonly reason?: unknown;
    readonly _tag?: unknown;
    readonly cause?: unknown;
  };
  if (e.reason != null && String(e.reason).length > 0) {
    return String(e.reason);
  }
  if (e.message != null && String(e.message).length > 0) {
    return String(e.message);
  }
  if (e._tag != null && String(e._tag).length > 0) {
    return String(e._tag);
  }
  if (e.cause !== undefined) return describeError(e.cause);
  return String(err);
}

function decodeFrame(payload: RawWsEventPayload): unknown {
  if (S.is(S.String)(payload)) return JSON.parse(payload);
  if (payload instanceof ArrayBuffer) {
    return JSON.parse(new TextDecoder().decode(payload));
  }
  if (payload instanceof Uint8Array) {
    return JSON.parse(new TextDecoder().decode(payload));
  }
  return payload;
}

interface TradeEvent {
  readonly symbol: string;
  readonly side: "Buy" | "Sell";
  readonly size: number;
  readonly ts: number;
}

interface LiquidationEvent {
  readonly symbol: string;
  readonly side: string;
  readonly size: number;
  readonly price: number;
  readonly bankruptcyPrice: number;
  readonly ts: number;
}

function toTradeEvent(raw: TradeWire): TradeEvent | null {
  const size = asNum(raw.v);
  const ts = asNum(raw.T);
  if (!Number.isFinite(size) || !Number.isFinite(ts)) return null;
  return { symbol: toWireSymbol(raw.s), side: raw.S, size, ts };
}

function toLiquidationEvent(raw: LiquidationWire): LiquidationEvent | null {
  const symbol = raw.symbol ?? raw.s;
  const side = raw.side ?? raw.S;
  if (symbol === undefined || symbol.length === 0) return null;
  if (side === undefined || side.length === 0) return null;
  const size = asNum(raw.size ?? raw.v);
  const price = asNum(raw.price ?? raw.p);
  const ts = asNum(raw.updatedTime ?? raw.T);
  if (
    !Number.isFinite(size) ||
    !Number.isFinite(price) ||
    !Number.isFinite(ts)
  ) {
    return null;
  }
  const bankruptcy = asNum(raw.bankruptcyPrice ?? raw.bp);
  return {
    symbol: toWireSymbol(symbol),
    side,
    size,
    price,
    bankruptcyPrice: Number.isFinite(bankruptcy) ? bankruptcy : 0,
    ts,
  };
}

// ---------------------------------------------------------------------------
// Recorder
// ---------------------------------------------------------------------------

interface Bucket {
  readonly min: number;
  buyVol: number;
  sellVol: number;
  trades: number;
}

export interface FlowRecorder {
  readonly start: Effect.Effect<void, never, never>;
  /** Stop and wait for every in-flight persistence write to settle. */
  readonly stop: Effect.Effect<void, never, never>;
  /** Completes when the recorder exits (clean stop, or retries exhausted). */
  readonly done: Effect.Effect<void, FlowRecorderError, never>;
}

class FlowRecorderImpl {
  private readonly repo: FlowRecorderRepository;
  private readonly symbols: readonly string[];
  private readonly ticker: boolean;
  private readonly exchange: string;
  private readonly baseUrl: string;
  private readonly wsFactory: FlowWebSocketFactory;
  private readonly pingIntervalMs: number;
  private readonly maxRetries: number;
  private readonly backoffMs: number;
  private readonly aggregateIntervalMs: number;
  private readonly onFlush: ((rows: readonly FlowOfiRow[]) => void) | undefined;
  private readonly onAggregate:
    | ((
        rows: readonly FlowOfiRow[],
        prices: ReadonlyMap<string, number>,
      ) => void)
    | undefined;
  private readonly onWarn: ((message: string) => void) | undefined;

  private ws: FlowWebSocket | null = null;
  private connected = false;
  private retries = 0;
  private stopping = false;
  private finished = false;
  private finishError: Error | null = null;
  private readonly doneWaiters: Array<(error: Error | null) => void> = [];
  private readonly buckets = new Map<string, Bucket>();
  private readonly lastPrices = new Map<string, number>();
  private readonly pendingSaves = new Set<Promise<void>>();
  private pingTimer: ReturnType<typeof setInterval> | undefined = undefined;
  private aggregateTimer: ReturnType<typeof setInterval> | undefined =
    undefined;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined = undefined;

  constructor(repo: FlowRecorderRepository, opts: FlowRecorderOpts) {
    this.repo = repo;
    this.symbols = (opts.symbols ?? []).map(toWireSymbol);
    this.ticker = opts.ticker ?? false;
    this.exchange = opts.exchange ?? FLOW_EXCHANGE;
    this.baseUrl = opts.baseUrl ?? FLOW_WS_URL;
    this.wsFactory = opts.wsFactory ?? defaultWsFactory;
    this.pingIntervalMs = opts.pingIntervalMs ?? 20_000;
    this.maxRetries = opts.maxRetries ?? 5;
    this.backoffMs = opts.backoffMs ?? 5_000;
    this.aggregateIntervalMs = opts.aggregateIntervalMs ?? 30_000;
    this.onFlush = opts.onFlush;
    this.onAggregate = opts.onAggregate;
    this.onWarn = opts.onWarn;
  }

  start(): void {
    this.pingTimer = setInterval(() => {
      if (this.connected && this.ws !== null) {
        try {
          this.ws.send(JSON.stringify({ op: "ping" }));
        } catch {
          // socket gone — the close handler will reconnect
        }
      }
    }, this.pingIntervalMs);
    this.aggregateTimer = setInterval(
      () => this.logAggregates(),
      this.aggregateIntervalMs,
    );
    this.connect();
  }

  stop(): void {
    if (this.stopping) return;
    this.stopping = true;
    clearInterval(this.pingTimer);
    clearInterval(this.aggregateTimer);
    clearTimeout(this.reconnectTimer);
    this.flushAll();
    if (this.ws !== null) {
      try {
        this.ws.close();
      } catch {
        // already closed
      }
    }
    this.finish(null);
  }

  /** Stop and wait for every in-flight persistence write to settle. */
  stopAndFlush(): Promise<void> {
    this.stop();
    return Promise.allSettled([...this.pendingSaves]).then(() => {});
  }

  done(): Effect.Effect<void, FlowRecorderError, never> {
    return Effect.callback<void, FlowRecorderError>((resume) => {
      if (this.finished) {
        resume(this.toResult());
        return;
      }
      this.doneWaiters.push(() => resume(this.toResult()));
    });
  }

  // -- connection lifecycle -------------------------------------------------

  private connect(): void {
    if (this.stopping || this.finished) return;
    this.drain(); // fresh connection, fresh state (no stale buckets across restarts)
    const ws = this.wsFactory(this.baseUrl);
    this.ws = ws;
    ws.on("open", () => {
      this.connected = true;
      this.retries = 0;
      this.sendSubscriptions(ws);
    });
    ws.on("message", (payload) => this.handleFrame(payload));
    ws.on("close", () => {
      this.connected = false;
      if (this.stopping || this.finished) return;
      this.flushAll(); // best-effort persist of partial minutes before reconnect
      if (this.finished) return;
      this.retries += 1;
      if (this.retries > this.maxRetries) {
        this.warn(
          `WebSocket closed; exceeded max reconnect retries (${this.maxRetries})`,
        );
        this.finish(
          new Error(
            `WebSocket closed after ${this.maxRetries} reconnect attempts`,
          ),
        );
        return;
      }
      this.warn(
        `WebSocket closed; reconnecting in ${this.backoffMs}ms ` +
          `(attempt ${this.retries}/${this.maxRetries})`,
      );
      this.reconnectTimer = setTimeout(() => this.connect(), this.backoffMs);
    });
    ws.on("error", (err) => {
      this.warn(`WebSocket error: ${describeError(err)}`);
    });
  }

  private drain(): void {
    this.buckets.clear();
    this.lastPrices.clear();
  }

  /**
   * Subscriptions are sent per channel because Bybit rejects the whole
   * subscribe message when any single arg is invalid — and the liquidation
   * topic is rejected on some endpoints (observed: this mainnet linear
   * endpoint returns "handler not found" for `liquidation`). Trades and
   * tickers must keep streaming even when liquidation is unavailable.
   */
  private sendSubscriptions(ws: FlowWebSocket): void {
    for (const group of chunk(
      this.symbols.map((s) => `publicTrade.${s}`),
      10,
    )) {
      ws.send(JSON.stringify({ op: "subscribe", args: group }));
    }
    ws.send(JSON.stringify({ op: "subscribe", args: ["liquidation"] }));
    if (this.ticker) {
      for (const group of chunk(
        this.symbols.map((s) => `tickers.${s}`),
        10,
      )) {
        ws.send(JSON.stringify({ op: "subscribe", args: group }));
      }
    }
  }

  // -- frame handling -------------------------------------------------------

  private handleFrame(payload: RawWsEventPayload): void {
    let decoded: unknown;
    try {
      decoded = decodeFrame(payload);
    } catch {
      this.warn(
        `Malformed WebSocket frame skipped: ${String(payload).slice(0, 120)}`,
      );
      return;
    }
    if (!S.is(FrameWireSchema)(decoded)) {
      this.warn("Malformed WebSocket message skipped (not an object)");
      return;
    }
    const { topic, op, success, ret_msg, data } = decoded;
    if (topic === undefined || topic.length === 0) {
      // Ping/pong + subscribe acks have no topic; surface rejected
      // subscriptions instead of failing silently.
      if (op === "subscribe" && success === false) {
        this.warn(
          `subscription rejected: ${String(ret_msg ?? "unknown error")}`,
        );
      }
      return;
    }
    try {
      if (topic === "liquidation") {
        if (S.is(S.Array(LiquidationWireSchema))(data)) {
          this.handleLiquidation(data);
        } else if (S.is(LiquidationWireSchema)(data)) {
          this.handleLiquidation([data]);
        } else {
          this.warn(
            `Malformed liquidation message skipped: ${String(data).slice(0, 200)}`,
          );
        }
      } else if (topic.startsWith("publicTrade.")) {
        if (S.is(S.Array(TradeWireSchema))(data)) {
          this.handleTrades(data);
        } else if (S.is(TradeWireSchema)(data)) {
          this.handleTrades([data]);
        } else {
          this.warn(
            `Malformed trade message skipped: ${String(data).slice(0, 200)}`,
          );
        }
      } else if (this.ticker && topic.startsWith("tickers.")) {
        if (S.is(TickerWireSchema)(data)) {
          this.handleTicker(topic.slice("tickers.".length), data);
        }
      }
    } catch (err) {
      this.warn(`Error handling ${topic} message: ${describeError(err)}`);
    }
  }

  private handleTrades(data: readonly TradeWire[]): void {
    for (const raw of data) {
      const trade = toTradeEvent(raw);
      if (trade === null) {
        this.warn(
          `Malformed trade event skipped: ${JSON.stringify(raw).slice(0, 200)}`,
        );
        continue;
      }
      this.accumulate(trade);
    }
  }

  private accumulate(trade: TradeEvent): void {
    const min = Math.floor(trade.ts / 60_000) * 60_000;
    const current = this.buckets.get(trade.symbol);
    if (current === undefined || current.min !== min) {
      if (current !== undefined) {
        if (min < current.min) return; // stale trade for an already-flushed minute
        this.flushBucket(trade.symbol); // minute rolled over — persist the complete one
      }
      this.buckets.set(trade.symbol, {
        min,
        buyVol: 0,
        sellVol: 0,
        trades: 0,
      });
    }
    const bucket = this.buckets.get(trade.symbol)!;
    if (trade.side === "Buy") bucket.buyVol += trade.size;
    else bucket.sellVol += trade.size;
    bucket.trades += 1;
  }

  private handleLiquidation(data: readonly LiquidationWire[]): void {
    const rows: FlowLiquidationRow[] = [];
    for (const raw of data) {
      const liq = toLiquidationEvent(raw);
      if (liq === null) {
        this.warn(
          `Malformed liquidation event skipped: ${JSON.stringify(raw).slice(0, 200)}`,
        );
        continue;
      }
      rows.push({
        exchange: this.exchange,
        symbol: liq.symbol,
        ts: liq.ts,
        side: liq.side,
        size: liq.size,
        price: liq.price,
        bankruptcyPrice: liq.bankruptcyPrice,
      });
    }
    if (rows.length > 0) {
      this.trackSave(
        Effect.runPromise(this.repo.saveLiquidations(rows)),
        (err) =>
          this.warn(
            `Failed to persist flow_liquidations rows: ${describeError(err)}`,
          ),
      );
    }
  }

  private handleTicker(symbol: string, data: TickerWire): void {
    const price = asNum(data.lastPrice);
    if (Number.isFinite(price)) this.lastPrices.set(symbol, price);
  }

  // -- persistence & aggregation -------------------------------------------

  private flushBucket(symbol: string): void {
    const bucket = this.buckets.get(symbol);
    if (bucket === undefined) return;
    this.buckets.delete(symbol);
    const row: FlowOfiRow = {
      exchange: this.exchange,
      symbol,
      ts: bucket.min,
      buyVol: round4(bucket.buyVol),
      sellVol: round4(bucket.sellVol),
      trades: bucket.trades,
    };
    this.persistOfi([row]);
    try {
      this.onFlush?.([row]);
    } catch {
      // observer errors never kill the recorder
    }
  }

  private flushAll(): void {
    for (const symbol of [...this.buckets.keys()]) this.flushBucket(symbol);
  }

  private persistOfi(rows: FlowOfiRow[]): void {
    if (rows.length === 0) return;
    this.trackSave(Effect.runPromise(this.repo.saveFlowOfi(rows)), (err) =>
      this.warn(`Failed to persist flow_ofi_1m rows: ${describeError(err)}`),
    );
  }

  private trackSave(save: Promise<void>, onError: (err: Error) => void): void {
    this.pendingSaves.add(save);
    void save.then(
      () => this.pendingSaves.delete(save),
      (err) => {
        this.pendingSaves.delete(save);
        onError(err);
      },
    );
  }

  private logAggregates(): void {
    if (this.buckets.size === 0) return;
    const rows: FlowOfiRow[] = [];
    for (const [symbol, bucket] of this.buckets) {
      rows.push({
        exchange: this.exchange,
        symbol,
        ts: bucket.min,
        buyVol: round4(bucket.buyVol),
        sellVol: round4(bucket.sellVol),
        trades: bucket.trades,
      });
    }
    try {
      this.onAggregate?.(rows, this.lastPrices);
    } catch {
      // observer errors never kill the recorder
    }
  }

  // -- completion -----------------------------------------------------------

  private warn(message: string): void {
    try {
      this.onWarn?.(message);
    } catch {
      // ignore observer failures
    }
  }

  private finish(error: Error | null): void {
    if (this.finished) return;
    this.finished = true;
    this.finishError = error;
    const waiters = this.doneWaiters.splice(0);
    for (const w of waiters) w(error);
  }

  private toResult(): Effect.Effect<void, FlowRecorderError, never> {
    const error = this.finishError;
    if (error === null) return Effect.void;
    return Effect.fail(
      new FlowRecorderError({ reason: error.message, cause: error }),
    );
  }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export function makeFlowRecorder(
  repo: FlowRecorderRepository,
  opts: FlowRecorderOpts = {},
): FlowRecorder {
  const impl = new FlowRecorderImpl(repo, opts);
  return {
    start: Effect.sync(() => impl.start()),
    stop: Effect.promise(() => impl.stopAndFlush()),
    done: impl.done(),
  };
}

/**
 * Ensures the flow tables exist, then runs the recorder until it stops
 * cleanly (interrupt/timeout run the close finalizer), exhausts its reconnect
 * retries (fails), or is stopped. Structured for BunRuntime.runMain, which
 * interrupts the fiber on SIGINT/SIGTERM.
 */
export function runFlowRecorder(
  repo: FlowRecorderRepository,
  opts: FlowRecorderOpts = {},
): Effect.Effect<void, FlowRecorderError, never> {
  return Effect.gen(function* () {
    yield* repo.ensureFlowTables();
    yield* Effect.scoped(
      Effect.gen(function* () {
        const impl = yield* Effect.acquireRelease(
          Effect.sync(() => {
            const i = new FlowRecorderImpl(repo, opts);
            i.start();
            return i;
          }),
          (i) => Effect.promise(() => i.stopAndFlush()),
        );
        yield* impl.done();
      }),
    );
  }).pipe(
    Effect.mapError((err) =>
      err instanceof FlowRecorderError
        ? err
        : new FlowRecorderError({
            reason: "flow-recorder failed",
            cause: err,
          }),
    ),
  );
}

/**
 * Resolve the recorder's default symbol list: the ranked top-100 from
 * `selectFlowUniverse` (mainnet 24h turnover, refreshed per start), else the
 * liquid fallback set. Never throws — a failed REST fetch falls back.
 */
export async function resolveFlowSymbols(
  overrides?: readonly string[],
): Promise<readonly string[]> {
  if (overrides !== undefined && overrides.length > 0) {
    return overrides.map(toWireSymbol);
  }
  try {
    const [tickers, instruments] = await Promise.all([
      Effect.runPromise(fetchTickers("https://api.bybit.com")),
      Effect.runPromise(fetchInstruments("https://api.bybit.com")),
    ]);
    const tickerBySymbol = new Map(
      tickers.map((ticker) => [ticker.symbol, ticker]),
    );
    const volumes = Object.fromEntries(
      tickers.map((ticker) => [ticker.symbol, ticker.turnover24h]),
    );
    const quotedInstruments = instruments.map((instrument) => {
      const ticker = tickerBySymbol.get(instrument.symbol);
      return ticker === undefined
        ? instrument
        : {
            ...instrument,
            bid1Price: ticker.bid1Price,
            ask1Price: ticker.ask1Price,
          };
    });
    const ranked = selectFlowUniverse(volumes, quotedInstruments, undefined, {
      topN: DEFAULT_FLOW_UNIVERSE_TOP_N,
    });
    const symbols = ranked.map((e) => toWireSymbol(e.symbol));
    if (symbols.length > 0) return symbols;
  } catch {
    // mainnet REST unavailable — fall back to the liquid set
  }
  return [...DEFAULT_FLOW_SYMBOLS];
}
