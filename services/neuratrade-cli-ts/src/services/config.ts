/**
 * Config service — loads and merges local (config.json) and runtime (runtime.json)
 * configuration with env → runtime → local → defaults precedence.
 *
 * Mirrors the Go loading/merging logic in cmd/neuratrade-cli/gateway.go.
 */
import * as nodePath from "path";
import * as os from "os";
import { Context, Effect, Layer } from "effect";
import { FileSystem } from "effect";
import type { LocalConfig as LocalConfigData } from "../schemas/local-config";
import type { RuntimeConfig as RuntimeConfigData } from "../schemas/runtime-config";
import { decodeLocalConfig } from "../schemas/local-config";
import { decodeRuntimeConfig } from "../schemas/runtime-config";

// ---------------------------------------------------------------------------
// Context.Tags
// ---------------------------------------------------------------------------

export class LocalConfig extends Context.Service<
  LocalConfig,
  LocalConfigData
>()("LocalConfig") {}

export class RuntimeConfig extends Context.Service<
  RuntimeConfig,
  RuntimeConfigData
>()("RuntimeConfig") {}

// ---------------------------------------------------------------------------
// Resolved Config — the fully-merged configuration
// ---------------------------------------------------------------------------

export interface ResolvedConfig extends RuntimeConfigData {
  readonly admin_api_key: string;
  readonly jwt_secret: string;
  readonly telegram_bot_token: string;
  readonly ai_api_key: string;
  readonly chat_id: string;
}

// ---------------------------------------------------------------------------
// Defaults (match Go defaultRuntimeConfig / empty localConfig)
// ---------------------------------------------------------------------------

/** An empty local config — all fields undefined. */
export function defaultLocalConfig(): LocalConfigData {
  return {} as LocalConfigData;
}

/** Default runtime config matching Go's defaultRuntimeConfig(home). */
export function defaultRuntimeConfig(home: string): RuntimeConfigData {
  return {
    server: { host: "0.0.0.0", port: 8080 },
    database: {
      driver: "sqlite",
      sqlite_path: nodePath.join(home, "data", "neuratrade.db"),
    },
    redis: { host: "127.0.0.1", port: 6379 },
    ccxt: {
      service_url: "http://localhost:3001",
      grpc_address: "127.0.0.1:50051",
    },
    telegram: {
      service_url: "http://localhost:3002",
      grpc_address: "127.0.0.1:50052",
      use_polling: true,
      api_base_url: "http://localhost:8080",
    },
    ai: {
      provider: "openai",
      model: "gpt-4o-mini",
      base_url: undefined,
      temperature: 0.7,
      max_tokens: 4096,
      min_confidence: 0.7,
      daily_budget: "10",
      routing_mode: "primary",
    },
    features: {
      enable_ai: true,
      enable_ai_scalping: true,
      enable_ai_signals: false,
      enable_ai_arbitrage: false,
      paper_trading: true,
      real_trading: false,
    },
    gateway: {
      bind_host: "127.0.0.1",
      ccxt_port: 3001,
      telegram_port: 3002,
      telegram_grpc_port: 50052,
      supervised: false,
      health_timeout_seconds: 150,
      signal_timeout_seconds: 5,
      graceful_timeout_seconds: 10,
      skip_telegram: false,
    },
  };
}

// ---------------------------------------------------------------------------
// Home directory resolution (mirrors Path service)
// ---------------------------------------------------------------------------

function resolveHome(explicitHome?: string): string {
  let raw: string;
  if (explicitHome && explicitHome.length > 0) {
    raw = explicitHome;
  } else if (
    process.env.NEURATRADE_HOME &&
    process.env.NEURATRADE_HOME.length > 0
  ) {
    raw = process.env.NEURATRADE_HOME;
  } else {
    raw = "~/.neuratrade";
  }
  if (raw.startsWith("~")) {
    return nodePath.join(os.homedir(), raw.slice(1));
  }
  return raw;
}

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

/**
 * Read and parse a JSON file. Returns null if the file doesn't exist or
 * cannot be read/parsed (catches all errors silently).
 */
function readJsonFileSafe(
  fs: FileSystem.FileSystem,
  filePath: string,
): Effect.Effect<unknown, never> {
  return Effect.gen(function* () {
    const exists = yield* fs.exists(filePath);
    if (!exists) return null;
    const content = yield* fs.readFileString(filePath);
    return JSON.parse(content) as unknown;
  }).pipe(Effect.catch(() => Effect.succeed(null)));
}

// ---------------------------------------------------------------------------
// Local-config field resolution (matches Go fallback chains)
// ---------------------------------------------------------------------------

function resolveAdminAPIKey(local: LocalConfigData): string {
  if (local.security?.admin_api_key) return local.security.admin_api_key;
  if (local.admin_api_key) return local.admin_api_key;
  if (local.ccxt?.admin_api_key) return local.ccxt.admin_api_key;
  return "";
}

function resolveJWTSecret(local: LocalConfigData): string {
  if (local.security?.jwt_secret) return local.security.jwt_secret;
  if (local.auth?.jwt_secret) return local.auth.jwt_secret;
  return "";
}

function resolveChatID(local: LocalConfigData): string {
  if (local.telegram?.chat_id) return local.telegram.chat_id;
  if (local.services?.telegram?.chat_id) return local.services.telegram.chat_id;
  if (local.telegram_test_chat_id) return local.telegram_test_chat_id;
  return "";
}

// ---------------------------------------------------------------------------
// Env helpers
// ---------------------------------------------------------------------------

/** Read an env var, returning undefined if unset or whitespace-only. */
function envString(key: string): string | undefined {
  const value = process.env[key];
  if (value === undefined) return undefined;
  const trimmed = value.trim();
  return trimmed !== "" ? trimmed : undefined;
}

/** Env var with fallback to defaultValue. */
function envOrDefault(key: string, defaultValue: string): string {
  return envString(key) ?? defaultValue;
}

// ---------------------------------------------------------------------------
// Merge: local overrides
// ---------------------------------------------------------------------------

function applyLocalOverrides(
  base: ResolvedConfig,
  local: LocalConfigData,
): ResolvedConfig {
  let result = { ...base };

  // Server
  if (local.server?.host?.trim()) {
    result = {
      ...result,
      server: { ...result.server, host: local.server.host.trim() },
    };
  }
  if (local.server?.port && local.server.port > 0) {
    result = {
      ...result,
      server: { ...result.server, port: local.server.port },
    };
  }

  // Database
  if (local.database?.driver?.trim()) {
    result = {
      ...result,
      database: { ...result.database, driver: local.database.driver.trim() },
    };
  }
  if (local.database?.sqlite_path?.trim()) {
    result = {
      ...result,
      database: {
        ...result.database,
        sqlite_path: local.database.sqlite_path.trim(),
      },
    };
  }

  // CCXT
  if (local.ccxt?.service_url?.trim()) {
    result = {
      ...result,
      ccxt: {
        ...result.ccxt,
        service_url: local.ccxt.service_url.trim(),
      },
    };
  }
  if (local.ccxt?.grpc_address?.trim()) {
    result = {
      ...result,
      ccxt: {
        ...result.ccxt,
        grpc_address: local.ccxt.grpc_address.trim(),
      },
    };
  }

  // Telegram
  if (local.telegram?.service_url?.trim()) {
    result = {
      ...result,
      telegram: {
        ...result.telegram,
        service_url: local.telegram.service_url.trim(),
      },
    };
  }
  if (local.telegram?.grpc_address?.trim()) {
    result = {
      ...result,
      telegram: {
        ...result.telegram,
        grpc_address: local.telegram.grpc_address.trim(),
      },
    };
  }
  if (local.telegram?.api_base_url?.trim()) {
    result = {
      ...result,
      telegram: {
        ...result.telegram,
        api_base_url: local.telegram.api_base_url.trim(),
      },
    };
  }

  // AI
  if (local.ai?.provider?.trim()) {
    result = {
      ...result,
      ai: { ...result.ai, provider: local.ai.provider.trim() },
    };
  }
  if (local.ai?.model?.trim()) {
    result = {
      ...result,
      ai: { ...result.ai, model: local.ai.model.trim() },
    };
  }
  if (local.ai?.base_url?.trim()) {
    result = {
      ...result,
      ai: { ...result.ai, base_url: local.ai.base_url.trim() },
    };
  }

  // Local-only fields
  return {
    ...result,
    admin_api_key: resolveAdminAPIKey(local),
    jwt_secret: resolveJWTSecret(local),
    telegram_bot_token: local.telegram?.bot_token ?? "",
    ai_api_key: local.ai?.api_key ?? "",
    chat_id: resolveChatID(local),
  };
}

// ---------------------------------------------------------------------------
// Merge: runtime overrides (only fields present in the raw JSON)
// ---------------------------------------------------------------------------

function isNonNullObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function applyRuntimeOverrides(
  base: ResolvedConfig,
  runtime: RuntimeConfigData,
  rawJson: unknown,
): ResolvedConfig {
  const raw = rawJson as Record<string, unknown>;
  let result = { ...base };

  // Server
  if (isNonNullObject(raw.server)) {
    const srv = raw.server as Record<string, unknown>;
    if ("host" in srv)
      result = {
        ...result,
        server: { ...result.server, host: runtime.server.host },
      };
    if ("port" in srv)
      result = {
        ...result,
        server: { ...result.server, port: runtime.server.port },
      };
  }

  // Database
  if (isNonNullObject(raw.database)) {
    const db = raw.database as Record<string, unknown>;
    if ("driver" in db)
      result = {
        ...result,
        database: { ...result.database, driver: runtime.database.driver },
      };
    if ("sqlite_path" in db)
      result = {
        ...result,
        database: {
          ...result.database,
          sqlite_path: runtime.database.sqlite_path,
        },
      };
  }

  // Redis
  if (isNonNullObject(raw.redis)) {
    const r = raw.redis as Record<string, unknown>;
    if ("host" in r)
      result = {
        ...result,
        redis: { ...result.redis, host: runtime.redis.host },
      };
    if ("port" in r)
      result = {
        ...result,
        redis: { ...result.redis, port: runtime.redis.port },
      };
  }

  // CCXT
  if (isNonNullObject(raw.ccxt)) {
    const c = raw.ccxt as Record<string, unknown>;
    if ("service_url" in c)
      result = {
        ...result,
        ccxt: { ...result.ccxt, service_url: runtime.ccxt.service_url },
      };
    if ("grpc_address" in c)
      result = {
        ...result,
        ccxt: { ...result.ccxt, grpc_address: runtime.ccxt.grpc_address },
      };
  }

  // Telegram
  if (isNonNullObject(raw.telegram)) {
    const t = raw.telegram as Record<string, unknown>;
    if ("service_url" in t)
      result = {
        ...result,
        telegram: {
          ...result.telegram,
          service_url: runtime.telegram.service_url,
        },
      };
    if ("grpc_address" in t)
      result = {
        ...result,
        telegram: {
          ...result.telegram,
          grpc_address: runtime.telegram.grpc_address,
        },
      };
    if ("use_polling" in t)
      result = {
        ...result,
        telegram: {
          ...result.telegram,
          use_polling: runtime.telegram.use_polling,
        },
      };
    if ("api_base_url" in t)
      result = {
        ...result,
        telegram: {
          ...result.telegram,
          api_base_url: runtime.telegram.api_base_url,
        },
      };
  }

  // AI
  if (isNonNullObject(raw.ai)) {
    const a = raw.ai as Record<string, unknown>;
    if ("provider" in a)
      result = {
        ...result,
        ai: { ...result.ai, provider: runtime.ai.provider },
      };
    if ("model" in a)
      result = {
        ...result,
        ai: { ...result.ai, model: runtime.ai.model },
      };
    if ("base_url" in a)
      result = {
        ...result,
        ai: { ...result.ai, base_url: runtime.ai.base_url },
      };
    if ("temperature" in a)
      result = {
        ...result,
        ai: { ...result.ai, temperature: runtime.ai.temperature },
      };
    if ("max_tokens" in a)
      result = {
        ...result,
        ai: { ...result.ai, max_tokens: runtime.ai.max_tokens },
      };
    if ("min_confidence" in a)
      result = {
        ...result,
        ai: { ...result.ai, min_confidence: runtime.ai.min_confidence },
      };
    if ("daily_budget" in a)
      result = {
        ...result,
        ai: { ...result.ai, daily_budget: runtime.ai.daily_budget },
      };
    if ("routing_mode" in a)
      result = {
        ...result,
        ai: { ...result.ai, routing_mode: runtime.ai.routing_mode },
      };
  }

  // Features (whole-object replacement)
  if (isNonNullObject(raw.features)) {
    result = { ...result, features: runtime.features };
  }

  // Gateway (whole-object replacement)
  if (isNonNullObject(raw.gateway)) {
    result = { ...result, gateway: runtime.gateway };
  }

  return result;
}

// ---------------------------------------------------------------------------
// Merge: env overrides (highest priority)
// ---------------------------------------------------------------------------

function applyEnvOverrides(
  base: ResolvedConfig,
  local: LocalConfigData,
  home: string,
): ResolvedConfig {
  let result = { ...base };

  // Server port: env(SERVER_PORT | PORT | BACKEND_HOST_PORT) → current
  const portStr =
    envString("SERVER_PORT") ??
    envString("PORT") ??
    envString("BACKEND_HOST_PORT");
  if (portStr) {
    const port = parseInt(portStr, 10);
    if (port > 0 && port < 65536) {
      result = { ...result, server: { ...result.server, port } };
    }
  }

  // Database driver: env(DATABASE_DRIVER)
  const dbDriver = envString("DATABASE_DRIVER");
  if (dbDriver) {
    result = {
      ...result,
      database: { ...result.database, driver: dbDriver },
    };
  }

  // SQLite path: env(SQLITE_PATH)
  const sqlitePath = envString("SQLITE_PATH");
  if (sqlitePath) {
    result = {
      ...result,
      database: { ...result.database, sqlite_path: sqlitePath },
    };
  }

  // Redis
  const redisHost = envString("REDIS_HOST");
  if (redisHost) {
    result = { ...result, redis: { ...result.redis, host: redisHost } };
  }
  const redisPort = envString("REDIS_PORT");
  if (redisPort) {
    const port = parseInt(redisPort, 10);
    if (port > 0 && port < 65536) {
      result = { ...result, redis: { ...result.redis, port } };
    }
  }

  // AI
  const aiProvider = envString("AI_PROVIDER");
  if (aiProvider) {
    result = { ...result, ai: { ...result.ai, provider: aiProvider } };
  }
  const aiModel = envString("AI_MODEL");
  if (aiModel) {
    result = { ...result, ai: { ...result.ai, model: aiModel } };
  }
  const aiBaseURL = envString("AI_BASE_URL");
  if (aiBaseURL) {
    result = { ...result, ai: { ...result.ai, base_url: aiBaseURL } };
  }

  // Local-only fields with env overrides
  return {
    ...result,
    admin_api_key: envOrDefault("ADMIN_API_KEY", result.admin_api_key),
    jwt_secret: envOrDefault("JWT_SECRET", result.jwt_secret),
    telegram_bot_token: envOrDefault(
      "TELEGRAM_BOT_TOKEN",
      result.telegram_bot_token,
    ),
    ai_api_key: envOrDefault("AI_API_KEY", result.ai_api_key),
    chat_id: envOrDefault("TELEGRAM_CHAT_ID", result.chat_id),
  };
}

// ---------------------------------------------------------------------------
// Load functions
// ---------------------------------------------------------------------------

/** Load local config (config.json) from the given home directory. */
export const loadLocalConfigEffect = (
  homeDir?: string,
): Effect.Effect<LocalConfigData, never, FileSystem.FileSystem> => {
  const home = resolveHome(homeDir);
  const configPath = nodePath.join(home, "config.json");

  return Effect.gen(function* () {
    const fs = yield* FileSystem.FileSystem;
    const json = yield* readJsonFileSafe(fs, configPath);
    if (json === null) return defaultLocalConfig();
    return yield* decodeLocalConfig(json).pipe(
      Effect.catch(() => Effect.succeed(defaultLocalConfig())),
    );
  });
};

/** Load runtime config (runtime.json) from the given home directory. */
export const loadRuntimeConfigEffect = (
  homeDir?: string,
): Effect.Effect<RuntimeConfigData, never, FileSystem.FileSystem> => {
  const home = resolveHome(homeDir);
  const runtimePath = nodePath.join(home, "runtime.json");

  return Effect.gen(function* () {
    const fs = yield* FileSystem.FileSystem;
    const json = yield* readJsonFileSafe(fs, runtimePath);
    if (json === null) return defaultRuntimeConfig(home);
    return yield* decodeRuntimeConfig(json).pipe(
      Effect.catch(() => Effect.succeed(defaultRuntimeConfig(home))),
    );
  });
};

// ---------------------------------------------------------------------------
// Resolved config
// ---------------------------------------------------------------------------

/**
 * Produce the fully-resolved configuration by merging:
 *   env vars → runtime config → local config → defaults
 *
 * This matches the Go merge logic in cmd/neuratrade-cli/gateway.go.
 */
export const resolvedConfigEffect = (
  homeDir?: string,
): Effect.Effect<ResolvedConfig, never, FileSystem.FileSystem> => {
  const home = resolveHome(homeDir);

  return Effect.gen(function* () {
    const fs = yield* FileSystem.FileSystem;

    // Load raw JSON (null if file missing or unreadable)
    const runtimeJson = yield* readJsonFileSafe(
      fs,
      nodePath.join(home, "runtime.json"),
    );
    const localJson = yield* readJsonFileSafe(
      fs,
      nodePath.join(home, "config.json"),
    );

    // Decode local config
    const local =
      localJson !== null
        ? yield* decodeLocalConfig(localJson).pipe(
            Effect.catch(() => Effect.succeed(defaultLocalConfig())),
          )
        : defaultLocalConfig();

    // 1. Start with defaults
    let result: ResolvedConfig = {
      ...defaultRuntimeConfig(home),
      admin_api_key: "",
      jwt_secret: "",
      telegram_bot_token: "",
      ai_api_key: "",
      chat_id: "",
    };

    // 2. Apply local config overrides
    result = applyLocalOverrides(result, local);

    // 3. Apply runtime config overrides (only fields present in raw JSON)
    if (runtimeJson !== null) {
      const runtime = yield* decodeRuntimeConfig(runtimeJson).pipe(
        Effect.catch(() => Effect.succeed(defaultRuntimeConfig(home))),
      );
      result = applyRuntimeOverrides(result, runtime, runtimeJson);
    }

    // 4. Apply env overrides (highest priority)
    result = applyEnvOverrides(result, local, home);

    return result;
  });
};

// ---------------------------------------------------------------------------
// ConfigLive Layer
// ---------------------------------------------------------------------------

/**
 * Layer that provides both `LocalConfig` and `RuntimeConfig` tags.
 *
 * Requires `FileSystem` in context (e.g. `BunFileSystem.layer`).
 */
export const ConfigLive = (
  homeDir?: string,
): Layer.Layer<LocalConfig | RuntimeConfig, never, FileSystem.FileSystem> =>
  Layer.merge(
    Layer.effect(LocalConfig, loadLocalConfigEffect(homeDir)),
    Layer.effect(RuntimeConfig, loadRuntimeConfigEffect(homeDir)),
  );
