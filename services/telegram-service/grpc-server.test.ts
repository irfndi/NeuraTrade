import { describe, test, expect, mock, beforeEach } from "bun:test";
import { TelegramGrpcServer } from "./grpc-server";
import { TelegramErrorCode } from "./telegram-errors";
import type * as grpc from "@grpc/grpc-js";

// Mock Bot class
class MockApi {
  sendMessageMock = mock(
    async (chatId: string | number, text: string, options?: any) => {
      return { message_id: 123, chat: { id: chatId }, text };
    },
  );

  async sendMessage(chatId: string | number, text: string, options?: any) {
    return this.sendMessageMock(chatId, text, options);
  }
}

class MockBot {
  api: MockApi;

  constructor() {
    this.api = new MockApi();
  }
}

// Helper to create mock gRPC call and callback
function createMockCall<TReq, TRes>(request: TReq) {
  return {
    request,
    metadata: {} as grpc.Metadata,
    getPeer: () => "test-peer",
    sendMetadata: mock(() => {}),
  } as unknown as grpc.ServerUnaryCall<TReq, TRes>;
}

function createMockCallback<TRes>() {
  const fn = mock((error: grpc.ServiceError | null, response?: TRes) => {});
  return fn as unknown as grpc.sendUnaryData<TRes>;
}

describe("TelegramGrpcServer", () => {
  let server: TelegramGrpcServer;
  let mockBot: MockBot;

  beforeEach(() => {
    mockBot = new MockBot();
    server = new TelegramGrpcServer(mockBot as any);
  });

  describe("sendMessage validation", () => {
    test("returns error when chatId is missing", () => {
      const call = createMockCall({ chatId: "", text: "Hello", parseMode: "" });
      const callback = createMockCallback();

      server.sendMessage(call as any, callback as any);

      expect(callback).toHaveBeenCalledWith(null, {
        ok: false,
        messageId: "",
        error: "Chat ID and Text are required",
        errorCode: TelegramErrorCode.INVALID_REQUEST,
        retryAfter: 0,
      });
      // Verify bot API was NOT called on validation failure
      expect(mockBot.api.sendMessageMock).not.toHaveBeenCalled();
    });

    test("returns error when text is missing", () => {
      const call = createMockCall({
        chatId: "123456",
        text: "",
        parseMode: "",
      });
      const callback = createMockCallback();

      server.sendMessage(call as any, callback as any);

      expect(callback).toHaveBeenCalledWith(null, {
        ok: false,
        messageId: "",
        error: "Chat ID and Text are required",
        errorCode: TelegramErrorCode.INVALID_REQUEST,
        retryAfter: 0,
      });
      // Verify bot API was NOT called on validation failure
      expect(mockBot.api.sendMessageMock).not.toHaveBeenCalled();
    });

    test("returns error when both chatId and text are missing", () => {
      const call = createMockCall({ chatId: "", text: "", parseMode: "" });
      const callback = createMockCallback();

      server.sendMessage(call as any, callback as any);

      expect(callback).toHaveBeenCalledWith(null, {
        ok: false,
        messageId: "",
        error: "Chat ID and Text are required",
        errorCode: TelegramErrorCode.INVALID_REQUEST,
        retryAfter: 0,
      });
      // Verify bot API was NOT called on validation failure
      expect(mockBot.api.sendMessageMock).not.toHaveBeenCalled();
    });
  });

  describe("sendMessage success", () => {
    test("successfully sends message and returns message ID", async () => {
      const call = createMockCall({
        chatId: "123456",
        text: "Hello World",
        parseMode: "HTML",
      });

      mockBot.api.sendMessageMock.mockResolvedValueOnce({
        message_id: 456,
        chat: { id: 123456 },
        text: "Hello World",
      });

      // Use Promise-based waiting for async operation
      await new Promise<void>((resolve) => {
        const callback = mock((err: any, resp: any) => {
          expect(resp).toEqual({
            ok: true,
            messageId: "456",
            error: "",
            errorCode: "",
            retryAfter: 0,
          });
          resolve();
        });
        server.sendMessage(call as any, callback as any);
      });
    });

    test("passes parse_mode correctly", async () => {
      const call = createMockCall({
        chatId: "123456",
        text: "<b>Bold</b>",
        parseMode: "HTML",
      });

      mockBot.api.sendMessageMock.mockResolvedValueOnce({
        message_id: 789,
        chat: { id: 123456 },
        text: "<b>Bold</b>",
      });

      // Use Promise-based waiting for async operation
      await new Promise<void>((resolve) => {
        const callback = mock((err: any, resp: any) => {
          resolve();
        });
        server.sendMessage(call as any, callback as any);
      });

      expect(mockBot.api.sendMessageMock).toHaveBeenCalledWith(
        "123456",
        "<b>Bold</b>",
        { parse_mode: "HTML" },
      );
    });
  });

  describe("sendMessage error handling", () => {
    test("handles error and returns error response", async () => {
      const call = createMockCall({
        chatId: "blocked-user",
        text: "Hello",
        parseMode: "",
      });

      const error = new Error("Some error occurred");
      mockBot.api.sendMessageMock.mockRejectedValue(error);

      // Use Promise-based waiting with timeout for retry scenarios
      await new Promise<void>((resolve) => {
        const callback = mock((err: any, resp: any) => {
          expect(resp.ok).toBe(false);
          expect(resp.error).toBeDefined();
          resolve();
        });
        server.sendMessage(call as any, callback as any);
      });
    });

    test("retries on network errors and can succeed", async () => {
      const call = createMockCall({
        chatId: "123456",
        text: "Hello",
        parseMode: "",
      });

      // First call fails with network error (retryable), second succeeds
      mockBot.api.sendMessageMock
        .mockRejectedValueOnce(new Error("Network connection failed"))
        .mockResolvedValueOnce({
          message_id: 999,
          chat: { id: 123456 },
          text: "Hello",
        });

      // Use Promise-based waiting for retry operation
      await new Promise<void>((resolve) => {
        const callback = mock((err: any, resp: any) => {
          expect(resp.ok).toBe(true);
          expect(resp.messageId).toBe("999");
          resolve();
        });
        server.sendMessage(call as any, callback as any);
      });
    });
  });

  describe("healthCheck", () => {
    test("returns healthy status", () => {
      const call = createMockCall({});
      const callback = createMockCallback();

      server.healthCheck(call as any, callback as any);

      expect(callback).toHaveBeenCalledWith(null, {
        status: "serving",
        version: "1.0.0",
        service: "telegram-service",
      });
    });

    test("always returns serving status", () => {
      // Call multiple times to ensure consistency
      for (let i = 0; i < 5; i++) {
        const call = createMockCall({});
        const callback = createMockCallback();

        server.healthCheck(call as any, callback as any);

        expect(callback).toHaveBeenCalledWith(null, {
          status: "serving",
          version: "1.0.0",
          service: "telegram-service",
        });
      }
    });
  });
});
