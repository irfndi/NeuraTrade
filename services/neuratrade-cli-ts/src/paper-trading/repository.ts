import { Context, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { Decimal } from "../utils/money.js";
import type { PaperPosition, PaperTrade } from "./types.js";

/**
 * Error raised when paper-trading persistence fails.
 */
export class PaperTradingRepositoryError {
  readonly _tag = "PaperTradingRepositoryError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

export interface PaperTradingRepositoryService {
  readonly ensureTables: () => Effect.Effect<
    void,
    PaperTradingRepositoryError,
    never
  >;
  readonly getOpenPosition: (
    exchange: string,
    symbol: string,
  ) => Effect.Effect<PaperPosition | null, PaperTradingRepositoryError, never>;
  readonly saveOpenPosition: (
    position: PaperPosition,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;
  readonly closePosition: (
    position: PaperPosition,
    exitPrice: number,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ) => Effect.Effect<PaperTrade, PaperTradingRepositoryError, never>;
  readonly scaleOutPosition: (
    position: PaperPosition,
    exitPrice: number,
    scaleOutPct: number,
    closedAt: Date,
  ) => Effect.Effect<
    { readonly trade: PaperTrade; readonly updatedPosition: PaperPosition },
    PaperTradingRepositoryError,
    never
  >;
  readonly getPortfolio: () => Effect.Effect<
    { readonly capital: number; readonly peakCapital: number },
    PaperTradingRepositoryError,
    never
  >;
  readonly setPortfolio: (
    capital: number,
    peakCapital: number,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;
  readonly listRecentTrades: (
    limit: number,
  ) => Effect.Effect<readonly PaperTrade[], PaperTradingRepositoryError, never>;

  readonly countTradesForDate: (
    date: Date,
  ) => Effect.Effect<number, PaperTradingRepositoryError, never>;

  readonly getTodayRealizedPnl: () => Effect.Effect<
    number,
    PaperTradingRepositoryError,
    never
  >;

  readonly getStartOfDayCapital: (
    date: Date,
    currentCapital: number,
  ) => Effect.Effect<number, PaperTradingRepositoryError, never>;
}

export const PaperTradingRepository =
  Context.GenericTag<PaperTradingRepositoryService>("PaperTradingRepository");

const ensureTablesSQL = `
CREATE TABLE IF NOT EXISTS paper_portfolio (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  capital REAL NOT NULL,
  peak_capital REAL NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS paper_positions (
  id TEXT PRIMARY KEY,
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  side TEXT NOT NULL,
  entry_price REAL NOT NULL,
  size REAL NOT NULL,
  stop_loss REAL NOT NULL,
  take_profit REAL NOT NULL,
  opened_at DATETIME NOT NULL,
  signal_id TEXT NOT NULL,
  scaled_out INTEGER NOT NULL DEFAULT 0,
  scale_out_price REAL NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS paper_trades (
  id TEXT PRIMARY KEY,
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  side TEXT NOT NULL,
  entry_price REAL NOT NULL,
  exit_price REAL NOT NULL,
  size REAL NOT NULL,
  pnl REAL NOT NULL,
  pnl_pct REAL NOT NULL,
  exit_reason TEXT NOT NULL,
  opened_at DATETIME NOT NULL,
  closed_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_paper_trades_symbol ON paper_trades(symbol);
CREATE INDEX IF NOT EXISTS idx_paper_trades_closed_at ON paper_trades(closed_at DESC);

CREATE TABLE IF NOT EXISTS paper_start_of_day_capital (
  date TEXT PRIMARY KEY,
  start_capital REAL NOT NULL,
  updated_at DATETIME NOT NULL
);
`;

export class PaperTradingRepositorySQLite implements PaperTradingRepositoryService {
  constructor(private readonly db: Database) {}

  ensureTables(): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db.exec(ensureTablesSQL);
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to create paper-trading tables: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getOpenPosition(
    exchange: string,
    symbol: string,
  ): Effect.Effect<PaperPosition | null, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            `SELECT id, exchange, symbol, timeframe, side, entry_price, size, stop_loss, take_profit, opened_at, signal_id, scaled_out, scale_out_price
             FROM paper_positions
             WHERE exchange = ? AND symbol = ?`,
          )
          .get(exchange, symbol) as {
          id: string;
          exchange: string;
          symbol: string;
          timeframe: string;
          side: string;
          entry_price: number;
          size: number;
          stop_loss: number;
          take_profit: number;
          opened_at: string;
          signal_id: string;
          scaled_out: number;
          scale_out_price: number;
        } | null;

        if (!row) return null;

        return {
          id: row.id,
          exchange: row.exchange,
          symbol: row.symbol,
          timeframe: row.timeframe,
          side: row.side as PaperPosition["side"],
          entryPrice: row.entry_price,
          size: row.size,
          stopLoss: row.stop_loss,
          takeProfit: row.take_profit,
          openedAt: new Date(row.opened_at),
          signalId: row.signal_id,
          scaledOut: Boolean(row.scaled_out),
          scaleOutPrice: row.scale_out_price,
        };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load open position: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  saveOpenPosition(
    position: PaperPosition,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT OR REPLACE INTO paper_positions
             (id, exchange, symbol, timeframe, side, entry_price, size, stop_loss, take_profit, opened_at, signal_id, scaled_out, scale_out_price)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            position.id,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            position.entryPrice,
            position.size,
            position.stopLoss,
            position.takeProfit,
            position.openedAt.toISOString(),
            position.signalId,
            position.scaledOut ? 1 : 0,
            position.scaleOutPrice,
          );
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to save open position: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  closePosition(
    position: PaperPosition,
    exitPrice: number,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ): Effect.Effect<PaperTrade, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const entryPrice = new Decimal(position.entryPrice);
        const exitPriceDec = new Decimal(exitPrice);
        const size = new Decimal(position.size);
        const priceDiff =
          position.side === "long"
            ? exitPriceDec.minus(entryPrice)
            : entryPrice.minus(exitPriceDec);
        const pnl = priceDiff.times(size);
        const pnlPct = pnl.div(entryPrice.times(size)).times(100).toNumber();
        const pnlNum = pnl.toNumber();
        const tradeId = `paper-trade-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

        // Wrap trade insertion and position deletion in a transaction so the
        // close is atomic.
        this.db.transaction(() => {
          const insert = this.db.query(
            `INSERT INTO paper_trades
             (id, exchange, symbol, timeframe, side, entry_price, exit_price, size, pnl, pnl_pct, exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          );
          insert.run(
            tradeId,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            position.entryPrice,
            exitPrice,
            position.size,
            pnlNum,
            pnlPct,
            exitReason,
            position.openedAt.toISOString(),
            closedAt.toISOString(),
          );

          this.db
            .query("DELETE FROM paper_positions WHERE id = ?")
            .run(position.id);
        })();

        return {
          id: tradeId,
          exchange: position.exchange,
          symbol: position.symbol,
          timeframe: position.timeframe,
          side: position.side,
          entryPrice: position.entryPrice,
          exitPrice,
          size: position.size,
          pnl: pnlNum,
          pnlPct,
          exitReason,
          openedAt: position.openedAt,
          closedAt,
        };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to close position: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  scaleOutPosition(
    position: PaperPosition,
    exitPrice: number,
    scaleOutPct: number,
    closedAt: Date,
  ): Effect.Effect<
    { readonly trade: PaperTrade; readonly updatedPosition: PaperPosition },
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const pct = Math.max(0, Math.min(100, scaleOutPct));
        const partialSize = new Decimal(position.size).times(pct / 100);
        if (partialSize.lessThanOrEqualTo(0)) {
          throw new Error("scale-out size must be positive");
        }

        const entryPrice = new Decimal(position.entryPrice);
        const exitPriceDec = new Decimal(exitPrice);
        const priceDiff =
          position.side === "long"
            ? exitPriceDec.minus(entryPrice)
            : entryPrice.minus(exitPriceDec);
        const pnl = priceDiff.times(partialSize);
        const pnlPct = pnl
          .div(entryPrice.times(partialSize))
          .times(100)
          .toNumber();
        const pnlNum = pnl.toNumber();
        const remainingSize = new Decimal(position.size)
          .minus(partialSize)
          .toNumber();
        const tradeId = `paper-trade-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

        this.db.transaction(() => {
          const insert = this.db.query(
            `INSERT INTO paper_trades
             (id, exchange, symbol, timeframe, side, entry_price, exit_price, size, pnl, pnl_pct, exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          );
          insert.run(
            tradeId,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            position.entryPrice,
            exitPrice,
            partialSize.toNumber(),
            pnlNum,
            pnlPct,
            "scale_out",
            position.openedAt.toISOString(),
            closedAt.toISOString(),
          );

          this.db
            .query(
              `UPDATE paper_positions
               SET size = ?, stop_loss = ?, scaled_out = 1
               WHERE id = ?`,
            )
            .run(remainingSize, position.entryPrice, position.id);
        })();

        const trade: PaperTrade = {
          id: tradeId,
          exchange: position.exchange,
          symbol: position.symbol,
          timeframe: position.timeframe,
          side: position.side,
          entryPrice: position.entryPrice,
          exitPrice,
          size: partialSize.toNumber(),
          pnl: pnlNum,
          pnlPct,
          exitReason: "scale_out",
          openedAt: position.openedAt,
          closedAt,
        };
        const updatedPosition: PaperPosition = {
          ...position,
          size: remainingSize,
          stopLoss: position.entryPrice,
          scaledOut: true,
        };
        return { trade, updatedPosition };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to scale out position: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getPortfolio(): Effect.Effect<
    { readonly capital: number; readonly peakCapital: number },
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            "SELECT capital, peak_capital FROM paper_portfolio WHERE id = 1",
          )
          .get() as { capital: number; peak_capital: number } | null;

        return row
          ? { capital: row.capital, peakCapital: row.peak_capital }
          : { capital: 10_000, peakCapital: 10_000 };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load portfolio: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  setPortfolio(
    capital: number,
    peakCapital: number,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT INTO paper_portfolio (id, capital, peak_capital, updated_at)
             VALUES (1, ?, ?, ?)
             ON CONFLICT(id) DO UPDATE SET
               capital = excluded.capital,
               peak_capital = excluded.peak_capital,
               updated_at = excluded.updated_at`,
          )
          .run(capital, peakCapital, new Date().toISOString());
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to save portfolio: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  listRecentTrades(
    limit: number,
  ): Effect.Effect<readonly PaperTrade[], PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const rows = this.db
          .query(
            `SELECT id, exchange, symbol, timeframe, side, entry_price, exit_price, size, pnl, pnl_pct, exit_reason, opened_at, closed_at
             FROM paper_trades
             ORDER BY closed_at DESC
             LIMIT ?`,
          )
          .all(limit) as Array<{
          id: string;
          exchange: string;
          symbol: string;
          timeframe: string;
          side: string;
          entry_price: number;
          exit_price: number;
          size: number;
          pnl: number;
          pnl_pct: number;
          exit_reason: string;
          opened_at: string;
          closed_at: string;
        }>;

        return rows.map((r) => ({
          id: r.id,
          exchange: r.exchange,
          symbol: r.symbol,
          timeframe: r.timeframe,
          side: r.side as PaperPosition["side"],
          entryPrice: r.entry_price,
          exitPrice: r.exit_price,
          size: r.size,
          pnl: r.pnl,
          pnlPct: r.pnl_pct,
          exitReason: r.exit_reason as PaperTrade["exitReason"],
          openedAt: new Date(r.opened_at),
          closedAt: new Date(r.closed_at),
        }));
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to list trades: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  countTradesForDate(
    date: Date,
  ): Effect.Effect<number, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            `SELECT COUNT(*) AS count
             FROM paper_trades
             WHERE date(closed_at) = date(?)`,
          )
          .get(date.toISOString()) as { count: number } | null;
        return row?.count ?? 0;
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to count trades: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getTodayRealizedPnl(): Effect.Effect<
    number,
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            `SELECT COALESCE(SUM(pnl), 0) AS pnl
             FROM paper_trades
             WHERE date(closed_at) = date('now')`,
          )
          .get() as { pnl: number } | null;
        return row?.pnl ?? 0;
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load today PnL: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getStartOfDayCapital(
    date: Date,
    currentCapital: number,
  ): Effect.Effect<number, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const dateKey = date.toISOString().slice(0, 10);
        const row = this.db
          .query(
            "SELECT start_capital FROM paper_start_of_day_capital WHERE date = ?",
          )
          .get(dateKey) as { start_capital: number } | null;
        if (row) {
          return row.start_capital;
        }
        this.db
          .query(
            `INSERT INTO paper_start_of_day_capital (date, start_capital, updated_at)
             VALUES (?, ?, ?)
             ON CONFLICT(date) DO UPDATE SET
               start_capital = excluded.start_capital,
               updated_at = excluded.updated_at`,
          )
          .run(dateKey, currentCapital, new Date().toISOString());
        return currentCapital;
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load start-of-day capital: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }
}

export const PaperTradingRepositorySQLiteLive = (db: Database) =>
  Layer.succeed(
    PaperTradingRepository,
    new PaperTradingRepositorySQLite(db) as PaperTradingRepositoryService,
  );
