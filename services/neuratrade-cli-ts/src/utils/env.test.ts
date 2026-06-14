import { describe, expect, test, afterEach } from "bun:test";
import { Effect } from "effect";
import {
  getEnvOrRuntimeString,
  getEnvOrRuntimePort,
  getEnvOrRuntimeBool,
  getEnvOrRuntimeDurationSeconds,
} from "./env.ts";

// ---------------------------------------------------------------------------
// Helpers to manage process.env in tests
// ---------------------------------------------------------------------------
const savedEnv: Record<string, string | undefined> = {};

function setEnv(key: string, value: string): void {
  savedEnv[key] = process.env[key];
  process.env[key] = value;
}

function clearEnv(key: string): void {
  savedEnv[key] = process.env[key];
  delete process.env[key];
}

afterEach(() => {
  // Restore every env var we touched
  for (const [key, val] of Object.entries(savedEnv)) {
    if (val === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = val;
    }
  }
  // Reset accumulator
  for (const key of Object.keys(savedEnv)) {
    delete savedEnv[key];
  }
});

// ===========================================================================
// getEnvOrRuntimeString
// ===========================================================================
describe("getEnvOrRuntimeString", () => {
  test("returns env var when set (even if whitespace-padded)", () => {
    setEnv("TEST_STR", "  from-env  ");
    const result = Effect.runSync(
      getEnvOrRuntimeString("TEST_STR", "from-runtime", "fallback"),
    );
    expect(result).toBe("from-env");
  });

  test("returns runtime value when env is empty", () => {
    clearEnv("TEST_STR");
    const result = Effect.runSync(
      getEnvOrRuntimeString("TEST_STR", "from-runtime", "fallback"),
    );
    expect(result).toBe("from-runtime");
  });

  test("returns fallback when env and runtime are empty", () => {
    clearEnv("TEST_STR");
    const result = Effect.runSync(
      getEnvOrRuntimeString("TEST_STR", "", "fallback-value"),
    );
    expect(result).toBe("fallback-value");
  });

  test("returns fallback when env is empty and runtime is whitespace-only", () => {
    clearEnv("TEST_STR");
    const result = Effect.runSync(
      getEnvOrRuntimeString("TEST_STR", "   ", "fb"),
    );
    expect(result).toBe("fb");
  });

  test("env takes precedence over both runtime and fallback", () => {
    setEnv("TEST_STR", "env-wins");
    const result = Effect.runSync(
      getEnvOrRuntimeString("TEST_STR", "runtime", "fallback"),
    );
    expect(result).toBe("env-wins");
  });
});

// ===========================================================================
// getEnvOrRuntimePort
// ===========================================================================
describe("getEnvOrRuntimePort", () => {
  test("returns env port when set", () => {
    setEnv("TEST_PORT", "9090");
    const result = Effect.runSync(
      getEnvOrRuntimePort("TEST_PORT", 3001, "8080"),
    );
    expect(result).toBe("9090");
  });

  test("returns runtime port when env is absent", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(
      getEnvOrRuntimePort("TEST_PORT", 3001, "8080"),
    );
    expect(result).toBe("3001");
  });

  test("returns fallback when env is absent and runtime is 0", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(getEnvOrRuntimePort("TEST_PORT", 0, "8080"));
    expect(result).toBe("8080");
  });

  test("returns fallback when runtime port is out of range (65536)", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(
      getEnvOrRuntimePort("TEST_PORT", 65536, "8080"),
    );
    expect(result).toBe("8080");
  });

  test("returns fallback when runtime port is negative", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(getEnvOrRuntimePort("TEST_PORT", -1, "8080"));
    expect(result).toBe("8080");
  });

  test("accepts port 1 as valid runtime port", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(getEnvOrRuntimePort("TEST_PORT", 1, "8080"));
    expect(result).toBe("1");
  });

  test("accepts port 65535 as valid runtime port", () => {
    clearEnv("TEST_PORT");
    const result = Effect.runSync(
      getEnvOrRuntimePort("TEST_PORT", 65535, "8080"),
    );
    expect(result).toBe("65535");
  });

  test("trims whitespace from env port value", () => {
    setEnv("TEST_PORT", "  4000  ");
    const result = Effect.runSync(
      getEnvOrRuntimePort("TEST_PORT", 3001, "8080"),
    );
    expect(result).toBe("4000");
  });
});

// ===========================================================================
// getEnvOrRuntimeBool
// ===========================================================================
describe("getEnvOrRuntimeBool", () => {
  test("returns true when env is 'true'", () => {
    setEnv("TEST_BOOL", "true");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, true),
    );
    expect(result).toBe(true);
  });

  test("returns false when env is 'false'", () => {
    setEnv("TEST_BOOL", "false");
    const result = Effect.runSync(getEnvOrRuntimeBool("TEST_BOOL", true, true));
    expect(result).toBe(false);
  });

  test("returns true when env is '1'", () => {
    setEnv("TEST_BOOL", "1");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, false),
    );
    expect(result).toBe(true);
  });

  test("returns false when env is '0'", () => {
    setEnv("TEST_BOOL", "0");
    const result = Effect.runSync(getEnvOrRuntimeBool("TEST_BOOL", true, true));
    expect(result).toBe(false);
  });

  test("returns runtime value when env is absent", () => {
    clearEnv("TEST_BOOL");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", true, false),
    );
    expect(result).toBe(true);
  });

  test("returns runtime value even when it is false", () => {
    clearEnv("TEST_BOOL");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, true),
    );
    // runtimeValue is explicitly provided (not undefined), so it wins
    expect(result).toBe(false);
  });

  test("returns fallback when runtime value is undefined", () => {
    clearEnv("TEST_BOOL");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", undefined, true),
    );
    expect(result).toBe(true);
  });

  test("returns fallback when env value is unparseable", () => {
    setEnv("TEST_BOOL", "not-a-bool");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, true),
    );
    expect(result).toBe(true);
  });

  test("handles 'yes' as true", () => {
    setEnv("TEST_BOOL", "yes");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, false),
    );
    expect(result).toBe(true);
  });

  test("handles 'no' as false", () => {
    setEnv("TEST_BOOL", "no");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", true, false),
    );
    expect(result).toBe(false);
  });

  test("trims whitespace from env value before parsing", () => {
    setEnv("TEST_BOOL", "  true  ");
    const result = Effect.runSync(
      getEnvOrRuntimeBool("TEST_BOOL", false, false),
    );
    expect(result).toBe(true);
  });
});

// ===========================================================================
// getEnvOrRuntimeDurationSeconds
// ===========================================================================
describe("getEnvOrRuntimeDurationSeconds", () => {
  test("returns env seconds when set and valid", () => {
    setEnv("TEST_DUR", "30");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(30);
  });

  test("returns runtime seconds when env is absent", () => {
    clearEnv("TEST_DUR");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(60);
  });

  test("returns fallback when env is absent and runtime is 0", () => {
    clearEnv("TEST_DUR");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 0, 15),
    );
    expect(result).toBe(15);
  });

  test("returns fallback when env value is not a number", () => {
    setEnv("TEST_DUR", "abc");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(10);
  });

  test("returns fallback when env value is negative", () => {
    setEnv("TEST_DUR", "-5");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(10);
  });

  test("returns fallback when env value is zero", () => {
    setEnv("TEST_DUR", "0");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(10);
  });

  test("trims whitespace from env value", () => {
    setEnv("TEST_DUR", "  45  ");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(45);
  });

  test("accepts large valid seconds value", () => {
    setEnv("TEST_DUR", "86400");
    const result = Effect.runSync(
      getEnvOrRuntimeDurationSeconds("TEST_DUR", 60, 10),
    );
    expect(result).toBe(86400);
  });
});
