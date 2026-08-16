import { Effect, Layer } from "effect";
import { BybitConfigLive, BybitConfig } from "../src/services/bybit-config.js";
import { BybitClient, BybitClientLiveConfig } from "../src/exchange/adapters/bybit-futures.js";

const program = Effect.gen(function* () {
  const config = yield* BybitConfig;
  console.log(`venue: ${config.useTestnet ? "TESTNET" : "LIVE"}`);
  const client = yield* BybitClient;

  const balance = yield* client.getBalance().pipe(Effect.result);
  if (balance._tag === "Failure") {
    console.log("balance fetch:", "ERR", String(balance.failure._tag));
  } else {
    const usdt = balance.success.find((c) => c.coin.toUpperCase() === "USDT");
    console.log("USDT balance:", usdt ? `available=${usdt.availableToWithdraw} wallet=${usdt.walletBalance}` : "not reported");
  }

  for (const symbol of ["FARTCOINUSDT", "NEARUSDT", "EPICUSDT", "KAITOUSDT"]) {
    const pos = yield* client.getPositions(symbol).pipe(Effect.result);
    if (pos._tag === "Success" && pos.success.length > 0) {
      for (const p of pos.success) {
        console.log(`POSITION ${symbol}: side=${p.side} qty=${p.size} entry=${p.avgPrice} pnl=${p.unrealisedPnl}`);
      }
    }
    const orders = yield* client.getOpenOrders(symbol).pipe(Effect.result);
    if (orders._tag === "Success" && orders.success.length > 0) {
      for (const o of orders.success) {
        console.log(`OPEN ORDER ${symbol}: side=${o.side} type=${o.orderType} qty=${o.qty} price=${o.price} status=${o.orderStatus}`);
      }
    }
  }
  console.log("done");
});

Effect.runPromise(
  program.pipe(
    Effect.provide(Layer.mergeAll(BybitConfigLive, Layer.provide(BybitClientLiveConfig, BybitConfigLive))),
  ),
).then(() => process.exit(0), (e) => { console.error("FAILED:", e); process.exit(1); });
