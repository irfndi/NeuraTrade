import { Effect, Layer } from "effect";
import { BybitConfigLive, BybitConfig } from "../src/services/bybit-config.js";
import { BybitClient, BybitClientLiveConfig } from "../src/exchange/adapters/bybit-futures.js";

// Market-close orphan positions (reduce-only) to free the account margin.
const closes: ReadonlyArray<{ symbol: string; side: "Sell" | "Buy"; qty: string }> = [
  { symbol: "FARTCOINUSDT", side: "Buy", qty: "176" }, // close short
  { symbol: "NEARUSDT", side: "Sell", qty: "15.6" }, // close long
  { symbol: "ENAUSDT", side: "Buy", qty: "3" }, // close short
];

const program = Effect.gen(function* () {
  const client = yield* BybitClient;
  for (const c of closes) {
    const r = yield* client
      .placeOrder({ category: "linear", symbol: c.symbol, side: c.side, orderType: "Market", qty: c.qty, reduceOnly: true, timeInForce: "IOC" })
      .pipe(Effect.result);
    if (r._tag === "Failure") {
      console.log(`CLOSE ${c.symbol}: FAILED ${JSON.stringify({ code: (r.failure as any).code, body: (r.failure as any).body })}`);
    } else {
      console.log(`CLOSE ${c.symbol}: OK orderId=${r.success.orderId}`);
    }
  }
  for (const c of closes) {
    const pos = yield* client.getPositions(c.symbol).pipe(Effect.result);
    if (pos._tag === "Success") {
      const live = pos.success.find((p) => Number(p.size) > 0);
      console.log(`${c.symbol} remaining:`, live ? `qty=${live.size}` : "flat");
    }
  }
});

Effect.runPromise(
  program.pipe(Effect.provide(Layer.mergeAll(BybitConfigLive, Layer.provide(BybitClientLiveConfig, BybitConfigLive)))),
).then(() => process.exit(0), (e) => { console.error("FAILED:", e); process.exit(1); });
