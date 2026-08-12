import { Context } from "grammy";
import { Effect } from "effect";
import { Api, ApiException, isApiError, extractApiError } from "./api";

export const formatOpportunitiesMessage = (opps: any[]) => {
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

export const handleStart = (api: Api) => async (ctx: Context) => {
  console.debug("[Bot] Received /start command from:", ctx.from?.id);
  const chatId = ctx.chat?.id;
  const userId = ctx.from?.id;

  if (!chatId || !userId) {
    await ctx.reply("Unable to start: missing chat information.");
    return;
  }

  const chatIdStr = String(chatId);

  // Check if user exists (with proper error handling)
  const userResult = await Effect.runPromise(
    Effect.catchIf(
      api.getUserByChatId(chatIdStr),
      (_error): _error is ApiException => true,
      (error) => {
        if (isApiError(error)) {
          if (error.type === "auth_failed") {
            console.error(
              "[Start] Authentication failed - ADMIN_API_KEY mismatch",
            );
            // Return special marker for auth error
            return Effect.succeed({ _authError: true as const });
          }
          if (error.type === "not_found") {
            // User doesn't exist, this is expected for new users
            return Effect.succeed(null);
          }
        }
        console.error("[Start] Unexpected error checking user:", error);
        return Effect.succeed(null);
      },
    ),
  );

  // Handle authentication configuration error
  if (userResult && "_authError" in userResult) {
    await ctx.reply(
      "⚠️ Service configuration error. Please try again later or contact support.",
    );
    return;
  }

  // Register new user if not found
  if (!userResult) {
    let registrationSucceeded = false;
    let registrationError: string | null = null;

    try {
      const registerResult = await Effect.runPromise(
        api.registerTelegramUser(chatIdStr, userId),
      );
      // Check for explicit success - registerResult !== null covers empty objects {}
      // which indicate API returned successfully (even with empty response body)
      if (registerResult !== null && registerResult !== undefined) {
        registrationSucceeded = true;
      }
    } catch (error) {
      // Use extractApiError for cleaner error extraction
      const apiError = extractApiError(error);
      if (apiError) {
        console.error(
          `[Start] Registration failed: ${apiError.type} - ${apiError.message}`,
        );
        registrationError =
          apiError.type === "network_error"
            ? "Unable to connect to the server. Please try again later."
            : apiError.type === "server_error"
              ? "Server is temporarily unavailable. Please try again in a few minutes."
              : apiError.message;
      } else {
        console.error("[Start] Unexpected registration error:", error);
        registrationError =
          "An unexpected error occurred. Please try again later.";
      }
    }

    if (!registrationSucceeded) {
      // Format error message consistently - avoid "Error: Error:" duplication
      const formattedError = registrationError
        ? registrationError.startsWith("Error:")
          ? registrationError
          : registrationError
        : "";
      await ctx.reply(
        "❌ Registration failed.\n\n" +
          (formattedError ? `${formattedError}\n\n` : "") +
          "Please try again using /start.",
      );
      return;
    }
  }

  const welcomeMsg =
    "🚀 Welcome to Celebrum AI!\n\n" +
    "✅ You're now registered and ready to receive arbitrage alerts!\n\n" +
    "Use /opportunities to see current opportunities.\n" +
    "Use /help to see available commands.";

  await ctx.reply(welcomeMsg);
};

export const handleHelp = () => async (ctx: Context) => {
  const msg =
    "🤖 Celebrum AI Bot Commands:\n\n" +
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
};

export const handleOpportunities = (api: Api) => async (ctx: Context) => {
  try {
    const response = await Effect.runPromise(api.getOpportunities());
    await ctx.reply(formatOpportunitiesMessage(response.opportunities));
  } catch (error) {
    let errorMessage = "An unexpected error occurred.";

    const apiError = extractApiError(error);
    if (apiError) {
      console.error(
        `[Opportunities] API error: ${apiError.type} - ${apiError.message}`,
      );
      errorMessage =
        apiError.type === "network_error"
          ? "Unable to connect to the server."
          : apiError.type === "server_error"
            ? "Server is temporarily unavailable."
            : apiError.message;
    } else {
      console.error("[Opportunities] Unexpected error:", error);
    }

    await ctx.reply(
      `❌ Failed to fetch opportunities.\n\n${errorMessage}\n\nPlease try again later.`,
    );
  }
};

export const handleStatus = (api: Api) => async (ctx: Context) => {
  const chatId = ctx.chat?.id;
  const userId = ctx.from?.id;
  if (!chatId) {
    await ctx.reply("Unable to lookup status: missing chat information.");
    return;
  }

  // Try to fetch user - with improved error detection
  let userResult: {
    user: { id: string; subscription_tier: string; created_at: string };
  } | null = null;
  let errorType: "auth_failed" | "not_found" | "error" | null = null;
  let errorMessage = "";

  try {
    userResult = await Effect.runPromise(api.getUserByChatId(String(chatId)));
  } catch (error) {
    if (isApiError(error)) {
      errorType =
        error.type === "auth_failed"
          ? "auth_failed"
          : error.type === "not_found"
            ? "not_found"
            : "error";
      errorMessage = error.message;

      if (error.type === "auth_failed") {
        console.error(
          "[Status] Authentication failed - check ADMIN_API_KEY configuration",
        );
      } else {
        console.error(`[Status] API error (${error.type}): ${error.message}`);
      }
    } else {
      errorType = "error";
      errorMessage = String(error);
      console.error("[Status] Unexpected error:", error);
    }
  }

  // Handle different error types with specific messages
  if (errorType === "auth_failed") {
    console.error(
      "[Status] Backend auth failed - ADMIN_API_KEY may be misconfigured or empty",
    );
    await ctx.reply(
      "⚠️ Service configuration issue detected.\n\n" +
        "The bot cannot communicate with the backend server.\n" +
        "This is likely a deployment configuration issue.\n\n" +
        "Please contact the administrator.",
    );
    return;
  }

  if (errorType === "not_found" || !userResult) {
    await ctx.reply(
      "👤 User not found in our system.\n\n" +
        "It looks like you haven't registered yet.\n" +
        "Use /start to register and start receiving arbitrage alerts!",
    );
    return;
  }

  if (errorType === "error") {
    await ctx.reply(
      `❌ An error occurred while fetching your status.\n\n` +
        `Error: ${errorMessage}\n\n` +
        `Please try again later or use /start to re-register.`,
    );
    return;
  }

  // Success case - get notification preferences
  const preference = userId
    ? await Effect.runPromise(
        Effect.catchIf(
          api.getNotificationPreference(String(userId)),
          (_error): _error is ApiException => true,
          () =>
            Effect.succeed({
              enabled: true,
              profit_threshold: 0.5,
              alert_frequency: "Every 5 minutes",
            }),
        ),
      )
    : {
        enabled: true,
        profit_threshold: 0.5,
        alert_frequency: "Every 5 minutes",
      };

  const createdAt = new Date(userResult.user.created_at).toLocaleDateString();
  const tier = userResult.user.subscription_tier;
  const notificationStatus = preference.enabled ? "Active" : "Paused";

  const msg =
    "📊 Account Status:\n\n" +
    `💰 Subscription: ${tier}\n` +
    `📅 Member since: ${createdAt}\n` +
    `🔔 Notifications: ${notificationStatus}`;

  await ctx.reply(msg);
};

export const handleSettings = (api: Api) => async (ctx: Context) => {
  const chatId = ctx.chat?.id;
  const userId = ctx.from?.id;
  if (!userId || !chatId) {
    await ctx.reply("Unable to fetch settings right now.");
    return;
  }

  // Fetch user for subscription tier (with improved error logging)
  const userResult = await Effect.runPromise(
    Effect.catchIf(
      api.getUserByChatId(String(chatId)),
      (_error): _error is ApiException => true,
      (error) => {
        if (isApiError(error) && error.type === "auth_failed") {
          console.error(
            "[Settings] Authentication failed - check ADMIN_API_KEY",
          );
        }
        return Effect.succeed(null);
      },
    ),
  );

  const preference = await Effect.runPromise(
    Effect.catchIf(
      api.getNotificationPreference(String(userId)),
      (_error): _error is ApiException => true,
      () =>
        Effect.succeed({
          enabled: true,
          profit_threshold: 0.5,
          alert_frequency: "Immediate (Periodic Scan 5m)",
        }),
    ),
  );

  const statusIcon = preference.enabled ? "✅" : "❌";
  const statusText = preference.enabled ? "ON" : "OFF";
  const threshold = preference.profit_threshold ?? 0.5;
  const frequency =
    preference.alert_frequency ?? "Immediate (Periodic Scan 5m)";
  const tier = userResult?.user?.subscription_tier ?? "Free Tier";

  const msg =
    "⚙️ Alert Settings:\n\n" +
    `🔔 Notifications: ${statusIcon} ${statusText}\n` +
    `📊 Min Profit Threshold: ${threshold}%\n` +
    `⏰ Alert Frequency: ${frequency}\n` +
    `💰 Subscription: ${tier}\n\n` +
    "To change settings:\n" +
    "/stop - Pause notifications\n" +
    "/resume - Resume notifications\n" +
    "/upgrade - Upgrade to premium for more options";

  await ctx.reply(msg);
};

export const handleStop = (api: Api) => async (ctx: Context) => {
  const userId = ctx.from?.id;
  if (!userId) {
    await ctx.reply("Unable to update notifications.");
    return;
  }

  await Effect.runPromise(
    Effect.catchIf(
      api.setNotificationPreference(String(userId), false),
      (_error): _error is ApiException => true,
      () => Effect.succeed(null),
    ),
  );

  const msg =
    "⏸️ Notifications Paused\n\n" +
    "You will no longer receive arbitrage alerts.\n\n" +
    "Use /resume to start receiving alerts again.";

  await ctx.reply(msg);
};

export const handleResume = (api: Api) => async (ctx: Context) => {
  const userId = ctx.from?.id;
  if (!userId) {
    await ctx.reply("Unable to update notifications.");
    return;
  }

  await Effect.runPromise(
    Effect.catchIf(
      api.setNotificationPreference(String(userId), true),
      (_error): _error is ApiException => true,
      () => Effect.succeed(null),
    ),
  );

  const msg =
    "▶️ Notifications Resumed\n\n" +
    "You will now receive arbitrage alerts again.\n\n" +
    "Use /opportunities to see current opportunities.";

  await ctx.reply(msg);
};

export const handleUpgrade = () => async (ctx: Context) => {
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
};

export const handleMode = (api: Api) => async (ctx: Context) => {
  const chatId = ctx.chat?.id;
  if (!chatId) {
    await ctx.reply("Unable to check mode: missing chat information.");
    return;
  }

  try {
    // Get current mode from API
    const modeResult = await Effect.runPromise(
      Effect.catchIf(
        api.getTradingMode(String(chatId)),
        (_error): _error is ApiException => true,
        (error) => {
          console.error("[Mode] Failed to get mode:", error);
          return Effect.succeed({
            mode: "dry",
            confirmations: 0,
            required_confirmations: 2,
          });
        },
      ),
    );

    const mode = modeResult.mode || "dry";
    const confirmations = modeResult.confirmations || 0;
    const required = modeResult.required_confirmations || 2;

    let msg: string;
    if (mode === "dry") {
      msg =
        "🧪 Current Mode: DRY (Paper Trading)\n\n" +
        "• No real orders executed\n" +
        "• Safe for testing\n\n" +
        `Confirmations: ${confirmations}/${required}\n\n` +
        "Commands:\n" +
        "/mode confirm - Add confirmation\n" +
        "/mode live - Switch to live (requires confirmations)\n" +
        "/mode dry - Switch to dry mode";
    } else {
      msg =
        "🔴 Current Mode: LIVE (Real Trading)\n\n" +
        "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
        "⚠️ REAL MONEY IS AT RISK\n\n" +
        "Commands:\n" +
        "/mode dry - Switch to safe dry mode";
    }

    await ctx.reply(msg);
  } catch (error) {
    console.error("[Mode] Unexpected error:", error);
    await ctx.reply("❌ Failed to get trading mode. Please try again.");
  }
};

export const handleModeAction = (api: Api) => async (ctx: Context) => {
  const chatId = ctx.chat?.id;
  const messageText = ctx.message?.text || "";

  if (!chatId) {
    await ctx.reply("Unable to change mode: missing chat information.");
    return;
  }

  // Parse the action from the message
  const parts = messageText.split(" ");
  const action = parts[1]?.toLowerCase() || "";

  try {
    if (action === "dry") {
      // Switch to dry mode
      await Effect.runPromise(api.setTradingMode(String(chatId), "dry"));
      await ctx.reply(
        "✅ Switched to DRY MODE\n\n" +
          "🧪 Paper trading active\n" +
          "No real orders will be executed.\n\n" +
          "Your funds are safe!",
      );
    } else if (action === "live") {
      // Attempt to switch to live mode
      const result = await Effect.runPromise(
        Effect.catchIf(
          api.setTradingMode(String(chatId), "live"),
          (_error): _error is ApiException => true,
          (error) => {
            return Effect.succeed({
              success: false,
              error: String(error),
            });
          },
        ),
      );

      if (result.success === false) {
        await ctx.reply(
          "⚠️ Cannot switch to LIVE MODE\n\n" +
            "Live mode requires multiple confirmations for safety.\n" +
            "Use /mode confirm to add a confirmation.\n\n" +
            "This protects against accidental live trading.",
        );
      } else {
        await ctx.reply(
          "🔴 LIVE MODE ACTIVATED\n\n" +
            "⚠️ REAL TRADING IS NOW ENABLED\n" +
            "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
            "⚠️ REAL MONEY IS AT RISK\n\n" +
            "Use /mode dry to return to safe mode anytime.",
        );
      }
    } else if (action === "confirm") {
      // Add confirmation
      const result = await Effect.runPromise(
        Effect.catchIf(
          api.addTradingModeConfirmation(String(chatId)),
          (_error): _error is ApiException => true,
          (error) => {
            return Effect.succeed({
              confirmations: 0,
              required: 2,
              error: String(error),
            });
          },
        ),
      );

      if (result.confirmations >= result.required) {
        await ctx.reply(
          `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
            "You have enough confirmations!\n" +
            "Use /mode live to switch to live trading.\n\n" +
            "⚠️ Remember: Live mode uses real money!",
        );
      } else {
        await ctx.reply(
          `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
            "More confirmations needed before live trading.\n" +
            "Use /mode confirm again to add another confirmation.\n\n" +
            "This protects against accidental live trading.",
        );
      }
    } else {
      await ctx.reply(
        "Unknown mode action.\n\n" +
          "Available commands:\n" +
          "/mode - Check current mode\n" +
          "/mode dry - Switch to paper trading\n" +
          "/mode live - Switch to real trading\n" +
          "/mode confirm - Add confirmation for live mode",
      );
    }
  } catch (error) {
    console.error("[ModeAction] Error:", error);
    await ctx.reply("❌ Failed to change mode. Please try again.");
  }
};

export const handleMessageText = () => async (ctx: Context) => {
  await ctx.reply("Thanks for your message! 👋\n\nTry /help for commands.");
};
