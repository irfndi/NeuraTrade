import { afterAll, beforeAll } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

let originalHome: string | undefined;
let testHome: string | undefined;

beforeAll(() => {
  originalHome = process.env.NEURATRADE_HOME;
  testHome = fs.mkdtempSync(path.join(os.tmpdir(), "neuratrade-cli-ts-test-"));
  process.env.NEURATRADE_HOME = testHome;
  process.env.DATABASE_DRIVER = "sqlite";
  process.env.SQLITE_PATH = path.join(testHome, "neuratrade.db");
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
