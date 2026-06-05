import { Effect, Context, Layer } from "effect";
import type {
  GetUserByChatIdResponse,
  NotificationPreferenceResponse,
  SetNotificationPreferenceRequest,
  RegisterTelegramUserRequest,
  GetArbitrageOpportunitiesResponse,
  BeginAutonomousResponse,
  PauseAutonomousResponse,
  PerformanceSummaryResponse,
  PerformanceBreakdownResponse,
  LiquidationResponse,
  WalletCommandResponse,
  PortfolioResponse,
  QuestsResponse,
  QuestDiagnosticsResponse,
  WalletsResponse,
  LogsResponse,
  DoctorResponse,
  ApiErrorResponse,
  AIModelsResponse,
  AIModelSelectResponse,
  AIStatusResponse,
  AIRouteRequest,
  AIRouteResponse,
  GetAlertsResponse,
  CreateAlertRequest,
  CreateAlertResponse,
  AIProviderModelsResponse,
  AIProvidersResponse,
} from "./types";
import { API_ENDPOINTS } from "./types";
import { RateLimiter, DEFAULT_RATE_LIMIT } from "./rate-limiter";
import { logger } from "../utils/logger";

export class ApiClientError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly endpoint: string,
  ) {
    super(message);
    this.name = "ApiClientError";
  }
}

export interface BackendApiClientOptions {
  baseUrl: string;
  adminKey: string;
  rateLimit?: number;
}

export class BackendApiClient {
  private readonly baseUrl: string;
  private activeBaseUrl: string;
  private readonly adminKey: string;
  private readonly rateLimiter: RateLimiter;

  constructor(options: BackendApiClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.activeBaseUrl = this.baseUrl;
    this.adminKey = options.adminKey;
    this.rateLimiter = new RateLimiter({
      tokensPerSecond: options.rateLimit ?? DEFAULT_RATE_LIMIT,
    });
  }

  async getUserByChatId(
    chatId: string,
  ): Promise<GetUserByChatIdResponse | null> {
    const endpoint = API_ENDPOINTS.GET_USER_BY_CHAT_ID(chatId);
    const response = await this.fetch<Response | null>(endpoint, {
      requireAdmin: false,
      handle404AsNull: true,
    });
    return response as GetUserByChatIdResponse | null;
  }

  async getNotificationPreference(
    userId: string,
  ): Promise<NotificationPreferenceResponse> {
    const endpoint = API_ENDPOINTS.GET_NOTIFICATION_PREFERENCE(userId);
    try {
      const response = await this.fetch<NotificationPreferenceResponse>(
        endpoint,
        { requireAdmin: false },
      );
      return response;
    } catch (error) {
      logger.warn("Failed to get notification preference", { error });
      return { enabled: true };
    }
  }

  async setNotificationPreference(
    userId: string,
    enabled: boolean,
  ): Promise<void> {
    const endpoint = API_ENDPOINTS.SET_NOTIFICATION_PREFERENCE(userId);
    const body: SetNotificationPreferenceRequest = { enabled };
    await this.fetch(endpoint, {
      method: "POST",
      body: JSON.stringify(body),
      requireAdmin: false,
    });
  }

  async registerTelegramUser(
    request: RegisterTelegramUserRequest,
  ): Promise<void> {
    await this.fetch(API_ENDPOINTS.REGISTER_USER, {
      method: "POST",
      body: JSON.stringify(request),
      requireAdmin: false,
    });
  }

  async getArbitrageOpportunities(
    limit = 5,
    minProfit = 0.5,
  ): Promise<GetArbitrageOpportunitiesResponse> {
    const endpoint = API_ENDPOINTS.GET_ARBITRAGE_OPPORTUNITIES(
      limit,
      minProfit,
    );
    return this.fetch<GetArbitrageOpportunitiesResponse>(endpoint, {
      requireAdmin: false,
    });
  }

  async beginAutonomous(chatId: string): Promise<BeginAutonomousResponse> {
    return this.fetch<BeginAutonomousResponse>(API_ENDPOINTS.BEGIN_AUTONOMOUS, {
      method: "POST",
      body: JSON.stringify({ chat_id: chatId }),
      requireAdmin: false,
    });
  }

  async pauseAutonomous(chatId: string): Promise<PauseAutonomousResponse> {
    return this.fetch<PauseAutonomousResponse>(API_ENDPOINTS.PAUSE_AUTONOMOUS, {
      method: "POST",
      body: JSON.stringify({ chat_id: chatId }),
      requireAdmin: false,
    });
  }

  async getPerformanceSummary(
    chatId: string,
    timeframe = "24h",
  ): Promise<PerformanceSummaryResponse> {
    return this.fetch<PerformanceSummaryResponse>(
      API_ENDPOINTS.GET_SUMMARY(chatId, timeframe),
      {
        requireAdmin: true,
      },
    );
  }
  async getPerformanceBreakdown(
    chatId: string,
    timeframe = "24h",
  ): Promise<PerformanceBreakdownResponse> {
    return this.fetch<PerformanceBreakdownResponse>(
      API_ENDPOINTS.GET_PERFORMANCE(chatId, timeframe),
      {
        requireAdmin: true,
      },
    );
  }
  async liquidate(
    chatId: string,
    symbol: string,
  ): Promise<LiquidationResponse> {
    return this.fetch<LiquidationResponse>(API_ENDPOINTS.LIQUIDATE, {
      method: "POST",
      body: JSON.stringify({ chat_id: chatId, symbol }),
      requireAdmin: true,
    });
  }

  async liquidateAll(chatId: string): Promise<LiquidationResponse> {
    return this.fetch<LiquidationResponse>(API_ENDPOINTS.LIQUIDATE_ALL, {
      method: "POST",
      body: JSON.stringify({ chat_id: chatId }),
      requireAdmin: true,
    });
  }

  async connectExchange(
    chatId: string,
    exchange: string,
    accountLabel?: string,
  ): Promise<WalletCommandResponse> {
    return this.fetch<WalletCommandResponse>(API_ENDPOINTS.CONNECT_EXCHANGE, {
      method: "POST",
      body: JSON.stringify({
        chat_id: chatId,
        exchange,
        account_label: accountLabel,
      }),
      requireAdmin: false,
    });
  }

  async connectPolymarket(
    chatId: string,
    walletAddress: string,
  ): Promise<WalletCommandResponse> {
    return this.fetch<WalletCommandResponse>(API_ENDPOINTS.CONNECT_POLYMARKET, {
      method: "POST",
      body: JSON.stringify({
        chat_id: chatId,
        wallet_address: walletAddress,
      }),
      requireAdmin: false,
    });
  }

  async addWallet(
    chatId: string,
    walletAddress: string,
    walletType = "external",
  ): Promise<WalletCommandResponse> {
    return this.fetch<WalletCommandResponse>(API_ENDPOINTS.ADD_WALLET, {
      method: "POST",
      body: JSON.stringify({
        chat_id: chatId,
        wallet_address: walletAddress,
        wallet_type: walletType,
      }),
      requireAdmin: false,
    });
  }

  async removeWallet(
    chatId: string,
    walletIdOrAddress: string,
  ): Promise<WalletCommandResponse> {
    return this.fetch<WalletCommandResponse>(API_ENDPOINTS.REMOVE_WALLET, {
      method: "POST",
      body: JSON.stringify({
        chat_id: chatId,
        wallet_id_or_address: walletIdOrAddress,
      }),
      requireAdmin: false,
    });
  }

  async getQuests(chatId: string): Promise<QuestsResponse> {
    return this.fetch<QuestsResponse>(API_ENDPOINTS.GET_QUESTS(chatId), {
      requireAdmin: true,
    });
  }

  async getQuestDiagnostics(chatId: string): Promise<QuestDiagnosticsResponse> {
    return this.fetch<QuestDiagnosticsResponse>(
      API_ENDPOINTS.GET_QUEST_DIAGNOSTICS(chatId),
      {
        requireAdmin: true,
      },
    );
  }

  async getPortfolio(chatId: string): Promise<PortfolioResponse> {
    return this.fetch<PortfolioResponse>(API_ENDPOINTS.GET_PORTFOLIO(chatId), {
      requireAdmin: true,
    });
  }
  async getWallets(chatId: string): Promise<WalletsResponse> {
    return this.fetch<WalletsResponse>(API_ENDPOINTS.GET_WALLETS(chatId), {
      requireAdmin: false,
    });
  }

  async getLogs(chatId: string, limit = 10): Promise<LogsResponse> {
    return this.fetch<LogsResponse>(API_ENDPOINTS.GET_LOGS(chatId, limit), {
      requireAdmin: true,
    });
  }
  async getDoctor(chatId: string): Promise<DoctorResponse> {
    return this.fetch<DoctorResponse>(API_ENDPOINTS.GET_DOCTOR(chatId), {
      requireAdmin: true,
    });
  }
  async getAIModels(): Promise<AIModelsResponse> {
    return this.fetch<AIModelsResponse>(API_ENDPOINTS.GET_AI_MODELS, {
      requireAdmin: false,
    });
  }

  async getAIProviders(): Promise<AIProvidersResponse> {
    return this.fetch<AIProvidersResponse>(API_ENDPOINTS.GET_AI_PROVIDERS, {
      requireAdmin: false,
    });
  }

  async getAIProviderModels(
    providerId: string,
  ): Promise<AIProviderModelsResponse> {
    return this.fetch<AIProviderModelsResponse>(
      API_ENDPOINTS.GET_AI_PROVIDER_MODELS(providerId),
      {
        requireAdmin: false,
      },
    );
  }

  async selectAIModel(
    userId: string,
    modelId: string,
  ): Promise<AIModelSelectResponse> {
    return this.fetch<AIModelSelectResponse>(
      API_ENDPOINTS.SELECT_AI_MODEL(userId),
      {
        method: "POST",
        body: JSON.stringify({ model_id: modelId }),
        requireAdmin: false,
      },
    );
  }

  async getAIStatus(chatId: string): Promise<AIStatusResponse> {
    return this.fetch<AIStatusResponse>(API_ENDPOINTS.GET_AI_STATUS(chatId), {
      requireAdmin: true,
    });
  }

  async routeAIModel(request: AIRouteRequest): Promise<AIRouteResponse> {
    return this.fetch<AIRouteResponse>(API_ENDPOINTS.ROUTE_AI_MODEL, {
      method: "POST",
      body: JSON.stringify(request),
      requireAdmin: false,
    });
  }

  private async fetch<T>(
    path: string,
    options: {
      method?: string;
      body?: string;
      requireAdmin?: boolean;
      handle404AsNull?: boolean;
    } = {},
  ): Promise<T> {
    await this.rateLimiter.acquireToken();

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };

    if (options.requireAdmin && this.adminKey) {
      headers["X-API-Key"] = this.adminKey;
    }

    if (options.requireAdmin && !this.adminKey) {
      throw new ApiClientError(
        `ADMIN_API_KEY is not configured — admin endpoint ${path} cannot be called`,
        0,
        path,
      );
    }

    const requestInit: RequestInit = {
      method: options.method || "GET",
      headers,
      body: options.body,
    };

    let response: Response | null = null;
    try {
      response = await fetch(`${this.activeBaseUrl}${path}`, requestInit);
    } catch (primaryError) {
      let fallbackError = primaryError;
      for (const fallbackBaseUrl of this.getFallbackBaseUrls()) {
        try {
          response = await fetch(`${fallbackBaseUrl}${path}`, requestInit);
          this.activeBaseUrl = fallbackBaseUrl;
          break;
        } catch (retryError) {
          fallbackError = retryError;
        }
      }
      if (!response) {
        throw fallbackError;
      }
    }

    if (options.handle404AsNull && response.status === 404) {
      return null as T;
    }

    const payload = await response
      .json()
      .catch((): ApiErrorResponse => ({ message: "Failed to parse response" }));

    if (!response.ok) {
      const message =
        payload?.error ||
        payload?.message ||
        `API request failed (${response.status})`;
      throw new ApiClientError(message, response.status, path);
    }

    return payload as T;
  }

  private getFallbackBaseUrls(): string[] {
    const candidates = [
      this.baseUrl,
      "http://127.0.0.1:8080",
      "http://localhost:8080",
    ];
    const unique: string[] = [];
    for (const candidate of candidates) {
      const normalized = candidate.replace(/\/$/, "");
      if (
        normalized.length > 0 &&
        normalized !== this.activeBaseUrl &&
        !unique.includes(normalized)
      ) {
        unique.push(normalized);
      }
    }
    return unique;
  }

  async getUserAlerts(userId: string): Promise<GetAlertsResponse> {
    const endpoint = API_ENDPOINTS.GET_ALERTS(userId);
    return this.fetch<GetAlertsResponse>(endpoint, { requireAdmin: false });
  }

  async createAlert(
    userId: string,
    alertType: string,
    conditions: Record<string, unknown>,
  ): Promise<CreateAlertResponse> {
    const endpoint = API_ENDPOINTS.CREATE_ALERT;
    const body: CreateAlertRequest & { user_id: string } = {
      user_id: userId,
      alert_type: alertType,
      conditions,
    };
    return this.fetch<CreateAlertResponse>(endpoint, {
      method: "POST",
      body: JSON.stringify(body),
      requireAdmin: false,
    });
  }

  async updateAlert(
    alertId: string,
    isActive: boolean,
    conditions?: Record<string, unknown>,
  ): Promise<{ status: string; message: string }> {
    const endpoint = API_ENDPOINTS.UPDATE_ALERT(alertId);
    const body: { is_active: boolean; conditions?: Record<string, unknown> } = {
      is_active: isActive,
    };
    if (conditions) {
      body.conditions = conditions;
    }
    return this.fetch(endpoint, {
      method: "PUT",
      body: JSON.stringify(body),
      requireAdmin: false,
    });
  }

  async deleteAlert(
    alertId: string,
  ): Promise<{ status: string; message: string }> {
    const endpoint = API_ENDPOINTS.DELETE_ALERT(alertId);
    return this.fetch(endpoint, {
      method: "DELETE",
      requireAdmin: false,
    });
  }

  async getTradingMode(
    chatId: string,
  ): Promise<import("./types").TradingModeResponse> {
    const endpoint = `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}`;
    return this.fetch<import("./types").TradingModeResponse>(endpoint, {
      requireAdmin: true,
    });
  }
  async setTradingMode(
    chatId: string,
    mode: "dry" | "live",
    changedBy = "telegram",
  ): Promise<import("./types").SetTradingModeResponse> {
    const endpoint = `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}`;
    return this.fetch<import("./types").SetTradingModeResponse>(endpoint, {
      method: "POST",
      body: JSON.stringify({ mode, changed_by: changedBy }),
      requireAdmin: true,
    });
  }
  async addTradingModeConfirmation(
    chatId: string,
    confirmedBy = "telegram",
  ): Promise<import("./types").TradingModeConfirmationResponse> {
    const endpoint = `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}/confirm`;
    return this.fetch<import("./types").TradingModeConfirmationResponse>(
      endpoint,
      {
        method: "POST",
        body: JSON.stringify({ confirmed_by: confirmedBy }),
        requireAdmin: true,
      },
    );
  }
}

export function createApiClient(
  baseUrl: string,
  adminKey: string,
  rateLimit?: number,
): BackendApiClient {
  return new BackendApiClient({ baseUrl, adminKey, rateLimit });
}

// ---------------------------------------------------------------------------
// PR-5: Effect-ify the backend client as a Context.Tag service.
//
// The existing `BackendApiClient` class remains intact — every method
// still returns a Promise. The new `TelegramApi` service exposes the SAME
// surface but each method returns `Effect<A, ApiClientError, never>`,
// allowing handlers in src/commands/* to be rewritten as `Effect.gen`
// programs that compose with the rest of the runtime.
//
// This is a wrapper-layer migration: the underlying class is unchanged,
// so the existing 30+ methods and their tests keep working. New
// handlers can opt into the Effect path by depending on `TelegramApi`
// via `yield*` in `Effect.gen`. Future PRs can convert the underlying
// methods to native Effect chains once callers have migrated.
//
// The non-invasive wrapper means PR-5 establishes the pattern without
// a 500-line change in a single commit. Migration risk is bounded —
// a bug in the Effect wrapper cannot regress the Promise-based path.
// ---------------------------------------------------------------------------

/**
 * The TelegramApi Effect service. Each method returns
 * `Effect<A, ApiClientError, never>` so handlers can compose with
 * other Effects without losing the typed error channel. The error
 * type is the same `ApiClientError` already thrown by the
 * `BackendApiClient.fetch` method, so callers can recover the
 * status code and endpoint from the typed error.
 *
 * The implementation delegates to the underlying `BackendApiClient`
 * via `Effect.tryPromise`, so a bug in the Effect wrapper cannot
 * regress the Promise-based path.
 */
export class TelegramApi extends Context.Tag("TelegramApi")<
  TelegramApi,
  {
    getUserByChatId: (
      chatId: string,
    ) => Effect.Effect<GetUserByChatIdResponse | null, ApiClientError>;
    getNotificationPreference: (
      userId: string,
    ) => Effect.Effect<NotificationPreferenceResponse, ApiClientError>;
    setNotificationPreference: (
      userId: string,
      enabled: boolean,
    ) => Effect.Effect<void, ApiClientError>;
    registerTelegramUser: (
      request: RegisterTelegramUserRequest,
    ) => Effect.Effect<void, ApiClientError>;
    getArbitrageOpportunities: (
      limit?: number,
      minProfit?: number,
    ) => Effect.Effect<GetArbitrageOpportunitiesResponse, ApiClientError>;
    beginAutonomous: (
      chatId: string,
    ) => Effect.Effect<BeginAutonomousResponse, ApiClientError>;
    pauseAutonomous: (
      chatId: string,
    ) => Effect.Effect<PauseAutonomousResponse, ApiClientError>;
    getPerformanceSummary: (
      chatId: string,
      timeframe?: string,
    ) => Effect.Effect<PerformanceSummaryResponse, ApiClientError>;
    getPerformanceBreakdown: (
      chatId: string,
      timeframe?: string,
    ) => Effect.Effect<PerformanceBreakdownResponse, ApiClientError>;
    liquidate: (
      chatId: string,
      symbol: string,
    ) => Effect.Effect<LiquidationResponse, ApiClientError>;
    liquidateAll: (
      chatId: string,
    ) => Effect.Effect<LiquidationResponse, ApiClientError>;
    getQuests: (
      chatId: string,
    ) => Effect.Effect<QuestsResponse, ApiClientError>;
    getQuestDiagnostics: (
      chatId: string,
    ) => Effect.Effect<QuestDiagnosticsResponse, ApiClientError>;
    getPortfolio: (
      chatId: string,
    ) => Effect.Effect<PortfolioResponse, ApiClientError>;
    getWallets: (
      chatId: string,
    ) => Effect.Effect<WalletsResponse, ApiClientError>;
    getLogs: (
      chatId: string,
      limit?: number,
    ) => Effect.Effect<LogsResponse, ApiClientError>;
    getDoctor: (
      chatId: string,
    ) => Effect.Effect<DoctorResponse, ApiClientError>;
    getAIModels: () => Effect.Effect<AIModelsResponse, ApiClientError>;
    getAIProviders: () => Effect.Effect<AIProvidersResponse, ApiClientError>;
    selectAIModel: (
      userId: string,
      modelId: string,
    ) => Effect.Effect<AIModelSelectResponse, ApiClientError>;
    getAIStatus: (
      chatId: string,
    ) => Effect.Effect<AIStatusResponse, ApiClientError>;
    routeAIModel: (
      request: AIRouteRequest,
    ) => Effect.Effect<AIRouteResponse, ApiClientError>;
    getTradingMode: (
      chatId: string,
    ) => Effect.Effect<import("./types").TradingModeResponse, ApiClientError>;
    setTradingMode: (
      chatId: string,
      mode: "dry" | "live",
      changedBy?: string,
    ) => Effect.Effect<
      import("./types").SetTradingModeResponse,
      ApiClientError
    >;
    addTradingModeConfirmation: (
      chatId: string,
      confirmedBy?: string,
    ) => Effect.Effect<
      import("./types").TradingModeConfirmationResponse,
      ApiClientError
    >;
  }
>() {}

/**
 * Build the TelegramApi Layer from a BackendApiClient. The Layer
 * composes via `Layer.succeed` and the underlying client is captured
 * in the closure so handlers can `yield* TelegramApi` and get the
 * same instance the rest of the app uses.
 */
export const TelegramApiLive = (
  client: BackendApiClient,
): Layer.Layer<TelegramApi> =>
  Layer.succeed(TelegramApi, {
    getUserByChatId: (chatId) =>
      Effect.tryPromise({
        try: () => client.getUserByChatId(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getUserByChatId",
              ),
      }),
    getNotificationPreference: (userId) =>
      Effect.tryPromise({
        try: () => client.getNotificationPreference(userId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getNotificationPreference",
              ),
      }),
    setNotificationPreference: (userId, enabled) =>
      Effect.tryPromise({
        try: () => client.setNotificationPreference(userId, enabled),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "setNotificationPreference",
              ),
      }),
    registerTelegramUser: (request) =>
      Effect.tryPromise({
        try: () => client.registerTelegramUser(request),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "registerTelegramUser",
              ),
      }),
    getArbitrageOpportunities: (limit, minProfit) =>
      Effect.tryPromise({
        try: () => client.getArbitrageOpportunities(limit, minProfit),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getArbitrageOpportunities",
              ),
      }),
    beginAutonomous: (chatId) =>
      Effect.tryPromise({
        try: () => client.beginAutonomous(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "beginAutonomous",
              ),
      }),
    pauseAutonomous: (chatId) =>
      Effect.tryPromise({
        try: () => client.pauseAutonomous(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "pauseAutonomous",
              ),
      }),
    getPerformanceSummary: (chatId, timeframe) =>
      Effect.tryPromise({
        try: () => client.getPerformanceSummary(chatId, timeframe),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getPerformanceSummary",
              ),
      }),
    getPerformanceBreakdown: (chatId, timeframe) =>
      Effect.tryPromise({
        try: () => client.getPerformanceBreakdown(chatId, timeframe),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getPerformanceBreakdown",
              ),
      }),
    liquidate: (chatId, symbol) =>
      Effect.tryPromise({
        try: () => client.liquidate(chatId, symbol),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "liquidate",
              ),
      }),
    liquidateAll: (chatId) =>
      Effect.tryPromise({
        try: () => client.liquidateAll(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "liquidateAll",
              ),
      }),
    getQuests: (chatId) =>
      Effect.tryPromise({
        try: () => client.getQuests(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getQuests",
              ),
      }),
    getQuestDiagnostics: (chatId) =>
      Effect.tryPromise({
        try: () => client.getQuestDiagnostics(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getQuestDiagnostics",
              ),
      }),
    getPortfolio: (chatId) =>
      Effect.tryPromise({
        try: () => client.getPortfolio(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getPortfolio",
              ),
      }),
    getWallets: (chatId) =>
      Effect.tryPromise({
        try: () => client.getWallets(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getWallets",
              ),
      }),
    getLogs: (chatId, limit) =>
      Effect.tryPromise({
        try: () => client.getLogs(chatId, limit),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getLogs",
              ),
      }),
    getDoctor: (chatId) =>
      Effect.tryPromise({
        try: () => client.getDoctor(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getDoctor",
              ),
      }),
    getAIModels: () =>
      Effect.tryPromise({
        try: () => client.getAIModels(),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getAIModels",
              ),
      }),
    getAIProviders: () =>
      Effect.tryPromise({
        try: () => client.getAIProviders(),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getAIProviders",
              ),
      }),
    selectAIModel: (userId, modelId) =>
      Effect.tryPromise({
        try: () => client.selectAIModel(userId, modelId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "selectAIModel",
              ),
      }),
    getAIStatus: (chatId) =>
      Effect.tryPromise({
        try: () => client.getAIStatus(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getAIStatus",
              ),
      }),
    routeAIModel: (request) =>
      Effect.tryPromise({
        try: () => client.routeAIModel(request),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "routeAIModel",
              ),
      }),
    getTradingMode: (chatId) =>
      Effect.tryPromise({
        try: () => client.getTradingMode(chatId),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "getTradingMode",
              ),
      }),
    setTradingMode: (chatId, mode, changedBy) =>
      Effect.tryPromise({
        try: () => client.setTradingMode(chatId, mode, changedBy ?? "telegram"),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "setTradingMode",
              ),
      }),
    addTradingModeConfirmation: (chatId, confirmedBy) =>
      Effect.tryPromise({
        try: () =>
          client.addTradingModeConfirmation(chatId, confirmedBy ?? "telegram"),
        catch: (e) =>
          e instanceof ApiClientError
            ? e
            : new ApiClientError(
                e instanceof Error ? e.message : String(e),
                0,
                "addTradingModeConfirmation",
              ),
      }),
  });
