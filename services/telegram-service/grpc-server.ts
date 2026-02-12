import * as grpc from "@grpc/grpc-js";
import {
  TelegramServiceService,
  TelegramServiceServer,
  StreamEventsRequest,
  TelegramEvent,
  SendMessageRequest,
  SendMessageResponse,
  HealthCheckRequest,
  HealthCheckResponse,
  SendActionAlertRequest,
  SendActionAlertResponse,
  SendQuestProgressRequest,
  SendQuestProgressResponse,
  SendMilestoneAlertRequest,
  SendMilestoneAlertResponse,
  SendRiskEventRequest,
  SendRiskEventResponse,
} from "./proto/telegram_service";
import { Bot } from "grammy";
import { logger } from "./src/utils/logger";

function formatActionAlert(req: SendActionAlertRequest): string {
  const riskIcon = req.riskCheckPassed ? "✅" : "❌";
  return (
    `🤖 ACTION: ${req.action}\n` +
    `Asset: ${req.asset}\n` +
    `Price: ${req.price}\n` +
    `Size: ${req.size}\n` +
    `Strategy: ${req.strategy}\n\n` +
    `Reasoning: ${req.reasoning}\n` +
    `Risk Check: ${riskIcon} ${req.riskCheckPassed ? "PASSED" : "FAILED"}\n` +
    (req.questId ? `Quest: ${req.questId}` : "")
  );
}

function formatQuestProgress(req: SendQuestProgressRequest): string {
  return (
    `📋 Quest: ${req.questName}\n` +
    `Progress: ${req.current}/${req.target} (${req.percent}%)\n` +
    `Time Remaining: ${req.timeRemaining}`
  );
}

function formatMilestoneAlert(req: SendMilestoneAlertRequest): string {
  return (
    `🎯 Milestone Reached: $${req.amount}\n` +
    `Phase: ${req.phase}\n` +
    `Next Target: $${req.nextTarget}`
  );
}

function formatRiskEvent(req: SendRiskEventRequest): string {
  const severityIcon = req.severity === "critical" ? "🚨" : "⚠️";
  let msg = `${severityIcon} ${req.severity.toUpperCase()}: ${req.eventType}\n\n${req.message}`;
  if (req.details && Object.keys(req.details).length > 0) {
    msg += "\n\nDetails:\n";
    for (const [key, value] of Object.entries(req.details)) {
      msg += `• ${key}: ${value}\n`;
    }
  }
  return msg;
}

export class TelegramGrpcServer {
  private bot: Bot;

  constructor(bot: Bot) {
    this.bot = bot;
  }

  sendMessage = (
    call: grpc.ServerUnaryCall<SendMessageRequest, SendMessageResponse>,
    callback: grpc.sendUnaryData<SendMessageResponse>,
  ): void => {
    const { chatId, text, parseMode } = call.request;

    if (!chatId || !text) {
      callback(null, {
        ok: false,
        messageId: "",
        error: "Chat ID and Text are required",
        errorCode: TelegramErrorCode.INVALID_REQUEST,
        retryAfter: 0,
      });
      return;
    }

    // Use retry logic for sending messages
    withRetry(
      () =>
        sendWithTimeout(
          this.bot.api.sendMessage(chatId, text, {
            parse_mode: parseMode as any,
          }),
        ),
      classifyError,
      GRPC_RETRY_CONFIG,
    )
      .then((result) => {
        if (result.success && result.data) {
          callback(null, {
            ok: true,
            messageId: result.data.message_id.toString(),
            error: "",
            errorCode: "",
            retryAfter: 0,
          });
        } else {
          const errorInfo = result.error as TelegramErrorInfo;
          console.error(
            `[gRPC] Failed to send message after ${result.attempts} attempts:`,
            errorInfo?.code,
            errorInfo?.message,
          );
          callback(null, {
            ok: false,
            messageId: "",
            error: errorInfo?.message || "Unknown error",
            errorCode: errorInfo?.code || TelegramErrorCode.UNKNOWN,
            retryAfter: errorInfo?.retryAfter || 0,
          });
        }
      })
      .catch((error) => {
        logger.error("Failed to send message via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: errorInfo.message,
          errorCode: errorInfo.code,
          retryAfter: errorInfo.retryAfter || 0,
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
    logger.info("StreamEvents subscribed", { chatId });
    call.end();
  };

  sendActionAlert = (
    call: grpc.ServerUnaryCall<SendActionAlertRequest, SendActionAlertResponse>,
    callback: grpc.sendUnaryData<SendActionAlertResponse>,
  ): void => {
    const { chatId } = call.request;

    if (!chatId) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Chat ID is required",
        },
        null,
      );
      return;
    }

    const text = formatActionAlert(call.request);

    this.bot.api
      .sendMessage(chatId, text)
      .then((sent) => {
        logger.info("Action alert sent via gRPC", { chatId, action: call.request.action });
        callback(null, {
          ok: true,
          messageId: sent.message_id.toString(),
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send action alert via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error.message || "Unknown error",
        });
      });
  };

  sendQuestProgress = (
    call: grpc.ServerUnaryCall<SendQuestProgressRequest, SendQuestProgressResponse>,
    callback: grpc.sendUnaryData<SendQuestProgressResponse>,
  ): void => {
    const { chatId } = call.request;

    if (!chatId) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Chat ID is required",
        },
        null,
      );
      return;
    }

    const text = formatQuestProgress(call.request);

    this.bot.api
      .sendMessage(chatId, text)
      .then((sent) => {
        logger.info("Quest progress sent via gRPC", { chatId, questId: call.request.questId });
        callback(null, {
          ok: true,
          messageId: sent.message_id.toString(),
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send quest progress via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error.message || "Unknown error",
        });
      });
  };

  sendMilestoneAlert = (
    call: grpc.ServerUnaryCall<SendMilestoneAlertRequest, SendMilestoneAlertResponse>,
    callback: grpc.sendUnaryData<SendMilestoneAlertResponse>,
  ): void => {
    const { chatId } = call.request;

    if (!chatId) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Chat ID is required",
        },
        null,
      );
      return;
    }

    const text = formatMilestoneAlert(call.request);

    this.bot.api
      .sendMessage(chatId, text)
      .then((sent) => {
        logger.info("Milestone alert sent via gRPC", { chatId, amount: call.request.amount });
        callback(null, {
          ok: true,
          messageId: sent.message_id.toString(),
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send milestone alert via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error.message || "Unknown error",
        });
      });
  };

  sendRiskEvent = (
    call: grpc.ServerUnaryCall<SendRiskEventRequest, SendRiskEventResponse>,
    callback: grpc.sendUnaryData<SendRiskEventResponse>,
  ): void => {
    const { chatId } = call.request;

    if (!chatId) {
      callback(
        {
          code: grpc.status.INVALID_ARGUMENT,
          details: "Chat ID is required",
        },
        null,
      );
      return;
    }

    const text = formatRiskEvent(call.request);

    this.bot.api
      .sendMessage(chatId, text)
      .then((sent) => {
        logger.warn("Risk event sent via gRPC", { 
          chatId, 
          eventType: call.request.eventType, 
          severity: call.request.severity 
        });
        callback(null, {
          ok: true,
          messageId: sent.message_id.toString(),
          error: "",
        });
      })
      .catch((error) => {
        logger.error("Failed to send risk event via gRPC", error, { chatId });
        callback(null, {
          ok: false,
          messageId: "",
          error: error.message || "Unknown error",
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
