import { Effect, Layer } from "effect";
import { createHmac } from "node:crypto";
import { ExchangeAdapter, ExchangeError, type ExchangeAdapterService } from "../adapter.js";
import type { Balance, OrderFill, OrderRequest, Position } from "../types.js";

const TESTNET_BASE_URL = "https://testnet.binance.vision";
const LIVE_BASE_URL = "https://api.binance.com";
const DEFAULT_TIMEOUT_MS = 30_000;

export interface BinanceLiveAdapterConfig {
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly live?: boolean;
}

export function makeBinanceLiveAdapter(config: BinanceLiveAdapterConfig): ExchangeAdapterService {
  const baseUrl = config.live ? LIVE_BASE_URL : TESTNET_BASE_URL;

  function signedRequest<T>(
    method: "GET" | "POST",
    path: string,
    params: Record<string, string | number>,
  ): Effect.Effect<T, ExchangeError, never> {
    return Effect.gen(function* () {
      const timestamp = Date.now();
      const query = new URLSearchParams(
        Object.entries({ ...params, timestamp }).map(([k, v]) => [k, String(v)]),
      ).toString();
      const signature = createHmac("sha256", config.apiSecret).update(query).digest("hex");
      const url = `${baseUrl}${path}?${query}&signature=${signature}`;

      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS);

      const response = yield* Effect.tryPromise({
        try: () =>
          fetch(url, {
            method,
            signal: controller.signal,
            headers: {
              Accept: "application/json",
              "X-MBX-APIKEY": config.apiKey,
              "Content-Type": "application/x-www-form-urlencoded",
            },
          }),
        catch: (err) =>
          new ExchangeError(
            `Binance network error: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      }).pipe(Effect.tap(() => Effect.sync(() => clearTimeout(timer))));

      if (!response.ok) {
        const body = yield* Effect.tryPromise({
          try: () => response.text(),
          catch: (err) =>
            new ExchangeError(
              `Binance response read error: ${err instanceof Error ? err.message : String(err)}`,
              err,
            ),
        });
        return yield* Effect.fail(
          new ExchangeError(`Binance HTTP ${response.status}: ${body.slice(0, 200)}`),
        );
      }

      return yield* Effect.tryPromise({
        try: () => response.json() as Promise<T>,
        catch: (err) =>
          new ExchangeError(
            `Binance JSON parse error: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }

  return {
    placeOrder: (request) =>
      Effect.gen(function* () {
        if (!config.apiKey || !config.apiSecret) {
          return yield* Effect.fail(
            new ExchangeError("Binance API key and secret are required"),
          );
        }

        const data = yield* signedRequest<{
          orderId: number;
          symbol: string;
          side: string;
          executedQty: string;
          fills: Array<{ price: string; qty: string; commission: string }>;
          transactTime: number;
        }>("POST", "/api/v3/order", {
          symbol: binanceSymbol(request.symbol),
          side: request.side.toUpperCase(),
          type: request.type.toUpperCase(),
          quantity: request.quantity,
        });

        const totalQty = data.fills.reduce((sum, f) => sum + Number(f.qty), 0);
        const totalCost = data.fills.reduce((sum, f) => sum + Number(f.price) * Number(f.qty), 0);
        const avgPrice = totalQty > 0 ? totalCost / totalQty : 0;
        const fee = data.fills.reduce((sum, f) => sum + Number(f.commission), 0);

        return {
          orderId: String(data.orderId),
          symbol: request.symbol,
          side: request.side,
          filledQty: totalQty,
          filledPrice: avgPrice,
          fee,
          timestamp: new Date(data.transactTime),
        };
      }),

    getBalance: (asset) =>
      Effect.gen(function* () {
        if (!config.apiKey || !config.apiSecret) {
          return yield* Effect.fail(
            new ExchangeError("Binance API key and secret are required"),
          );
        }

        const data = yield* signedRequest<{ balances: Array<{ asset: string; free: string; locked: string }> }>(
          "GET",
          "/api/v3/account",
          {},
        );

        const match = data.balances.find((b) => b.asset === asset.toUpperCase());
        return {
          asset,
          free: match ? Number(match.free) : 0,
          locked: match ? Number(match.locked) : 0,
        };
      }),

    getPosition: (_symbol) => Effect.succeed(null),

    closePosition: (symbol) =>
      Effect.gen(function* () {
        const balances = yield* signedRequest<{ balances: Array<{ asset: string; free: string }> }>(
          "GET",
          "/api/v3/account",
          {},
        );

        const baseAsset = symbol.split("/")[0]?.toUpperCase() ?? "";
        const match = balances.balances.find((b) => b.asset === baseAsset);
        const qty = match ? Number(match.free) : 0;

        if (qty <= 0) return null;

        return yield* makeBinanceLiveAdapter(config).placeOrder({
          symbol,
          side: "sell",
          type: "market",
          quantity: qty,
        });
      }),
  };
}

function binanceSymbol(symbol: string): string {
  return symbol.replace("/", "").toUpperCase();
}

export const BinanceLiveExchangeAdapterLive = (config: BinanceLiveAdapterConfig) =>
  Layer.succeed(ExchangeAdapter, makeBinanceLiveAdapter(config));
