import { Command, Options } from "./kit/kit.ts";
import { Console, Effect, Layer } from "effect";
import {
  BybitClient,
  BybitClientLiveConfig,
  type BybitClosedPnl,
  type BybitOrder,
  type BybitPosition,
} from "../exchange/adapters/bybit-futures.js";
import { BybitConfigLive } from "../services/bybit-config.js";
import { money, type Money } from "../utils/money.js";

const limitOption = Options.integer("limit").pipe(
  Options.withDefault(100),
  Options.withDescription("Recent closed-PnL rows to request from Bybit"),
);

const startingEquityOption = Options.text("starting-equity").pipe(
  Options.withDefault("50"),
  Options.withDescription("Paper-testnet starting equity used for drawdown"),
);

const jsonOption = Options.boolean("json").pipe(
  Options.withDefault(false),
  Options.withDescription("Print machine-readable JSON"),
);

interface SymbolClosedSummary {
  readonly symbol: string;
  readonly closedCount: number;
  readonly fills: number;
  readonly reportedClosedPnl: Money;
  readonly fees: Money;
}

function sumClosedPnl(rows: readonly BybitClosedPnl[]): SymbolClosedSummary[] {
  const grouped = new Map<
    string,
    { count: number; fills: number; pnl: Money; fees: Money }
  >();
  for (const row of rows) {
    const current = grouped.get(row.symbol) ?? {
      count: 0,
      fills: 0,
      pnl: money(0),
      fees: money(0),
    };
    grouped.set(row.symbol, {
      count: current.count + 1,
      fills: current.fills + Math.max(0, Number(row.fillCount) || 0),
      pnl: current.pnl.plus(money(row.closedPnl)),
      fees: current.fees.plus(money(row.openFee)).plus(money(row.closeFee)),
    });
  }
  return [...grouped.entries()]
    .map(([symbol, value]) => ({
      symbol,
      closedCount: value.count,
      fills: value.fills,
      reportedClosedPnl: value.pnl,
      fees: value.fees,
    }))
    .sort((left, right) => right.reportedClosedPnl.comparedTo(left.reportedClosedPnl));
}

function positionJson(position: BybitPosition) {
  return {
    symbol: position.symbol,
    side: position.side,
    size: position.size,
    avgEntryPrice: position.avgPrice,
    markPrice: position.markPrice,
    unrealizedPnl: position.unrealisedPnl,
    leverage: position.leverage,
    liquidationPrice: position.liqPrice,
    positionIdx: position.positionIdx,
  };
}

function orderJson(order: BybitOrder) {
  return {
    orderId: order.orderId,
    symbol: order.symbol,
    side: order.side,
    type: order.orderType,
    status: order.orderStatus,
    qty: order.qty,
    price: order.price,
    avgPrice: order.avgPrice,
    filledQty: order.cumExecQty,
    fee: order.cumExecFee,
  };
}

export const bybitSnapshotCommand = Command.make(
  "bybit-snapshot",
  {
    limit: limitOption,
    startingEquity: startingEquityOption,
    json: jsonOption,
  },
  (args) =>
    Effect.gen(function* () {
      if (args.limit < 1 || args.limit > 100) {
        return yield* Effect.fail(new Error("--limit must be between 1 and 100"));
      }
      const startingEquity = money(args.startingEquity);
      if (startingEquity.isNaN() || startingEquity.lessThanOrEqualTo(0)) {
        return yield* Effect.fail(new Error("--starting-equity must be positive"));
      }

      const client = yield* BybitClient;
      const [coins, positions, closedPnl, openOrders] = yield* Effect.all([
        client.getBalance(),
        client.getPositions(),
        client.getClosedPnl({ limit: args.limit }),
        client.getOpenOrders(),
      ]);
      const usdt = coins.find((coin) => coin.coin.toUpperCase() === "USDT");
      const activePositions = positions.filter((position) => Number(position.size) > 0);
      const closedBySymbol = sumClosedPnl(closedPnl);
      const equity = money(usdt?.equity ?? 0);
      const report = {
        observedAt: new Date().toISOString(),
        account: {
          asset: "USDT",
          equity: equity.toString(),
          walletBalance: usdt?.walletBalance ?? "0",
          availableToWithdraw: usdt?.availableToWithdraw ?? "0",
          usdValue: usdt?.usdValue ?? "0",
          deltaFromStartingEquity: equity.minus(startingEquity).toString(),
          drawdownPct: startingEquity
            .minus(equity)
            .div(startingEquity)
            .times(100)
            .toString(),
        },
        openPositions: activePositions.map(positionJson),
        openOrders: openOrders.map(orderJson),
        closedPnl: {
          rows: closedPnl.length,
          bySymbol: closedBySymbol.map((row) => ({
            symbol: row.symbol,
            closedCount: row.closedCount,
            fills: row.fills,
            reportedClosedPnl: row.reportedClosedPnl.toString(),
            fees: row.fees.toString(),
          })),
        },
      };

      if (args.json) {
        yield* Console.log(JSON.stringify(report));
        return report;
      }

      yield* Console.log(`Bybit account snapshot @ ${report.observedAt}`);
      yield* Console.log(
        `USDT equity=${report.account.equity} wallet=${report.account.walletBalance} ` +
          `delta=${report.account.deltaFromStartingEquity} drawdown=${report.account.drawdownPct}%`,
      );
      yield* Console.log(`Open positions: ${activePositions.length}`);
      for (const position of activePositions) {
        yield* Console.log(
          `  ${position.symbol} ${position.side} size=${position.size} ` +
            `entry=${position.avgPrice} mark=${position.markPrice} ` +
            `uPnL=${position.unrealisedPnl} lev=${position.leverage}x`,
        );
      }
      yield* Console.log(`Open orders: ${openOrders.length}`);
      yield* Console.log(`Recent closed PnL rows: ${closedPnl.length}`);
      for (const row of closedBySymbol) {
        yield* Console.log(
          `  ${row.symbol} closed=${row.closedCount} fills=${row.fills} ` +
            `reportedPnL=${row.reportedClosedPnl.toString()} fees=${row.fees.toString()}`,
        );
      }
      return report;
    }).pipe(
      Effect.provide(
        Layer.provide(BybitClientLiveConfig, BybitConfigLive),
      ),
    ),
).pipe(
  Command.withDescription(
    "Read-only Bybit account, position, order, and recent closed-PnL snapshot",
  ),
);
