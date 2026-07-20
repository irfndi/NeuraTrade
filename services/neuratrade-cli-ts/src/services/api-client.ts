/**
 * API client service — HTTP client for the NeuraTrade backend API.
 *
 * Mirrors the Go APIClient in cmd/neuratrade-cli/main.go.
 * Uses Effect-TS for typed errors and dependency injection.
 */
import { Context, Data, Effect, Layer } from "effect";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUT_MS = 5000;

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

export class HttpError extends Data.TaggedError("HttpError")<{
  readonly status: number;
  readonly body: string;
  readonly endpoint: string;
}> {}

export class NetworkError extends Data.TaggedError("NetworkError")<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

export class TimeoutError extends Data.TaggedError("TimeoutError")<{
  readonly endpoint: string;
  readonly timeoutMs: number;
}> {}

export class JsonParseError extends Data.TaggedError("JsonParseError")<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

export type ApiClientError =
  | HttpError
  | NetworkError
  | TimeoutError
  | JsonParseError;

// ---------------------------------------------------------------------------
// Request / Response types (mirror Go structs)
// ---------------------------------------------------------------------------

export interface GenerateAuthCodeResponse {
  readonly success: boolean;
  readonly message: string;
  readonly user_id?: string;
  readonly expires_at?: string;
}

export interface AIProvider {
  readonly id: string;
  readonly name: string;
  readonly is_active: boolean;
  readonly model_count: number;
}

export interface AIProvidersResponse {
  readonly providers: ReadonlyArray<AIProvider>;
}

export interface AIModel {
  readonly model_id: string;
  readonly display_name: string;
  readonly provider: string;
  readonly cost: string;
  readonly supports_tools: boolean;
  readonly supports_vision: boolean;
}

export interface AIModelsResponse {
  readonly models: ReadonlyArray<AIModel>;
}

export interface PortfolioAsset {
  readonly symbol: string;
  readonly amount: string;
  readonly value: string;
}

export interface PortfolioResponse {
  readonly total_value: string;
  readonly cash: string;
  readonly assets: ReadonlyArray<PortfolioAsset>;
  readonly pnl_24h: string;
}

export interface BalanceResponse {
  readonly total_balance: string;
  readonly available: string;
  readonly locked: string;
  readonly currency: string;
}

export interface BacktestRequest {
  readonly start_time: string;
  readonly end_time: string;
  readonly symbols?: ReadonlyArray<string>;
  readonly exchange?: string;
  readonly initial_capital?: string;
  readonly mode?: string;
}

export interface BacktestResponse {
  readonly run_id: string;
  readonly status: string;
  readonly mode: string;
  readonly summary: Record<string, unknown>;
  readonly gate_summary: ReadonlyArray<unknown>;
}

export interface HealthResponse {
  readonly status: string;
  readonly services?: Record<string, string>;
  readonly timestamp?: string;
}

// ---------------------------------------------------------------------------
// ApiClient interface + Context.Tag
// ---------------------------------------------------------------------------

export interface ApiClientImpl {
  readonly generateAuthCode: (
    userId: string,
  ) => Effect.Effect<GenerateAuthCodeResponse, ApiClientError>;
  readonly getAIProviders: () => Effect.Effect<
    AIProvidersResponse,
    ApiClientError
  >;
  readonly getAIModels: (
    provider?: string,
  ) => Effect.Effect<AIModelsResponse, ApiClientError>;
  readonly getPortfolio: () => Effect.Effect<PortfolioResponse, ApiClientError>;
  readonly getBalance: (
    chatId: string,
  ) => Effect.Effect<BalanceResponse, ApiClientError>;
  readonly runScalpingBacktest: (
    request: BacktestRequest,
  ) => Effect.Effect<BacktestResponse, ApiClientError>;
  readonly health: () => Effect.Effect<HealthResponse, ApiClientError>;
}

export class ApiClient extends Context.Service<ApiClient, ApiClientImpl>()(
  "ApiClient",
) {}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/**
 * Make an HTTP request and return the raw Response.
 * Maps network errors to NetworkError, timeouts to TimeoutError,
 * and non-2xx responses to HttpError.
 */
function apiRequest(
  baseUrl: string,
  apiKey: string,
  method: string,
  endpoint: string,
  timeoutMs: number,
  body?: unknown,
): Effect.Effect<Response, ApiClientError> {
  return Effect.gen(function* () {
    const url = `${baseUrl}${endpoint}`;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (apiKey !== "") {
      headers["X-API-Key"] = apiKey;
    }

    const init: RequestInit = {
      method,
      headers,
      signal: AbortSignal.timeout(timeoutMs),
    };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }

    const response = yield* Effect.tryPromise({
      try: () => fetch(url, init),
      catch: (error): ApiClientError => {
        if (error instanceof DOMException && error.name === "TimeoutError") {
          return new TimeoutError({ endpoint, timeoutMs });
        }
        return new NetworkError({
          cause: error instanceof Error ? error.message : String(error),
          endpoint,
        });
      },
    });

    if (!response.ok) {
      const text = yield* Effect.tryPromise({
        try: () => response.text(),
        catch: (): ApiClientError =>
          new HttpError({ status: response.status, body: "", endpoint }),
      });
      return yield* Effect.fail<ApiClientError>(
        new HttpError({ status: response.status, body: text, endpoint }),
      );
    }

    return response;
  });
}

function parseJson<T>(
  response: Response,
  endpoint: string,
): Effect.Effect<T, ApiClientError> {
  return Effect.tryPromise({
    try: () => response.json() as Promise<T>,
    catch: (error): JsonParseError =>
      new JsonParseError({
        cause: error instanceof Error ? error.message : String(error),
        endpoint,
      }),
  });
}

// ---------------------------------------------------------------------------
// ApiClientLive layer
// ---------------------------------------------------------------------------

/**
 * Layer that provides the ApiClient service.
 *
 * @param baseUrl - The backend API base URL (e.g. "http://localhost:8080")
 * @param apiKey - The admin API key (sent via X-API-Key header)
 * @param timeoutMs - Request timeout in milliseconds (default: 5000)
 */
export const ApiClientLive = (
  baseUrl: string,
  apiKey: string,
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Layer.Layer<ApiClient> =>
  Layer.succeed(ApiClient, {
    generateAuthCode: (userId) =>
      apiRequest(
        baseUrl,
        apiKey,
        "POST",
        "/api/v1/telegram/generate-binding-code",
        timeoutMs,
        { user_id: userId },
      ).pipe(
        Effect.flatMap((res) =>
          parseJson<GenerateAuthCodeResponse>(
            res,
            "/api/v1/telegram/generate-binding-code",
          ),
        ),
      ),

    getAIProviders: () =>
      apiRequest(
        baseUrl,
        apiKey,
        "GET",
        "/api/v1/ai/providers",
        timeoutMs,
      ).pipe(
        Effect.flatMap((res) =>
          parseJson<AIProvidersResponse>(res, "/api/v1/ai/providers"),
        ),
      ),

    getAIModels: (provider?: string) => {
      const endpoint = provider
        ? `/api/v1/ai/providers/${encodeURIComponent(provider)}/models`
        : "/api/v1/ai/models";
      return apiRequest(baseUrl, apiKey, "GET", endpoint, timeoutMs).pipe(
        Effect.flatMap((res) => parseJson<AIModelsResponse>(res, endpoint)),
      );
    },

    getPortfolio: () =>
      apiRequest(
        baseUrl,
        apiKey,
        "GET",
        "/api/v1/telegram/internal/portfolio",
        timeoutMs,
      ).pipe(
        Effect.flatMap((res) =>
          parseJson<PortfolioResponse>(
            res,
            "/api/v1/telegram/internal/portfolio",
          ),
        ),
      ),

    getBalance: (chatId: string) => {
      const endpoint = `/api/v1/telegram/internal/portfolio?chat_id=${encodeURIComponent(chatId)}`;
      return apiRequest(baseUrl, apiKey, "GET", endpoint, timeoutMs).pipe(
        Effect.flatMap((res) =>
          parseJson<{ total_equity: string; available_balance: string }>(
            res,
            endpoint,
          ),
        ),
        Effect.map((payload) => ({
          total_balance: payload.total_equity,
          available: payload.available_balance,
          locked: "0",
          currency: "USDT" as const,
        })),
      );
    },

    runScalpingBacktest: (request) =>
      apiRequest(
        baseUrl,
        apiKey,
        "POST",
        "/api/v1/backtest/scalping/run",
        timeoutMs,
        request,
      ).pipe(
        Effect.flatMap((res) =>
          parseJson<BacktestResponse>(res, "/api/v1/backtest/scalping/run"),
        ),
      ),

    health: () =>
      apiRequest(baseUrl, apiKey, "GET", "/health", timeoutMs).pipe(
        Effect.flatMap((res) => parseJson<HealthResponse>(res, "/health")),
      ),
  });
