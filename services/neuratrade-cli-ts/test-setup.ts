import { afterAll, beforeAll } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

let originalHome: string | undefined;
let testHome: string | undefined;

/**
 * Live exchange credentials must never leak into the test process. The
 * repo-root .env is auto-loaded by Bun into test workers, so clear the
 * Bitget credential and sandbox vars here (the single preload point) so
 * config tests can assert on a clean environment.
 */
const LIVE_CREDENTIAL_KEYS = [
  "BITGET_API_KEY",
  "BITGET_API_SECRET",
  "BITGET_PASSPHRASE",
  "BITGET_USE_SANDBOX",
] as const;

for (const key of LIVE_CREDENTIAL_KEYS) {
  delete process.env[key];
}

beforeAll(() => {
  originalHome = process.env.NEURATRADE_HOME;
  testHome = fs.mkdtempSync(path.join(os.tmpdir(), "neuratrade-cli-ts-test-"));
  process.env.NEURATRADE_HOME = testHome;
  process.env.DATABASE_DRIVER = "sqlite";
  process.env.SQLITE_PATH = path.join(testHome, "neuratrade.db");
  for (const key of LIVE_CREDENTIAL_KEYS) {
    delete process.env[key];
  }
});

afterAll(() => {
  if (testHome) {
    try {
      fs.rmSync(testHome, { recursive: true, force: true });
    } catch {
      // ignore cleanup errors
    }
  }
  if (originalHome !== undefined) {
    process.env.NEURATRADE_HOME = originalHome;
  } else {
    delete process.env.NEURATRADE_HOME;
  }
});
