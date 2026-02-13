import { describe, expect, test } from "bun:test";
import { EventEmitter } from "events";
import * as grpc from "@grpc/grpc-js";
import type { Bot } from "grammy";
import { TelegramGrpcServer } from "./grpc-server";
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
    expect(sentMessages[0].text).toContain("ACTION: BUY");

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
});
