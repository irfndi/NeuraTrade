import type { Bot } from "grammy";
import { ApiClientError } from "../api/client";
import type { BackendApiClient } from "../api/client";
import type { AIModelInfo } from "../api/types";
import { logger } from "../utils/logger";

const MAX_PROVIDERS_IN_AI_MODELS = 15;
const MAX_MODELS_PER_PROVIDER = 4;
const TELEGRAM_MAX_MESSAGE_CHARS = 3900;

function resolveTelegramIdentity(ctx: {
  chat?: { id?: string | number };
  from?: { id?: string | number };
}): string | null {
  const chatId = ctx.chat?.id;
  if (chatId !== undefined && chatId !== null) {
    return String(chatId);
  }
  const fromId = ctx.from?.id;
  if (fromId !== undefined && fromId !== null) {
    return String(fromId);
  }
  return null;
}

function splitIntoTelegramMessages(text: string): string[] {
  if (text.length <= TELEGRAM_MAX_MESSAGE_CHARS) {
    return [text];
  }

  const chunks: string[] = [];
  let remaining = text;
  while (remaining.length > TELEGRAM_MAX_MESSAGE_CHARS) {
    let splitAt = remaining.lastIndexOf("\n", TELEGRAM_MAX_MESSAGE_CHARS);
    if (splitAt <= 0) {
      splitAt = TELEGRAM_MAX_MESSAGE_CHARS;
    }
    chunks.push(remaining.slice(0, splitAt).trimEnd());
    remaining = remaining.slice(splitAt).trimStart();
  }
  if (remaining.length > 0) {
    chunks.push(remaining);
  }
  return chunks;
}

function getErrorContext(error: unknown): { status?: number; detail: string } {
  if (error instanceof ApiClientError) {
    return {
      status: error.status,
      detail: `${error.message} (endpoint=${error.endpoint})`,
    };
  }
  if (error instanceof Error) {
    return { detail: error.message };
  }
  return { detail: String(error) };
}

export function registerAICommands(bot: Bot, api: BackendApiClient): void {
  bot.command("ai_models", async (ctx) => {
    try {
      const result = await api.getAIModels();

      if (!result || !result.models || result.models.length === 0) {
        await ctx.reply("No AI models available. Try again later.");
        return;
      }

      const providerGroups: Record<string, AIModelInfo[]> = {};
      for (const model of result.models) {
        if (!providerGroups[model.provider]) {
          providerGroups[model.provider] = [];
        }
        providerGroups[model.provider].push(model);
      }

      const providers = Object.entries(providerGroups).sort(
        (a, b) => b[1].length - a[1].length,
      );
      const selectedProviders = providers.slice(0, MAX_PROVIDERS_IN_AI_MODELS);
      const omittedProviders = Math.max(
        0,
        providers.length - selectedProviders.length,
      );

      const lines = [
        `🤖 Available AI Models (showing ${selectedProviders.length} providers)`,
        "",
      ];

      for (const [provider, models] of selectedProviders) {
        lines.push(`[${provider.toUpperCase()}]`);
        for (const m of models.slice(0, MAX_MODELS_PER_PROVIDER)) {
          const tools = m.supports_tools ? "🔧" : "";
          const vision = m.supports_vision ? "👁" : "";
          lines.push(`- ${m.model_id} ${tools}${vision}`.trimEnd());
        }
        if (models.length > MAX_MODELS_PER_PROVIDER) {
          lines.push(
            `- ... and ${models.length - MAX_MODELS_PER_PROVIDER} more`,
          );
        }
        lines.push("");
      }

      if (omittedProviders > 0) {
        lines.push(`... and ${omittedProviders} more providers`);
        lines.push("");
      }

      lines.push("🔧 = Tool support | 👁 = Vision support");
      lines.push("\nUse /ai_select <model> to select a model.");

      for (const chunk of splitIntoTelegramMessages(lines.join("\n"))) {
        await ctx.reply(chunk);
      }
    } catch (error) {
      const errorContext = getErrorContext(error);
      logger.error("[AI] /ai_models failed", new Error(errorContext.detail), {
        status: errorContext.status,
        chatId: ctx.chat?.id,
        fromId: ctx.from?.id,
      });
      await ctx.reply("Failed to fetch AI models. Please try again later.");
    }
  });

  bot.command("ai_select", async (ctx) => {
    const userId = resolveTelegramIdentity(ctx);
    const args = ctx.match?.toString().trim().split(/\s+/) || [];

    if (!userId) {
      await ctx.reply("Unable to identify user.");
      return;
    }

    if (args.length === 0 || !args[0]) {
      await ctx.reply(
        "Usage: /ai_select <model_id>\n" +
          "Example: /ai_select gpt-4-turbo\n\n" +
          "Use /ai_models to see available models.",
      );
      return;
    }

    const modelId = args[0];

    try {
      const result = await api.selectAIModel(String(userId), modelId);

      if (!result || !result.success) {
        await ctx.reply(
          `Failed to select model "${modelId}". ` +
            "Make sure the model ID is correct.",
        );
        return;
      }

      await ctx.reply(
        `✅ AI model selected: ${result.model?.display_name || modelId}\n` +
          `Provider: ${result.model?.provider || "Unknown"}\n` +
          `Cost: $${result.model?.cost || "N/A"} per 1M tokens`,
      );
    } catch (error) {
      const errorContext = getErrorContext(error);
      logger.error("[AI] /ai_select failed", new Error(errorContext.detail), {
        status: errorContext.status,
        chatId: ctx.chat?.id,
        fromId: ctx.from?.id,
      });
      await ctx.reply(
        `Failed to select model "${modelId}". Please try again later.`,
      );
    }
  });

  bot.command("ai_status", async (ctx) => {
    const userId = resolveTelegramIdentity(ctx);

    if (!userId) {
      await ctx.reply("Unable to identify user.");
      return;
    }

    try {
      const result = await api.getAIStatus(String(userId));

      if (!result) {
        await ctx.reply(
          "No AI configuration found. Use /ai_models to select a model.",
        );
        return;
      }

      const lines = [
        "🤖 AI Configuration:",
        "",
        `📊 Selected Model: ${result.selected_model || "None"}`,
        `🔗 Provider: ${result.provider || "N/A"}`,
        `💰 Daily Spend: $${result.daily_spend || "0.00"}`,
        `📅 Monthly Spend: $${result.monthly_spend || "0.00"}`,
        `🎯 Budget Limit: $${result.budget_limit || "Unlimited"}`,
      ];

      if (result.daily_budget_exceeded) {
        lines.push("\n⚠️ Daily budget exceeded. AI features limited.");
      }

      lines.push("\nUse /ai_models to change model.");

      await ctx.reply(lines.join("\n"));
    } catch (error) {
      const errorContext = getErrorContext(error);
      logger.error("[AI] /ai_status failed", new Error(errorContext.detail), {
        status: errorContext.status,
        chatId: ctx.chat?.id,
        fromId: ctx.from?.id,
      });
      await ctx.reply("Failed to fetch AI status. Please try again later.");
    }
  });

  bot.command("ai_route", async (ctx) => {
    const args = ctx.match?.toString().trim().split(/\s+/) || [];

    let requirements: "fast" | "balanced" | "accurate" = "balanced";
    const caps: string[] = [];

    for (const arg of args) {
      switch (arg.toLowerCase()) {
        case "fast":
          requirements = "fast";
          break;
        case "accurate":
          requirements = "accurate";
          break;
        case "tools":
          caps.push("tools");
          break;
        case "vision":
          caps.push("vision");
          break;
        case "reasoning":
          caps.push("reasoning");
          break;
      }
    }

    try {
      const result = await api.routeAIModel({
        latency_preference: requirements,
        require_tools: caps.includes("tools"),
        require_vision: caps.includes("vision"),
        require_reasoning: caps.includes("reasoning"),
      });

      if (!result || !result.model) {
        await ctx.reply(
          "No suitable model found for the specified requirements.",
        );
        return;
      }

      const lines = [
        "🎯 Routed Model:",
        "",
        `📊 ${result.model.display_name}`,
        `🔗 Provider: ${result.model.provider}`,
        `⚡ Latency: ${result.model.latency_class}`,
        `💰 Cost: $${result.model.cost} per 1M tokens`,
        `📈 Score: ${result.score?.toFixed(1) || "N/A"}`,
      ];

      if (result.reason) {
        lines.push(`📝 Reason: ${result.reason}`);
      }

      if (result.alternatives && result.alternatives.length > 0) {
        lines.push("\nAlternatives:");
        for (const alt of result.alternatives.slice(0, 3)) {
          lines.push(`  • ${alt.display_name} (${alt.provider})`);
        }
      }

      await ctx.reply(lines.join("\n"));
    } catch (error) {
      const errorContext = getErrorContext(error);
      logger.error("[AI] /ai_route failed", new Error(errorContext.detail), {
        status: errorContext.status,
        chatId: ctx.chat?.id,
        fromId: ctx.from?.id,
      });
      await ctx.reply("Failed to route model. Please try again later.");
    }
  });
}
