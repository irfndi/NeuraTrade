import {
  TelegramErrorInfo,
  isRetryableError,
} from "./telegram-errors";

/**
 * Configuration for retry behavior
 */
export interface RetryConfig {
  /** Maximum number of retry attempts (default: 3) */
  maxRetries: number;
  /** Initial delay in milliseconds (default: 1000) */
  initialDelayMs: number;
  /** Maximum delay in milliseconds (default: 30000) */
  maxDelayMs: number;
  /** Backoff multiplier (default: 2) */
  backoffFactor: number;
  /** Add random jitter to delay (default: true) */
  jitter: boolean;
}

/**
 * Default retry configuration
 */
export const DEFAULT_RETRY_CONFIG: RetryConfig = {
  maxRetries: 3,
  initialDelayMs: 1000,
  maxDelayMs: 30000,
  backoffFactor: 2,
  jitter: true,
};

/**
 * Result of a retry operation
 */
export interface RetryResult<T> {
  success: boolean;
  data?: T;
  error?: TelegramErrorInfo;
  attempts: number;
}

/**
 * Calculate delay for a retry attempt with exponential backoff
 */
export function calculateDelay(
  attempt: number,
  config: RetryConfig,
  retryAfter?: number,
): number {
  // If Telegram specified a retry_after, use it (converted to ms)
  if (retryAfter && retryAfter > 0) {
    return retryAfter * 1000;
  }

  // Calculate exponential backoff
  let delay = config.initialDelayMs * Math.pow(config.backoffFactor, attempt);

  // Apply max delay cap
  delay = Math.min(delay, config.maxDelayMs);

  // Add jitter if enabled (random factor between 0.5 and 1.5)
  if (config.jitter) {
    const jitterFactor = 0.5 + Math.random();
    delay = Math.floor(delay * jitterFactor);
  }

  return delay;
}

/**
 * Sleep for a given number of milliseconds
 */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Execute a function with retry logic
 *
 * @param fn - The async function to execute
 * @param errorClassifier - Function to classify errors
 * @param config - Retry configuration (optional)
 * @returns RetryResult with success/failure and data/error
 */
export async function withRetry<T>(
  fn: () => Promise<T>,
  errorClassifier: (error: unknown) => TelegramErrorInfo,
  config: Partial<RetryConfig> = {},
): Promise<RetryResult<T>> {
  const fullConfig: RetryConfig = { ...DEFAULT_RETRY_CONFIG, ...config };
  let lastError: TelegramErrorInfo | undefined;

  for (let attempt = 0; attempt <= fullConfig.maxRetries; attempt++) {
    try {
      const result = await fn();
      return {
        success: true,
        data: result,
        attempts: attempt + 1,
      };
    } catch (error) {
      lastError = errorClassifier(error);

      // Log the error with attempt info
      console.error(
        `[Retry] Attempt ${attempt + 1}/${fullConfig.maxRetries + 1} failed:`,
        lastError.code,
        lastError.message,
      );

      // Don't retry non-retryable errors
      if (!isRetryableError(lastError.code)) {
        console.log(
          `[Retry] Error ${lastError.code} is not retryable, giving up`,
        );
        return {
          success: false,
          error: lastError,
          attempts: attempt + 1,
        };
      }

      // Don't retry if we've exhausted attempts
      if (attempt >= fullConfig.maxRetries) {
        console.log(`[Retry] Max retries (${fullConfig.maxRetries}) exhausted`);
        break;
      }

      // Calculate and apply delay before next attempt
      const delay = calculateDelay(attempt, fullConfig, lastError.retryAfter);
      console.log(`[Retry] Waiting ${delay}ms before retry...`);
      await sleep(delay);
    }
  }

  return {
    success: false,
    error: lastError,
    attempts: fullConfig.maxRetries + 1,
  };
}

/**
 * Execute a function with retry logic, throwing on final failure
 *
 * @param fn - The async function to execute
 * @param errorClassifier - Function to classify errors
 * @param config - Retry configuration (optional)
 * @returns The result of the function
 * @throws The last error if all retries fail
 */
export async function withRetryThrow<T>(
  fn: () => Promise<T>,
  errorClassifier: (error: unknown) => TelegramErrorInfo,
  config: Partial<RetryConfig> = {},
): Promise<T> {
  const result = await withRetry(fn, errorClassifier, config);

  if (result.success && result.data !== undefined) {
    return result.data;
  }

  throw (
    result.error?.originalError ||
    new Error(result.error?.message || "Unknown error")
  );
}
