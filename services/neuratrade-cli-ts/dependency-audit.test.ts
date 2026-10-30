import { describe, expect, it } from "bun:test";

/**
 * P2 clever-cabin-x0i: the lodash transitive chain is gone since the
 * alchemy 2.0.0-beta.84 refresh (`@prisma/dev > chevrotain` no longer pulls
 * it), and the `lodash` override was pruned from package.json because
 * nothing resolves lodash at all anymore.
 *
 * Guard: if lodash ever re-enters the resolved tree, it must be past the
 * vulnerable range (<= 4.17.23). Failing the build here is the point — an
 * unshielded reintroduction of GHSA-r5fr-rjxr-66jc should be loud, not
 * silently overridden.
 */

/** bun.lock is JSONC-style (trailing commas), so regex over the raw text. */
const overridesBlock = /"overrides"\s*:\s*\{([\s\S]*?)\n {2}\},/;

describe("CLI dependency audit guard (clever-cabin-x0i)", () => {
  it("does not resolve lodash at all; if it ever re-enters, it must be past the vulnerable range", async () => {
    const raw = await Bun.file(new URL("./bun.lock", import.meta.url)).text();
    const m = raw.match(/"lodash@([^"]+)"/);
    if (m === null) {
      // Absent from the tree: the only remaining override is the sharp
      // floor still carried for alchemy's cloudflare-runtime.
      const overrides = raw.match(overridesBlock)?.[1] ?? "";
      expect(overrides).toContain('"sharp"');
      expect(overrides).not.toContain('"lodash"');
      return;
    }
    const version = m[1];
    const parts = version.split(".").map(Number);
    const vulnerable =
      parts[0] < 4 ||
      (parts[0] === 4 && parts[1] < 18) ||
      (parts[0] === 4 && parts[1] === 17 && parts[2] <= 23);
    expect(vulnerable).toBe(false);
  });

  it("does not carry dead lodash override plumbing in package.json", async () => {
    const pkg = (await Bun.file(
      new URL("./package.json", import.meta.url),
    ).json()) as {
      dependencies?: Record<string, string>;
      overrides?: Record<string, string>;
    };
    expect(pkg.dependencies?.["lodash"]).toBeUndefined();
    expect(pkg.overrides?.["lodash"]).toBeUndefined();
  });
});
