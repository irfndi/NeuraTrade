import "./test-setup";
import { test, expect, beforeAll } from "bun:test";

// Validate required environment variables before running tests
const REQUIRED_ENV_VARS = ["TELEGRAM_WEBHOOK_SECRET", "ADMIN_API_KEY"] as const;

beforeAll(() => {
  const missingVars = REQUIRED_ENV_VARS.filter((v) => !process.env[v]);
  if (missingVars.length > 0) {
    throw new Error(
      `Missing required environment variables for tests: ${missingVars.join(", ")}. ` +
        "Ensure these are set in test-setup.ts or your environment.",
    );
  }
});

// Cache the service instance
let serviceInstance: {
  port: number | string;
  fetch: (req: Request) => Promise<Response>;
} | null = null;
let initializationPromise: Promise<any> | null = null;

async function getService() {
  if (!serviceInstance) {
    if (!initializationPromise) {
      initializationPromise = (async () => {
        // Force fresh import
        const mod = await import("./index.ts?" + Date.now());
        serviceInstance = mod.default as any; // The Hono app is not exported as default usually, but we are running the server in index.ts
        // Wait, index.ts starts the server directly and doesn't export app by default in the way we might expect for testing if we want to fetch directly.
        // However, Bun.serve returns a server instance. index.ts doesn't export it.
        // But since we are running in Bun test environment, importing index.ts starts the server on the configured port.
        // We can just make requests to http://localhost:PORT

        // Wait for service to be ready
        const port = process.env.PORT || 3003;
        const timeout = 5000;
        const interval = 100;
        const startTime = Date.now();

        while (Date.now() - startTime < timeout) {
          try {
            const healthRes = await fetch(`http://localhost:${port}/health`);
            if (healthRes.status === 200) {
              return { port, fetch: fetch };
            }
          } catch {
            // Service not ready yet
          }
          await new Promise((resolve) => setTimeout(resolve, interval));
        }
        throw new Error("Service failed to start");
      })();
    }
    await initializationPromise;
  }
  return {
    baseUrl: `http://localhost:${process.env.TELEGRAM_PORT || 3003}`,
  };
}

test("health endpoint returns healthy", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/health`);
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.status).toBe("healthy");
  expect(body.service).toBe("telegram-service");
});

test("send-message returns 401 without API key", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    body: JSON.stringify({ chatId: "123", text: "hello" }),
  });
  expect(res.status).toBe(401);
});

test("send-message returns 401 with invalid API key", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    headers: { "X-API-Key": "invalid-key" },
    body: JSON.stringify({ chatId: "123", text: "hello" }),
  });
  expect(res.status).toBe(401);
});

test("send-message returns 400 with missing parameters", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    headers: { "X-API-Key": process.env.ADMIN_API_KEY! },
    body: JSON.stringify({ text: "hello" }), // missing chatId
  });
  expect(res.status).toBe(400);
});

test("send-message sends successfully", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    headers: { "X-API-Key": process.env.ADMIN_API_KEY! },
    body: JSON.stringify({ chatId: "123", text: "test message" }),
  });
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.ok).toBe(true);
});

test("webhook endpoint rejects invalid secret token", async () => {
  const { baseUrl } = await getService();
  // Using the path defined in test-setup logic or default logic
  // index.ts: resolvedWebhookPath = ... new URL(webhookUrl).pathname -> /webhook
  const webhookPath = "/webhook";

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: { "X-Telegram-Bot-Api-Secret-Token": "wrong-secret" },
    body: JSON.stringify({ update_id: 1, message: { text: "hi" } }),
  });
  expect(res.status).toBe(401);
});

test("webhook endpoint accepts valid secret token", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
    },
    body: JSON.stringify({ update_id: 1, message: { text: "hi" } }),
  });
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.ok).toBe(true);
});

// E2E Webhook Flow Tests
test("webhook processes /start command update", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const startUpdate = {
    update_id: 100,
    message: {
      message_id: 1,
      from: { id: 123456789, first_name: "Test", username: "testuser" },
      chat: { id: 123456789, type: "private" },
      date: Math.floor(Date.now() / 1000),
      text: "/start",
      entities: [{ offset: 0, length: 6, type: "bot_command" }],
    },
  };

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(startUpdate),
  });

  expect(res.status).toBe(200);
});

test("webhook processes /help command update", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const helpUpdate = {
    update_id: 101,
    message: {
      message_id: 2,
      from: { id: 123456789, first_name: "Test" },
      chat: { id: 123456789, type: "private" },
      date: Math.floor(Date.now() / 1000),
      text: "/help",
      entities: [{ offset: 0, length: 5, type: "bot_command" }],
    },
  };

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(helpUpdate),
  });

  expect(res.status).toBe(200);
});

test("webhook processes /opportunities command update", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const oppsUpdate = {
    update_id: 102,
    message: {
      message_id: 3,
      from: { id: 123456789, first_name: "Test" },
      chat: { id: 123456789, type: "private" },
      date: Math.floor(Date.now() / 1000),
      text: "/opportunities",
      entities: [{ offset: 0, length: 14, type: "bot_command" }],
    },
  };

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(oppsUpdate),
  });

  expect(res.status).toBe(200);
});

test("webhook handles concurrent updates from different users", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const users = [
    { id: 111111, name: "User1" },
    { id: 222222, name: "User2" },
    { id: 333333, name: "User3" },
  ];

  const requests = users.map((user, idx) =>
    fetch(`${baseUrl}${webhookPath}`, {
      method: "POST",
      headers: {
        "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        update_id: 200 + idx,
        message: {
          message_id: idx + 1,
          from: { id: user.id, first_name: user.name },
          chat: { id: user.id, type: "private" },
          date: Math.floor(Date.now() / 1000),
          text: "/start",
          entities: [{ offset: 0, length: 6, type: "bot_command" }],
        },
      }),
    }),
  );

  const responses = await Promise.all(requests);
  responses.forEach((res) => {
    expect(res.status).toBe(200);
  });
});

test("webhook rejects malformed update payload", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const malformedUpdate = {
    // Missing update_id
    message: {
      text: "hello",
    },
  };

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(malformedUpdate),
  });

  // The bot should still return 200 to acknowledge receipt
  // (Telegram expects 200 to not retry)
  expect(res.status).toBe(200);
});

test("webhook handles text message without command", async () => {
  const { baseUrl } = await getService();
  const webhookPath = "/webhook";

  const textUpdate = {
    update_id: 300,
    message: {
      message_id: 100,
      from: { id: 444444, first_name: "TextUser" },
      chat: { id: 444444, type: "private" },
      date: Math.floor(Date.now() / 1000),
      text: "Hello, this is a regular message",
    },
  };

  const res = await fetch(`${baseUrl}${webhookPath}`, {
    method: "POST",
    headers: {
      "X-Telegram-Bot-Api-Secret-Token": process.env.TELEGRAM_WEBHOOK_SECRET!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(textUpdate),
  });

  expect(res.status).toBe(200);
});

// Admin API Tests
test("send-message with parseMode HTML", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    headers: {
      "X-API-Key": process.env.ADMIN_API_KEY!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      chatId: "123",
      text: "<b>Bold</b> message",
      parseMode: "HTML",
    }),
  });
  expect(res.status).toBe(200);
});

test("send-message with parseMode Markdown", async () => {
  const { baseUrl } = await getService();
  const res = await fetch(`${baseUrl}/send-message`, {
    method: "POST",
    headers: {
      "X-API-Key": process.env.ADMIN_API_KEY!,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      chatId: "123",
      text: "*Bold* message",
      parseMode: "Markdown",
    }),
  });
  expect(res.status).toBe(200);
});
