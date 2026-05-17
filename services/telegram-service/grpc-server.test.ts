import { describe, expect, test } from "bun:test";
import { EventEmitter } from "events";
import * as grpc from "@grpc/grpc-js";
import type { Bot } from "grammy";
import {
  createAuthInterceptor,
  formatGrpcBindTarget,
  safeCredentialEqual,
  TelegramGrpcServer,
} from "./grpc-server";
import type {
  SendActionAlertRequest,
  SendActionAlertResponse,
  SendMessageRequest,
  SendMessageResponse,
  StreamEventsRequest,
  TelegramEvent,
} from "./proto/telegram_service";

interface SentMessage {
  chatId: string;
  text: string;
  options?: unknown;
}

class MockStreamCall extends EventEmitter {
  readonly request: StreamEventsRequest;
  readonly written: TelegramEvent[] = [];
  ended = false;

  constructor(chatId: string) {
    super();
    this.request = { chatId };
  }

  write(event: TelegramEvent): boolean {
    this.written.push(event);
    return true;
  }

  end(): this {
    this.ended = true;
    return this;
  }
}

class FakeInterceptingCall implements grpc.ServerInterceptingCallInterface {
  listener?: {
    onReceiveMetadata(metadata: grpc.Metadata): void;
    onReceiveMessage(message: unknown): void;
    onReceiveHalfClose(): void;
    onCancel(): void;
  };
  sentMetadata: grpc.Metadata[] = [];
  sentStatuses: Parameters<
    grpc.ServerInterceptingCallInterface["sendStatus"]
  >[0][] = [];

  start(listener: NonNullable<FakeInterceptingCall["listener"]>): void {
    this.listener = listener;
  }

  sendMetadata(metadata: grpc.Metadata): void {
    this.sentMetadata.push(metadata);
  }

  sendMessage(_message: unknown, callback: () => void): void {
    callback();
  }

  sendStatus(
    status: Parameters<grpc.ServerInterceptingCallInterface["sendStatus"]>[0],
  ): void {
    this.sentStatuses.push(status);
  }

  startRead(): void {}

  getPeer(): string {
    return "test-peer";
  }

  getDeadline(): grpc.Deadline {
    return Infinity;
  }

  getHost(): string {
    return "localhost";
  }

  getAuthContext(): ReturnType<
    grpc.ServerInterceptingCallInterface["getAuthContext"]
  > {
    return {};
  }

  getConnectionInfo(): ReturnType<
    grpc.ServerInterceptingCallInterface["getConnectionInfo"]
  > {
    return {};
  }

  getMetricsRecorder(): ReturnType<
    grpc.ServerInterceptingCallInterface["getMetricsRecorder"]
  > {
    return {} as ReturnType<
      grpc.ServerInterceptingCallInterface["getMetricsRecorder"]
    >;
  }
}

function createBotMock(): { bot: Bot; sentMessages: SentMessage[] } {
  const sentMessages: SentMessage[] = [];
  const bot = {
    api: {
      async sendMessage(
        chatId: string,
        text: string,
        options?: unknown,
      ): Promise<{ message_id: number }> {
        sentMessages.push({ chatId, text, options });
        return { message_id: sentMessages.length };
      },
    },
  };

  return { bot: bot as unknown as Bot, sentMessages };
}

function invokeUnary<Req, Res>(
  handler: (
    call: grpc.ServerUnaryCall<Req, Res>,
    callback: grpc.sendUnaryData<Res>,
  ) => void,
  request: Req,
): Promise<{
  error: unknown;
  response: Res | null;
}> {
  return new Promise((resolve) => {
    const call = { request } as grpc.ServerUnaryCall<Req, Res>;
    handler(call, (error, response) => {
      resolve({
        error,
        response: response ?? null,
      });
    });
  });
}

function invokeAuthInterceptor(
  methodPath: string,
  adminApiKey: string,
  metadata: grpc.Metadata,
): {
  call: FakeInterceptingCall;
  metadataForwarded: boolean;
} {
  const call = new FakeInterceptingCall();
  const intercepted = createAuthInterceptor(adminApiKey)(
    {
      path: methodPath,
      requestStream: false,
      responseStream: false,
      requestDeserialize: (value: Buffer) => value,
      responseSerialize: (value: unknown) => Buffer.from(String(value)),
    },
    call,
  );

  let metadataForwarded = false;
  intercepted.start({
    onReceiveMetadata: () => {
      metadataForwarded = true;
    },
    onReceiveMessage: () => {},
    onReceiveHalfClose: () => {},
    onCancel: () => {},
  });
  call.listener?.onReceiveMetadata(metadata);

  return { call, metadataForwarded };
}

describe("TelegramGrpcServer", () => {
  test("publishes StreamEvents when SendActionAlert succeeds", async () => {
    const { bot, sentMessages } = createBotMock();
    const server = new TelegramGrpcServer(bot);

    const stream = new MockStreamCall("chat-1");
    server.streamEvents(
      stream as unknown as grpc.ServerWritableStream<
        StreamEventsRequest,
        TelegramEvent
      >,
    );

    const request: SendActionAlertRequest = {
      chatId: "chat-1",
      action: "BUY",
      asset: "BTC/USDT",
      price: "101000",
      size: "0.05",
      strategy: "scalping",
      reasoning: "Momentum breakout + spread divergence",
      riskCheckPassed: true,
      questId: "quest-hourly-1",
    };

    const result = await invokeUnary<
      SendActionAlertRequest,
      SendActionAlertResponse
    >(server.sendActionAlert, request);

    expect(result.error).toBeNull();
    expect(result.response?.ok).toBe(true);
    expect(sentMessages).toHaveLength(1);
    expect(sentMessages[0].text).toContain("Action: BUY");

    expect(stream.written).toHaveLength(1);
    expect(stream.written[0].type).toBe("action");
    expect(stream.written[0].action?.asset).toBe("BTC/USDT");
    expect(stream.written[0].action?.reasoning).toContain("Momentum breakout");
  });

  test("rejects SendActionAlert when chat ID is missing", async () => {
    const { bot } = createBotMock();
    const server = new TelegramGrpcServer(bot);

    const request: SendActionAlertRequest = {
      chatId: "",
      action: "BUY",
      asset: "BTC/USDT",
      price: "100000",
      size: "0.10",
      strategy: "arbitrage",
      reasoning: "Test",
      riskCheckPassed: true,
      questId: "quest-1",
    };

    const result = await invokeUnary<
      SendActionAlertRequest,
      SendActionAlertResponse
    >(server.sendActionAlert, request);

    expect(result.error).not.toBeNull();
    const errorWithCode = result.error as { code?: number } | null;
    expect(errorWithCode?.code).toBe(grpc.status.INVALID_ARGUMENT);
    expect(result.response).toBeNull();
  });

  test("ignores invalid parse mode and still sends message", async () => {
    const { bot, sentMessages } = createBotMock();
    const server = new TelegramGrpcServer(bot);

    const request: SendMessageRequest = {
      chatId: "chat-2",
      text: "hello world",
      parseMode: "INVALID_MODE",
    };

    const result = await invokeUnary<SendMessageRequest, SendMessageResponse>(
      server.sendMessage,
      request,
    );

    expect(result.error).toBeNull();
    expect(result.response?.ok).toBe(true);
    expect(sentMessages).toHaveLength(1);
    expect(sentMessages[0].options).toBeUndefined();
  });

  test("ends stream immediately when chat ID is empty", () => {
    const { bot } = createBotMock();
    const server = new TelegramGrpcServer(bot);

    const stream = new MockStreamCall("");
    server.streamEvents(
      stream as unknown as grpc.ServerWritableStream<
        StreamEventsRequest,
        TelegramEvent
      >,
    );

    expect(stream.ended).toBe(true);
    expect(stream.written).toHaveLength(0);
  });

  test("gRPC auth allows only exact HealthCheck path without API key", () => {
    const adminApiKey = "test-admin-key-that-is-at-least-32-characters";

    const health = invokeAuthInterceptor(
      "/telegram.TelegramService/HealthCheck",
      adminApiKey,
      new grpc.Metadata(),
    );
    expect(health.metadataForwarded).toBe(true);
    expect(health.call.sentStatuses).toHaveLength(0);

    const healthLikeAdmin = invokeAuthInterceptor(
      "/telegram.TelegramService/HealthCheckAdmin",
      adminApiKey,
      new grpc.Metadata(),
    );
    expect(healthLikeAdmin.metadataForwarded).toBe(false);
    expect(healthLikeAdmin.call.sentStatuses[0].code).toBe(
      grpc.status.UNAUTHENTICATED,
    );
  });

  test("gRPC auth rejects invalid API key and accepts valid API key", () => {
    const adminApiKey = "test-admin-key-that-is-at-least-32-characters";
    const invalidMetadata = new grpc.Metadata();
    invalidMetadata.set("x-api-key", "wrong-key");
    const invalid = invokeAuthInterceptor(
      "/telegram.TelegramService/SendMessage",
      adminApiKey,
      invalidMetadata,
    );
    expect(invalid.metadataForwarded).toBe(false);
    expect(invalid.call.sentStatuses[0].code).toBe(grpc.status.UNAUTHENTICATED);

    const validMetadata = new grpc.Metadata();
    validMetadata.set("x-api-key", adminApiKey);
    const valid = invokeAuthInterceptor(
      "/telegram.TelegramService/SendMessage",
      adminApiKey,
      validMetadata,
    );
    expect(valid.metadataForwarded).toBe(true);
    expect(valid.call.sentStatuses).toHaveLength(0);
  });

  test("formats IPv4 and IPv6 gRPC bind targets", () => {
    expect(formatGrpcBindTarget("127.0.0.1", 50052)).toBe("127.0.0.1:50052");
    expect(formatGrpcBindTarget("::1", 50052)).toBe("[::1]:50052");
    expect(formatGrpcBindTarget("[::1]", 50052)).toBe("[::1]:50052");
  });

  test("credential comparison accepts only exact secret matches", () => {
    expect(safeCredentialEqual("secret-token", "secret-token")).toBe(true);
    expect(safeCredentialEqual("secret-token", "secret-token-extra")).toBe(
      false,
    );
    expect(safeCredentialEqual("secret-token-extra", "secret-token")).toBe(
      false,
    );
    expect(safeCredentialEqual("", "secret-token")).toBe(false);
  });

  test("credential comparison rejects over-limit matching prefixes", () => {
    const longSecret = "a".repeat(4097);
    expect(safeCredentialEqual(longSecret, longSecret)).toBe(false);
    expect(safeCredentialEqual(`${"a".repeat(4096)}b`, `${"a".repeat(4096)}c`)).toBe(
      false,
    );
  });
});
