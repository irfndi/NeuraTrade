import { Effect } from "effect";
import { Bot, GrammyError, HttpError } from "grammy";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger } from "hono/logger";
import { secureHeaders } from "hono/secure-headers";
import { startGrpcServer } from "./grpc-server";

const resolvePort = (raw: string | undefined, fallback: number) => {
  if (!raw) {
    return fallback;
  }
  const numericPort = Number(raw);
  if (!Number.isNaN(numericPort) && numericPort > 0 && numericPort < 65536) {
    return numericPort;
  }
  console.warn(
    `Invalid port value provided (${raw}). Falling back to default (${fallback}).`,
  );
  return fallback;
};

type TelegramConfig = {
  botToken: string;
  webhookUrl: string | null;
  webhookPath: string;
  webhookSecret: string | null;
  usePolling: boolean;
  port: number;
  apiBaseUrl: string;
  adminApiKey: string;
};

const loadConfig = Effect.try((): TelegramConfig => {
  const botToken = process.env.TELEGRAM_BOT_TOKEN;
  if (!botToken) {
    throw new Error("TELEGRAM_BOT_TOKEN environment variable must be set");
  }

  const adminApiKey = process.env.ADMIN_API_KEY || "";
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

  const apiBaseUrl = (
    process.env.TELEGRAM_API_BASE_URL || "http://localhost:8080"
  ).replace(/\/$/, "");

  const webhookUrlRaw = (process.env.TELEGRAM_WEBHOOK_URL || "").trim();
  const webhookUrl = webhookUrlRaw.length > 0 ? webhookUrlRaw : null;
  const webhookPath = (process.env.TELEGRAM_WEBHOOK_PATH || "").trim();
  const resolvedWebhookPath = webhookPath
    ? webhookPath
    : webhookUrl
      ? new URL(webhookUrl).pathname
      : "/telegram/webhook";

  const usePollingEnv = (process.env.TELEGRAM_USE_POLLING || "").toLowerCase();
  const usePolling =
    usePollingEnv === "true" || usePollingEnv === "1" || webhookUrl === null;

  return {
    botToken,
    webhookUrl,
    webhookPath: resolvedWebhookPath.startsWith("/")
      ? resolvedWebhookPath
      : `/${resolvedWebhookPath}`,
    webhookSecret: process.env.TELEGRAM_WEBHOOK_SECRET || null,
    usePolling,
    port: resolvePort(process.env.TELEGRAM_PORT, 3002),
    apiBaseUrl,
    adminApiKey,
  };
});

const config = Effect.runSync(loadConfig);

const bot = new Bot(config.botToken);

const apiFetch = <T>(
  path: string,
  init: RequestInit = {},
  requireAdmin = false,
) =>
  Effect.tryPromise(async () => {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...(init.headers as Record<string, string> | undefined),
    };

    if (requireAdmin) {
      headers["X-API-Key"] = config.adminApiKey;
    }

    const response = await fetch(`${config.apiBaseUrl}${path}`, {
      ...init,
      headers,
    });

    const payload = await response
      .json()
      .catch(() => ({ message: "Failed to parse response" }));

    if (!response.ok) {
      const message =
        payload?.error ||
        payload?.message ||
        `API request failed (${response.status})`;
      throw new Error(message);
    }

    return payload as T;
  });

const getUserByChatId = (chatId: string) =>
  apiFetch<{
    user: { id: string; subscription_tier: string; created_at: string };
  }>(`/api/v1/telegram/internal/users/${encodeURIComponent(chatId)}`, {}, true);

const getNotificationPreference = (userId: string) =>
  apiFetch<{ enabled: boolean }>(
    `/api/v1/telegram/internal/notifications/${encodeURIComponent(userId)}`,
    {},
    true,
  );

const setNotificationPreference = (userId: string, enabled: boolean) =>
  apiFetch(
    `/api/v1/telegram/internal/notifications/${encodeURIComponent(userId)}`,
    {
      method: "POST",
      body: JSON.stringify({ enabled }),
    },
    true,
  );

const registerTelegramUser = (chatId: string, userId: number) =>
  apiFetch(
    "/api/v1/users/register",
    {
      method: "POST",
      body: JSON.stringify({
        email: `telegram_${userId}@neuratrade.ai`,
        password: `${globalThis.crypto.randomUUID()}${globalThis.crypto.randomUUID()}`,
        telegram_chat_id: chatId,
      }),
    },
    false,
  );

const formatOpportunitiesMessage = (opps: any[]) => {
  if (!opps || opps.length === 0) {
    return "📊 No arbitrage opportunities found right now.";
  }

  const top = opps.slice(0, 5);
  const lines = ["⚡ Top Arbitrage Opportunities", ""];

  top.forEach((opp, index) => {
    lines.push(`${index + 1}. ${opp.symbol}`);
    lines.push(`   Buy: ${opp.buy_exchange} @ ${opp.buy_price}`);
    lines.push(`   Sell: ${opp.sell_exchange} @ ${opp.sell_price}`);
    lines.push(`   Profit: ${Number(opp.profit_percent).toFixed(2)}%`);
    lines.push("");
  });

  return lines.join("\n");
};

bot.command("start", async (ctx) => {
  const chatId = ctx.chat?.id;
  const userId = ctx.from?.id;

  if (!chatId || !userId) {
    await ctx.reply("Unable to start: missing chat information.");
    return;
  }

  const chatIdStr = String(chatId);

  const userResult = await Effect.runPromise(
    Effect.catchAll(getUserByChatId(chatIdStr), () => Effect.succeed(null)),
  );

  if (!userResult) {
    await Effect.runPromise(
      Effect.catchAll(registerTelegramUser(chatIdStr, userId), () =>
        Effect.succeed(null),
      ),
    );
  }

  const welcomeMsg =
    "🚀 Welcome to NeuraTrade!\n\n" +
    "✅ You're now registered and ready to receive arbitrage alerts!\n\n" +
    "Use /opportunities to see current opportunities.\n" +
    "Use /help to see available commands.";

  await ctx.reply(welcomeMsg);
});

bot.command("help", async (ctx) => {
  const msg =
    "🤖 NeuraTrade Bot Commands:\n\n" +
    "/start - Register and get started\n" +
    "/opportunities - View current arbitrage opportunities\n" +
    "/settings - Configure your alert preferences\n" +
    "/upgrade - Upgrade to premium subscription\n" +
    "/status - Check your account status\n" +
    "/stop - Pause all notifications\n" +
    "/resume - Resume notifications\n" +
    "/help - Show this help message\n\n" +
    "💡 Tip: You'll receive automatic alerts when profitable opportunities are detected!";

  await ctx.reply(msg);
});

bot.command("opportunities", async (ctx) => {
  const response = await Effect.runPromise(
    Effect.catchAll(
      apiFetch<{ opportunities: any[] }>(
        "/api/v1/arbitrage/opportunities?limit=5&min_profit=0.5",
      ),
      (error) => Effect.fail(error as Error),
    ),
  ).catch(async (error) => {
    await ctx.reply(
      `❌ Failed to fetch opportunities. Please try again later. (${(error as Error).message})`,
    );
    return null;
  });

  if (!response) {
    return;
  }

  await ctx.reply(formatOpportunitiesMessage(response.opportunities));
});

bot.command("status", async (ctx) => {
  const chatId = ctx.chat?.id;
  const userId = ctx.from?.id;
  if (!chatId) {
    await ctx.reply("Unable to lookup status: missing chat information.");
    return;
  }

  const userResult = await Effect.runPromise(
    Effect.catchAll(getUserByChatId(String(chatId)), () =>
      Effect.succeed(null),
    ),
  );

  if (!userResult) {
    await ctx.reply("User not found. Please use /start to register.");
    return;
  }

  const preference = userId
    ? await Effect.runPromise(
        Effect.catchAll(getNotificationPreference(String(userId)), () =>
          Effect.succeed({ enabled: true }),
        ),
      )
    : { enabled: true };

  const createdAt = new Date(userResult.user.created_at).toLocaleDateString();
  const tier = userResult.user.subscription_tier;
  const notificationStatus = preference.enabled ? "Active" : "Paused";

  const msg =
    "📊 Account Status:\n\n" +
    `💰 Subscription: ${tier}\n` +
    `📅 Member since: ${createdAt}\n` +
    `🔔 Notifications: ${notificationStatus}`;

  await ctx.reply(msg);
});

bot.command("settings", async (ctx) => {
  const userId = ctx.from?.id;
  if (!userId) {
    await ctx.reply("Unable to fetch settings right now.");
    return;
  }

  const preference = await Effect.runPromise(
    Effect.catchAll(getNotificationPreference(String(userId)), () =>
      Effect.succeed({ enabled: true }),
    ),
  );

  const statusIcon = preference.enabled ? "✅" : "❌";
  const statusText = preference.enabled ? "ON" : "OFF";

  const msg =
    "⚙️ Alert Settings:\n\n" +
    `🔔 Notifications: ${statusIcon} ${statusText}\n` +
    "📊 Min Profit Threshold: 0.5%\n" +
    "⏰ Alert Frequency: Every 5 minutes\n" +
    "💰 Subscription: Free Tier\n\n" +
    "To change settings:\n" +
    "/stop - Pause notifications\n" +
    "/resume - Resume notifications\n" +
    "/upgrade - Upgrade to premium for more options";

  await ctx.reply(msg);
});

bot.command("stop", async (ctx) => {
  const userId = ctx.from?.id;
  if (!userId) {
    await ctx.reply("Unable to update notifications.");
    return;
  }

  await Effect.runPromise(
    Effect.catchAll(setNotificationPreference(String(userId), false), () =>
      Effect.succeed(null),
    ),
  );

  const msg =
    "⏸️ Notifications Paused\n\n" +
    "You will no longer receive arbitrage alerts.\n\n" +
    "Use /resume to start receiving alerts again.";

  await ctx.reply(msg);
});

bot.command("resume", async (ctx) => {
  const userId = ctx.from?.id;
  if (!userId) {
    await ctx.reply("Unable to update notifications.");
    return;
  }

  await Effect.runPromise(
    Effect.catchAll(setNotificationPreference(String(userId), true), () =>
      Effect.succeed(null),
    ),
  );

  const msg =
    "▶️ Notifications Resumed\n\n" +
    "You will now receive arbitrage alerts again.\n\n" +
    "Use /opportunities to see current opportunities.";

  await ctx.reply(msg);
});

bot.command("upgrade", async (ctx) => {
  const msg =
    "🎯 Upgrade to Premium\n\n" +
    "✨ Premium Benefits:\n" +
    "• Unlimited alerts\n" +
    "• Instant notifications\n" +
    "• Custom profit thresholds\n" +
    "• Website dashboard access\n" +
    "• Priority support\n\n" +
    "💰 Price: $29/month\n\n" +
    "To upgrade, please contact support.";

  await ctx.reply(msg);
});

bot.on("message:text", async (ctx) => {
  await ctx.reply("Thanks for your message! 👋\n\nTry /help for commands.");
});

bot.catch((err) => {
  const ctx = err.ctx;
  console.error(`Error while handling update ${ctx.update.update_id}:`);
  const error = err.error;
  if (error instanceof GrammyError) {
    console.error("Error in request:", error.description);
  } else if (error instanceof HttpError) {
    console.error("Could not contact Telegram:", error);
  } else {
    console.error("Unknown error:", error);
  }
});

const app = new Hono();
app.use("*", secureHeaders());
app.use("*", cors());
app.use("*", logger());

app.get("/health", (c) => {
  return c.json({ status: "healthy", service: "telegram-service" }, 200);
});

app.post("/send-message", async (c) => {
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

if (!config.usePolling) {
  app.post(config.webhookPath, async (c) => {
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

const grpcPort = process.env.TELEGRAM_GRPC_PORT
  ? parseInt(process.env.TELEGRAM_GRPC_PORT)
  : 50052;
const grpcServer = startGrpcServer(bot, grpcPort);

const startBot = async () => {
  if (config.usePolling) {
    console.log("Starting Telegram bot in polling mode");
    await bot.api.deleteWebhook({ drop_pending_updates: true });
    bot.start();
    return;
  }

  if (!config.webhookUrl) {
    throw new Error("TELEGRAM_WEBHOOK_URL must be set for webhook mode");
  }

  console.log(`Setting Telegram webhook to ${config.webhookUrl}`);
  await bot.api.setWebhook(config.webhookUrl, {
    secret_token: config.webhookSecret || undefined,
  });
};

startBot().catch((error) => {
  console.error("Failed to start Telegram bot:", error);
  process.exit(1);
});

process.on("SIGTERM", () => {
  console.log("SIGTERM received, shutting down...");
  server.stop();
  grpcServer.forceShutdown();
  process.exit(0);
});

process.on("SIGINT", () => {
  console.log("SIGINT received, shutting down...");
  server.stop();
  grpcServer.forceShutdown();
  process.exit(0);
});
