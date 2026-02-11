import type { Bot } from "grammy";
import type { BackendApiClient } from "../../api/client";
import type { SessionManager } from "../../session";
import type { WalletInfo } from "../../api/types";
import { getChatId, getCommandArgs } from "./helpers";

function formatWalletList(wallets: readonly WalletInfo[]): string {
  if (wallets.length === 0) {
    return "👛 No wallets connected yet.\nUse /connect_exchange or /connect_polymarket.";
  }

  const lines = ["👛 Connected Wallets"];

  wallets.forEach((wallet, index) => {
    lines.push(
      "",
      `${index + 1}. ${wallet.provider} (${wallet.type})`,
      `   Address: ${wallet.address_masked}`,
      `   Status: ${wallet.status}`,
    );
    if (wallet.wallet_id) {
      lines.push(`   ID: ${wallet.wallet_id}`);
    }
    if (wallet.connected_at) {
      lines.push(`   Connected: ${wallet.connected_at}`);
    }
  });

  return lines.join("\n");
}

export function registerWalletCommands(
  bot: Bot,
  api: BackendApiClient,
  sessions: SessionManager,
): void {
  bot.command("connect_exchange", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to connect exchange: missing chat information.");
      return;
    }

    const args = getCommandArgs(ctx);
    if (!args) {
      sessions.setSession(chatId, {
        step: "awaiting_exchange_selection",
        data: {},
      });
      await ctx.reply(
        "Usage: /connect_exchange <exchange> [account_label]\nExample: /connect_exchange binance main",
      );
      return;
    }

    const [exchange, ...labelTokens] = args.split(/\s+/).filter(Boolean);
    const accountLabel = labelTokens.join(" ");

    try {
      const response = await api.connectExchange(
        chatId,
        exchange,
        accountLabel || undefined,
      );
      sessions.clearSession(chatId);
      await ctx.reply(response.message || `✅ Exchange connected: ${exchange}`);
    } catch (error) {
      await ctx.reply(
        `❌ Failed to connect exchange (${(error as Error).message}).`,
      );
    }
  });

  bot.command("connect_polymarket", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply(
        "Unable to connect Polymarket: missing chat information.",
      );
      return;
    }

    const walletAddress = getCommandArgs(ctx);
    if (!walletAddress) {
      sessions.setSession(chatId, {
        step: "awaiting_wallet_address",
        data: {},
      });
      await ctx.reply(
        "Usage: /connect_polymarket <wallet_address>\nExample: /connect_polymarket 0x1234...abcd",
      );
      return;
    }

    try {
      const response = await api.connectPolymarket(chatId, walletAddress);
      sessions.clearSession(chatId);
      await ctx.reply(response.message || "✅ Polymarket wallet connected.");
    } catch (error) {
      await ctx.reply(
        `❌ Failed to connect Polymarket wallet (${(error as Error).message}).`,
      );
    }
  });

  bot.command("add_wallet", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to add wallet: missing chat information.");
      return;
    }

    const args = getCommandArgs(ctx);
    if (!args) {
      sessions.setSession(chatId, {
        step: "awaiting_wallet_address",
        data: {},
      });
      await ctx.reply(
        "Usage: /add_wallet <wallet_address> [type]\nExample: /add_wallet 0x1234...abcd external",
      );
      return;
    }

    const [walletAddress, walletType = "external"] = args
      .split(/\s+/)
      .filter(Boolean);

    try {
      const response = await api.addWallet(chatId, walletAddress, walletType);
      sessions.clearSession(chatId);
      await ctx.reply(response.message || "✅ Wallet added.");
    } catch (error) {
      await ctx.reply(`❌ Failed to add wallet (${(error as Error).message}).`);
    }
  });

  bot.command("remove_wallet", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to remove wallet: missing chat information.");
      return;
    }

    const walletIdOrAddress = getCommandArgs(ctx);
    if (!walletIdOrAddress) {
      await ctx.reply(
        "Usage: /remove_wallet <wallet_id_or_address>\nExample: /remove_wallet wallet_01",
      );
      return;
    }

    try {
      const response = await api.removeWallet(chatId, walletIdOrAddress);
      sessions.clearSession(chatId);
      await ctx.reply(response.message || "✅ Wallet removed.");
    } catch (error) {
      await ctx.reply(
        `❌ Failed to remove wallet (${(error as Error).message}).`,
      );
    }
  });

  bot.command("wallet", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch wallets: missing chat information.");
      return;
    }

    try {
      const response = await api.getWallets(chatId);
      await ctx.reply(formatWalletList(response.wallets ?? []));
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch wallets (${(error as Error).message}).`,
      );
    }
  });
}
