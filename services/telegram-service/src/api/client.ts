import type {
  GetUserByChatIdResponse,
  NotificationPreferenceResponse,
  SetNotificationPreferenceRequest,
  RegisterTelegramUserRequest,
  GetArbitrageOpportunitiesResponse,
  ApiErrorResponse,
} from "./types";
import { API_ENDPOINTS } from "./types";
import { RateLimiter, DEFAULT_RATE_LIMIT } from "./rate-limiter";

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
  private readonly adminKey: string;
  private readonly rateLimiter: RateLimiter;

  constructor(options: BackendApiClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
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
      requireAdmin: true,
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
        { requireAdmin: true },
      );
      return response;
    } catch {
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
      requireAdmin: true,
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

    const response = await fetch(`${this.baseUrl}${path}`, {
      method: options.method || "GET",
      headers,
      body: options.body,
    });

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
}

export function createApiClient(
  baseUrl: string,
  adminKey: string,
  rateLimit?: number,
): BackendApiClient {
  return new BackendApiClient({ baseUrl, adminKey, rateLimit });
}
