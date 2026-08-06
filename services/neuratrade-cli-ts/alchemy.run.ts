import * as Alchemy from "alchemy";
import * as Cloudflare from "alchemy/Cloudflare";
import * as Config from "effect/Config";
import * as Effect from "effect/Effect";
import * as Redacted from "effect/Redacted";

/**
 * NeuraTrade TS port on Cloudflare (alchemy — Infrastructure-as-Effects).
 *
 * Deploys the grid universe watch as an async Worker: a cron-driven scan of
 * a seeded symbol universe against live Bitget public candles, persisting
 * survivors to KV as a WatchlistEntry-compatible whitelist.
 *
 *   bun run deploy:cf    # first run: interactive Cloudflare OAuth
 *   bun run dev:cf       # alchemy dev (local)
 *   bun run destroy:cf
 *
 * Secrets are read from the deployer env and bound as Cloudflare
 * secret_text: CF_ADMIN_API_KEY (guards /scan and /seed).
 */
const adminKey = Config.redacted("CF_ADMIN_API_KEY").pipe(
  Config.withDefault(Redacted.make("")),
);

// Bitget demo (PAPTRADING) credentials — bound from the deployer env as
// Cloudflare secret_text bindings. The universe-watch scan itself uses only
// public market data; these are available for private-API features (e.g. the
// leverage probe) without leaking into the worker bundle.
const bitgetApiKey = Config.redacted("BITGET_API_KEY").pipe(
  Config.withDefault(Redacted.make("")),
);
const bitgetApiSecret = Config.redacted("BITGET_API_SECRET").pipe(
  Config.withDefault(Redacted.make("")),
);
const bitgetPassphrase = Config.redacted("BITGET_PASSPHRASE").pipe(
  Config.withDefault(Redacted.make("")),
);
const bitgetUseSandbox = Config.boolean("BITGET_USE_SANDBOX").pipe(
  Config.withDefault(true),
);

export const UniverseWatch = Cloudflare.Worker("neuratrade-universe-watch", {
  main: "./src/cloudflare/worker.ts",
  env: {
    watchlist: Cloudflare.KV.Namespace("NeuraTradeWatchlist"),
    adminKey,
    bitgetApiKey,
    bitgetApiSecret,
    bitgetPassphrase,
    bitgetUseSandbox,
  },
  crons: ["0 */6 * * *"],
});

export type UniverseWatchEnv = Cloudflare.InferEnv<typeof UniverseWatch>;

export default Alchemy.Stack(
  "NeuraTradeCli",
  {
    providers: Cloudflare.providers(),
    state: Cloudflare.state(),
  },
  Effect.gen(function* () {
    const worker = yield* UniverseWatch;

    return {
      url: worker.url.as<string>(),
    };
  }),
);
