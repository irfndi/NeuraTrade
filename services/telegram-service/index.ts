import { Bot, GrammyError, HttpError } from "grammy";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger as honoLogger } from "hono/logger";
import { secureHeaders } from "hono/secure-headers";
import { config } from "./src/config";
import { BackendApiClient } from "./src/api/client";
import { registerAllCommands } from "./src/commands";
import { SessionManager } from "./src/session";
import { logger } from "./src/utils/logger";
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

const bot = new Bot(config.botToken);
const api = new BackendApiClient({
  baseUrl: config.apiBaseUrl,
  adminKey: config.adminApiKey,
});
const sessions = new SessionManager();

registerAllCommands(bot, api, sessions);

bot.on("message:text", async (ctx) => {
  await ctx.reply("Thanks for your message! 👋\n\nTry /help for commands.");
});

bot.catch((err) => {
  const ctx = err.ctx;
  const error = err.error;
  if (error instanceof GrammyError) {
    logger.error("Telegram request error", error, {
      updateId: ctx.update.update_id,
      description: error.description,
    });
  } else if (error instanceof HttpError) {
    logger.error("Telegram connection error", error, {
      updateId: ctx.update.update_id,
    });
  } else {
    logger.error("Unknown bot error", error as Error, {
      updateId: ctx.update.update_id,
    });
  }
});

const app = new Hono();
app.use("*", secureHeaders());
app.use("*", cors());
app.use("*", honoLogger());

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
    logger.error("Failed to send message", error as Error, { chatId });
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
  reusePort: process.env.BUN_REUSE_PORT === "true",
});

logger.info("Telegram service started", {
  port: config.port,
  mode: config.usePolling ? "polling" : "webhook",
});

const grpcServer = startGrpcServer(bot, config.grpcPort);

const startBot = async () => {
  // Skip bot startup if token is not configured
  if (!bot) {
    console.warn("⚠️ Bot startup skipped: TELEGRAM_BOT_TOKEN not configured");
    console.warn("   Service running in degraded mode (health check only)");
    return;
  }

  if (config.usePolling) {
    logger.info("Starting bot in polling mode");
    await bot.api.deleteWebhook({ drop_pending_updates: true });
    bot.start();
    return;
  }

  if (!config.webhookUrl) {
    throw new Error("TELEGRAM_WEBHOOK_URL must be set for webhook mode");
  }

  logger.info("Setting Telegram webhook", { webhookUrl: config.webhookUrl });
  await bot.api.setWebhook(config.webhookUrl, {
    secret_token: config.webhookSecret || undefined,
  });
};

startBot().catch((error) => {
  logger.error("Failed to start bot", error);
  process.exit(1);
});

process.on("SIGTERM", () => {
  logger.info("SIGTERM received, shutting down");
  server.stop();
  if (grpcServer) {
    grpcServer.forceShutdown();
  }
  process.exit(0);
});

process.on("SIGINT", () => {
  logger.info("SIGINT received, shutting down");
  server.stop();
  if (grpcServer) {
    grpcServer.forceShutdown();
  }
  process.exit(0);
});
