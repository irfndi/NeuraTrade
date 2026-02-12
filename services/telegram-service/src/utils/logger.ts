type LogLevel = "info" | "warn" | "error";

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  context?: Record<string, unknown>;
  error?: {
    name: string;
    message: string;
    stack?: string;
  };
}

function isProduction(): boolean {
  return (
    process.env.NODE_ENV === "production" ||
    process.env.SENTRY_ENVIRONMENT === "production"
  );
}

function formatPretty(entry: LogEntry): string {
  const timestamp = entry.timestamp;
  const level = entry.level.toUpperCase().padEnd(5);
  const context = entry.context ? ` ${JSON.stringify(entry.context)}` : "";
  const error = entry.error
    ? `\n  ${entry.error.name}: ${entry.error.message}${entry.error.stack ? `\n  ${entry.error.stack}` : ""}`
    : "";
  return `${timestamp} [${level}] ${entry.message}${context}${error}`;
}

function formatJson(entry: LogEntry): string {
  return JSON.stringify(entry);
}

function log(
  level: LogLevel,
  message: string,
  error?: Error,
  context?: Record<string, unknown>,
): void {
  const entry: LogEntry = {
    timestamp: new Date().toISOString(),
    level,
    message,
  };

  if (context && Object.keys(context).length > 0) {
    entry.context = context;
  }

  if (error) {
    entry.error = {
      name: error.name,
      message: error.message,
      stack: error.stack,
    };
  }

  const formatted = isProduction() ? formatJson(entry) : formatPretty(entry);

  if (level === "error") {
    console.error(formatted);
  } else if (level === "warn") {
    console.warn(formatted);
  } else {
    console.log(formatted);
  }
}

export interface Logger {
  info(message: string, context?: Record<string, unknown>): void;
  warn(message: string, context?: Record<string, unknown>): void;
  error(
    message: string,
    error?: Error,
    context?: Record<string, unknown>,
  ): void;
}

export const logger: Logger = {
  info(message: string, context?: Record<string, unknown>): void {
    log("info", message, undefined, context);
  },
  warn(message: string, context?: Record<string, unknown>): void {
    log("warn", message, undefined, context);
  },
  error(
    message: string,
    error?: Error,
    context?: Record<string, unknown>,
  ): void {
    log("error", message, error, context);
  },
};

export function createLogger(context: Record<string, unknown> = {}): Logger {
  return {
    info(message: string, additionalContext?: Record<string, unknown>): void {
      log("info", message, undefined, { ...context, ...additionalContext });
    },
    warn(message: string, additionalContext?: Record<string, unknown>): void {
      log("warn", message, undefined, { ...context, ...additionalContext });
    },
    error(
      message: string,
      error?: Error,
      additionalContext?: Record<string, unknown>,
    ): void {
      log("error", message, error, { ...context, ...additionalContext });
    },
  };
}
