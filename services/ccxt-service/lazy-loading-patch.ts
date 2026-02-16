/**
 * LAZY LOADING IMPLEMENTATION FOR CCXT SERVICE
 *
 * This patch implements lazy loading for exchanges to prevent resource exhaustion:
 *
 * 1. Only initialize exchanges with API keys at startup
 * 2. Defer initialization of public-data-only exchanges until first request
 * 3. Track initialized exchanges to avoid duplicate initialization
 * 4. Provide on-demand initialization when specific exchanges are requested
 *
 * BENEFITS:
 * - Reduced memory footprint (only load what's needed)
 * - Faster startup time (skip unused exchanges)
 * - Prevents rate limiting (fewer unnecessary connections)
 * - Better resource utilization
 */

// Add these module-level variables at the top of index.ts (after exchanges declaration):
/*
const initializedExchanges = new Set<string>(); // Track initialized exchanges
const pendingApiKeyExchanges = new Set<string>(); // Exchanges waiting for API keys
*/

// Add this helper function before initializeExchange:
function hasApiKeyConfigured(exchangeId: string): boolean {
  const apiKey = process.env[`API_KEY_${exchangeId.toUpperCase()}`];
  const apiSecret = process.env[`API_SECRET_${exchangeId.toUpperCase()}`];
  return !!(apiKey && apiSecret);
}

// Replace initializeExchange function with this lazy-loading version:
function initializeExchange(
  exchangeId: string,
  forceInitialize = false,
): boolean {
  try {
    if (blacklistedExchanges.has(exchangeId)) {
      console.log(`Skipping blacklisted exchange: ${exchangeId}`);
      return false;
    }

    // Check if already initialized
    if (initializedExchanges.has(exchangeId)) {
      return true;
    }

    // Check if API keys are required but not configured
    const hasKeys = hasApiKeyConfigured(exchangeId);
    if (!forceInitialize && !hasKeys) {
      // Mark as pending API key configuration, don't initialize yet
      pendingApiKeyExchanges.add(exchangeId);
      console.log(
        `⏳ Deferred initialization for ${exchangeId} (waiting for API keys)`,
      );
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

    // Initialize the exchange
    const exchange = new ExchangeClass(config);

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
    initializedExchanges.add(exchangeId);

    const keyStatus = hasKeys ? "🔑 with API keys" : "📡 public data only";
    console.log(
      `✓ Successfully initialized exchange: ${exchangeId} (${exchange.name}) ${keyStatus}`,
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

// Update startup initialization to only load exchanges with API keys:
/*
// Replace the initialization loop with:
console.log("=== Initializing Exchanges (Lazy Loading Enabled) ===");

// Only initialize exchanges with API keys at startup
for (const exchangeId of priorityExchanges) {
  if (hasApiKeyConfigured(exchangeId)) {
    if (initializeExchange(exchangeId, true)) {
      initializedCount++;
    } else {
      failedCount++;
      failedExchanges.push(exchangeId);
    }
  } else {
    console.log(`⏭️  Skipping ${exchangeId} (no API keys configured, will load on demand)`);
    pendingApiKeyExchanges.add(exchangeId);
  }
}

console.log(
  `Startup initialization complete: ${initializedCount} exchanges loaded`,
  `${pendingApiKeyExchanges.size} deferred for lazy loading`
);
*/

// Add on-demand initialization endpoint:
/*
app.post("/api/admin/exchanges/load/:exchange", adminAuth, async (c) => {
  const exchange = c.req.param("exchange").toLowerCase();
  
  const success = initializeExchange(exchange, true);
  
  if (success) {
    return c.json({
      success: true,
      message: `Exchange ${exchange} loaded successfully`,
      initialized: Object.keys(exchanges).length,
    });
  } else {
    return c.json({
      success: false,
      error: `Failed to load exchange ${exchange}`,
    }, 400);
  }
});

// Add endpoint to list pending exchanges:
app.get("/api/admin/exchanges/pending", adminAuth, async (c) => {
  return c.json({
    pending: Array.from(pendingApiKeyExchanges),
    count: pendingApiKeyExchanges.size,
  });
});
*/
