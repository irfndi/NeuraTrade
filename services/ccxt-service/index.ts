/*
Worker utilities note (Bun >= 1.2.21):
- For cross-thread messaging that moves large JSON, send raw strings via postMessage(jsonString)
  instead of wrapping it inside objects/arrays (e.g., postMessage({ data: jsonString })).
- Keep the large payload as a standalone top-level string to leverage Bun's fast path; parse on the
  receiver only when needed (e.g., const data = JSON.parse(message)).
- If you need metadata, consider sending a small metadata message separately, or keep the large payload
  as a top-level string field to preserve fast-path benefits.
*/

import { Effect } from "effect";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
// import { compress } from 'hono/compress'; // Removed due to CompressionStream not available in Bun
import { secureHeaders } from "hono/secure-headers";
import { validator } from "hono/validator";
// Use ESM import so test mocks can intercept ccxt module
import ccxt from "ccxt";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "fs";
import { join } from "path";
import os from "os";
import {
  isSentryEnabled,
  sentryMiddleware,
  initializeSentry,
  captureException,
  flush as sentryFlush,
} from "./sentry";
import { startGrpcServer } from "./grpc-server";
import type {
  HealthResponse,
  ExchangesResponse,
  TickerResponse,
  OrderBookResponse,
  OHLCVResponse,
  MarketsResponse,
  MultiTickerRequest,
  MultiTickerResponse,
  ErrorResponse,
  ExchangeManager,
  FundingRate,
  FundingRateResponse,
  PlaceOrderRequest,
  PlaceOrderResponse,
  CancelOrderResponse,
  GetOrderResponse,
  GetOpenOrdersResponse,
  GetClosedOrdersResponse,
  GetOrderTradesResponse,
  ExchangesListResponse,
} from "./types";

import { getEnvWithNeuratradeFallback } from "./config";

// Load environment variables
const resolvePort = () => {
  const DEFAULT_PORT = 3001;
  const envPorts = [
    process.env.CCXT_SERVICE_PORT,
    process.env.CCXT_PORT,
    process.env.PORT,
  ];

  for (const value of envPorts) {
    if (value) {
      const numericPort = Number(value);
      if (
        !Number.isNaN(numericPort) &&
        numericPort > 0 &&
        numericPort < 65536
      ) {
        return numericPort;
      }
      console.warn(
        `Invalid port value provided (${value}). Falling back to default (${DEFAULT_PORT}).`,
      );
      break;
    }
  }

  return DEFAULT_PORT;
};

type RuntimeConfig = {
  port: number;
  adminApiKey: string;
};

const loadRuntimeConfig = Effect.try((): RuntimeConfig => {
  const adminApiKey = getEnvWithNeuratradeFallback("ADMIN_API_KEY") || "";
  const isProduction =
    process.env.NODE_ENV === "production" ||
    process.env.SENTRY_ENVIRONMENT === "production";

  // Validate ADMIN_API_KEY only in production
  if (isProduction) {
    if (!adminApiKey) {
      throw new Error(
        "ADMIN_API_KEY environment variable must be set in production",
      );
    }

    if (
      adminApiKey === "admin-secret-key-change-me" ||
      adminApiKey === "admin-dev-key-change-in-production"
    ) {
      throw new Error(
        "ADMIN_API_KEY cannot use default/example values. Please set a secure API key.",
      );
    }

    if (adminApiKey.length < 32) {
      throw new Error(
        "ADMIN_API_KEY must be at least 32 characters long for security",
      );
    }
  } else if (!adminApiKey) {
    console.warn(
      "⚠️ WARNING: ADMIN_API_KEY is not set. Admin endpoints will be disabled.",
    );
  }

  return {
    port: resolvePort(),
    adminApiKey,
  };
});

let runtimeConfig: RuntimeConfig;
try {
  runtimeConfig = Effect.runSync(loadRuntimeConfig);
} catch (error) {
  console.error(
    error instanceof Error
      ? error.message
      : "Failed to load runtime configuration",
  );
  process.exit(1);
}

const PORT = runtimeConfig.port;
process.env.PORT = PORT.toString(); // Ensure consistency across Bun/Node
const ADMIN_API_KEY = runtimeConfig.adminApiKey;

// Initialize Hono app
const app = new Hono();

// Middleware
app.use("*", secureHeaders());
app.use("*", cors());
// app.use('*', compress()); // Removed due to CompressionStream not available in Bun
app.use("*", logger());
if (isSentryEnabled) {
  app.use("*", sentryMiddleware);
}

app.onError((err, c) => {
  // Capture exception to Sentry if enabled
  if (isSentryEnabled) {
    captureException(err, {
      path: c.req.path,
      method: c.req.method,
      url: c.req.url,
    });
  }
  console.error("Application error:", err);
  return c.json(
    {
      error: "Internal Server Error",
      message:
        err instanceof Error ? err.message : "Unexpected server error occurred",
      timestamp: new Date().toISOString(),
    },
    500,
  );
});

// Authentication middleware for admin endpoints
const adminAuth = async (c: any, next: any) => {
  // If ADMIN_API_KEY is not configured, disable admin endpoints
  if (!ADMIN_API_KEY) {
    return c.json(
      {
        error: "Service Unavailable",
        message: "Admin endpoints are disabled (ADMIN_API_KEY not set)",
        timestamp: new Date().toISOString(),
      },
      503,
    );
  }

  const authHeader = c.req.header("Authorization");
  const apiKey = c.req.header("X-API-Key");

  // Check for API key in Authorization header (Bearer token) or X-API-Key header
  const providedKey = authHeader?.replace("Bearer ", "") || apiKey;

  if (!providedKey || providedKey !== ADMIN_API_KEY) {
    return c.json(
      {
        error: "Unauthorized",
        message: "Valid API key required for admin endpoints",
        timestamp: new Date().toISOString(),
      },
      401,
    );
  }

  await next();
};

// Simple rate limiting can be implemented later if needed
// For now, we rely on exchange-level rate limiting via CCXT

// Exchange configuration for different types of exchanges
const exchangeConfigs: Record<string, any> = {
  binance: {
    enableRateLimit: true,
    timeout: 30000,
    rateLimit: 1200,
    options: { defaultType: "future" },
  },
  bybit: {
    enableRateLimit: true,
    options: { defaultType: "future" },
  },
  okx: {
    enableRateLimit: true,
    options: { defaultType: "future" },
  },
  coinbase: { enableRateLimit: true },
  kraken: { enableRateLimit: true },
  // Default config for other exchanges
  default: {
    enableRateLimit: true,
    timeout: 30000,
    rateLimit: 2000,
  },
};

// Configuration file path
const CONFIG_FILE_PATH = join(process.cwd(), "exchange-config.json");

// Default exchange configuration
const defaultExchangeConfig = {
  blacklistedExchanges: [
    "test",
    "mock",
    "sandbox",
    "demo",
    "testnet",
    "coinbaseprime", // Use coinbaseexchange instead
    "ftx",
    "ftxus", // Defunct exchanges
    "liquid",
    "quoine", // Defunct exchanges
    "idex",
    "ethfinex", // Deprecated exchanges
    "yobit",
    "livecoin",
    "coinfloor", // Problematic exchanges
    "southxchange",
    "coinmate",
    "lakebtc", // Often unreliable
  ],
};

/**
 * Loads persisted exchange configuration from disk or returns the default configuration.
 *
 * @returns The parsed exchange configuration if the config file exists and is valid, otherwise a copy of the default exchange configuration.
 */
function loadExchangeConfig() {
  const loadEffect = Effect.try(() => {
    if (!existsSync(CONFIG_FILE_PATH)) {
      return { ...defaultExchangeConfig };
    }

    const configData = readFileSync(CONFIG_FILE_PATH, "utf8");
    const config = JSON.parse(configData);
    console.log("Loaded exchange configuration from file");
    return config;
  });

  try {
    return Effect.runSync(loadEffect);
  } catch (error) {
    console.warn(
      "Failed to load exchange configuration from file, using defaults:",
      error,
    );
    return { ...defaultExchangeConfig };
  }
}

/**
 * Persist the exchange configuration object to disk at the configured path.
 *
 * @param config - The exchange configuration object to write to disk
 * @returns `true` if the file was written successfully, `false` otherwise
 */
function saveExchangeConfig(config: any) {
  const saveEffect = Effect.try(() => {
    writeFileSync(CONFIG_FILE_PATH, JSON.stringify(config, null, 2), "utf8");
    console.log("Saved exchange configuration to file");
    return true;
  });

  try {
    return Effect.runSync(saveEffect);
  } catch (error) {
    console.error("Failed to save exchange configuration:", error);
    return false;
  }
}

// Load exchange configuration
const exchangeConfig = loadExchangeConfig();

// Convert to Set for faster lookups during initialization
const blacklistedExchanges = new Set(exchangeConfig.blacklistedExchanges);

interface UserExchangeConfig {
  enabled: string[];
  apiKeys: Record<string, { apiKey: string; secret: string }>;
  addedAt: Record<string, string>;
  devMode: boolean;
  marketData: Record<string, unknown>;
}

// Load exchange config from ~/.neuratrade/config.json
function loadUserExchangeConfig(): UserExchangeConfig {
  try {
    const configPath = join(os.homedir(), ".neuratrade", "config.json");
    if (existsSync(configPath)) {
      const configData = readFileSync(configPath, "utf-8");
      const config = JSON.parse(configData);

      // Check ccxt.exchanges path (new config format)
      const ccxtExchanges = config.ccxt?.exchanges || {};
      const apiKeys: Record<string, { apiKey: string; secret: string }> = {};
      const addedAt: Record<string, string> = {};

      for (const [exchangeName, exchangeConfig] of Object.entries(
        ccxtExchanges,
      ) as [string, any][]) {
        if (
          typeof exchangeConfig?.added_at === "string" &&
          exchangeConfig.added_at.length > 0
        ) {
          addedAt[exchangeName] = exchangeConfig.added_at;
        }

        if (exchangeConfig?.api_key && exchangeConfig?.api_secret) {
          apiKeys[exchangeName] = {
            apiKey: exchangeConfig.api_key,
            secret: exchangeConfig.api_secret,
          };
        }
      }

      // Also check exchanges.api_keys path (legacy format)
      const legacyApiKeys = config.exchanges?.api_keys || {};
      for (const [exchangeName, exchangeConfig] of Object.entries(
        legacyApiKeys,
      ) as [string, any][]) {
        if (!apiKeys[exchangeName] && exchangeConfig?.apiKey) {
          apiKeys[exchangeName] = {
            apiKey: exchangeConfig.apiKey,
            secret: exchangeConfig.secret,
          };
        }
      }

      // Get enabled exchanges from ccxt.exchanges or default
      const enabledFromConfig = Object.keys(ccxtExchanges).filter(
        (name) => ccxtExchanges[name]?.enabled !== false,
      );

      return {
        enabled:
          enabledFromConfig.length > 0
            ? enabledFromConfig
            : config.exchanges?.enabled || [],
        apiKeys,
        addedAt,
        devMode: config.server?.dev_mode || false,
        marketData: config.market_data || {},
      };
    }
  } catch (e) {
    console.log("No user config found, using defaults");
  }
  return { enabled: [], apiKeys: {}, addedAt: {}, devMode: false, marketData: {} };
}

const userConfig = loadUserExchangeConfig();

// Get exchanges to load - ONLY user-configured ones (no automatic loading of all CCXT)
const exchangesToLoad =
  userConfig.enabled.length > 0
    ? userConfig.enabled
    : [
        // Default set for first-time users (no config yet)
        "binance",
        "bybit",
        "okx",
        "kraken",
        "kucoin",
        "gateio",
        "mexc",
        "bitget",
        "coinbase",
        "bingx",
        "cryptocom",
      ];

// Initialize supported exchanges dynamically
const exchanges: ExchangeManager = {};

/**
 * Initialize and register a CCXT exchange by its identifier into the service's active exchanges map.
 *
 * Attempts to instantiate the exchange with configured options, validates that it supports market and ticker operations and has basic identity properties, and stores the instance in the `exchanges` map on success.
 *
 * @param exchangeId - The CCXT exchange identifier (e.g., "binance", "bybit")
 * @returns `true` if the exchange was successfully instantiated and registered in the active exchanges map, `false` otherwise (including when blacklisted, not supported by CCXT, missing required capabilities, or on initialization error)
 */
function initializeExchange(exchangeId: string): boolean {
  try {
    if (blacklistedExchanges.has(exchangeId)) {
      console.log(`Skipping blacklisted exchange: ${exchangeId}`);
      return false;
    }

    // Check if exchange class exists in CCXT
    const ExchangeClass = (ccxt as any)[exchangeId];
    if (!ExchangeClass || typeof ExchangeClass !== "function") {
      console.warn(`Exchange class not found for: ${exchangeId}`);
      return false;
    }

    // Get configuration for this exchange
    const config = exchangeConfigs[exchangeId] || exchangeConfigs.default;

    // Merge API credentials if available
    const credentials = userConfig.apiKeys?.[exchangeId];
    const configWithAuth = credentials?.apiKey
      ? {
          ...config,
          apiKey: credentials.apiKey,
          secret: credentials.secret,
        }
      : config;

    // Initialize the exchange
    const exchange = new ExchangeClass(configWithAuth);

    // Basic validation - check if exchange has required methods
    if (!exchange.fetchTicker) {
      console.warn(`Exchange ${exchangeId} missing fetchTicker method`);
      return false;
    }

    if (!exchange.loadMarkets) {
      console.warn(`Exchange ${exchangeId} missing loadMarkets method`);
      return false;
    }

    // Additional validation - check if exchange has basic properties
    if (!exchange.id) {
      console.warn(`Exchange ${exchangeId} missing id property`);
      return false;
    }

    exchanges[exchangeId] = exchange;
    console.log(
      `✓ Successfully initialized exchange: ${exchangeId} (${exchange.name})${credentials?.apiKey ? " with API credentials" : ""}`,
    );
    return true;
  } catch (error) {
    console.warn(
      `✗ Failed to initialize exchange ${exchangeId}:`,
      error instanceof Error ? error.message : error,
    );
    return false;
  }
}

// Get all available exchanges from CCXT (for reference only)
const allExchanges = ccxt.exchanges;
console.log(`Total CCXT exchanges available: ${allExchanges.length}`);
console.log(
  `Blacklisted exchanges: ${Array.from(blacklistedExchanges).join(", ")}`,
);
console.log(
  `User-configured exchanges to load: ${exchangesToLoad.length}`,
  exchangesToLoad.length > 0 ? `(${exchangesToLoad.join(", ")})` : "(none)",
);

// Initialize ONLY user-configured exchanges (efficient, non-bloated approach)
let initializedCount = 0;
let failedCount = 0;
const failedExchanges: string[] = [];

for (const exchangeId of exchangesToLoad) {
  if (initializeExchange(exchangeId)) {
    initializedCount++;
  } else {
    failedCount++;
    failedExchanges.push(exchangeId);
  }
}

console.log(
  `Exchange initialization complete: ${initializedCount}/${exchangesToLoad.length}`,
);

if (failedCount > 0) {
  console.warn(
    `Failed to initialize ${failedCount} exchanges:`,
    failedExchanges.join(", "),
  );
}

console.log(`Active exchanges:`, Object.keys(exchanges).sort().join(", "));
console.log(
  `💡 Tip: Use 'neuratrade exchanges add --name <exchange>' to add more exchanges dynamically`,
);

// Market data cache with cleanup
interface MarketDataCache {
  ticker: Record<string, any>;
  orderbook: Record<string, any>;
  lastUpdate: number;
}

const marketDataCache: Map<string, MarketDataCache> = new Map();

// Cleanup old market data
function cleanupMarketData() {
  const maxAgeMs = (userConfig.marketData?.max_age_minutes || 10) * 60 * 1000;
  const now = Date.now();
  let cleaned = 0;

  for (const [key, data] of marketDataCache.entries()) {
    if (now - data.lastUpdate > maxAgeMs) {
      marketDataCache.delete(key);
      cleaned++;
    }
  }

  if (cleaned > 0) {
    console.log(`🧹 Cleaned ${cleaned} old market data entries`);
  }
}

// Start cleanup interval
const cleanupIntervalMs =
  (userConfig.marketData?.cleanup_interval_minutes || 5) * 60 * 1000;
const cleanupIntervalId: NodeJS.Timeout = setInterval(
  cleanupMarketData,
  cleanupIntervalMs,
);
console.log(
  `🧹 Market data cleanup scheduled every ${userConfig.marketData?.cleanup_interval_minutes || 5} minutes`,
);

// Health check endpoint - verifies actual service functionality
app.get("/health", async (c) => {
  const activeExchangeCount = Object.keys(exchanges).length;
  const isHealthy = activeExchangeCount > 0;

  // Optionally verify at least one priority exchange can respond
  let exchangeConnectivity = "unknown";
  if (isHealthy) {
    // Quick sanity check on a priority exchange (binance is usually reliable)
    const testExchange = exchanges["binance"] || Object.values(exchanges)[0];
    if (testExchange) {
      try {
        // Just check if exchange object is valid, don't make external call for health check
        exchangeConnectivity = testExchange.id ? "configured" : "misconfigured";
      } catch {
        exchangeConnectivity = "error";
      }
    }
  }

  const response: HealthResponse & {
    exchanges_count: number;
    exchange_connectivity: string;
  } = {
    status: isHealthy ? "healthy" : "unhealthy",
    timestamp: new Date().toISOString(),
    service: "ccxt-service",
    version: "1.0.0",
    exchanges_count: activeExchangeCount,
    exchange_connectivity: exchangeConnectivity,
  };

  if (!isHealthy) {
    return c.json(response, 503);
  }

  return c.json(response);
});

// Get supported exchanges
app.get("/api/exchanges", (c) => {
  try {
    // Return array of ExchangeInfo objects for proper API contract
    const exchangeList = Object.keys(exchanges).map((id) => ({
      id,
      name: exchanges[id].name || id,
      countries: (exchanges[id].countries || []).map((country) =>
        String(country),
      ),
      urls: exchanges[id].urls || {},
    }));

    const response: ExchangesResponse = { exchanges: exchangeList };
    return c.json(response);
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Get configured exchanges (user-configured only)
app.get("/api/v1/exchanges", (c) => {
  try {
    const configuredExchanges = Object.keys(exchanges).map((id) => ({
      name: id,
      enabled: true,
      has_auth: !!userConfig.apiKeys?.[id]?.apiKey,
      added_at: userConfig.addedAt?.[id] || new Date().toISOString(),
    }));

    const response: ExchangesListResponse = {
      exchanges: configuredExchanges,
      count: configuredExchanges.length,
    };
    return c.json(response);
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Add a new exchange
app.post("/api/v1/exchanges", adminAuth, async (c) => {
  try {
    const body = await c.req.json();
    const { name, api_key, secret } = body as {
      name: string;
      api_key?: string;
      secret?: string;
    };

    if (!name) {
      return c.json(
        {
          success: false,
          message: "Exchange name is required",
        },
        400,
      );
    }

    // Check if exchange already exists
    if (exchanges[name]) {
      return c.json(
        {
          success: false,
          message: `Exchange ${name} is already configured`,
        },
        400,
      );
    }

    // Try to initialize the exchange
    const success = initializeExchange(name);
    if (!success) {
      return c.json(
        {
          success: false,
          message: `Failed to initialize exchange ${name}. It may be unsupported or temporarily unavailable.`,
        },
        400,
      );
    }

    // Update in-memory config
    userConfig.enabled = [...new Set([...(userConfig.enabled || []), name])];
    userConfig.addedAt = userConfig.addedAt || {};
    userConfig.addedAt[name] = userConfig.addedAt[name] || new Date().toISOString();
    if (api_key || secret) {
      userConfig.apiKeys = userConfig.apiKeys || {};
      userConfig.apiKeys[name] = {
        apiKey: api_key || "",
        secret: secret || "",
      };
      if (exchanges[name]) {
        delete exchanges[name];
        initializeExchange(name);
      }
    }

    // Persist to config file
    const configDir = join(os.homedir(), ".neuratrade");
    const configPath = join(configDir, "config.json");

    // Ensure directory exists with secure permissions
    if (!existsSync(configDir)) {
      mkdirSync(configDir, { recursive: true, mode: 0o700 });
    }

    // Read existing full config to preserve unrelated sections
    let existingFullConfig: any = {};
    if (existsSync(configPath)) {
      try {
        existingFullConfig = JSON.parse(readFileSync(configPath, "utf-8"));
      } catch {
        // Start fresh if parse fails
      }
    }

    // Build ccxt.exchanges structure
    const ccxtExchanges = existingFullConfig.ccxt?.exchanges || {};
    const updatedCcxtExchanges = {
      ...ccxtExchanges,
      [name]: {
        enabled: true,
        added_at: userConfig.addedAt[name],
        ...(api_key && { api_key, api_secret: secret }),
      },
    };

    const newConfig = {
      ...existingFullConfig,
      ccxt: {
        ...existingFullConfig.ccxt,
        exchanges: updatedCcxtExchanges,
      },
      // Maintain legacy format for backward compatibility
      exchanges: {
        enabled: userConfig.enabled,
        api_keys: Object.fromEntries(
          Object.entries(userConfig.apiKeys || {}).map(([k, v]) => [
            k,
            { apiKey: v.apiKey, secret: v.secret },
          ]),
        ),
      },
    };

    writeFileSync(configPath, JSON.stringify(newConfig, null, 2), {
      mode: 0o600,
    });

    return c.json({
      success: true,
      message: `Exchange ${name} added successfully. Market data will be available shortly.`,
      name,
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Remove an exchange
app.delete("/api/v1/exchanges", adminAuth, async (c) => {
  try {
    const body = await c.req.json();
    const { name } = body as { name: string };

    if (!name) {
      return c.json(
        {
          success: false,
          message: "Exchange name is required",
        },
        400,
      );
    }

    if (!exchanges[name]) {
      return c.json(
        {
          success: false,
          message: `Exchange ${name} is not configured`,
        },
        404,
      );
    }

    // Remove from active exchanges
    delete exchanges[name];

    // Update in-memory config
    userConfig.enabled = (userConfig.enabled || []).filter(
      (e: string) => e !== name,
    );
    if (userConfig.apiKeys?.[name]) {
      delete userConfig.apiKeys[name];
    }
    if (userConfig.addedAt?.[name]) {
      delete userConfig.addedAt[name];
    }

    // Update config file
    const configDir = join(os.homedir(), ".neuratrade");
    const configPath = join(configDir, "config.json");

    // Ensure directory exists with secure permissions
    if (!existsSync(configDir)) {
      mkdirSync(configDir, { recursive: true, mode: 0o700 });
    }

    // Read existing full config to preserve unrelated sections
    let existingFullConfig: any = {};
    if (existsSync(configPath)) {
      try {
        existingFullConfig = JSON.parse(readFileSync(configPath, "utf-8"));
      } catch {
        // Continue with empty config if parse fails
      }
    }

    // Remove from ccxt.exchanges
    const ccxtExchanges = existingFullConfig.ccxt?.exchanges || {};
    const { [name]: _, ...remainingCcxtExchanges } = ccxtExchanges;

    // Remove from legacy exchanges.enabled
    const legacyEnabled = existingFullConfig.exchanges?.enabled || [];
    const updatedLegacyEnabled = legacyEnabled.filter(
      (e: string) => e !== name,
    );

    // Remove from legacy exchanges.api_keys
    const legacyApiKeys = existingFullConfig.exchanges?.api_keys || {};
    const { [name]: __, ...remainingLegacyApiKeys } = legacyApiKeys;

    const newConfig = {
      ...existingFullConfig,
      ccxt: {
        ...existingFullConfig.ccxt,
        exchanges: remainingCcxtExchanges,
      },
      exchanges: {
        enabled: updatedLegacyEnabled,
        api_keys: remainingLegacyApiKeys,
      },
    };
    writeFileSync(configPath, JSON.stringify(newConfig, null, 2), {
      mode: 0o600,
    });

    return c.json({
      success: true,
      message: `Exchange ${name} removed successfully`,
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Reload exchanges configuration
app.post("/api/v1/exchanges/reload", adminAuth, async (c) => {
  try {
    // Reload user config and update module-level variable
    const newConfig = loadUserExchangeConfig();
    userConfig.enabled = newConfig.enabled;
    userConfig.apiKeys = newConfig.apiKeys;
    userConfig.addedAt = newConfig.addedAt;
    userConfig.devMode = newConfig.devMode;
    userConfig.marketData = newConfig.marketData;

    // Disable exchanges not in new config
    for (const exchangeId of Object.keys(exchanges)) {
      if (!userConfig.enabled.includes(exchangeId)) {
        delete exchanges[exchangeId];
      }
    }

    // Enable new exchanges
    for (const exchangeId of userConfig.enabled) {
      if (!exchanges[exchangeId]) {
        initializeExchange(exchangeId);
      }
    }

    return c.json({
      success: true,
      message: `Exchange configuration reloaded. ${Object.keys(exchanges).length} exchanges active.`,
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Get ticker data
app.get("/api/ticker/:exchange/*", async (c) => {
  const exchange = c.req.param("exchange");
  const pathParts = c.req.path.split("/");
  const symbol = pathParts.slice(4).join("/");

  try {
    if (!exchanges[exchange]) {
      const errorResponse: ErrorResponse = {
        error: "Exchange not supported",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    let retries = exchange === "binance" ? 3 : 1;
    let lastError: any;

    for (let i = 0; i < retries; i++) {
      try {
        const [ticker, orderbook] = await Promise.all([
          exchanges[exchange].fetchTicker(symbol),
          exchanges[exchange]
            .fetchOrderBook(symbol, 5)
            .catch(() =>
              exchanges[exchange].fetchOrderBook(symbol).catch(() => null),
            ),
        ]);

        // Add bid/ask from orderbook if not in ticker
        if (orderbook) {
          if (!ticker.bid && orderbook.bids?.length > 0) {
            ticker.bid = orderbook.bids[0][0];
          }
          if (!ticker.ask && orderbook.asks?.length > 0) {
            ticker.ask = orderbook.asks[0][0];
          }
        }

        const response: TickerResponse = {
          exchange,
          symbol,
          ticker,
          timestamp: new Date().toISOString(),
        };

        return c.json(response);
      } catch (error) {
        lastError = error;
        if (i < retries - 1) {
          await new Promise((resolve) => setTimeout(resolve, 1000 * (i + 1)));
        }
      }
    }

    throw lastError;
  } catch (error: any) {
    const errorMessage =
      error instanceof Error ? error.message : "Unknown error";
    const errorResponse: ErrorResponse = {
      error: errorMessage,
      timestamp: new Date().toISOString(),
    };

    // Determine error type using constructor name (most reliable) or message patterns
    const errorName = error?.constructor?.name || error?.name || "";

    // Symbol not found errors - return 404 (non-retryable)
    const isSymbolNotFound =
      errorName === "BadSymbol" ||
      errorName === "InvalidOrder" ||
      errorMessage.includes("does not have market symbol") ||
      errorMessage.includes("market not found") ||
      errorMessage.includes("symbol not found") ||
      errorMessage.includes("invalid symbol");

    if (isSymbolNotFound) {
      return c.json(errorResponse, 404);
    }

    // Exchange unavailable errors - return 503 (retryable)
    const isExchangeUnavailable =
      errorName === "ExchangeNotAvailable" ||
      errorName === "RequestTimeout" ||
      errorName === "NetworkError" ||
      errorName === "DDoSProtection" ||
      errorName === "RateLimitExceeded" ||
      errorMessage.includes("ExchangeNotAvailable") ||
      errorMessage.includes("RequestTimeout") ||
      errorMessage.includes("NetworkError");

    if (isExchangeUnavailable) {
      return c.json(errorResponse, 503);
    }

    return c.json(errorResponse, 500);
  }
});

// Get order book
app.get(
  "/api/orderbook/:exchange/:symbol",
  validator("query", (value, c) => {
    const limit = value.limit ? parseInt(value.limit as string) : 20;
    if (isNaN(limit) || limit <= 0) {
      return c.text("Invalid limit parameter", 400);
    }
    return { limit };
  }),
  async (c) => {
    try {
      const exchange = c.req.param("exchange");
      const symbol = c.req.param("symbol");
      const { limit } = c.req.valid("query");

      if (!exchanges[exchange]) {
        const errorResponse: ErrorResponse = {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 400);
      }

      const orderbook = await exchanges[exchange].fetchOrderBook(symbol, limit);
      const response: OrderBookResponse = {
        exchange,
        symbol,
        orderbook,
        timestamp: new Date().toISOString(),
      };

      return c.json(response);
    } catch (error) {
      const errorResponse: ErrorResponse = {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 500);
    }
  },
);

// Get OHLCV data
app.get(
  "/api/ohlcv/:exchange/:symbol",
  validator("query", (value, c) => {
    const timeframe = (value.timeframe as string) || "1h";
    const limit = value.limit ? parseInt(value.limit as string) : 100;
    if (isNaN(limit) || limit <= 0) {
      return c.text("Invalid limit parameter", 400);
    }
    return { timeframe, limit };
  }),
  async (c) => {
    try {
      const exchange = c.req.param("exchange");
      const symbol = c.req.param("symbol");
      const { timeframe, limit } = c.req.valid("query");

      if (!exchanges[exchange]) {
        const errorResponse: ErrorResponse = {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 400);
      }

      const ohlcv = await exchanges[exchange].fetchOHLCV(
        symbol,
        timeframe,
        undefined,
        limit,
      );
      const response: OHLCVResponse = {
        exchange,
        symbol,
        timeframe,
        ohlcv,
        timestamp: new Date().toISOString(),
      };

      return c.json(response);
    } catch (error) {
      const errorResponse: ErrorResponse = {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 500);
    }
  },
);

// Get multiple tickers for arbitrage
app.post(
  "/api/tickers",
  validator("json", (value, c) => {
    const { symbols, exchanges: requestedExchanges } =
      value as MultiTickerRequest;

    if (!symbols || !Array.isArray(symbols)) {
      return c.text("Symbols array is required", 400);
    }

    return { symbols, exchanges: requestedExchanges };
  }),
  async (c) => {
    try {
      const { symbols, exchanges: requestedExchanges } = c.req.valid("json");

      const exchangesToQuery = requestedExchanges || Object.keys(exchanges);

      // CRITICAL FIX: Return flat tickers array instead of nested results
      // This matches what the Go client expects for JSON unmarshaling
      const tickers: Array<any> = [];

      for (const exchangeId of exchangesToQuery) {
        if (!exchanges[exchangeId]) continue;

        for (const symbol of symbols) {
          try {
            const ticker = await exchanges[exchangeId].fetchTicker(symbol);
            // Add exchange and symbol to each ticker for context
            tickers.push({
              ...ticker,
              exchange: exchangeId,
              symbol: symbol,
            });
          } catch (error) {
            // Skip errored symbols - don't include them in response
            // The Go client doesn't need to know about individual symbol failures
            console.warn(
              `Failed to fetch ticker for ${exchangeId}:${symbol}: ${error instanceof Error ? error.message : "Unknown error"}`,
            );
          }
        }
      }

      const response: MultiTickerResponse = {
        tickers,
        timestamp: new Date().toISOString(),
      };

      return c.json(response);
    } catch (error) {
      const errorResponse: ErrorResponse = {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 500);
    }
  },
);

// Get trading pairs for an exchange
app.get("/api/markets/:exchange", async (c) => {
  try {
    const exchange = c.req.param("exchange");

    if (!exchanges[exchange]) {
      const errorResponse: ErrorResponse = {
        error: "Exchange not supported",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    const markets = await exchanges[exchange].loadMarkets();
    const symbols = Object.keys(markets);

    const response: MarketsResponse = {
      exchange,
      symbols,
      count: symbols.length,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Get funding rates for an exchange
app.get(
  "/api/funding-rates/:exchange",
  validator("query", (value, _c) => {
    const symbols = value.symbols
      ? (value.symbols as string).split(",")
      : undefined;
    return { symbols };
  }),
  async (c) => {
    try {
      const exchange = c.req.param("exchange");
      const { symbols } = c.req.valid("query");

      if (!exchanges[exchange]) {
        const errorResponse: ErrorResponse = {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 400);
      }

      // Check if exchange supports funding rates
      if (
        !exchanges[exchange].has["fetchFundingRates"] &&
        !exchanges[exchange].has["fetchFundingRate"]
      ) {
        const errorResponse: ErrorResponse = {
          error: "Exchange does not support funding rates",
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 400);
      }

      let fundingRates: FundingRate[] = [];

      if (symbols && symbols.length > 0) {
        // Fetch funding rates for specific symbols
        for (const symbol of symbols) {
          try {
            let fundingRate;
            if (exchanges[exchange].has["fetchFundingRate"]) {
              fundingRate = await exchanges[exchange].fetchFundingRate(symbol);
            } else {
              // Fallback to fetchFundingRates with single symbol
              const rates = await exchanges[exchange].fetchFundingRates([
                symbol,
              ]);
              fundingRate = rates[symbol];
            }

            if (fundingRate) {
              const normalizedSymbol = (fundingRate as any).symbol || symbol;
              fundingRates.push({
                symbol: normalizedSymbol,
                fundingRate: fundingRate.fundingRate || 0,
                fundingTimestamp: fundingRate.fundingTimestamp || Date.now(),
                nextFundingTime: fundingRate.nextFundingDatetime
                  ? new Date(fundingRate.nextFundingDatetime).getTime()
                  : 0,
                markPrice: fundingRate.markPrice || 0,
                indexPrice: fundingRate.indexPrice || 0,
                timestamp: fundingRate.timestamp || Date.now(),
              });
            }
          } catch (error) {
            console.warn(
              `Failed to fetch funding rate for ${symbol} on ${exchange}:`,
              error,
            );
          }
        }
      } else {
        // Fetch all funding rates
        try {
          if (exchanges[exchange].has["fetchFundingRates"]) {
            const rates = await exchanges[exchange].fetchFundingRates();
            fundingRates = Object.values(rates).map((rate: any) => ({
              symbol: rate.symbol,
              fundingRate: rate.fundingRate || 0,
              fundingTimestamp: rate.fundingTimestamp || Date.now(),
              nextFundingTime: rate.nextFundingDatetime
                ? new Date(rate.nextFundingDatetime).getTime()
                : 0,
              markPrice: rate.markPrice || 0,
              indexPrice: rate.indexPrice || 0,
              timestamp: rate.timestamp || Date.now(),
            }));
          }
        } catch (error) {
          console.warn(
            `Failed to fetch all funding rates for ${exchange}:`,
            error,
          );
        }
      }

      const response: FundingRateResponse = {
        exchange,
        fundingRates,
        count: fundingRates.length,
        timestamp: new Date().toISOString(),
      };

      return c.json(response);
    } catch (error) {
      const errorResponse: ErrorResponse = {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 500);
    }
  },
);

// Backward compatibility: Single funding rate endpoint
app.get("/api/funding-rate/:exchange/*", async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const pathParts = c.req.path.split("/");
    const rawSymbol = pathParts.slice(4).join("/"); // Extract everything after /api/funding-rate/{exchange}/

    let symbol = rawSymbol || "";
    try {
      symbol = decodeURIComponent(rawSymbol);
    } catch {
      // keep raw symbol if decode fails
    }

    if (!exchanges[exchange]) {
      const errorResponse: ErrorResponse = {
        error: "Exchange not supported",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    // Check if exchange supports funding rates
    if (
      !exchanges[exchange].has["fetchFundingRates"] &&
      !exchanges[exchange].has["fetchFundingRate"]
    ) {
      const errorResponse: ErrorResponse = {
        error: "Exchange does not support funding rates",
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    const ex = exchanges[exchange];

    const normalizeSimple = (s: string) =>
      (s || "").replace(/[^A-Za-z0-9]/g, "").toUpperCase();
    const stripMarginSuffix = (s: string) => (s || "").split(":")[0];

    const normalizedReq = normalizeSimple(symbol);

    let fundingRate: any | undefined;
    let canonicalSymbol: string | undefined;

    // Try to resolve canonical symbol from markets
    try {
      const markets = await ex.loadMarkets();
      const symbols = Object.keys(markets);
      canonicalSymbol = symbols.find(
        (s: string) => normalizeSimple(stripMarginSuffix(s)) === normalizedReq,
      );
    } catch (err) {
      console.warn(`loadMarkets failed for ${exchange}:`, err);
    }

    // Try direct fetchFundingRate with canonical symbol
    if (!fundingRate && canonicalSymbol && ex.has["fetchFundingRate"]) {
      try {
        fundingRate = await ex.fetchFundingRate(canonicalSymbol);
      } catch (err) {
        console.warn(
          `fetchFundingRate failed for ${canonicalSymbol} on ${exchange}:`,
          err,
        );
      }
    }

    // Try fetchFundingRates with canonical symbol if supported
    if (!fundingRate && canonicalSymbol && ex.has["fetchFundingRates"]) {
      try {
        const rates = await ex.fetchFundingRates([canonicalSymbol]);
        if (rates) {
          // Some exchanges return an object map, others may return array
          if (Array.isArray(rates)) {
            fundingRate = rates.find(
              (r: any) =>
                r?.symbol &&
                normalizeSimple(stripMarginSuffix(r.symbol)) === normalizedReq,
            );
          } else {
            fundingRate =
              (rates as any)[canonicalSymbol] ||
              Object.values(rates as any).find(
                (r: any) =>
                  r?.symbol &&
                  normalizeSimple(stripMarginSuffix(r.symbol)) ===
                    normalizedReq,
              );
          }
        }
      } catch (err) {
        console.warn(
          `fetchFundingRates([${canonicalSymbol}]) failed on ${exchange}:`,
          err,
        );
      }
    }

    // Final fallback: fetch all funding rates and scan
    if (!fundingRate && ex.has["fetchFundingRates"]) {
      try {
        const rates = await ex.fetchFundingRates();
        const values = Array.isArray(rates)
          ? rates
          : Object.values(rates || {});
        fundingRate = values.find(
          (r: any) =>
            r?.symbol &&
            normalizeSimple(stripMarginSuffix(r.symbol)) === normalizedReq,
        );
      } catch (err) {
        console.warn(`fetchAllFundingRates failed on ${exchange}:`, err);
      }
    }

    if (!fundingRate) {
      const errorResponse: ErrorResponse = {
        error: `Funding rate not found for ${symbol} on ${exchange}`,
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 404);
    }

    const response: FundingRate = {
      symbol: fundingRate.symbol || canonicalSymbol || symbol,
      fundingRate: fundingRate.fundingRate || 0,
      fundingTimestamp:
        fundingRate.fundingTimestamp || fundingRate.timestamp || Date.now(),
      nextFundingTime: fundingRate.nextFundingDatetime
        ? new Date(fundingRate.nextFundingDatetime).getTime()
        : fundingRate.nextFundingTime || 0,
      markPrice: fundingRate.markPrice || 0,
      indexPrice: fundingRate.indexPrice || 0,
      timestamp: fundingRate.timestamp || Date.now(),
    };

    return c.json(response);
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Order Execution Endpoints

// Place an order
app.post(
  "/api/order",
  adminAuth,
  validator("json", (value, c) => {
    const req = value as PlaceOrderRequest;
    if (!req.exchange || !req.symbol || !req.side || !req.type || !req.amount) {
      return c.text(
        "Missing required fields: exchange, symbol, side, type, amount",
        400,
      );
    }
    if (req.side !== "buy" && req.side !== "sell") {
      return c.text("side must be 'buy' or 'sell'", 400);
    }
    if (
      req.type !== "market" &&
      req.type !== "limit" &&
      req.type !== "stop" &&
      req.type !== "stop_limit"
    ) {
      return c.text(
        "type must be 'market', 'limit', 'stop', or 'stop_limit'",
        400,
      );
    }
    if (req.amount <= 0) {
      return c.text("amount must be greater than 0", 400);
    }
    return req;
  }),
  async (c) => {
    try {
      const req = c.req.valid("json") as PlaceOrderRequest;

      if (!exchanges[req.exchange]) {
        return c.json(
          {
            error: "Exchange not supported",
            timestamp: new Date().toISOString(),
          } as ErrorResponse,
          400,
        );
      }

      const ex = exchanges[req.exchange];

      if (!ex.has["createOrder"]) {
        return c.json(
          {
            error: "Exchange does not support order creation",
            timestamp: new Date().toISOString(),
          } as ErrorResponse,
          400,
        );
      }

      const order = await ex.createOrder(
        req.symbol,
        req.type,
        req.side,
        req.amount,
        req.price,
        req.params,
      );

      const response: PlaceOrderResponse = {
        order,
        timestamp: new Date().toISOString(),
      };

      return c.json(response);
    } catch (error) {
      return c.json(
        {
          error: error instanceof Error ? error.message : "Unknown error",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        500,
      );
    }
  },
);

// Cancel an order
app.delete("/api/order/:exchange/:orderId", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const orderId = c.req.param("orderId");
    const symbol = c.req.query("symbol") || undefined;

    if (!exchanges[exchange]) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const ex = exchanges[exchange];

    if (!ex.has["cancelOrder"]) {
      return c.json(
        {
          error: "Exchange does not support order cancellation",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    await ex.cancelOrder(orderId, symbol);

    const response: CancelOrderResponse = {
      success: true,
      orderId,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      } as ErrorResponse,
      500,
    );
  }
});

// Get order status
app.get("/api/order/:exchange/:orderId", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const orderId = c.req.param("orderId");
    const symbol = c.req.query("symbol") || undefined;

    if (!exchanges[exchange]) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const ex = exchanges[exchange];

    if (!ex.has["fetchOrder"]) {
      return c.json(
        {
          error: "Exchange does not support fetching order",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const order = await ex.fetchOrder(orderId, symbol);

    const response: GetOrderResponse = {
      order,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      } as ErrorResponse,
      500,
    );
  }
});

// Get open orders
app.get("/api/orders/:exchange", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const symbol = c.req.query("symbol") || undefined;

    if (!exchanges[exchange]) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const ex = exchanges[exchange];

    if (!ex.has["fetchOpenOrders"]) {
      return c.json(
        {
          error: "Exchange does not support fetching open orders",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const orders = await ex.fetchOpenOrders(symbol || undefined);

    const response: GetOpenOrdersResponse = {
      orders,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      } as ErrorResponse,
      500,
    );
  }
});

// Get closed orders
app.get("/api/orders/:exchange/closed", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const symbol = c.req.query("symbol") || undefined;
    const limit = c.req.query("limit")
      ? parseInt(c.req.query("limit") as string)
      : undefined;

    if (!exchanges[exchange]) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const ex = exchanges[exchange];

    if (!ex.has["fetchClosedOrders"]) {
      return c.json(
        {
          error: "Exchange does not support fetching closed orders",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const orders = await ex.fetchClosedOrders(
      symbol || undefined,
      undefined,
      limit,
    );

    const response: GetClosedOrdersResponse = {
      orders,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      } as ErrorResponse,
      500,
    );
  }
});

// Get order trades/fills
app.get("/api/order/:exchange/:orderId/trades", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");
    const orderId = c.req.param("orderId");
    const symbol = c.req.query("symbol") || undefined;

    if (!exchanges[exchange]) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const ex = exchanges[exchange];

    if (!ex.has["fetchOrderTrades"]) {
      return c.json(
        {
          error: "Exchange does not support fetching order trades",
          timestamp: new Date().toISOString(),
        } as ErrorResponse,
        400,
      );
    }

    const trades = await ex.fetchOrderTrades(orderId, symbol);

    const response: GetOrderTradesResponse = {
      trades,
      timestamp: new Date().toISOString(),
    };

    return c.json(response);
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      } as ErrorResponse,
      500,
    );
  }
});

// Exchange management endpoints

// Add exchange to blacklist
app.post("/api/admin/exchanges/blacklist/:exchange", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");

    if (!exchangeConfig.blacklistedExchanges.includes(exchange)) {
      exchangeConfig.blacklistedExchanges.push(exchange);
      blacklistedExchanges.add(exchange);

      // Save configuration to file
      const saved = saveExchangeConfig(exchangeConfig);
      if (!saved) {
        console.error("Failed to persist blacklist changes to file");
        return c.json(
          {
            error: "Failed to persist configuration changes",
            timestamp: new Date().toISOString(),
          },
          500,
        );
      }

      // Remove from active exchanges if it exists
      if (exchanges[exchange]) {
        delete exchanges[exchange];
      }

      console.log(`Exchange ${exchange} added to blacklist`);
    }

    return c.json({
      message: `Exchange ${exchange} blacklisted successfully`,
      blacklistedExchanges: exchangeConfig.blacklistedExchanges,
      timestamp: new Date().toISOString(),
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Remove exchange from blacklist
app.delete("/api/admin/exchanges/blacklist/:exchange", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange");

    const index = exchangeConfig.blacklistedExchanges.indexOf(exchange);
    if (index > -1) {
      exchangeConfig.blacklistedExchanges.splice(index, 1);
      blacklistedExchanges.delete(exchange);

      // Save configuration to file
      const saved = saveExchangeConfig(exchangeConfig);
      if (!saved) {
        const errorResponse: ErrorResponse = {
          error: "Failed to persist blacklist changes to file",
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 500);
      }

      // Try to initialize the exchange if it's available
      const initialized = initializeExchange(exchange);
      if (!initialized) {
        const errorResponse: ErrorResponse = {
          error: `Failed to initialize ${exchange} after removing from blacklist`,
          timestamp: new Date().toISOString(),
        };
        return c.json(errorResponse, 500);
      }

      console.log(
        `Exchange ${exchange} removed from blacklist and initialized`,
      );
    }

    return c.json({
      message: `Exchange ${exchange} removed from blacklist`,
      blacklistedExchanges: exchangeConfig.blacklistedExchanges,
      activeExchanges: Object.keys(exchanges),
      timestamp: new Date().toISOString(),
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Get exchange configuration
app.get("/api/admin/exchanges/config", adminAuth, async (c) => {
  return c.json({
    config: exchangeConfig,
    activeExchanges: Object.keys(exchanges),
    availableExchanges: ccxt.exchanges,
    timestamp: new Date().toISOString(),
  });
});

// Refresh exchanges (re-initialize user-configured exchanges)
app.post("/api/admin/exchanges/refresh", adminAuth, async (c) => {
  try {
    // Clear current exchanges
    Object.keys(exchanges).forEach((key) => delete exchanges[key]);

    // Re-initialize user-configured exchanges
    const newConfig = loadUserExchangeConfig();
    const exchangesToInit =
      newConfig.enabled.length > 0
        ? newConfig.enabled
        : [
            "binance",
            "bybit",
            "okx",
            "kraken",
            "kucoin",
            "gateio",
            "mexc",
            "bitget",
            "coinbase",
            "bingx",
            "cryptocom",
          ];

    let initializedCount = 0;
    let failedCount = 0;
    const failedExchanges: string[] = [];

    for (const exchangeId of exchangesToInit) {
      if (initializeExchange(exchangeId)) {
        initializedCount++;
      } else {
        failedCount++;
        failedExchanges.push(exchangeId);
      }
    }

    console.log(
      `Refreshed and initialized ${initializedCount}/${exchangesToInit.length} exchanges`,
    );

    if (failedCount > 0) {
      console.warn(
        `Failed to initialize ${failedCount} exchanges:`,
        failedExchanges.join(", "),
      );
    }

    return c.json({
      message: "Exchanges refreshed successfully",
      activeExchanges: Object.keys(exchanges),
      timestamp: new Date().toISOString(),
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Add new exchange dynamically
app.post("/api/admin/exchanges/add/:exchange", adminAuth, async (c) => {
  try {
    const exchange = c.req.param("exchange").toLowerCase();

    // Check if exchange is available in CCXT
    if (!ccxt.exchanges.includes(exchange)) {
      const errorResponse: ErrorResponse = {
        error: `Exchange ${exchange} is not available in CCXT library`,
        availableExchanges: ccxt.exchanges,
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    // Check if already blacklisted
    if (exchangeConfig.blacklistedExchanges.includes(exchange)) {
      const errorResponse: ErrorResponse = {
        error: `Exchange ${exchange} is blacklisted. Remove from blacklist first.`,
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 400);
    }

    // Try to initialize the exchange
    const success = initializeExchange(exchange);
    if (!success) {
      const errorResponse: ErrorResponse = {
        error: `Failed to initialize exchange ${exchange}`,
        timestamp: new Date().toISOString(),
      };
      return c.json(errorResponse, 500);
    }

    return c.json({
      message: `Exchange ${exchange} added successfully`,
      activeExchanges: Object.keys(exchanges),
      timestamp: new Date().toISOString(),
    });
  } catch (error) {
    const errorResponse: ErrorResponse = {
      error: error instanceof Error ? error.message : "Unknown error",
      timestamp: new Date().toISOString(),
    };
    return c.json(errorResponse, 500);
  }
});

// Get balance for an exchange (requires API keys and admin auth)
app.get("/api/balance/:exchange", adminAuth, async (c) => {
  const exchange = c.req.param("exchange").toLowerCase();

  try {
    if (!ccxt.exchanges.includes(exchange)) {
      return c.json(
        {
          error: "Exchange not supported",
          timestamp: new Date().toISOString(),
        },
        400,
      );
    }

    if (exchanges[exchange] && exchanges[exchange].apiKey) {
      const balance = await exchanges[exchange].fetchBalance();
      return c.json({
        exchange,
        total: balance.total || {},
        free: balance.free || {},
        used: balance.used || {},
        raw: balance,
        timestamp: new Date().toISOString(),
      });
    }

    return c.json(
      {
        error:
          "Exchange not initialized with API keys. Use POST /api/balance/:exchange with API keys.",
        timestamp: new Date().toISOString(),
      },
      400,
    );
  } catch (error) {
    return c.json(
      {
        error: error instanceof Error ? error.message : "Unknown error",
        timestamp: new Date().toISOString(),
      },
      500,
    );
  }
});

// Create exchange with API keys and get balance
interface BalanceRequest {
  apiKey: string;
  apiSecret: string;
  testnet?: boolean;
}

// Placeholder for balance - use existing order endpoint for now

// Global error handler
app.onError((error, c) => {
  console.error("Error:", error);
  const errorResponse: ErrorResponse = {
    error: "Internal server error",
    message: error.message,
    timestamp: new Date().toISOString(),
  };
  return c.json(errorResponse, 500);
});

// 404 handler
app.notFound((c) => {
  const errorResponse: ErrorResponse = {
    error: "Not Found",
    message: `Route ${c.req.path} not found`,
    timestamp: new Date().toISOString(),
  };
  return c.json(errorResponse, 404);
});

// Export app as named export for tests
// NOTE: We explicitly do NOT use `export default app` because Bun auto-starts
// a server when the default export has a `fetch` function, which causes EADDRINUSE
// since we already call Bun.serve() manually.
export { app };

const shouldAutoServe =
  import.meta.main &&
  process.env.BUN_NO_SERVER !== "true" &&
  process.env.CCXT_AUTO_SERVE !== "false";

if (shouldAutoServe) {
  console.log(`🚀 CCXT Service starting on port ${PORT}`);
  console.log(`📊 Supported exchanges: ${Object.keys(exchanges).join(", ")}`);
  console.log(`Starting Bun server with app.fetch type: ${typeof app.fetch}`);

  const grpcPort = process.env.CCXT_GRPC_PORT
    ? parseInt(process.env.CCXT_GRPC_PORT)
    : 50051;
  const grpcServer = startGrpcServer(exchanges, grpcPort);

  const startServer = async () => {
    try {
      const server = Bun.serve({
        fetch: app.fetch,
        port: Number(PORT),
        // reusePort can cause EADDRINUSE on some Linux configurations
        // Only enable if explicitly requested via environment variable
        reusePort: process.env.BUN_REUSE_PORT === "true",
      });
      console.log(
        `✅ CCXT Service successfully started on port ${server.port}`,
      );

      // Initialize Sentry AFTER server startup to avoid auto-instrumentation conflicts
      if (isSentryEnabled) {
        await initializeSentry();
      }

      // Handle graceful shutdown
      process.on("SIGTERM", async () => {
        console.log("SIGTERM received, shutting down gracefully...");
        // Flush Sentry events before shutdown
        if (isSentryEnabled) {
          await sentryFlush(2000);
        }
        // Clear intervals to prevent memory leaks
        clearInterval(cleanupIntervalId);
        server.stop();
        grpcServer.forceShutdown();
        process.exit(0);
      });

      process.on("SIGINT", async () => {
        console.log("SIGINT received, shutting down gracefully...");
        // Flush Sentry events before shutdown
        if (isSentryEnabled) {
          await sentryFlush(2000);
        }
        // Clear intervals to prevent memory leaks
        clearInterval(cleanupIntervalId);
        server.stop();
        grpcServer.forceShutdown();
        process.exit(0);
      });
    } catch (error: any) {
      if (error?.code === "EADDRINUSE") {
        console.error(
          `❌ ERROR: Port ${PORT} is already in use. Cannot start CCXT service.`,
        );
        console.error(
          `   This typically happens when a previous instance is still running.`,
        );
        console.error(`   Solutions:`);
        console.error(`   1. Set a different PORT environment variable`);
        console.error(
          `   2. Kill the process using port ${PORT}: lsof -ti:${PORT} | xargs kill -9`,
        );
        console.error(
          `   3. Set CCXT_AUTO_SERVE=false if you don't need the server`,
        );
        process.exit(1);
      } else {
        console.error(`❌ Failed to start CCXT service:`, error);
        // Capture startup error to Sentry if possible
        if (isSentryEnabled) {
          captureException(error, { phase: "startup" });
        }
        throw error;
      }
    }
  };

  startServer();
} else if (import.meta.main) {
  console.log(
    "CCXT auto-serve disabled (BUN_NO_SERVER=true or CCXT_AUTO_SERVE=false); server not started",
  );
}
