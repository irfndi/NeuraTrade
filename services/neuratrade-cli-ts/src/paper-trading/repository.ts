import { Context, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { Decimal, toNumber } from "../utils/money.js";
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

export interface WatchlistEntry {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly returnPct: number;
  readonly profitableWindowsPct: number;
  readonly aggregateReturnPct: number;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly targetRatio: number;
  readonly chopGateAdx: number;
  readonly oosTrades: number;
  readonly fillsPerDay: number;
  readonly edgePerTradePct: number;
  readonly volatility: number;
  readonly allocatedWeight: number;
  readonly updatedAt: Date;
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
    exitPrice: Decimal,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ) => Effect.Effect<PaperTrade, PaperTradingRepositoryError, never>;
  readonly scaleOutPosition: (
    position: PaperPosition,
    exitPrice: Decimal,
    scaleOutPct: number,
    closedAt: Date,
  ) => Effect.Effect<
    { readonly trade: PaperTrade; readonly updatedPosition: PaperPosition },
    PaperTradingRepositoryError,
    never
  >;
  readonly getPortfolio: () => Effect.Effect<
    { readonly capital: Decimal; readonly peakCapital: Decimal },
    PaperTradingRepositoryError,
    never
  >;
  readonly setPortfolio: (
    capital: Decimal,
    peakCapital: Decimal,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;
  readonly listRecentTrades: (
    limit: number,
  ) => Effect.Effect<readonly PaperTrade[], PaperTradingRepositoryError, never>;

  readonly countTradesForDate: (
    date: Date,
  ) => Effect.Effect<number, PaperTradingRepositoryError, never>;

  readonly getTodayRealizedPnl: () => Effect.Effect<
    Decimal,
    PaperTradingRepositoryError,
    never
  >;

  readonly getStartOfDayCapital: (
    date: Date,
    currentCapital: Decimal,
  ) => Effect.Effect<Decimal, PaperTradingRepositoryError, never>;

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

  readonly listAllGridTrades: (
    exchange: string,
    timeframe: string,
    limit: number,
    liveOnly?: boolean,
  ) => Effect.Effect<
    readonly GridPaperTrade[],
    PaperTradingRepositoryError,
    never
  >;

  readonly listWatchlist: (
    exchange: string,
    timeframe: string,
  ) => Effect.Effect<
    readonly WatchlistEntry[],
    PaperTradingRepositoryError,
    never
  >;

  readonly upsertWatchlist: (
    entries: readonly WatchlistEntry[],
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;

  readonly clearWatchlist: (
    exchange: string,
    timeframe: string,
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;

  readonly replaceWatchlist: (
    exchange: string,
    timeframe: string,
    entries: readonly WatchlistEntry[],
  ) => Effect.Effect<void, PaperTradingRepositoryError, never>;
}

export const PaperTradingRepository =
  Context.Service<PaperTradingRepositoryService>("PaperTradingRepository");

const ensureTablesSQL = `
CREATE TABLE IF NOT EXISTS paper_portfolio (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  capital REAL NOT NULL,
  peak_capital REAL NOT NULL,
  capital_decimal TEXT,
  peak_capital_decimal TEXT,
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
  entry_price_decimal TEXT,
  size_decimal TEXT,
  stop_loss_decimal TEXT,
  take_profit_decimal TEXT,
  opened_at DATETIME NOT NULL,
  signal_id TEXT NOT NULL,
  scaled_out INTEGER NOT NULL DEFAULT 0,
  scale_out_price REAL NOT NULL DEFAULT 0,
  scale_out_price_decimal TEXT
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
  entry_price_decimal TEXT,
  exit_price_decimal TEXT,
  size_decimal TEXT,
  pnl_decimal TEXT,
  pnl_pct_decimal TEXT,
  exit_reason TEXT NOT NULL,
  opened_at DATETIME NOT NULL,
  closed_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_paper_trades_symbol ON paper_trades(symbol);
CREATE INDEX IF NOT EXISTS idx_paper_trades_closed_at ON paper_trades(closed_at DESC);

CREATE TABLE IF NOT EXISTS paper_start_of_day_capital (
  date TEXT PRIMARY KEY,
  start_capital REAL NOT NULL,
  start_capital_decimal TEXT,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS grid_paper_state (
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  capital REAL NOT NULL,
  peak_capital REAL NOT NULL,
  capital_decimal TEXT,
  peak_capital_decimal TEXT,
  paused INTEGER NOT NULL DEFAULT 0,
  side TEXT,
  entry_price REAL,
  entry_price_decimal TEXT,
  entry_order_id TEXT,
  entry_client_oid TEXT,
  entry_filled_qty REAL,
  entry_filled_qty_decimal TEXT,
  entry_fee REAL,
  entry_fee_decimal TEXT,
  entry_fill_source TEXT,
  strategy_config_fingerprint TEXT,
  cohort_id TEXT,
  candidate_lock_at DATETIME,
  dataset_cutoff_at DATETIME,
  entry_opened_at DATETIME,
  execution_environment TEXT,
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
  entry_price_decimal TEXT,
  exit_price_decimal TEXT,
  capital_before_decimal TEXT,
  capital_after_decimal TEXT,
  pnl_pct_decimal TEXT,
  fill_source TEXT,
  entry_order_id TEXT,
  entry_client_oid TEXT,
  exit_order_id TEXT,
  exit_client_oid TEXT,
  entry_filled_qty REAL,
  entry_filled_qty_decimal TEXT,
  exit_filled_qty REAL,
  exit_filled_qty_decimal TEXT,
  entry_fee REAL,
  entry_fee_decimal TEXT,
  exit_fee REAL,
  exit_fee_decimal TEXT,
  realized_pnl_pct REAL,
  realized_pnl_pct_decimal TEXT,
  strategy_config_fingerprint TEXT,
  cohort_id TEXT,
  candidate_lock_at DATETIME,
  dataset_cutoff_at DATETIME,
  entry_opened_at DATETIME,
  execution_environment TEXT,
  exit_reason TEXT NOT NULL,
  opened_at DATETIME NOT NULL,
  closed_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_grid_paper_trades_symbol
  ON grid_paper_trades(exchange, symbol, timeframe);
CREATE INDEX IF NOT EXISTS idx_grid_paper_trades_closed_at
  ON grid_paper_trades(closed_at DESC);

CREATE TABLE IF NOT EXISTS watchlist (
  exchange TEXT NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  return_pct REAL NOT NULL,
  profitable_windows_pct REAL NOT NULL,
  aggregate_return_pct REAL NOT NULL,
  grid_step_pct REAL NOT NULL,
  grid_max_grids INTEGER NOT NULL,
  grid_pause_after_loss_bars INTEGER NOT NULL,
  target_ratio REAL NOT NULL DEFAULT 1,
  chop_gate_adx REAL NOT NULL DEFAULT 0,
  oos_trades INTEGER NOT NULL DEFAULT 0,
  fills_per_day REAL NOT NULL DEFAULT 0,
  edge_per_trade_pct REAL NOT NULL DEFAULT 0,
  volatility REAL NOT NULL DEFAULT 0,
  allocated_weight REAL NOT NULL DEFAULT 0,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (exchange, symbol, timeframe)
);

CREATE INDEX IF NOT EXISTS idx_watchlist_scope
  ON watchlist(exchange, timeframe);
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
        addColumn(
          "ALTER TABLE paper_positions ADD COLUMN scaled_out INTEGER NOT NULL DEFAULT 0",
        );
        addColumn(
          "ALTER TABLE paper_positions ADD COLUMN scale_out_price REAL NOT NULL DEFAULT 0",
        );
        for (const tableColumn of [
          "grid_paper_state entry_order_id TEXT",
          "grid_paper_state entry_client_oid TEXT",
          "grid_paper_state entry_filled_qty REAL",
          "grid_paper_state entry_fee REAL",
          "grid_paper_state entry_fill_source TEXT",
          "grid_paper_state strategy_config_fingerprint TEXT",
          "grid_paper_state cohort_id TEXT",
          "grid_paper_state candidate_lock_at DATETIME",
          "grid_paper_state dataset_cutoff_at DATETIME",
          "grid_paper_state entry_opened_at DATETIME",
          "grid_paper_state execution_environment TEXT",
          "grid_paper_trades fill_source TEXT",
          "grid_paper_trades entry_order_id TEXT",
          "grid_paper_trades entry_client_oid TEXT",
          "grid_paper_trades exit_order_id TEXT",
          "grid_paper_trades exit_client_oid TEXT",
          "grid_paper_trades entry_filled_qty REAL",
          "grid_paper_trades exit_filled_qty REAL",
          "grid_paper_trades entry_fee REAL",
          "grid_paper_trades exit_fee REAL",
          "grid_paper_trades realized_pnl_pct REAL",
          "grid_paper_trades strategy_config_fingerprint TEXT",
          "grid_paper_trades cohort_id TEXT",
          "grid_paper_trades candidate_lock_at DATETIME",
          "grid_paper_trades dataset_cutoff_at DATETIME",
          "grid_paper_trades entry_opened_at DATETIME",
          "grid_paper_trades execution_environment TEXT",
        ]) {
          const [table, column, type] = tableColumn.split(" ");
          addColumn(`ALTER TABLE ${table} ADD COLUMN ${column} ${type}`);
        }
        for (const tableColumn of [
          "paper_portfolio capital_decimal TEXT",
          "paper_portfolio peak_capital_decimal TEXT",
          "paper_positions entry_price_decimal TEXT",
          "paper_positions size_decimal TEXT",
          "paper_positions stop_loss_decimal TEXT",
          "paper_positions take_profit_decimal TEXT",
          "paper_positions scale_out_price_decimal TEXT",
          "paper_trades entry_price_decimal TEXT",
          "paper_trades exit_price_decimal TEXT",
          "paper_trades size_decimal TEXT",
          "paper_trades pnl_decimal TEXT",
          "paper_trades pnl_pct_decimal TEXT",
          "paper_start_of_day_capital start_capital_decimal TEXT",
          "grid_paper_state capital_decimal TEXT",
          "grid_paper_state peak_capital_decimal TEXT",
          "grid_paper_state entry_price_decimal TEXT",
          "grid_paper_state entry_filled_qty_decimal TEXT",
          "grid_paper_state entry_fee_decimal TEXT",
          "grid_paper_trades entry_price_decimal TEXT",
          "grid_paper_trades exit_price_decimal TEXT",
          "grid_paper_trades capital_before_decimal TEXT",
          "grid_paper_trades capital_after_decimal TEXT",
          "grid_paper_trades pnl_pct_decimal TEXT",
          "grid_paper_trades entry_filled_qty_decimal TEXT",
          "grid_paper_trades exit_filled_qty_decimal TEXT",
          "grid_paper_trades entry_fee_decimal TEXT",
          "grid_paper_trades exit_fee_decimal TEXT",
          "grid_paper_trades realized_pnl_pct_decimal TEXT",
        ]) {
          const [table, column, type] = tableColumn.split(" ");
          addColumn(`ALTER TABLE ${table} ADD COLUMN ${column} ${type}`);
        }

        // The CREATE TABLE IF NOT EXISTS above cannot add columns to a
        // watchlist table created by an older schema, so check pragma and
        // ALTER in any contract column that is still missing. Guarded per
        // column and re-runnable: only missing columns are touched.
        const watchlistContractColumns: Record<string, string> = {
          target_ratio: "REAL NOT NULL DEFAULT 1",
          chop_gate_adx: "REAL NOT NULL DEFAULT 0",
          oos_trades: "INTEGER NOT NULL DEFAULT 0",
          fills_per_day: "REAL NOT NULL DEFAULT 0",
          edge_per_trade_pct: "REAL NOT NULL DEFAULT 0",
          volatility: "REAL NOT NULL DEFAULT 0",
          allocated_weight: "REAL NOT NULL DEFAULT 0",
        };
        const existingWatchlistColumns = new Set(
          (
            this.db.query("PRAGMA table_info(watchlist)").all() as Array<{
              name: string;
            }>
          ).map((c) => c.name),
        );
        for (const [column, type] of Object.entries(
          watchlistContractColumns,
        )) {
          if (!existingWatchlistColumns.has(column)) {
            addColumn(`ALTER TABLE watchlist ADD COLUMN ${column} ${type}`);
          }
        }
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
            `SELECT id, exchange, symbol, timeframe, side,
                    COALESCE(entry_price_decimal, CAST(entry_price AS TEXT)) AS entry_price_value,
                    COALESCE(size_decimal, CAST(size AS TEXT)) AS size_value,
                    COALESCE(stop_loss_decimal, CAST(stop_loss AS TEXT)) AS stop_loss_value,
                    COALESCE(take_profit_decimal, CAST(take_profit AS TEXT)) AS take_profit_value,
                    opened_at, signal_id, scaled_out,
                    COALESCE(scale_out_price_decimal, CAST(scale_out_price AS TEXT)) AS scale_out_price_value
             FROM paper_positions
             WHERE exchange = ? AND symbol = ?`,
          )
          .get(exchange, symbol) as {
          id: string;
          exchange: string;
          symbol: string;
          timeframe: string;
          side: string;
          entry_price_value: string;
          size_value: string;
          stop_loss_value: string;
          opened_at: string;
          signal_id: string;
          scaled_out: number;
          take_profit_value: string;
          scale_out_price_value: string;
        } | null;

        if (!row) return null;

        return {
          id: row.id,
          exchange: row.exchange,
          symbol: row.symbol,
          timeframe: row.timeframe,
          side: row.side as PaperPosition["side"],
          entryPrice: new Decimal(row.entry_price_value),
          size: new Decimal(row.size_value),
          stopLoss: new Decimal(row.stop_loss_value),
          takeProfit: new Decimal(row.take_profit_value),
          openedAt: new Date(row.opened_at),
          signalId: row.signal_id,
          scaledOut: Boolean(row.scaled_out),
          scaleOutPrice: new Decimal(row.scale_out_price_value),
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
            (id, exchange, symbol, timeframe, side, entry_price, size, stop_loss, take_profit,
             entry_price_decimal, size_decimal, stop_loss_decimal, take_profit_decimal,
             opened_at, signal_id, scaled_out, scale_out_price, scale_out_price_decimal)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            position.id,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            toNumber(position.entryPrice),
            toNumber(position.size),
            toNumber(position.stopLoss),
            toNumber(position.takeProfit),
            position.entryPrice.toString(),
            position.size.toString(),
            position.stopLoss.toString(),
            position.takeProfit.toString(),
            position.openedAt.toISOString(),
            position.signalId,
            position.scaledOut ? 1 : 0,
            toNumber(position.scaleOutPrice),
            position.scaleOutPrice.toString(),
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
    exitPrice: Decimal,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ): Effect.Effect<PaperTrade, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const entryPrice = position.entryPrice;
        const exitPriceDec = exitPrice;
        const size = position.size;
        const priceDiff =
          position.side === "long"
            ? exitPriceDec.minus(entryPrice)
            : entryPrice.minus(exitPriceDec);
        const pnl = priceDiff.times(size);
        const pnlPct = pnl.div(entryPrice.times(size)).times(100);
        const tradeId = `paper-trade-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

        // Wrap trade insertion and position deletion in a transaction so the
        // close is atomic.
        this.db.transaction(() => {
          const insert = this.db.query(
            `INSERT INTO paper_trades
             (id, exchange, symbol, timeframe, side, entry_price, exit_price, size, pnl, pnl_pct,
              entry_price_decimal, exit_price_decimal, size_decimal, pnl_decimal, pnl_pct_decimal,
              exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          );
          insert.run(
            tradeId,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            toNumber(position.entryPrice),
            toNumber(exitPrice),
            toNumber(position.size),
            toNumber(pnl),
            toNumber(pnlPct),
            position.entryPrice.toString(),
            exitPrice.toString(),
            position.size.toString(),
            pnl.toString(),
            pnlPct.toString(),
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

  scaleOutPosition(
    position: PaperPosition,
    exitPrice: Decimal,
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
        const partialSize = position.size.times(pct / 100);
        if (partialSize.lessThanOrEqualTo(0)) {
          throw new Error("scale-out size must be positive");
        }

        const entryPrice = position.entryPrice;
        const exitPriceDec = exitPrice;
        const priceDiff =
          position.side === "long"
            ? exitPriceDec.minus(entryPrice)
            : entryPrice.minus(exitPriceDec);
        const pnl = priceDiff.times(partialSize);
        const pnlPct = pnl.div(entryPrice.times(partialSize)).times(100);
        const remainingSize = position.size.minus(partialSize);
        const tradeId = `paper-trade-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

        this.db.transaction(() => {
          const insert = this.db.query(
            `INSERT INTO paper_trades
             (id, exchange, symbol, timeframe, side, entry_price, exit_price, size, pnl, pnl_pct,
              entry_price_decimal, exit_price_decimal, size_decimal, pnl_decimal, pnl_pct_decimal,
              exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          );
          insert.run(
            tradeId,
            position.exchange,
            position.symbol,
            position.timeframe,
            position.side,
            toNumber(position.entryPrice),
            toNumber(exitPrice),
            toNumber(partialSize),
            toNumber(pnl),
            toNumber(pnlPct),
            position.entryPrice.toString(),
            exitPrice.toString(),
            partialSize.toString(),
            pnl.toString(),
            pnlPct.toString(),
            "scale_out",
            position.openedAt.toISOString(),
            closedAt.toISOString(),
          );

          this.db
            .query(
              `UPDATE paper_positions
               SET size = ?, stop_loss = ?, size_decimal = ?, stop_loss_decimal = ?, scaled_out = 1
               WHERE id = ?`,
            )
            .run(
              toNumber(remainingSize),
              toNumber(position.entryPrice),
              remainingSize.toString(),
              position.entryPrice.toString(),
              position.id,
            );
        })();

        const trade: PaperTrade = {
          id: tradeId,
          exchange: position.exchange,
          symbol: position.symbol,
          timeframe: position.timeframe,
          side: position.side,
          entryPrice: position.entryPrice,
          exitPrice,
          size: partialSize,
          pnl,
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
    { readonly capital: Decimal; readonly peakCapital: Decimal },
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const row = this.db
          .query(
            `SELECT COALESCE(capital_decimal, CAST(capital AS TEXT)) AS capital_value,
                    COALESCE(peak_capital_decimal, CAST(peak_capital AS TEXT)) AS peak_capital_value
             FROM paper_portfolio WHERE id = 1`,
          )
          .get() as {
          capital_value: string;
          peak_capital_value: string;
        } | null;

        return row
          ? {
              capital: new Decimal(row.capital_value),
              peakCapital: new Decimal(row.peak_capital_value),
            }
          : { capital: new Decimal(10_000), peakCapital: new Decimal(10_000) };
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to load portfolio: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  setPortfolio(
    capital: Decimal,
    peakCapital: Decimal,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `INSERT INTO paper_portfolio
             (id, capital, peak_capital, capital_decimal, peak_capital_decimal, updated_at)
             VALUES (1, ?, ?, ?, ?, ?)
             ON CONFLICT(id) DO UPDATE SET
               capital = excluded.capital,
               peak_capital = excluded.peak_capital,
               capital_decimal = excluded.capital_decimal,
               peak_capital_decimal = excluded.peak_capital_decimal,
               updated_at = excluded.updated_at`,
          )
          .run(
            toNumber(capital),
            toNumber(peakCapital),
            capital.toString(),
            peakCapital.toString(),
            new Date().toISOString(),
          );
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
            `SELECT id, exchange, symbol, timeframe, side,
                    COALESCE(entry_price_decimal, CAST(entry_price AS TEXT)) AS entry_price_value,
                    COALESCE(exit_price_decimal, CAST(exit_price AS TEXT)) AS exit_price_value,
                    COALESCE(size_decimal, CAST(size AS TEXT)) AS size_value,
                    COALESCE(pnl_decimal, CAST(pnl AS TEXT)) AS pnl_value,
                    COALESCE(pnl_pct_decimal, CAST(pnl_pct AS TEXT)) AS pnl_pct_value,
                    exit_reason, opened_at, closed_at
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
          entry_price_value: string;
          exit_price_value: string;
          size_value: string;
          pnl_value: string;
          pnl_pct_value: string;
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
          entryPrice: new Decimal(r.entry_price_value),
          exitPrice: new Decimal(r.exit_price_value),
          size: new Decimal(r.size_value),
          pnl: new Decimal(r.pnl_value),
          pnlPct: new Decimal(r.pnl_pct_value),
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
             FROM (
               SELECT closed_at FROM paper_trades
               UNION ALL
               SELECT closed_at FROM grid_paper_trades
             ) AS all_paper_trades
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
    Decimal,
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const paperRows = this.db
          .query(
            `SELECT COALESCE(pnl_decimal, CAST(pnl AS TEXT)) AS pnl_value
             FROM paper_trades
             WHERE date(closed_at) = date('now')`,
          )
          .all() as Array<{ pnl_value: string }>;
        const gridRows = this.db
          .query(
            `SELECT
               COALESCE(capital_after_decimal, CAST(capital_after AS TEXT)) AS capital_after_value,
               COALESCE(capital_before_decimal, CAST(capital_before AS TEXT)) AS capital_before_value
             FROM grid_paper_trades
             WHERE date(closed_at) = date('now')`,
          )
          .all() as Array<{
          capital_after_value: string;
          capital_before_value: string;
        }>;
        const paperPnl = paperRows.reduce(
          (total, row) => total.plus(new Decimal(row.pnl_value)),
          new Decimal(0),
        );
        return gridRows.reduce(
          (total, row) =>
            total
              .plus(new Decimal(row.capital_after_value))
              .minus(new Decimal(row.capital_before_value)),
          paperPnl,
        );
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
    currentCapital: Decimal,
  ): Effect.Effect<Decimal, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        const dateKey = date.toISOString().slice(0, 10);
        const row = this.db
          .query(
            `SELECT COALESCE(start_capital_decimal, CAST(start_capital AS TEXT)) AS start_capital_value
             FROM paper_start_of_day_capital WHERE date = ?`,
          )
          .get(dateKey) as { start_capital_value: string } | null;
        if (row) return new Decimal(row.start_capital_value);
        this.db
          .query(
            `INSERT INTO paper_start_of_day_capital
             (date, start_capital, start_capital_decimal, updated_at)
             VALUES (?, ?, ?, ?)
             ON CONFLICT(date) DO UPDATE SET
               start_capital = excluded.start_capital,
               start_capital_decimal = excluded.start_capital_decimal,
               updated_at = excluded.updated_at`,
          )
          .run(
            dateKey,
            toNumber(currentCapital),
            currentCapital.toString(),
            new Date().toISOString(),
          );
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
            `SELECT exchange, symbol, timeframe,
                    COALESCE(capital_decimal, CAST(capital AS TEXT)) AS capital_value,
                    COALESCE(peak_capital_decimal, CAST(peak_capital AS TEXT)) AS peak_capital_value,
                    paused, side,
                    COALESCE(entry_price_decimal, CAST(entry_price AS TEXT)) AS entry_price_value,
                    entry_order_id, entry_client_oid,
                    COALESCE(entry_filled_qty_decimal, CAST(entry_filled_qty AS TEXT)) AS entry_filled_qty_value,
                    COALESCE(entry_fee_decimal, CAST(entry_fee AS TEXT)) AS entry_fee_value,
                    entry_fill_source,
                    strategy_config_fingerprint, cohort_id, candidate_lock_at,
                    dataset_cutoff_at, entry_opened_at, execution_environment,
                    grid_step_pct, grid_max_grids, grid_pause_after_loss_bars,
                    fee_pct, slippage_bps, trend_filter_period, max_position_pct,
                    max_drawdown_pct, leverage, killed, last_timestamp, updated_at
             FROM grid_paper_state
             WHERE exchange = ? AND symbol = ? AND timeframe = ?`,
          )
          .get(exchange, symbol, timeframe) as {
          exchange: string;
          symbol: string;
          timeframe: string;
          capital_value: string;
          peak_capital_value: string;
          paused: number;
          side: string | null;
          entry_price_value: string | null;
          entry_order_id: string | null;
          entry_client_oid: string | null;
          entry_filled_qty_value: string | null;
          entry_fee_value: string | null;
          entry_fill_source: "simulated" | "live" | null;
          strategy_config_fingerprint: string | null;
          cohort_id: string | null;
          candidate_lock_at: string | null;
          dataset_cutoff_at: string | null;
          entry_opened_at: string | null;
          execution_environment: "bitget-demo" | "bitget-live" | null;
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
          capital: new Decimal(row.capital_value),
          peakCapital: new Decimal(row.peak_capital_value),
          paused: row.paused,
          side: (row.side as GridPaperState["side"]) ?? null,
          entryPrice: new Decimal(row.entry_price_value ?? 0),
          entryOrderId: row.entry_order_id ?? undefined,
          entryClientOid: row.entry_client_oid ?? undefined,
          entryFilledQty: row.entry_filled_qty_value
            ? new Decimal(row.entry_filled_qty_value)
            : undefined,
          entryFee: row.entry_fee_value
            ? new Decimal(row.entry_fee_value)
            : undefined,
          entryFillSource: row.entry_fill_source ?? undefined,
          strategyConfigFingerprint:
            row.strategy_config_fingerprint ?? undefined,
          cohortId: row.cohort_id ?? undefined,
          candidateLockAt: row.candidate_lock_at
            ? new Date(row.candidate_lock_at)
            : undefined,
          datasetCutoffAt: row.dataset_cutoff_at
            ? new Date(row.dataset_cutoff_at)
            : undefined,
          entryOpenedAt: row.entry_opened_at
            ? new Date(row.entry_opened_at)
            : undefined,
          executionEnvironment: row.execution_environment ?? undefined,
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
             (exchange, symbol, timeframe, capital, peak_capital, capital_decimal, peak_capital_decimal, paused, side,
              entry_price, entry_price_decimal, entry_order_id, entry_client_oid, entry_filled_qty,
              entry_filled_qty_decimal, entry_fee, entry_fee_decimal, entry_fill_source,
              strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
              entry_opened_at, execution_environment,
              grid_step_pct, grid_max_grids, grid_pause_after_loss_bars,
              fee_pct, slippage_bps, trend_filter_period, max_position_pct,
              max_drawdown_pct, leverage, killed, last_timestamp, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
                     ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
                     ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(exchange, symbol, timeframe) DO UPDATE SET
               capital = excluded.capital,
               peak_capital = excluded.peak_capital,
               capital_decimal = excluded.capital_decimal,
               peak_capital_decimal = excluded.peak_capital_decimal,
               paused = excluded.paused,
               side = excluded.side,
               entry_price = excluded.entry_price,
               entry_price_decimal = excluded.entry_price_decimal,
               entry_order_id = excluded.entry_order_id,
               entry_client_oid = excluded.entry_client_oid,
               entry_filled_qty = excluded.entry_filled_qty,
               entry_filled_qty_decimal = excluded.entry_filled_qty_decimal,
               entry_fee = excluded.entry_fee,
               entry_fee_decimal = excluded.entry_fee_decimal,
               entry_fill_source = excluded.entry_fill_source,
               strategy_config_fingerprint = excluded.strategy_config_fingerprint,
               cohort_id = excluded.cohort_id,
               candidate_lock_at = excluded.candidate_lock_at,
               dataset_cutoff_at = excluded.dataset_cutoff_at,
               entry_opened_at = excluded.entry_opened_at,
               execution_environment = excluded.execution_environment,
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
            toNumber(state.capital),
            toNumber(state.peakCapital),
            state.capital.toString(),
            state.peakCapital.toString(),
            state.paused,
            state.side,
            toNumber(state.entryPrice),
            state.entryPrice.toString(),
            state.entryOrderId ?? null,
            state.entryClientOid ?? null,
            state.entryFilledQty ? toNumber(state.entryFilledQty) : null,
            state.entryFilledQty?.toString() ?? null,
            state.entryFee ? toNumber(state.entryFee) : null,
            state.entryFee?.toString() ?? null,
            state.entryFillSource ?? null,
            state.strategyConfigFingerprint ?? null,
            state.cohortId ?? null,
            state.candidateLockAt?.toISOString() ?? null,
            state.datasetCutoffAt?.toISOString() ?? null,
            state.entryOpenedAt?.toISOString() ?? null,
            state.executionEnvironment ?? null,
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
              capital_before, capital_after, pnl_pct,
              entry_price_decimal, exit_price_decimal, capital_before_decimal,
              capital_after_decimal, pnl_pct_decimal, fill_source, entry_order_id,
              entry_client_oid, exit_order_id, exit_client_oid, entry_filled_qty,
              entry_filled_qty_decimal, exit_filled_qty, exit_filled_qty_decimal,
              entry_fee, entry_fee_decimal, exit_fee, exit_fee_decimal,
              realized_pnl_pct, realized_pnl_pct_decimal,
              strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
              entry_opened_at, execution_environment, exit_reason, opened_at, closed_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            trade.id,
            trade.exchange,
            trade.symbol,
            trade.timeframe,
            trade.side,
            toNumber(trade.entryPrice),
            toNumber(trade.exitPrice),
            toNumber(trade.capitalBefore),
            toNumber(trade.capitalAfter),
            toNumber(trade.pnlPct),
            trade.entryPrice.toString(),
            trade.exitPrice.toString(),
            trade.capitalBefore.toString(),
            trade.capitalAfter.toString(),
            trade.pnlPct.toString(),
            trade.fillSource ?? null,
            trade.entryOrderId ?? null,
            trade.entryClientOid ?? null,
            trade.exitOrderId ?? null,
            trade.exitClientOid ?? null,
            trade.entryFilledQty ? toNumber(trade.entryFilledQty) : null,
            trade.entryFilledQty?.toString() ?? null,
            trade.exitFilledQty ? toNumber(trade.exitFilledQty) : null,
            trade.exitFilledQty?.toString() ?? null,
            trade.entryFee ? toNumber(trade.entryFee) : null,
            trade.entryFee?.toString() ?? null,
            trade.exitFee ? toNumber(trade.exitFee) : null,
            trade.exitFee?.toString() ?? null,
            trade.realizedPnlPct ? toNumber(trade.realizedPnlPct) : null,
            trade.realizedPnlPct?.toString() ?? null,
            trade.strategyConfigFingerprint ?? null,
            trade.cohortId ?? null,
            trade.candidateLockAt?.toISOString() ?? null,
            trade.datasetCutoffAt?.toISOString() ?? null,
            trade.entryOpenedAt?.toISOString() ?? null,
            trade.executionEnvironment ?? null,
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
            `SELECT id, exchange, symbol, timeframe, side,
                    COALESCE(entry_price_decimal, CAST(entry_price AS TEXT)) AS entry_price_value,
                    COALESCE(exit_price_decimal, CAST(exit_price AS TEXT)) AS exit_price_value,
                    COALESCE(capital_before_decimal, CAST(capital_before AS TEXT)) AS capital_before_value,
                    COALESCE(capital_after_decimal, CAST(capital_after AS TEXT)) AS capital_after_value,
                    COALESCE(pnl_pct_decimal, CAST(pnl_pct AS TEXT)) AS pnl_pct_value,
                    fill_source, entry_order_id, entry_client_oid, exit_order_id, exit_client_oid,
                    COALESCE(entry_filled_qty_decimal, CAST(entry_filled_qty AS TEXT)) AS entry_filled_qty_value,
                    COALESCE(exit_filled_qty_decimal, CAST(exit_filled_qty AS TEXT)) AS exit_filled_qty_value,
                    COALESCE(entry_fee_decimal, CAST(entry_fee AS TEXT)) AS entry_fee_value,
                    COALESCE(exit_fee_decimal, CAST(exit_fee AS TEXT)) AS exit_fee_value,
                    COALESCE(realized_pnl_pct_decimal, CAST(realized_pnl_pct AS TEXT)) AS realized_pnl_pct_value,
                    strategy_config_fingerprint, cohort_id, candidate_lock_at,
                    dataset_cutoff_at, entry_opened_at, execution_environment,
                    exit_reason, opened_at, closed_at
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
          entry_price_value: string;
          exit_price_value: string;
          capital_before_value: string;
          capital_after_value: string;
          pnl_pct_value: string;
          fill_source: "simulated" | "live" | null;
          entry_order_id: string | null;
          entry_client_oid: string | null;
          exit_order_id: string | null;
          exit_client_oid: string | null;
          entry_filled_qty_value: string | null;
          exit_filled_qty_value: string | null;
          entry_fee_value: string | null;
          exit_fee_value: string | null;
          realized_pnl_pct_value: string | null;
          strategy_config_fingerprint: string | null;
          cohort_id: string | null;
          candidate_lock_at: string | null;
          dataset_cutoff_at: string | null;
          entry_opened_at: string | null;
          execution_environment: "bitget-demo" | "bitget-live" | null;
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
          entryPrice: new Decimal(r.entry_price_value),
          exitPrice: new Decimal(r.exit_price_value),
          capitalBefore: new Decimal(r.capital_before_value),
          capitalAfter: new Decimal(r.capital_after_value),
          pnlPct: new Decimal(r.pnl_pct_value),
          fillSource: r.fill_source ?? undefined,
          entryOrderId: r.entry_order_id ?? undefined,
          entryClientOid: r.entry_client_oid ?? undefined,
          exitOrderId: r.exit_order_id ?? undefined,
          exitClientOid: r.exit_client_oid ?? undefined,
          entryFilledQty: r.entry_filled_qty_value
            ? new Decimal(r.entry_filled_qty_value)
            : undefined,
          exitFilledQty: r.exit_filled_qty_value
            ? new Decimal(r.exit_filled_qty_value)
            : undefined,
          entryFee: r.entry_fee_value
            ? new Decimal(r.entry_fee_value)
            : undefined,
          exitFee: r.exit_fee_value ? new Decimal(r.exit_fee_value) : undefined,
          realizedPnlPct: r.realized_pnl_pct_value
            ? new Decimal(r.realized_pnl_pct_value)
            : undefined,
          strategyConfigFingerprint: r.strategy_config_fingerprint ?? undefined,
          cohortId: r.cohort_id ?? undefined,
          candidateLockAt: r.candidate_lock_at
            ? new Date(r.candidate_lock_at)
            : undefined,
          datasetCutoffAt: r.dataset_cutoff_at
            ? new Date(r.dataset_cutoff_at)
            : undefined,
          entryOpenedAt: r.entry_opened_at
            ? new Date(r.entry_opened_at)
            : undefined,
          executionEnvironment: r.execution_environment ?? undefined,
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

  listWatchlist(
    exchange: string,
    timeframe: string,
  ): Effect.Effect<
    readonly WatchlistEntry[],
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const rows = this.db
          .query(
            `SELECT exchange, symbol, timeframe, return_pct,
                    profitable_windows_pct, aggregate_return_pct,
                    grid_step_pct, grid_max_grids, grid_pause_after_loss_bars,
                    target_ratio, chop_gate_adx, oos_trades,
                    fills_per_day, edge_per_trade_pct, volatility,
                    allocated_weight, updated_at
             FROM watchlist
             WHERE exchange = ? AND timeframe = ?
             ORDER BY aggregate_return_pct DESC`,
          )
          .all(exchange, timeframe) as Array<{
          exchange: string;
          symbol: string;
          timeframe: string;
          return_pct: number;
          profitable_windows_pct: number;
          aggregate_return_pct: number;
          grid_step_pct: number;
          grid_max_grids: number;
          grid_pause_after_loss_bars: number;
          target_ratio: number | null;
          chop_gate_adx: number | null;
          oos_trades: number | null;
          fills_per_day: number | null;
          edge_per_trade_pct: number | null;
          volatility: number | null;
          allocated_weight: number | null;
          updated_at: string;
        }>;

        return rows.map((r) => ({
          exchange: r.exchange,
          symbol: r.symbol,
          timeframe: r.timeframe,
          returnPct: r.return_pct,
          profitableWindowsPct: r.profitable_windows_pct,
          aggregateReturnPct: r.aggregate_return_pct,
          gridStepPct: r.grid_step_pct,
          gridMaxGrids: r.grid_max_grids,
          gridPauseAfterLossBars: r.grid_pause_after_loss_bars,
          targetRatio: r.target_ratio ?? 1,
          chopGateAdx: r.chop_gate_adx ?? 0,
          oosTrades: r.oos_trades ?? 0,
          fillsPerDay: r.fills_per_day ?? 0,
          edgePerTradePct: r.edge_per_trade_pct ?? 0,
          volatility: r.volatility ?? 0,
          allocatedWeight: r.allocated_weight ?? 0,
          updatedAt: new Date(r.updated_at),
        }));
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to list watchlist: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  private runWatchlistUpsert(entries: readonly WatchlistEntry[]): void {
    const upsert = this.db.query(
      `INSERT INTO watchlist
       (exchange, symbol, timeframe, return_pct, profitable_windows_pct,
        aggregate_return_pct, grid_step_pct, grid_max_grids,
        grid_pause_after_loss_bars, target_ratio, chop_gate_adx, oos_trades,
        fills_per_day, edge_per_trade_pct, volatility, allocated_weight,
        updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(exchange, symbol, timeframe) DO UPDATE SET
         return_pct = excluded.return_pct,
         profitable_windows_pct = excluded.profitable_windows_pct,
         aggregate_return_pct = excluded.aggregate_return_pct,
         grid_step_pct = excluded.grid_step_pct,
         grid_max_grids = excluded.grid_max_grids,
         grid_pause_after_loss_bars = excluded.grid_pause_after_loss_bars,
         target_ratio = excluded.target_ratio,
         chop_gate_adx = excluded.chop_gate_adx,
         oos_trades = excluded.oos_trades,
         fills_per_day = excluded.fills_per_day,
         edge_per_trade_pct = excluded.edge_per_trade_pct,
         volatility = excluded.volatility,
         allocated_weight = excluded.allocated_weight,
         updated_at = excluded.updated_at`,
    );
    for (const e of entries) {
      upsert.run(
        e.exchange,
        e.symbol,
        e.timeframe,
        e.returnPct,
        e.profitableWindowsPct,
        e.aggregateReturnPct,
        e.gridStepPct,
        e.gridMaxGrids,
        e.gridPauseAfterLossBars,
        e.targetRatio,
        e.chopGateAdx,
        e.oosTrades,
        e.fillsPerDay,
        e.edgePerTradePct,
        e.volatility,
        e.allocatedWeight,
        e.updatedAt.toISOString(),
      );
    }
  }

  upsertWatchlist(
    entries: readonly WatchlistEntry[],
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db.transaction(() => {
          this.runWatchlistUpsert(entries);
        })();
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to upsert watchlist: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  replaceWatchlist(
    exchange: string,
    timeframe: string,
    entries: readonly WatchlistEntry[],
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db.transaction(() => {
          this.db
            .query("DELETE FROM watchlist WHERE exchange = ? AND timeframe = ?")
            .run(exchange, timeframe);
          this.runWatchlistUpsert(entries);
        })();
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to replace watchlist: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  clearWatchlist(
    exchange: string,
    timeframe: string,
  ): Effect.Effect<void, PaperTradingRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query("DELETE FROM watchlist WHERE exchange = ? AND timeframe = ?")
          .run(exchange, timeframe);
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to clear watchlist: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  listAllGridTrades(
    exchange: string,
    timeframe: string,
    limit: number,
    liveOnly = false,
  ): Effect.Effect<
    readonly GridPaperTrade[],
    PaperTradingRepositoryError,
    never
  > {
    return Effect.try({
      try: () => {
        const rows = this.db
          .query(
            `SELECT id, exchange, symbol, timeframe, side,
                    COALESCE(entry_price_decimal, CAST(entry_price AS TEXT)) AS entry_price_value,
                    COALESCE(exit_price_decimal, CAST(exit_price AS TEXT)) AS exit_price_value,
                    COALESCE(capital_before_decimal, CAST(capital_before AS TEXT)) AS capital_before_value,
                    COALESCE(capital_after_decimal, CAST(capital_after AS TEXT)) AS capital_after_value,
                    COALESCE(pnl_pct_decimal, CAST(pnl_pct AS TEXT)) AS pnl_pct_value,
                    fill_source, entry_order_id, entry_client_oid, exit_order_id, exit_client_oid,
                    COALESCE(entry_filled_qty_decimal, CAST(entry_filled_qty AS TEXT)) AS entry_filled_qty_value,
                    COALESCE(exit_filled_qty_decimal, CAST(exit_filled_qty AS TEXT)) AS exit_filled_qty_value,
                    COALESCE(entry_fee_decimal, CAST(entry_fee AS TEXT)) AS entry_fee_value,
                    COALESCE(exit_fee_decimal, CAST(exit_fee AS TEXT)) AS exit_fee_value,
                    COALESCE(realized_pnl_pct_decimal, CAST(realized_pnl_pct AS TEXT)) AS realized_pnl_pct_value,
                    strategy_config_fingerprint, cohort_id, candidate_lock_at,
                    dataset_cutoff_at, entry_opened_at, execution_environment,
                    exit_reason, opened_at, closed_at
             FROM grid_paper_trades
             WHERE exchange = ? AND timeframe = ?${liveOnly ? " AND fill_source = 'live'" : ""}
             ORDER BY closed_at DESC
             LIMIT ?`,
          )
          .all(exchange, timeframe, limit) as Array<{
          id: string;
          exchange: string;
          symbol: string;
          timeframe: string;
          side: string;
          entry_price_value: string;
          exit_price_value: string;
          capital_before_value: string;
          capital_after_value: string;
          pnl_pct_value: string;
          fill_source: "simulated" | "live" | null;
          entry_order_id: string | null;
          entry_client_oid: string | null;
          exit_order_id: string | null;
          exit_client_oid: string | null;
          entry_filled_qty_value: string | null;
          exit_filled_qty_value: string | null;
          entry_fee_value: string | null;
          exit_fee_value: string | null;
          realized_pnl_pct_value: string | null;
          strategy_config_fingerprint: string | null;
          cohort_id: string | null;
          candidate_lock_at: string | null;
          dataset_cutoff_at: string | null;
          entry_opened_at: string | null;
          execution_environment: "bitget-demo" | "bitget-live" | null;
          exit_reason: string;
          opened_at: string;
          closed_at: string;
        }>;

        return rows.map((r) => ({
          id: r.id,
          exchange: r.exchange,
          symbol: r.symbol,
          timeframe: r.timeframe,
          side: r.side as GridPaperTrade["side"],
          entryPrice: new Decimal(r.entry_price_value),
          exitPrice: new Decimal(r.exit_price_value),
          capitalBefore: new Decimal(r.capital_before_value),
          capitalAfter: new Decimal(r.capital_after_value),
          pnlPct: new Decimal(r.pnl_pct_value),
          fillSource: r.fill_source ?? undefined,
          entryOrderId: r.entry_order_id ?? undefined,
          entryClientOid: r.entry_client_oid ?? undefined,
          exitOrderId: r.exit_order_id ?? undefined,
          exitClientOid: r.exit_client_oid ?? undefined,
          entryFilledQty: r.entry_filled_qty_value
            ? new Decimal(r.entry_filled_qty_value)
            : undefined,
          exitFilledQty: r.exit_filled_qty_value
            ? new Decimal(r.exit_filled_qty_value)
            : undefined,
          entryFee: r.entry_fee_value
            ? new Decimal(r.entry_fee_value)
            : undefined,
          exitFee: r.exit_fee_value ? new Decimal(r.exit_fee_value) : undefined,
          realizedPnlPct: r.realized_pnl_pct_value
            ? new Decimal(r.realized_pnl_pct_value)
            : undefined,
          strategyConfigFingerprint: r.strategy_config_fingerprint ?? undefined,
          cohortId: r.cohort_id ?? undefined,
          candidateLockAt: r.candidate_lock_at
            ? new Date(r.candidate_lock_at)
            : undefined,
          datasetCutoffAt: r.dataset_cutoff_at
            ? new Date(r.dataset_cutoff_at)
            : undefined,
          entryOpenedAt: r.entry_opened_at
            ? new Date(r.entry_opened_at)
            : undefined,
          executionEnvironment: r.execution_environment ?? undefined,
          exitReason: r.exit_reason as GridPaperTrade["exitReason"],
          openedAt: new Date(r.opened_at),
          closedAt: new Date(r.closed_at),
        }));
      },
      catch: (err) =>
        new PaperTradingRepositoryError(
          `Failed to list all grid trades: ${err instanceof Error ? err.message : String(err)}`,
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
