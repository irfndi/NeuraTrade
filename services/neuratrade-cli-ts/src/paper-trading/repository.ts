import { Context, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { Decimal } from "../utils/money.js";
import type {
  GridPaperState,
  GridPaperTrade,
  PaperPosition,
  PaperTrade,
} from "./types.js";

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

  readonly getGridState: (
    exchange: string,
    symbol: string,
    timeframe: string,
  ) => Effect.Effect<GridPaperState | null, PaperTradingRepositoryError, never>;

  readonly saveGridState: (
    state: GridPaperState,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;

  /**
   * Delete the persisted grid state for a key. Used when starting an
   * explicit replay run so a stale replay pointer from an earlier session
   * cannot lock the walk out ("no new replay candle" forever).
   */
  readonly resetGridState: (
    exchange: string,
    symbol: string,
    timeframe: string,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;

  readonly recordGridTrade: (
    trade: GridPaperTrade,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;

  readonly listRecentGridTrades: (
    exchange: string,
    symbol: string,
    timeframe: string,
    limit: number,
  ) => Effect.Effect<
    readonly GridPaperTrade[],
    PaperTradingRepositoryError,
    never
  >;
}

export const PaperTradingRepository =
  Context.Service<PaperTradingRepositoryService>("PaperTradingRepository");

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

CREATE TABLE IF NOT EXISTS grid_paper_state (
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  capital REAL NOT NULL,
  peak_capital REAL NOT NULL,
  paused INTEGER NOT NULL DEFAULT 0,
  side TEXT,
  entry_price REAL,
  grid_step_pct REAL NOT NULL,
  grid_max_grids INTEGER NOT NULL,
  grid_pause_after_loss_bars INTEGER NOT NULL,
  fee_pct REAL NOT NULL,
  slippage_bps REAL NOT NULL,
  trend_filter_period INTEGER NOT NULL,
  max_position_pct REAL NOT NULL DEFAULT 100,
  max_drawdown_pct REAL NOT NULL DEFAULT 100,
  leverage REAL NOT NULL DEFAULT 1,
  killed INTEGER NOT NULL DEFAULT 0,
  last_timestamp DATETIME,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (exchange, symbol, timeframe)
);

CREATE TABLE IF NOT EXISTS grid_paper_trades (
  id TEXT PRIMARY KEY,
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  side TEXT NOT NULL,
  entry_price REAL NOT NULL,
  exit_price REAL NOT NULL,
  capital_before REAL NOT NULL,
  capital_after REAL NOT NULL,
  pnl_pct REAL NOT NULL,
  exit_reason TEXT NOT NULL,
  opened_at DATETIME NOT NULL,
  closed_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_grid_paper_trades_symbol
  ON grid_paper_trades(exchange, symbol, timeframe);
CREATE INDEX IF NOT EXISTS idx_grid_paper_trades_closed_at
  ON grid_paper_trades(closed_at DESC);
`;

export class PaperTradingRepositorySQLite implements PaperTradingRepositoryService {
  constructor(private readonly db: Database) {}

  ensureTables(): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        // Allow concurrent reads while a single writer updates the DB, and
        // wait briefly instead of failing immediately when the DB is locked.
        this.db.exec("PRAGMA journal_mode = WAL;");
        this.db.exec("PRAGMA busy_timeout = 5000;");

        this.db.exec(ensureTablesSQL);

        // Idempotent column migrations for older DB files.
        const addColumn = (sql: string) => {
          try {
            this.db.exec(sql);
          } catch (migrateErr) {
            const msg =
              migrateErr instanceof Error
                ? migrateErr.message
                : String(migrateErr);
            if (!msg.toLowerCase().includes("duplicate column name")) {
              throw migrateErr;
            }
          }
        };
        addColumn(
          "ALTER TABLE grid_paper_state ADD COLUMN max_position_pct REAL NOT NULL DEFAULT 100",
        );
        addColumn(
          "ALTER TABLE grid_paper_state ADD COLUMN max_drawdown_pct REAL NOT NULL DEFAULT 100",
        );
        addColumn(
          "ALTER TABLE grid_paper_state ADD COLUMN leverage REAL NOT NULL DEFAULT 1",
        );
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

  getGridState(
    exchange: string,
    symbol: string,
    timeframe: string,
  ): Effect.Effect<GridPaperState | null, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            `SELECT exchange, symbol, timeframe, capital, peak_capital, paused, side,
                    entry_price, grid_step_pct, grid_max_grids, grid_pause_after_loss_bars,
                    fee_pct, slippage_bps, trend_filter_period, max_position_pct,
                    max_drawdown_pct, leverage, killed, last_timestamp, updated_at
             FROM grid_paper_state
             WHERE exchange = ? AND symbol = ? AND timeframe = ?`,
          )
          .get(exchange, symbol, timeframe) as {
          exchange: string;
          symbol: string;
          timeframe: string;
          capital: number;
          peak_capital: number;
          paused: number;
          side: string | null;
          entry_price: number | null;
          grid_step_pct: number;
          grid_max_grids: number;
          grid_pause_after_loss_bars: number;
          fee_pct: number;
          slippage_bps: number;
          trend_filter_period: number;
          max_position_pct: number;
          max_drawdown_pct: number;
          leverage: number;
          killed: number;
          last_timestamp: string | null;
          updated_at: string;
        } | null;

        if (!row) return null;

        return {
          exchange: row.exchange,
          symbol: row.symbol,
          timeframe: row.timeframe,
          capital: row.capital,
          peakCapital: row.peak_capital,
          paused: row.paused,
          side: (row.side as GridPaperState["side"]) ?? null,
          entryPrice: row.entry_price ?? 0,
          gridStepPct: row.grid_step_pct,
          gridMaxGrids: row.grid_max_grids,
          gridPauseAfterLossBars: row.grid_pause_after_loss_bars,
          feePct: row.fee_pct,
          slippageBps: row.slippage_bps,
          trendFilterPeriod: row.trend_filter_period,
          maxPositionPct: row.max_position_pct,
          maxDrawdownPct: row.max_drawdown_pct,
          leverage: row.leverage,
          killed: Boolean(row.killed),
          lastTimestamp: row.last_timestamp
            ? new Date(row.last_timestamp)
            : null,
          updatedAt: new Date(row.updated_at),
        };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load grid paper state: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  saveGridState(
    state: GridPaperState,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT INTO grid_paper_state
             (exchange, symbol, timeframe, capital, peak_capital, paused, side,
              entry_price, grid_step_pct, grid_max_grids, grid_pause_after_loss_bars,
              fee_pct, slippage_bps, trend_filter_period, max_position_pct,
              max_drawdown_pct, leverage, killed, last_timestamp, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(exchange, symbol, timeframe) DO UPDATE SET
               capital = excluded.capital,
               peak_capital = excluded.peak_capital,
               paused = excluded.paused,
               side = excluded.side,
               entry_price = excluded.entry_price,
               grid_step_pct = excluded.grid_step_pct,
               grid_max_grids = excluded.grid_max_grids,
               grid_pause_after_loss_bars = excluded.grid_pause_after_loss_bars,
               fee_pct = excluded.fee_pct,
               slippage_bps = excluded.slippage_bps,
               trend_filter_period = excluded.trend_filter_period,
               max_position_pct = excluded.max_position_pct,
               max_drawdown_pct = excluded.max_drawdown_pct,
               leverage = excluded.leverage,
               killed = excluded.killed,
               last_timestamp = excluded.last_timestamp,
               updated_at = excluded.updated_at`,
          )
          .run(
            state.exchange,
            state.symbol,
            state.timeframe,
            state.capital,
            state.peakCapital,
            state.paused,
            state.side,
            state.entryPrice,
            state.gridStepPct,
            state.gridMaxGrids,
            state.gridPauseAfterLossBars,
            state.feePct,
            state.slippageBps,
            state.trendFilterPeriod,
            state.maxPositionPct,
            state.maxDrawdownPct,
            state.leverage,
            state.killed ? 1 : 0,
            state.lastTimestamp ? state.lastTimestamp.toISOString() : null,
            state.updatedAt.toISOString(),
          );
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to save grid paper state: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  resetGridState(
    exchange: string,
    symbol: string,
    timeframe: string,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `DELETE FROM grid_paper_state
             WHERE exchange = ? AND symbol = ? AND timeframe = ?`,
          )
          .run(exchange, symbol, timeframe);
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to reset grid paper state: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  recordGridTrade(
    trade: GridPaperTrade,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT INTO grid_paper_trades
             (id, exchange, symbol, timeframe, side, entry_price, exit_price,
              capital_before, capital_after, pnl_pct, exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            trade.id,
            trade.exchange,
            trade.symbol,
            trade.timeframe,
            trade.side,
            trade.entryPrice,
            trade.exitPrice,
            trade.capitalBefore,
            trade.capitalAfter,
            trade.pnlPct,
            trade.exitReason,
            trade.openedAt.toISOString(),
            trade.closedAt.toISOString(),
          );
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to record grid paper trade: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  listRecentGridTrades(
    exchange: string,
    symbol: string,
    timeframe: string,
    limit: number,
  ): Effect.Effect<
    readonly GridPaperTrade[],
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const rows = this.db
          .query(
            `SELECT id, exchange, symbol, timeframe, side, entry_price, exit_price,
                    capital_before, capital_after, pnl_pct, exit_reason, opened_at, closed_at
             FROM grid_paper_trades
             WHERE exchange = ? AND symbol = ? AND timeframe = ?
             ORDER BY closed_at DESC
             LIMIT ?`,
          )
          .all(exchange, symbol, timeframe, limit) as Array<{
          id: string;
          exchange: string;
          symbol: string;
          timeframe: string;
          side: string;
          entry_price: number;
          exit_price: number;
          capital_before: number;
          capital_after: number;
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
          side: (r.side as GridPaperState["side"]) ?? "long",
          entryPrice: r.entry_price,
          exitPrice: r.exit_price,
          capitalBefore: r.capital_before,
          capitalAfter: r.capital_after,
          pnlPct: r.pnl_pct,
          exitReason: r.exit_reason as GridPaperTrade["exitReason"],
          openedAt: new Date(r.opened_at),
          closedAt: new Date(r.closed_at),
        }));
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to list grid paper trades: ${err instanceof Error ? err.message : String(err)}`,
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
