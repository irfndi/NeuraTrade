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
