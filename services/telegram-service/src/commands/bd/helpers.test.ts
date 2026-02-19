import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import path from "node:path";

import { persistChatIdToLocalConfig } from "./helpers";

describe("persistChatIdToLocalConfig", () => {
  const originalHome = process.env.NEURATRADE_HOME;
  let tempHome = "";

  beforeEach(async () => {
    tempHome = path.join(process.cwd(), ".tmp-test-neuratrade-home");
    await Bun.$`mkdir -p ${tempHome}`.quiet();
    await Bun.write(path.join(tempHome, "config.json"), JSON.stringify({
      telegram: {},
    }));
    process.env.NEURATRADE_HOME = tempHome;
  });

  afterEach(async () => {
    process.env.NEURATRADE_HOME = originalHome;
    await Bun.$`rm -rf ${tempHome}`.quiet();
  });

  test("writes chat_id to telegram and services.telegram sections", async () => {
    await persistChatIdToLocalConfig("123456789");

    const cfg = await Bun.file(path.join(tempHome, "config.json")).json();
    const parsed = cfg as {
      telegram?: { chat_id?: string };
      services?: { telegram?: { chat_id?: string } };
    };

    expect(parsed.telegram?.chat_id).toBe("123456789");
    expect(parsed.services?.telegram?.chat_id).toBe("123456789");
  });
});
