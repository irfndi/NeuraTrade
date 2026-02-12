import type { MiddlewareHandler } from "hono";

// Sentry configuration
const sentryDsn = process.env.SENTRY_DSN || "";
const sentryEnvironment =
  process.env.SENTRY_ENVIRONMENT || process.env.NODE_ENV || "development";
const sentryRelease = process.env.SENTRY_RELEASE || "telegram-service@1.0.0";
const tracesSampleRate = Number(process.env.SENTRY_TRACES_SAMPLE_RATE || "0.2");

// Only enable Sentry if DSN is provided
export const isSentryEnabled = Boolean(sentryDsn);

// Sentry SDK instance - lazy loaded to avoid auto-instrumentation issues
let Sentry: typeof import("@sentry/bun") | null = null;
let sentryInitialized = false;

/**
 * Initialize Sentry SDK manually after server startup.
 * This avoids the auto-instrumentation issue with Bun.serve().
 */
export async function initializeSentry(): Promise<boolean> {
  if (!isSentryEnabled) {
    console.log("Sentry is disabled (no SENTRY_DSN provided)");
    return false;
  }

  if (sentryInitialized) {
    return true;
  }

  try {
    // Dynamic import to avoid auto-instrumentation on module load
    Sentry = await import("@sentry/bun");

    Sentry.init({
      dsn: sentryDsn,
      environment: sentryEnvironment,
      release: sentryRelease,
      tracesSampleRate: Number.isFinite(tracesSampleRate)
        ? tracesSampleRate
        : 0.2,
      attachStacktrace: true,
      // Disable automatic integrations that cause issues with Bun.serve
      integrations: (defaultIntegrations) => {
        return defaultIntegrations.filter((integration) => {
          // Filter out Http integration that auto-instruments Bun.serve
          return integration.name !== "Http";
        });
      },
      beforeSend(event) {
        // Add service context to all events
        event.tags = {
          ...event.tags,
          service: "telegram-service",
          runtime: "bun",
        };
        return event;
      },
    });

    sentryInitialized = true;
    console.log(
      `✓ Sentry initialized for telegram-service (environment: ${sentryEnvironment})`,
    );
    return true;
  } catch (error) {
    console.error("Failed to initialize Sentry:", error);
    return false;
  }
}

/**
 * Capture an exception and send to Sentry.
 */
export function captureException(
  error: Error | unknown,
  context?: Record<string, unknown>,
): void {
  if (!Sentry || !sentryInitialized) {
    console.error("Sentry not initialized, logging error locally:", error);
    return;
  }

  const SentrySDK = Sentry;
  SentrySDK.withScope((scope) => {
    if (context) {
      Object.entries(context).forEach(([key, value]) => {
        scope.setExtra(key, value);
      });
    }
    SentrySDK.captureException(error);
  });
}

/**
 * Capture a message and send to Sentry.
 */
export function captureMessage(
  message: string,
  level: "debug" | "info" | "warning" | "error" | "fatal" = "info",
  context?: Record<string, unknown>,
): void {
  if (!Sentry || !sentryInitialized) {
    console.log(`[${level.toUpperCase()}] ${message}`);
    return;
  }

  const SentrySDK = Sentry;
  SentrySDK.withScope((scope) => {
    scope.setLevel(level);
    if (context) {
      Object.entries(context).forEach(([key, value]) => {
        scope.setExtra(key, value);
      });
    }
    SentrySDK.captureMessage(message);
  });
}

/**
 * Add a breadcrumb for debugging context.
 */
export function addBreadcrumb(
  category: string,
  message: string,
  level: "debug" | "info" | "warning" | "error" = "info",
  data?: Record<string, unknown>,
): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  Sentry.addBreadcrumb({
    category,
    message,
    level,
    data,
    timestamp: Date.now() / 1000,
  });
}

/**
 * Set user context for error tracking.
 */
export function setUser(user: {
  id?: string;
  email?: string;
  username?: string;
}): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  Sentry.setUser(user);
}

/**
 * Set a tag on the current scope.
 */
export function setTag(key: string, value: string): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  Sentry.setTag(key, value);
}

/**
 * Set extra context data on the current scope.
 */
export function setExtra(key: string, value: unknown): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  Sentry.setExtra(key, value);
}

/**
 * Start a new span for performance tracing.
 */
export function startSpan<T>(
  name: string,
  operation: string,
  callback: () => T | Promise<T>,
): T | Promise<T> {
  if (!Sentry || !sentryInitialized) {
    return callback();
  }

  return Sentry.startSpan(
    {
      name,
      op: operation,
    },
    callback,
  );
}

/**
 * Flush pending events before shutdown.
 */
export async function flush(timeout: number = 2000): Promise<boolean> {
  if (!Sentry || !sentryInitialized) {
    return true;
  }

  return Sentry.flush(timeout);
}

/**
 * Middleware for Hono that provides request tracing and error capture.
 */
export const sentryMiddleware: MiddlewareHandler = async (c, next) => {
  if (!Sentry || !sentryInitialized) {
    await next();
    return;
  }

  const startTime = Date.now();
  const requestPath = c.req.path;
  const requestMethod = c.req.method;

  // Add breadcrumb for the request
  addBreadcrumb("http", `${requestMethod} ${requestPath}`, "info", {
    url: c.req.url,
  });

  try {
    // Use startSpan for performance tracing
    await Sentry.startSpan(
      {
        name: `${requestMethod} ${requestPath}`,
        op: "http.server",
        attributes: {
          "http.method": requestMethod,
          "http.url": c.req.url,
        },
      },
      async () => {
        await next();
      },
    );

    // Record successful request metrics
    const duration = Date.now() - startTime;
    addBreadcrumb("http", `Response ${c.res.status}`, "info", {
      status: c.res.status,
      duration_ms: duration,
    });
  } catch (error) {
    // Capture the exception with request context
    captureException(error, {
      request: {
        method: requestMethod,
        url: c.req.url,
        path: requestPath,
      },
      duration_ms: Date.now() - startTime,
    });

    // Re-throw to let Hono's error handler deal with it
    throw error;
  }
};

/**
 * Wrapper for tracing bot command operations.
 */
export async function traceBotCommand<T>(
  command: string,
  chatId: number | string,
  userId: number | string | undefined,
  fn: () => Promise<T>,
): Promise<T> {
  if (!Sentry || !sentryInitialized) {
    return fn();
  }

  return Sentry.startSpan(
    {
      name: `bot.command.${command}`,
      op: "bot.command",
      attributes: {
        "bot.command": command,
        "bot.chat_id": String(chatId),
        ...(userId && { "bot.user_id": String(userId) }),
      },
    },
    async () => {
      try {
        const result = await fn();
        addBreadcrumb("bot", `Command /${command} succeeded`, "info", {
          command,
          chat_id: chatId,
          user_id: userId,
        });
        return result;
      } catch (error) {
        addBreadcrumb("bot", `Command /${command} failed`, "error", {
          command,
          chat_id: chatId,
          user_id: userId,
          error: error instanceof Error ? error.message : String(error),
        });
        captureException(error, {
          command,
          chat_id: chatId,
          user_id: userId,
        });
        throw error;
      }
    },
  );
}

/**
 * Track message delivery status.
 */
export function trackMessageDelivery(
  chatId: number | string,
  success: boolean,
  messageType: string,
  error?: string,
): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  if (success) {
    addBreadcrumb("bot.message", `Delivered ${messageType}`, "info", {
      chat_id: chatId,
      message_type: messageType,
    });
  } else {
    addBreadcrumb("bot.message", `Failed to deliver ${messageType}`, "error", {
      chat_id: chatId,
      message_type: messageType,
      error,
    });
    captureMessage(`Message delivery failed: ${messageType}`, "warning", {
      chat_id: chatId,
      message_type: messageType,
      error,
    });
  }
}

/**
 * Track webhook vs polling mode.
 */
export function trackBotMode(mode: "webhook" | "polling"): void {
  if (!Sentry || !sentryInitialized) {
    return;
  }

  setTag("bot.mode", mode);
  addBreadcrumb("bot", `Bot running in ${mode} mode`, "info");
}
