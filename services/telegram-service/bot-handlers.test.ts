import { test, expect, describe, mock } from "bun:test";
import {
  handleStart,
  handleHelp,
  handleOpportunities,
  handleStatus,
  handleSettings,
  handleStop,
  handleResume,
  handleUpgrade,
  formatOpportunitiesMessage,
} from "./bot-handlers";
import { Api, ApiException } from "./api";
import { Effect } from "effect";

// Mock helpers
const createMockApi = (): Api =>
  ({
    getUserByChatId: mock(() =>
      Effect.succeed({
        user: {
          id: "user123",
          subscription_tier: "Premium",
          created_at: "2023-01-01",
        },
      }),
    ),
    getNotificationPreference: mock(() =>
      Effect.succeed({
        enabled: true,
        profit_threshold: 0.5,
        alert_frequency: "Every 5 minutes",
      }),
    ),
    setNotificationPreference: mock(() =>
      Effect.succeed({ status: "success", enabled: true }),
    ),
    registerTelegramUser: mock(() => Effect.succeed({})),
    getOpportunities: mock(() =>
      Effect.succeed({
        opportunities: [
          {
            symbol: "BTC/USDT",
            buy_exchange: "ex1",
            buy_price: 100,
            sell_exchange: "ex2",
            sell_price: 110,
            profit_percent: 10,
          },
        ],
      }),
    ),
  }) as any;

const createMockContext = () =>
  ({
    chat: { id: 12345 },
    from: { id: 67890 },
    reply: mock(() => Promise.resolve()),
  }) as any;

describe("Bot Handlers", () => {
  test("formatOpportunitiesMessage formats correctly", () => {
    const opps = [
      {
        symbol: "BTC/USDT",
        buy_exchange: "Binance",
        buy_price: "50000",
        sell_exchange: "Kraken",
        sell_price: "51000",
        profit_percent: "2.0",
      },
    ];
    const msg = formatOpportunitiesMessage(opps);
    expect(msg).toContain("BTC/USDT");
    expect(msg).toContain("Binance @ 50000");
    expect(msg).toContain("Kraken @ 51000");
    expect(msg).toContain("Profit: 2.00%");
  });

  test("handleStart registers user if not found", async () => {
    const api = createMockApi();
    // Simulate user not found first
    (api.getUserByChatId as any).mockImplementationOnce(() =>
      Effect.fail(
        new ApiException({ type: "not_found", message: "Not found" }),
      ),
    );

    const ctx = createMockContext();
    await handleStart(api)(ctx);

    expect(api.getUserByChatId).toHaveBeenCalled();
    expect(api.registerTelegramUser).toHaveBeenCalled();
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Welcome to Celebrum AI");
  });

  test("handleStart welcomes existing user", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleStart(api)(ctx);

    expect(api.getUserByChatId).toHaveBeenCalled();
    expect(api.registerTelegramUser).not.toHaveBeenCalled();
    expect(ctx.reply).toHaveBeenCalled();
  });

  test("handleStart shows error when registration fails", async () => {
    const api = createMockApi();
    // Simulate user not found first
    (api.getUserByChatId as any).mockImplementationOnce(() =>
      Effect.fail(
        new ApiException({ type: "not_found", message: "Not found" }),
      ),
    );
    // Simulate registration failure
    (api.registerTelegramUser as any).mockImplementationOnce(() =>
      Effect.fail(
        new ApiException({ type: "unknown", message: "Registration failed" }),
      ),
    );

    const ctx = createMockContext();
    await handleStart(api)(ctx);

    expect(api.getUserByChatId).toHaveBeenCalled();
    expect(api.registerTelegramUser).toHaveBeenCalled();
    expect(ctx.reply).toHaveBeenCalled();
    // Should show error message, not welcome message
    expect(ctx.reply.mock.calls[0][0]).toContain("Registration failed");
  });

  test("handleHelp sends help message", async () => {
    const api = createMockApi(); // Not used but needed for consistent setup if we change help signature
    const ctx = createMockContext();
    await handleHelp()(ctx);
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("/start");
  });

  test("handleOpportunities fetches and displays opportunities", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleOpportunities(api)(ctx);

    expect(api.getOpportunities).toHaveBeenCalled();
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("BTC/USDT");
  });

  test("handleOpportunities handles error", async () => {
    const api = createMockApi();
    (api.getOpportunities as any).mockImplementation(() =>
      Effect.fail(new Error("Network Error")),
    );
    const ctx = createMockContext();

    await handleOpportunities(api)(ctx);

    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain(
      "Failed to fetch opportunities",
    );
  });

  test("handleStatus shows status", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleStatus(api)(ctx);

    expect(api.getUserByChatId).toHaveBeenCalled();
    expect(api.getNotificationPreference).toHaveBeenCalled();
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Account Status");
    expect(ctx.reply.mock.calls[0][0]).toContain("Premium");
  });

  test("handleStatus asks to register if user not found", async () => {
    const api = createMockApi();
    (api.getUserByChatId as any).mockImplementation(() =>
      Effect.fail(
        new ApiException({ type: "not_found", message: "Not found" }),
      ),
    );
    const ctx = createMockContext();
    await handleStatus(api)(ctx);
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("User not found");
  });

  test("handleSettings shows settings", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleSettings(api)(ctx);

    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Alert Settings");
    expect(ctx.reply.mock.calls[0][0]).toContain("Every 5 minutes");
  });

  test("handleStop disables notifications", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleStop(api)(ctx);

    expect(api.setNotificationPreference).toHaveBeenCalledWith(
      expect.any(String),
      false,
    );
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Notifications Paused");
  });

  test("handleResume enables notifications", async () => {
    const api = createMockApi();
    const ctx = createMockContext();
    await handleResume(api)(ctx);

    expect(api.setNotificationPreference).toHaveBeenCalledWith(
      expect.any(String),
      true,
    );
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Notifications Resumed");
  });

  test("handleUpgrade shows upgrade info", async () => {
    const ctx = createMockContext();
    await handleUpgrade()(ctx);
    expect(ctx.reply).toHaveBeenCalled();
    expect(ctx.reply.mock.calls[0][0]).toContain("Upgrade to Premium");
  });
});
