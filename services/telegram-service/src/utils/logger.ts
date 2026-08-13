type LogLevel = "info" | "warn" | "error";

/**
 * A single value attached to a log entry's context. Log context is rendered
 * through JSON serialization, so values are JSON-compatible primitives plus
 * the errors that callers commonly attach for diagnostics.
 */
export type LogContextValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | Error
  | readonly LogContextValue[]
  | { readonly [key: string]: LogContextValue };

/** Structured context attached to a log entry. */
export type LogContext = Record<string, LogContextValue>;

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  context?: LogContext;
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
  context?: LogContext,
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
  info(message: string, context?: LogContext): void;
  warn(message: string, context?: LogContext): void;
  error(message: string, error?: Error, context?: LogContext): void;
}

export const logger: Logger = {
  info(message: string, context?: LogContext): void {
    log("info", message, undefined, context);
  },
  warn(message: string, context?: LogContext): void {
    log("warn", message, undefined, context);
  },
  error(message: string, error?: Error, context?: LogContext): void {
    log("error", message, error, context);
  },
};

export function createLogger(context: LogContext = {}): Logger {
  return {
    info(message: string, additionalContext?: LogContext): void {
      log("info", message, undefined, { ...context, ...additionalContext });
    },
    warn(message: string, additionalContext?: LogContext): void {
      log("warn", message, undefined, { ...context, ...additionalContext });
    },
    error(
      message: string,
      error?: Error,
      additionalContext?: LogContext,
    ): void {
      log("error", message, error, { ...context, ...additionalContext });
    },
  };
}
