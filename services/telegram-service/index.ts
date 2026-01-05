import { Effect } from "effect";
import { Bot, GrammyError, HttpError } from "grammy";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
import { secureHeaders } from "hono/secure-headers";
import { startGrpcServer } from "./grpc-server";
import {
  isSentryEnabled,
  sentryMiddleware,
  initializeSentry,
  captureException,
  flush as sentryFlush,
  trackBotMode,
} from "./sentry";
import { loadConfig } from "./config";
import { createApi } from "./api";
import {
  handleStart,
  handleHelp,
  handleOpportunities,
  handleStatus,
  handleSettings,
  handleStop,
  handleResume,
  handleUpgrade,
  handleMessageText,
} from "./bot-handlers";

const config = loadConfig();
const api = createApi(config);

// Only create bot if token is available
const bot = config.botTokenMissing ? null : new Bot(config.botToken);

// Only register bot commands if bot is available
if (bot) {
  bot.command("start", handleStart(api));
  bot.command("help", handleHelp());
  bot.command("opportunities", handleOpportunities(api));
  bot.command("status", handleStatus(api));
  bot.command("settings", handleSettings(api));
  bot.command("stop", handleStop(api));
  bot.command("resume", handleResume(api));
  bot.command("upgrade", handleUpgrade());
  bot.on("message:text", handleMessageText());

  bot.catch((err) => {
    const ctx = err.ctx;
    console.error(`Error while handling update ${ctx.update.update_id}:`);
    const error = err.error;

    // Capture error to Sentry with context
    if (isSentryEnabled) {
      captureException(error, {
        update_id: ctx.update.update_id,
        chat_id: ctx.chat?.id,
        user_id: ctx.from?.id,
        message_text: ctx.message?.text,
        error_type:
          error instanceof GrammyError
            ? "GrammyError"
            : error instanceof HttpError
              ? "HttpError"
              : "UnknownError",
      });
    }

    if (error instanceof GrammyError) {
      console.error("Error in request:", error.description);
    } else if (error instanceof HttpError) {
      console.error("Could not contact Telegram:", error);
    } else {
      console.error("Unknown error:", error);
    }
  });
}

const app = new Hono();
app.use("*", secureHeaders());
app.use("*", cors());
app.use("*", logger());
if (isSentryEnabled) {
  app.use("*", sentryMiddleware);
}

app.get("/health", (c) => {
  // Return degraded status if bot is not configured
  if (config.botTokenMissing) {
    return c.json(
      {
        status: "degraded",
        service: "telegram-service",
        error: "TELEGRAM_BOT_TOKEN not configured",
        bot_active: false,
      },
      200, // Still return 200 so container doesn't restart in a loop
    );
  }

  if (config.configError) {
    return c.json(
      {
        status: "degraded",
        service: "telegram-service",
        error: config.configError,
        bot_active: !!bot,
      },
      200,
    );
  }

  return c.json(
    { status: "healthy", service: "telegram-service", bot_active: true },
    200,
  );
});

app.post("/send-message", async (c) => {
  // If bot is not configured, return service unavailable
  if (!bot) {
    return c.json(
      { error: "Bot not available (TELEGRAM_BOT_TOKEN not configured)" },
      503,
    );
  }

  // If ADMIN_API_KEY is not configured, disable admin endpoints
  if (!config.adminApiKey) {
    return c.json(
      { error: "Admin endpoints are disabled (ADMIN_API_KEY not set)" },
      503,
    );
  }

  const apiKey = c.req.header("X-API-Key");
  if (!apiKey || apiKey !== config.adminApiKey) {
    return c.json({ error: "Unauthorized" }, 401);
  }

  const body = await c.req.json();
  const { chatId, text, parseMode } = body;

  if (!chatId || !text) {
    return c.json({ error: "Missing chatId or text" }, 400);
  }

  try {
    await bot.api.sendMessage(chatId, text, { parse_mode: parseMode });
    return c.json({ ok: true });
  } catch (error) {
    console.error("Failed to send message:", error);
    return c.json(
      { error: "Failed to send message", details: String(error) },
      500,
    );
  }
});

if (!config.usePolling && bot) {
  app.post(config.webhookPath, async (c) => {
    if (!bot) {
      return c.json(
        { error: "Bot not available (TELEGRAM_BOT_TOKEN not configured)" },
        503,
      );
    }

    if (config.webhookSecret) {
      const provided = c.req.header("X-Telegram-Bot-Api-Secret-Token");
      if (!provided || provided !== config.webhookSecret) {
        return c.json({ error: "Unauthorized" }, 401);
      }
    }

    const update = await c.req.json();
    await bot.handleUpdate(update);
    return c.json({ ok: true });
  });
}

const server = Bun.serve({
  fetch: app.fetch,
  port: config.port,
  // reusePort can cause EADDRINUSE on some Linux configurations
  // Only enable if explicitly requested via environment variable
  reusePort: process.env.BUN_REUSE_PORT === "true",
});

console.log(`🤖 Telegram service listening on port ${server.port}`);

// Initialize Sentry AFTER server startup to avoid auto-instrumentation conflicts
if (isSentryEnabled) {
  initializeSentry().then((initialized) => {
    if (initialized) {
      console.log("✓ Sentry initialized for telegram-service");
    }
  });
}

const grpcPort = process.env.TELEGRAM_GRPC_PORT
  ? parseInt(process.env.TELEGRAM_GRPC_PORT)
  : 50052;

// Only start gRPC server if bot is available
const grpcServer = bot ? startGrpcServer(bot, grpcPort) : null;

const startBot = async () => {
  // Skip bot startup if token is not configured
  if (!bot) {
    console.warn("⚠️ Bot startup skipped: TELEGRAM_BOT_TOKEN not configured");
    console.warn("   Service running in degraded mode (health check only)");
    return;
  }

  if (config.usePolling) {
    console.log("Starting Telegram bot in polling mode");
    if (isSentryEnabled) {
      trackBotMode("polling");
    }
    await bot.api.deleteWebhook({ drop_pending_updates: true });
    bot.start();
    return;
  }

  if (!config.webhookUrl) {
    throw new Error("TELEGRAM_WEBHOOK_URL must be set for webhook mode");
  }

  // Initialize bot before handling webhook updates
  // This fetches bot info which is required for update handling
  await bot.init();

  console.log(`Setting Telegram webhook to ${config.webhookUrl}`);
  if (isSentryEnabled) {
    trackBotMode("webhook");
  }
  await bot.api.setWebhook(config.webhookUrl, {
    secret_token: config.webhookSecret || undefined,
  });
};

startBot().catch((error) => {
  console.error("Failed to start Telegram bot:", error);
  // Don't exit if bot fails to start - keep health check running
  if (bot) {
    console.error("Bot failed to start but service will continue running");
  }
});

process.on("SIGTERM", async () => {
  console.log("SIGTERM received, shutting down...");
  // Flush Sentry events before shutdown
  if (isSentryEnabled) {
    await sentryFlush(2000);
  }
  server.stop();
  if (grpcServer) {
    grpcServer.forceShutdown();
  }
  process.exit(0);
});

process.on("SIGINT", async () => {
  console.log("SIGINT received, shutting down...");
  // Flush Sentry events before shutdown
  if (isSentryEnabled) {
    await sentryFlush(2000);
  }
  server.stop();
  if (grpcServer) {
    grpcServer.forceShutdown();
  }
  process.exit(0);
});
