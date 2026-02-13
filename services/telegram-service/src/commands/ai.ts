import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import type { AIModelInfo } from "../api/types";

export function registerAICommands(bot: Bot, api: BackendApiClient): void {
  bot.command("ai_models", async (ctx) => {
    const userId = ctx.from?.id;

    if (!userId) {
      await ctx.reply("Unable to identify user.");
      return;
    }

    try {
      const result = await api.getAIModels();

      if (!result || !result.models || result.models.length === 0) {
        await ctx.reply("No AI models available. Try again later.");
        return;
      }

      const lines = ["🤖 Available AI Models:\n"];

      const providerGroups: Record<string, AIModelInfo[]> = {};
      for (const model of result.models) {
        if (!providerGroups[model.provider]) {
          providerGroups[model.provider] = [];
        }
        providerGroups[model.provider].push(model);
      }

      for (const [provider, models] of Object.entries(providerGroups)) {
        lines.push(`<b>${provider.toUpperCase()}</b>`);
        for (const m of models.slice(0, 5)) {
          const tools = m.supports_tools ? "🔧" : "";
          const vision = m.supports_vision ? "👁" : "";
          lines.push(`  ${m.model_id} ${tools}${vision}`);
        }
        if (models.length > 5) {
          lines.push(`  ... and ${models.length - 5} more`);
        }
        lines.push("");
      }

      lines.push("🔧 = Tool support | 👁 = Vision support");
      lines.push("\nUse /ai_select <model> to select a model.");

      await ctx.reply(lines.join("\n"), { parse_mode: "HTML" });
    } catch {
      await ctx.reply("Failed to fetch AI models. Please try again later.");
    }
  });

  bot.command("ai_select", async (ctx) => {
    const userId = ctx.from?.id;
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
        `✅ AI model selected: <b>${result.model?.display_name || modelId}</b>\n` +
        `Provider: ${result.model?.provider || "Unknown"}\n` +
        `Cost: $${result.model?.cost || "N/A"} per 1M tokens`,
        { parse_mode: "HTML" },
      );
    } catch {
      await ctx.reply(
        `Failed to select model "${modelId}". Please try again later.`,
      );
    }
  });

  bot.command("ai_status", async (ctx) => {
    const userId = ctx.from?.id;

    if (!userId) {
      await ctx.reply("Unable to identify user.");
      return;
    }

    try {
      const result = await api.getAIStatus(String(userId));

      if (!result) {
        await ctx.reply("No AI configuration found. Use /ai_models to select a model.");
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
    } catch {
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
        await ctx.reply("No suitable model found for the specified requirements.");
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
    } catch {
      await ctx.reply("Failed to route model. Please try again later.");
    }
  });
}
