import { Context, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
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
  readonly ensureTables: () => Effect.Effect<void, PaperTradingRepositoryError, never>;
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
}

export const PaperTradingRepository = Context.GenericTag<PaperTradingRepositoryService>(
  "PaperTradingRepository",
);

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
  signal_id TEXT NOT NULL
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
            `SELECT id, exchange, symbol, timeframe, side, entry_price, size, stop_loss, take_profit, opened_at, signal_id
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
        };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load open position: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  saveOpenPosition(position: PaperPosition): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT OR REPLACE INTO paper_positions
             (id, exchange, symbol, timeframe, side, entry_price, size, stop_loss, take_profit, opened_at, signal_id)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
        const priceDiff =
          position.side === "long"
            ? exitPrice - position.entryPrice
            : position.entryPrice - exitPrice;
        const pnl = priceDiff * position.size;
        const pnlPct = (pnl / (position.entryPrice * position.size)) * 100;
        const tradeId = `paper-trade-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

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
          pnl,
          pnlPct,
          exitReason,
          position.openedAt.toISOString(),
          closedAt.toISOString(),
        );

        this.db
          .query("DELETE FROM paper_positions WHERE id = ?")
          .run(position.id);

        return {
          id: tradeId,
          exchange: position.exchange,
          symbol: position.symbol,
          timeframe: position.timeframe,
          side: position.side,
          entryPrice: position.entryPrice,
          exitPrice,
          size: position.size,
          pnl,
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

  getPortfolio(): Effect.Effect<
    { readonly capital: number; readonly peakCapital: number },
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const row = this.db
          .query("SELECT capital, peak_capital FROM paper_portfolio WHERE id = 1")
          .get() as { capital: number; peak_capital: number } | null;

        return row ? { capital: row.capital, peakCapital: row.peak_capital } : { capital: 10_000, peakCapital: 10_000 };
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

  listRecentTrades(limit: number): Effect.Effect<readonly PaperTrade[], PaperTradingRepositoryError, never> {
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
}

export const PaperTradingRepositorySQLiteLive = (db: Database) =>
  Layer.succeed(
    PaperTradingRepository,
    new PaperTradingRepositorySQLite(db) as PaperTradingRepositoryService,
  );
