export interface GridSweepRow {
  readonly tradesPerMonth: number;
  readonly profitFactor: number;
  readonly winRate: number;
  readonly oosTrades: number;
  readonly oosReturnPct: number;
  readonly maxDdPct: number;
}

export interface GridSweepFloors {
  readonly minimumTradesPerMonth: number;
  readonly minimumProfitFactor: number;
  readonly minimumWinRatePct: number;
  readonly minimumOosTrades: number;
  readonly minimumOosReturnPct: number;
  readonly maximumDrawdownPct: number;
}

export const DEFAULT_GRID_SWEEP_FLOORS = {
  minimumTradesPerMonth: 10,
  minimumProfitFactor: 1.3,
  minimumWinRatePct: 50,
  minimumOosTrades: 10,
  minimumOosReturnPct: 0,
  maximumDrawdownPct: 15,
} as const satisfies GridSweepFloors;

export function passesGridSweepFloors(
  row: GridSweepRow,
  floors: GridSweepFloors = DEFAULT_GRID_SWEEP_FLOORS,
): boolean {
  return (
    row.tradesPerMonth >= floors.minimumTradesPerMonth &&
    row.profitFactor >= floors.minimumProfitFactor &&
    row.winRate >= floors.minimumWinRatePct &&
    row.oosTrades >= floors.minimumOosTrades &&
    row.oosReturnPct >= floors.minimumOosReturnPct &&
    row.maxDdPct <= floors.maximumDrawdownPct
  );
}
