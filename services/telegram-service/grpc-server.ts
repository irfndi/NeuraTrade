import * as grpc from "@grpc/grpc-js";
import {
  TelegramServiceService,
  type TelegramServiceServer,
  type StreamEventsRequest,
  type TelegramEvent,
  type SendMessageRequest,
  type SendMessageResponse,
  type HealthCheckRequest,
  type HealthCheckResponse,
  type SendActionAlertRequest,
  type SendActionAlertResponse,
  type SendQuestProgressRequest,
  type SendQuestProgressResponse,
  type SendMilestoneAlertRequest,
  type SendMilestoneAlertResponse,
  type SendRiskEventRequest,
  type SendRiskEventResponse,
} from "./proto/telegram_service";
import { Bot } from "grammy";
import {
  formatActionAlertMessage,
  formatQuestProgressMessage,
  formatMilestoneAlertMessage,
  formatRiskEventMessage,
  type RiskSeverity,
} from "./src/messages";
import { logger } from "./src/utils/logger";

type ParseMode = "HTML" | "Markdown" | "MarkdownV2";

function toParseMode(parseMode: string): ParseMode | undefined {
  if (
    parseMode === "HTML" ||
    parseMode === "Markdown" ||
    parseMode === "MarkdownV2"
  ) {
    return parseMode;
  }

  return undefined;
}

function normalizeRiskSeverity(value: string): RiskSeverity {
  return value.toLowerCase() === "critical" ? "critical" : "warning";
}

function ensureChatId<TResponse extends { ok: boolean; messageId: string; error: string }>(
  chatId: string,
  callback: grpc.sendUnaryData<TResponse>,
): chatId is string {
  if (chatId.length > 0) {
    return true;
  }

  callback(
    {
      code: grpc.status.INVALID_ARGUMENT,
      details: "Chat ID is required",
    },
    null,
  );
  return false;
}

export class TelegramGrpcServer {
  private readonly bot: Bot;
  private readonly eventStreams = new Map<
    string,
    Set<grpc.ServerWritableStream<StreamEventsRequest, TelegramEvent>>
  >();

  constructor(bot: Bot) {
    this.bot = bot;
  }

  private async sendBotMessage(
    chatId: string,
    text: string,
    parseMode?: ParseMode,
  ): Promise<string> {
    const options = parseMode ? { parse_mode: parseMode } : undefined;
    const sent = await this.bot.api.sendMessage(chatId, text, options);
    return sent.message_id.toString();
  }

  private addEventStream(
    chatId: string,
    call: grpc.ServerWritableStream<StreamEventsRequest, TelegramEvent>,
  ): void {
    const current = this.eventStreams.get(chatId);
    if (current) {
      current.add(call);
      return;
    }

    this.eventStreams.set(chatId, new Set([call]));
  }

  private removeEventStream(
    chatId: string,
    call: grpc.ServerWritableStream<StreamEventsRequest, TelegramEvent>,
  ): void {
    const current = this.eventStreams.get(chatId);
    if (!current) {
      return;
    }

    current.delete(call);
    if (current.size === 0) {
      this.eventStreams.delete(chatId);
    }
  }

  private publishEvent(event: TelegramEvent): void {
    const streams = this.eventStreams.get(event.chatId);
    if (!streams || streams.size === 0) {
      return;
    }

    for (const stream of streams) {
      try {
        stream.write(event);
      } catch {
        this.removeEventStream(event.chatId, stream);
      }
    }
  }

  sendMessage = (
    call: grpc.ServerUnaryCall<SendMessageRequest, SendMessageResponse>,
    callback: grpc.sendUnaryData<SendMessageResponse>,
  ): void => {
    const { chatId, text } = call.request;

    if (!chatId || !text) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Chat ID and text are required",
        },
        null,
      );
      return;
    }

    const parseMode = toParseMode(call.request.parseMode);

    this.sendBotMessage(chatId, text, parseMode)
      .then((messageId) => {
        callback(null, {
          ok: true,
          messageId,
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send message via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error instanceof Error ? error.message : "Unknown error",
        });
      });
  };

  healthCheck = (
    call: grpc.ServerUnaryCall<HealthCheckRequest, HealthCheckResponse>,
    callback: grpc.sendUnaryData<HealthCheckResponse>,
  ): void => {
    void call;
    callback(null, {
      status: "serving",
      version: "1.0.0",
      service: "telegram-service",
    });
  };

  streamEvents = (
    call: grpc.ServerWritableStream<StreamEventsRequest, TelegramEvent>,
  ): void => {
    const { chatId } = call.request;
    if (!chatId) {
      logger.warn("Rejected StreamEvents subscription with empty chat ID");
      call.end();
      return;
    }

    this.addEventStream(chatId, call);
    logger.info("StreamEvents subscribed", { chatId });

    const cleanup = () => {
      this.removeEventStream(chatId, call);
      logger.info("StreamEvents unsubscribed", { chatId });
    };

    call.on("cancelled", cleanup);
    call.on("close", cleanup);
    call.on("error", cleanup);
  };

  sendActionAlert = (
    call: grpc.ServerUnaryCall<SendActionAlertRequest, SendActionAlertResponse>,
    callback: grpc.sendUnaryData<SendActionAlertResponse>,
  ): void => {
    const { chatId } = call.request;
    if (!ensureChatId(chatId, callback)) {
      return;
    }

    const text = formatActionAlertMessage({
      action: call.request.action,
      asset: call.request.asset,
      price: call.request.price,
      size: call.request.size,
      strategy: call.request.strategy,
      reasoning: call.request.reasoning,
      riskCheckPassed: call.request.riskCheckPassed,
      questId: call.request.questId,
    });

    this.sendBotMessage(chatId, text)
      .then((messageId) => {
        this.publishEvent({
          type: "action",
          chatId,
          timestamp: Date.now(),
          action: {
            action: call.request.action,
            asset: call.request.asset,
            price: call.request.price,
            size: call.request.size,
            strategy: call.request.strategy,
            reasoning: call.request.reasoning,
            riskCheckPassed: call.request.riskCheckPassed,
            questId: call.request.questId,
          },
        });

        callback(null, {
          ok: true,
          messageId,
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send action alert via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error instanceof Error ? error.message : "Unknown error",
        });
      });
  };

  sendQuestProgress = (
    call: grpc.ServerUnaryCall<SendQuestProgressRequest, SendQuestProgressResponse>,
    callback: grpc.sendUnaryData<SendQuestProgressResponse>,
  ): void => {
    const { chatId } = call.request;
    if (!ensureChatId(chatId, callback)) {
      return;
    }

    const text = formatQuestProgressMessage({
      questId: call.request.questId,
      questName: call.request.questName,
      current: call.request.current,
      target: call.request.target,
      percent: call.request.percent,
      timeRemaining: call.request.timeRemaining,
    });

    this.sendBotMessage(chatId, text)
      .then((messageId) => {
        this.publishEvent({
          type: "quest_progress",
          chatId,
          timestamp: Date.now(),
          quest: {
            questId: call.request.questId,
            questName: call.request.questName,
            current: call.request.current,
            target: call.request.target,
            percent: call.request.percent,
            timeRemaining: call.request.timeRemaining,
          },
        });

        callback(null, {
          ok: true,
          messageId,
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send quest progress via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error instanceof Error ? error.message : "Unknown error",
        });
      });
  };

  sendMilestoneAlert = (
    call: grpc.ServerUnaryCall<SendMilestoneAlertRequest, SendMilestoneAlertResponse>,
    callback: grpc.sendUnaryData<SendMilestoneAlertResponse>,
  ): void => {
    const { chatId } = call.request;
    if (!ensureChatId(chatId, callback)) {
      return;
    }

    const text = formatMilestoneAlertMessage({
      amount: call.request.amount,
      phase: call.request.phase,
      nextTarget: call.request.nextTarget,
    });

    this.sendBotMessage(chatId, text)
      .then((messageId) => {
        this.publishEvent({
          type: "milestone",
          chatId,
          timestamp: Date.now(),
          milestone: {
            amount: call.request.amount,
            phase: call.request.phase,
            nextTarget: call.request.nextTarget,
          },
        });

        callback(null, {
          ok: true,
          messageId,
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send milestone alert via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error instanceof Error ? error.message : "Unknown error",
        });
      });
  };

  sendRiskEvent = (
    call: grpc.ServerUnaryCall<SendRiskEventRequest, SendRiskEventResponse>,
    callback: grpc.sendUnaryData<SendRiskEventResponse>,
  ): void => {
    const { chatId } = call.request;
    if (!ensureChatId(chatId, callback)) {
      return;
    }

    const severity = normalizeRiskSeverity(call.request.severity);
    const text = formatRiskEventMessage({
      eventType: call.request.eventType,
      severity,
      message: call.request.message,
      details: call.request.details,
    });

    this.sendBotMessage(chatId, text)
      .then((messageId) => {
        this.publishEvent({
          type: "risk_event",
          chatId,
          timestamp: Date.now(),
          risk: {
            eventType: call.request.eventType,
            severity: call.request.severity,
            message: call.request.message,
            details: call.request.details,
          },
        });

        callback(null, {
          ok: true,
          messageId,
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send risk event via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error instanceof Error ? error.message : "Unknown error",
        });
      });
  };
}

export function startGrpcServer(bot: Bot, port: number) {
  const server = new grpc.Server();
  const service = new TelegramGrpcServer(bot);

  server.addService(
    TelegramServiceService,
    service as unknown as TelegramServiceServer,
  );

  const bindAddr = `0.0.0.0:${port}`;
  server.bindAsync(
    bindAddr,
    grpc.ServerCredentials.createInsecure(),
    (err, boundPort) => {
      if (err) {
        logger.error("Failed to bind gRPC server", err);
        return;
      }
      logger.info("Telegram gRPC Service started", { port: boundPort });
    },
  );

  return server;
}
