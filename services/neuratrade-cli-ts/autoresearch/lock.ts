/**
 * Exclusive file lock for shared champion.json across parallel workers.
 */
import {
  openSync,
  closeSync,
  unlinkSync,
  writeFileSync,
  readFileSync,
  mkdirSync,
} from "node:fs";
import { dirname } from "node:path";

export function withFileLock<T>(
  lockPath: string,
  fn: () => T,
  opts: { retries?: number; sleepMs?: number } = {},
): T {
  const retries = opts.retries ?? 200;
  const sleepMs = opts.sleepMs ?? 25;
  mkdirSync(dirname(lockPath), { recursive: true });

  for (let i = 0; i < retries; i++) {
    let fd: number | undefined;
    try {
      fd = openSync(lockPath, "wx");
      try {
        return fn();
      } finally {
        closeSync(fd);
        try {
          unlinkSync(lockPath);
        } catch {
          /* ignore */
        }
      }
    } catch {
      if (fd !== undefined) {
        try {
          closeSync(fd);
        } catch {
          /* ignore */
        }
      }
      Bun.sleepSync(sleepMs);
    }
  }
  throw new Error(`timeout acquiring lock ${lockPath}`);
}

export function readJsonFile<T>(path: string): T | null {
  try {
    return JSON.parse(readFileSync(path, "utf8")) as T;
  } catch {
    return null;
  }
}

export function writeJsonFile(path: string, value: unknown): void {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}
