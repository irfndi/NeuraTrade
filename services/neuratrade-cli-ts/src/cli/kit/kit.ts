/**
 * Minimal hand-rolled CLI kit — replaces the subset of `@effect/cli` that
 * NeuraTrade's command modules use (removed in the Effect v4 migration).
 *
 * Covered surface:
 * - Named options: text / integer / float / boolean / choice
 * - Defaults, required, optional (`Option<A>`), aliases, descriptions
 * - Subcommand trees with `--help` / `-h` at every level and `--version`
 * - `--opt value`, `--opt=value`, `-a value`, `-a=value` forms
 * - Unknown-option / missing-required / invalid-value errors (non-zero exit)
 *
 * The public `Command` / `Options` namespaces intentionally mirror the
 * `@effect/cli` API shape so command modules port over with only an import
 * change.
 */
import { Console, Effect, Option } from "effect";

// ---------------------------------------------------------------------------
// Pipeable
// ---------------------------------------------------------------------------

export interface Pipeable {
  pipe<A, B>(this: A, ab: (_: A) => B): B;
  pipe<A, B, C>(this: A, ab: (_: A) => B, bc: (_: B) => C): C;
  pipe<A, B, C, D>(
    this: A,
    ab: (_: A) => B,
    bc: (_: B) => C,
    cd: (_: C) => D,
  ): D;
  pipe<A, B, C, D, E>(
    this: A,
    ab: (_: A) => B,
    bc: (_: B) => C,
    cd: (_: C) => D,
    de: (_: D) => E,
  ): E;
}

function pipeImpl<A>(this: A, ...fns: ReadonlyArray<(_: A) => A>): A {
  return fns.reduce((acc, fn) => fn(acc), this);
}

const pipeable: Pipeable = { pipe: pipeImpl as Pipeable["pipe"] };

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export type OptionKind = "text" | "integer" | "float" | "boolean" | "choice";

/** Tracks how an option's value is produced at the type level. */
export type Requirement = "required" | "defaulted" | "optional";

export interface OptionSpec<
  out A,
  out R extends Requirement = Requirement,
> extends Pipeable {
  readonly kind: OptionKind;
  /** Kebab-case flag name, without the leading `--`. */
  readonly name: string;
  readonly alias: string | undefined;
  readonly description: string;
  readonly choices: ReadonlyArray<string> | undefined;
  readonly hasDefault: boolean;
  readonly defaultValue: unknown;
  readonly isOptional: boolean;
  // Phantom type carriers — never present at runtime.
  readonly _A?: A;
  readonly _R?: R;
}

// biome-ignore lint/suspicious/noExplicitAny: erased spec
export type AnyOptionSpec = OptionSpec<any, any>;

/** A scalar value a CLI option can parse out of argv. */
export type ParsedScalar = string | number | boolean;

/** A parsed option value: either a raw scalar or an optional wrapper. */
export type ParsedOptionValue = ParsedScalar | Option.Option<ParsedScalar>;

function makeSpec<A, R extends Requirement>(fields: {
  readonly kind: OptionKind;
  readonly name: string;
  readonly choices?: ReadonlyArray<string>;
}): OptionSpec<A, R> {
  return {
    ...pipeable,
    kind: fields.kind,
    name: fields.name,
    alias: undefined,
    description: "",
    choices: fields.choices,
    hasDefault: false,
    defaultValue: undefined,
    isOptional: false,
  };
}

const text = (name: string): OptionSpec<string, "required"> =>
  makeSpec({ kind: "text", name });

const integer = (name: string): OptionSpec<number, "required"> =>
  makeSpec({ kind: "integer", name });

const float = (name: string): OptionSpec<number, "required"> =>
  makeSpec({ kind: "float", name });

const boolean_ = (name: string): OptionSpec<boolean, "required"> =>
  makeSpec({ kind: "boolean", name });

const choice = <const C extends ReadonlyArray<string>>(
  name: string,
  choices: C,
): OptionSpec<C[number], "required"> =>
  makeSpec({ kind: "choice", name, choices });

const withDefault =
  <V>(value: V) =>
  <A, R extends Requirement>(
    spec: OptionSpec<A, R>,
  ): OptionSpec<A, "defaulted"> =>
    ({
      ...spec,
      hasDefault: true,
      defaultValue: value,
      isOptional: false,
    }) as OptionSpec<A, "defaulted">;

const withDescription =
  (description: string) =>
  <A, R extends Requirement>(spec: OptionSpec<A, R>): OptionSpec<A, R> => ({
    ...spec,
    description,
  });

const withAlias =
  (alias: string) =>
  <A, R extends Requirement>(spec: OptionSpec<A, R>): OptionSpec<A, R> => ({
    ...spec,
    alias,
  });

const optional = <A, R extends Requirement>(
  spec: OptionSpec<A, R>,
): OptionSpec<A, "optional"> =>
  ({
    ...spec,
    isOptional: true,
    hasDefault: false,
    defaultValue: undefined,
  }) as OptionSpec<A, "optional">;

export const Options = {
  text,
  integer,
  float,
  boolean: boolean_,
  choice,
  withDefault,
  withDescription,
  withAlias,
  optional,
} as const;

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

export interface Command<
  A = unknown,
  E = unknown,
  R = unknown,
> extends Pipeable {
  readonly name: string;
  readonly description: string;
  /** Ordered [handler-arg key, spec] pairs. */
  readonly options: ReadonlyArray<readonly [string, AnyOptionSpec]>;
  readonly subcommands: ReadonlyArray<AnyCommand>;
  readonly handler: (
    // biome-ignore lint/suspicious/noExplicitAny: erased at the dispatch layer
    args: any,
  ) => Effect.Effect<A, E, R>;
}

// biome-ignore lint/suspicious/noExplicitAny: erased command
export type AnyCommand = Command<any, any, any>;

/** Maps a command's options record to the handler args type. */
export type OptionsArgs<O extends Record<string, AnyOptionSpec>> = {
  [K in keyof O]: O[K] extends OptionSpec<infer A, infer R>
    ? R extends "optional"
      ? Option.Option<A>
      : A
    : never;
};

const make = <const O extends Record<string, AnyOptionSpec>, A, E, R>(
  name: string,
  options: O,
  handler: (args: OptionsArgs<O>) => Effect.Effect<A, E, R>,
): Command<A, E, R> => ({
  ...pipeable,
  name,
  description: "",
  options: Object.entries(options),
  subcommands: [],
  handler: handler as Command<A, E, R>["handler"],
});

const withDescriptionCmd =
  (description: string) =>
  <A, E, R>(self: Command<A, E, R>): Command<A, E, R> => ({
    ...self,
    description,
  });

type AOf<C> = C extends Command<infer a, infer _e, infer _r> ? a : never;
type EOf<C> = C extends Command<infer _a, infer e, infer _r> ? e : never;
type ROf<C> = C extends Command<infer _a, infer _e, infer r> ? r : never;

const withSubcommands =
  <const S extends ReadonlyArray<AnyCommand>>(subcommands: S) =>
  <A, E, R>(
    self: Command<A, E, R>,
  ): Command<A | AOf<S[number]>, E | EOf<S[number]>, R | ROf<S[number]>> => ({
    ...self,
    subcommands,
  });

// ---------------------------------------------------------------------------
// Parse outcomes
// ---------------------------------------------------------------------------

export interface CliConfig {
  readonly name: string;
  readonly version: string;
}

export type Outcome =
  | { readonly _tag: "help"; readonly text: string }
  | { readonly _tag: "version"; readonly text: string }
  | { readonly _tag: "error"; readonly message: string }
  | {
      readonly _tag: "execute";
      // biome-ignore lint/suspicious/noExplicitAny: erased at the dispatch layer
      readonly run: () => Effect.Effect<any, any, any>;
    };

// ---------------------------------------------------------------------------
// Help text
// ---------------------------------------------------------------------------

function optionPlaceholder(spec: AnyOptionSpec): string {
  switch (spec.kind) {
    case "boolean":
      return "";
    case "choice":
      return ` <${(spec.choices ?? []).join("|")}>`;
    case "integer":
      return " <integer>";
    case "float":
      return " <float>";
    default:
      return " <text>";
  }
}

function optionAnnotation(spec: AnyOptionSpec): string {
  if (spec.isOptional) return "(optional)";
  if (spec.hasDefault) return `(default: ${String(spec.defaultValue)})`;
  if (spec.kind === "boolean") return "";
  return "(required)";
}

interface RenderedOptionLine {
  readonly flags: string;
  readonly detail: string;
}

function renderOptionLine(spec: AnyOptionSpec): RenderedOptionLine {
  const alias = spec.alias !== undefined ? `-${spec.alias}, ` : "";
  const flags = `${alias}--${spec.name}${optionPlaceholder(spec)}`;
  const annotation = optionAnnotation(spec);
  const detail =
    spec.description.length > 0
      ? annotation.length > 0
        ? `${spec.description} ${annotation}`
        : spec.description
      : annotation;
  return { flags, detail };
}

function subcommandUsage(sub: AnyCommand): string {
  const parts = [sub.name];
  for (const [, spec] of sub.options) {
    const body = `--${spec.name}${optionPlaceholder(spec)}`;
    if (spec.isOptional || spec.hasDefault || spec.kind === "boolean") {
      parts.push(`[${body}]`);
    } else {
      parts.push(body);
    }
  }
  return parts.join(" ");
}

export function helpText(
  command: AnyCommand,
  path: ReadonlyArray<AnyCommand>,
  config: CliConfig,
): string {
  const lines: string[] = [];
  const fullPath = path.map((c) => c.name).join(" ");

  lines.push(`${config.name} ${config.version}`);
  lines.push("");
  lines.push("USAGE");
  lines.push(
    command.subcommands.length > 0
      ? `  $ ${fullPath} <command>`
      : `  $ ${fullPath}${command.options.length > 0 ? " [options]" : ""}`,
  );

  if (command.description.length > 0) {
    lines.push("");
    lines.push("DESCRIPTION");
    lines.push(`  ${command.description}`);
  }

  if (command.options.length > 0) {
    lines.push("");
    lines.push("OPTIONS");
    const rendered = command.options.map(([, spec]) => renderOptionLine(spec));
    const width = Math.max(...rendered.map((r) => r.flags.length));
    for (const r of rendered) {
      lines.push(`  ${r.flags.padEnd(width)}  ${r.detail}`.trimEnd());
    }
  }

  if (command.subcommands.length > 0) {
    lines.push("");
    lines.push("COMMANDS");
    const rows = command.subcommands.map(
      (sub) => [subcommandUsage(sub), sub.description] as const,
    );
    const width = Math.max(...rows.map(([usage]) => usage.length));
    for (const [usage, desc] of rows) {
      lines.push(`  ${usage.padEnd(width)}  ${desc}`.trimEnd());
    }
  }

  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Option parsing
// ---------------------------------------------------------------------------

interface ParsedFlag {
  readonly key: string;
  readonly spec: AnyOptionSpec;
  readonly raw: string | boolean;
}

type ParseResult =
  | { readonly _tag: "ok"; readonly values: Record<string, ParsedOptionValue> }
  | { readonly _tag: "err"; readonly message: string };

/** Name and alias lookups for a command's options. */
interface FlagLookup {
  readonly byName: ReadonlyMap<string, readonly [string, AnyOptionSpec]>;
  readonly byAlias: ReadonlyMap<string, readonly [string, AnyOptionSpec]>;
}

function flagLookup(command: AnyCommand): FlagLookup {
  const byName = new Map<string, readonly [string, AnyOptionSpec]>();
  const byAlias = new Map<string, readonly [string, AnyOptionSpec]>();
  for (const [key, spec] of command.options) {
    byName.set(spec.name, [key, spec]);
    if (spec.alias !== undefined) byAlias.set(spec.alias, [key, spec]);
  }
  return { byName, byAlias };
}

function parseBoolean(raw: string): boolean | undefined {
  if (raw === "true") return true;
  if (raw === "false") return false;
  return undefined;
}

type FlagTokenResult =
  | { readonly _tag: "err"; readonly message: string }
  | { readonly _tag: "ok"; readonly advance: number };

/**
 * Parse one `--name` / `-a` token (with optional `=value`) into `flags`.
 * `prefix` reproduces the long/short error-message display exactly.
 */
function parseNamedFlag(
  command: AnyCommand,
  lookup: ReadonlyMap<string, readonly [string, AnyOptionSpec]>,
  token: string,
  prefix: "--" | "-",
  tokens: ReadonlyArray<string>,
  index: number,
  flags: ParsedFlag[],
): FlagTokenResult {
  const body = token.slice(prefix.length);
  const eq = body.indexOf("=");
  const bare = eq >= 0 ? body.slice(0, eq) : body;
  const inline = eq >= 0 ? body.slice(eq + 1) : undefined;
  const entry = lookup.get(bare);
  if (entry === undefined) {
    return {
      _tag: "err",
      message: `Unknown option '${prefix}${bare}' for command '${command.name}'`,
    };
  }
  const [key, spec] = entry;
  const display = `${prefix}${bare}`;
  if (spec.kind === "boolean") {
    return parseBooleanFlag(key, spec, inline, display, flags);
  }
  return parseValuedFlag(key, spec, inline, display, tokens, index, flags);
}

/** Boolean flags are bare (`--flag`) or take an inline `=true|false` value. */
function parseBooleanFlag(
  key: string,
  spec: AnyOptionSpec,
  inline: string | undefined,
  display: string,
  flags: ParsedFlag[],
): FlagTokenResult {
  if (inline === undefined) {
    flags.push({ key, spec, raw: true });
    return { _tag: "ok", advance: 1 };
  }
  const parsed = parseBoolean(inline);
  if (parsed === undefined) {
    return {
      _tag: "err",
      message: `Invalid value '${inline}' for option '${display}': expected 'true' or 'false'`,
    };
  }
  flags.push({ key, spec, raw: parsed });
  return { _tag: "ok", advance: 1 };
}

/** Value flags take an inline `=value` or consume the next token. */
function parseValuedFlag(
  key: string,
  spec: AnyOptionSpec,
  inline: string | undefined,
  display: string,
  tokens: ReadonlyArray<string>,
  index: number,
  flags: ParsedFlag[],
): FlagTokenResult {
  if (inline !== undefined) {
    flags.push({ key, spec, raw: inline });
    return { _tag: "ok", advance: 1 };
  }
  if (index + 1 >= tokens.length) {
    return { _tag: "err", message: `Missing value for option '${display}'` };
  }
  flags.push({ key, spec, raw: tokens[index + 1] });
  return { _tag: "ok", advance: 2 };
}

/** Value for an option that was never provided (or the required-option error). */
function applyMissing(
  command: AnyCommand,
  key: string,
  spec: AnyOptionSpec,
  values: Record<string, ParsedOptionValue>,
): ParseResult {
  if (spec.isOptional) {
    values[key] = Option.none();
    return { _tag: "ok", values };
  }
  if (spec.hasDefault) {
    values[key] = spec.defaultValue as ParsedScalar;
    return { _tag: "ok", values };
  }
  if (spec.kind === "boolean") {
    values[key] = false;
    return { _tag: "ok", values };
  }
  return {
    _tag: "err",
    message: `Missing required option '--${spec.name}' for command '${command.name}'`,
  };
}

function wrapOptional(
  spec: AnyOptionSpec,
  value: ParsedScalar,
): ParsedOptionValue {
  return spec.isOptional ? Option.some(value) : value;
}

type ScalarResult =
  | { readonly _tag: "ok"; readonly value: ParsedOptionValue }
  | { readonly _tag: "err"; readonly message: string };

/** Validate + convert one provided string value by option kind. */
function convertScalar(spec: AnyOptionSpec, raw: string): ScalarResult {
  switch (spec.kind) {
    case "integer": {
      if (!/^[+-]?\d+$/.test(raw)) {
        return {
          _tag: "err",
          message: `Invalid value '${raw}' for option '--${spec.name}': expected an integer`,
        };
      }
      return {
        _tag: "ok",
        value: wrapOptional(spec, Number.parseInt(raw, 10)),
      };
    }
    case "float": {
      const n = Number(raw);
      if (Number.isNaN(n)) {
        return {
          _tag: "err",
          message: `Invalid value '${raw}' for option '--${spec.name}': expected a number`,
        };
      }
      return { _tag: "ok", value: wrapOptional(spec, n) };
    }
    case "choice": {
      const choices = spec.choices ?? [];
      if (!choices.includes(raw)) {
        return {
          _tag: "err",
          message: `Invalid value '${raw}' for option '--${spec.name}': expected one of ${choices.join(", ")}`,
        };
      }
      return { _tag: "ok", value: wrapOptional(spec, raw) };
    }
    default:
      // text (boolean never reaches here with a string value: boolean flags
      // are parsed to real booleans during the token walk above).
      return { _tag: "ok", value: wrapOptional(spec, raw) };
  }
}

function parseCommandOptions(
  command: AnyCommand,
  tokens: ReadonlyArray<string>,
): ParseResult {
  const { byName, byAlias } = flagLookup(command);
  const flags: ParsedFlag[] = [];
  let i = 0;

  while (i < tokens.length) {
    const token = tokens[i];

    if (token.startsWith("--") || (token.startsWith("-") && token.length > 1)) {
      const isLong = token.startsWith("--");
      const result = parseNamedFlag(
        command,
        isLong ? byName : byAlias,
        token,
        isLong ? "--" : "-",
        tokens,
        i,
        flags,
      );
      if (result._tag === "err") return result;
      i += result.advance;
      continue;
    }

    // Positional token.
    if (command.subcommands.length > 0) {
      const names = command.subcommands.map((s) => `'${s.name}'`).join(", ");
      return {
        _tag: "err",
        message: `Invalid subcommand '${token}' for ${command.name} - use one of ${names}`,
      };
    }
    return {
      _tag: "err",
      message: `Received unknown argument: '${token}'`,
    };
  }

  const values: Record<string, ParsedOptionValue> = {};
  const provided = new Map<string, string | boolean>();
  for (const flag of flags) provided.set(flag.key, flag.raw);

  for (const [key, spec] of command.options) {
    const raw = provided.get(key);
    if (raw === undefined) {
      const missing = applyMissing(command, key, spec, values);
      if (missing._tag === "err") return missing;
      continue;
    }
    if (raw === true || raw === false) {
      values[key] = spec.isOptional ? Option.some(raw) : raw;
      continue;
    }
    const converted = convertScalar(spec, raw);
    if (converted._tag === "err") {
      return { _tag: "err", message: converted.message };
    }
    values[key] = converted.value;
  }

  return { _tag: "ok", values };
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

/**
 * Index of every option name/alias across the whole command tree, used to
 * decide a flag's arity (does it consume a value?) during the token walk,
 * before the flag has been attributed to the command that defines it.
 * Root-first, depth-first; first registration wins.
 */
function collectTreeFlags(
  command: AnyCommand,
  acc: Map<string, AnyOptionSpec>,
): void {
  for (const [, spec] of command.options) {
    if (!acc.has(spec.name)) acc.set(spec.name, spec);
    if (spec.alias !== undefined && !acc.has(spec.alias)) {
      acc.set(spec.alias, spec);
    }
  }
  for (const sub of command.subcommands) collectTreeFlags(sub, acc);
}

/** First command on the resolved path that defines the flag (name or alias). */
function flagOwner(
  path: ReadonlyArray<AnyCommand>,
  bare: string,
  isAlias: boolean,
): AnyCommand | undefined {
  for (const command of path) {
    const { byName, byAlias } = flagLookup(command);
    if (isAlias ? byAlias.has(bare) : byName.has(bare)) return command;
  }
  return undefined;
}

/** True for `--flag` and `-f` tokens (but not a bare `-`). */
function isFlagToken(token: string): boolean {
  return token.startsWith("--") || (token.startsWith("-") && token.length > 1);
}

interface ScannedFlag {
  readonly bare: string;
  readonly isAlias: boolean;
  readonly token: string;
  readonly valueToken?: string;
}

/**
 * Decompose one flag token. Unknown flags still consume a following non-dash
 * token so it is not mistaken for a subcommand/positional; they error at
 * attribution below.
 */
function scanFlagToken(
  argv: ReadonlyArray<string>,
  index: number,
  treeFlags: ReadonlyMap<string, AnyOptionSpec>,
): ScannedFlag {
  const token = argv[index];
  const isAlias = !token.startsWith("--");
  const body = token.slice(isAlias ? 1 : 2);
  const eq = body.indexOf("=");
  const bare = eq >= 0 ? body.slice(0, eq) : body;
  const inline = eq >= 0 ? body.slice(eq + 1) : undefined;
  const spec = treeFlags.get(bare);
  const takesValue =
    inline === undefined &&
    (spec === undefined
      ? index + 1 < argv.length && !argv[index + 1].startsWith("-")
      : spec.kind !== "boolean");
  const valueToken =
    takesValue && index + 1 < argv.length ? argv[index + 1] : undefined;
  return { bare, isAlias, token, valueToken };
}

function findSubcommand(
  command: AnyCommand,
  token: string,
): AnyCommand | undefined {
  if (command.subcommands.length === 0) return undefined;
  return command.subcommands.find((s) => s.name === token);
}

function badPositionalMessage(bad: {
  readonly token: string;
  readonly at: AnyCommand;
}): string {
  const { token, at } = bad;
  return at.subcommands.length > 0
    ? `Invalid subcommand '${token}' for ${at.name} - use one of ${at.subcommands
        .map((s) => `'${s.name}'`)
        .join(", ")}`
    : `Received unknown argument: '${token}'`;
}

type FlagAttribution =
  | { readonly _tag: "ok"; readonly buckets: ReadonlyMap<AnyCommand, string[]> }
  | { readonly _tag: "err"; readonly message: string };

/** Bucket each flag's tokens under the first path command that defines it. */
function attributeFlags(
  flags: ReadonlyArray<ScannedFlag>,
  path: ReadonlyArray<AnyCommand>,
  current: AnyCommand,
  config: CliConfig,
): FlagAttribution {
  const buckets = new Map<AnyCommand, string[]>();
  for (const flag of flags) {
    const owner = flagOwner(path, flag.bare, flag.isAlias);
    if (owner === undefined) {
      const help = helpText(current, path, config);
      return {
        _tag: "err",
        message: `Unknown option '--${flag.bare}' for command '${current.name}'\n\n${help}`,
      };
    }
    let bucket = buckets.get(owner);
    if (bucket === undefined) {
      bucket = [];
      buckets.set(owner, bucket);
    }
    bucket.push(flag.token);
    if (flag.valueToken !== undefined) bucket.push(flag.valueToken);
  }
  return { _tag: "ok", buckets };
}

/**
 * Pure evaluation of argv against a command tree. Descends subcommands,
 * parses options at every level, and returns a discriminated outcome.
 * The handler is captured in a closure — nothing is executed here.
 *
 * Flags parse regardless of position relative to the subcommand:
 * `cmd --bars 5 replay` and `cmd replay --bars 5` are equivalent. Each flag
 * is attributed to the first command on the resolved path that defines it,
 * so a flag that precedes the subcommand still reaches the command it
 * belongs to instead of being rejected as unknown at the parent level.
 */
export function evaluate(
  root: AnyCommand,
  argv: ReadonlyArray<string>,
  config: CliConfig,
): Outcome {
  if (argv.includes("--version")) {
    return { _tag: "version", text: config.version };
  }

  const treeFlags = new Map<string, AnyOptionSpec>();
  collectTreeFlags(root, treeFlags);

  // Descend the subcommand tree, collecting flags wherever they appear.
  // Non-boolean flags consume the next token as their value even if it
  // looks like a flag (e.g. `--fee -1.5`), so values never become
  // positional tokens and never collide with subcommand names.
  const path: AnyCommand[] = [root];
  let current = root;
  let helpRequested = false;
  const flags: ScannedFlag[] = [];
  let firstBadPositional:
    | { readonly token: string; readonly at: AnyCommand }
    | undefined;
  let i = 0;
  while (i < argv.length) {
    const token = argv[i];
    if (token === "--help" || token === "-h") {
      helpRequested = true;
      i += 1;
      continue;
    }
    if (isFlagToken(token)) {
      const flag = scanFlagToken(argv, i, treeFlags);
      flags.push(flag);
      i += flag.valueToken !== undefined ? 2 : 1;
      continue;
    }
    // Positional token: a subcommand name descends; anything else is a
    // stray argument (recorded, reported only when no --help was given).
    const sub = findSubcommand(current, token);
    if (sub !== undefined) {
      path.push(sub);
      current = sub;
      i += 1;
      continue;
    }
    if (firstBadPositional === undefined) {
      firstBadPositional = { token, at: current };
    }
    i += 1;
  }

  if (helpRequested) {
    return { _tag: "help", text: helpText(current, path, config) };
  }

  if (firstBadPositional !== undefined) {
    const { at } = firstBadPositional;
    const message = badPositionalMessage(firstBadPositional);
    const help = helpText(at, path.slice(0, path.indexOf(at) + 1), config);
    return { _tag: "error", message: `${message}\n\n${help}` };
  }

  // Attribute each flag to the first command on the path that defines it,
  // rebuilding that command's token stream in original order so
  // parseCommandOptions sees exactly what it saw when the flag followed
  // the subcommand.
  const attributed = attributeFlags(flags, path, current, config);
  if (attributed._tag === "err") {
    return { _tag: "error", message: attributed.message };
  }

  // Parse options from the root down so parent-level errors surface first.
  let leafValues: Record<string, ParsedOptionValue> = {};
  for (const command of path) {
    const parsed = parseCommandOptions(
      command,
      attributed.buckets.get(command) ?? [],
    );
    if (parsed._tag === "err") {
      const help = helpText(
        command,
        path.slice(0, path.indexOf(command) + 1),
        config,
      );
      return { _tag: "error", message: `${parsed.message}\n\n${help}` };
    }
    leafValues = parsed.values;
  }

  const leaf = current;
  return {
    _tag: "execute",
    run: () => leaf.handler(leafValues),
  };
}

/**
 * Evaluate + perform the outcome as an Effect. Help/version print to stdout;
 * parse errors print to stderr and set `process.exitCode = 1` (the process
 * still exits 1 because the returned effect succeeds — see the NodeRuntime
 * teardown, which only forces an exit on failure).
 */
export function runEffect<A, E, R>(
  root: Command<A, E, R>,
  argv: ReadonlyArray<string>,
  config: CliConfig,
): Effect.Effect<void, E, R> {
  return Effect.suspend(() => {
    const outcome = evaluate(root as AnyCommand, argv, config);
    switch (outcome._tag) {
      case "help":
      case "version":
        return Console.log(outcome.text);
      case "error":
        return Console.error(outcome.message).pipe(
          Effect.andThen(
            Effect.sync(() => {
              process.exitCode = 1;
            }),
          ),
        );
      case "execute":
        return Effect.map(outcome.run() as Effect.Effect<A, E, R>, () => {});
    }
  });
}

/**
 * `Command.run` — mirrors the `@effect/cli` entry point: drops argv[0..1]
 * (runtime + script) and returns an Effect of the handler's value. Parse
 * errors print to stderr and fail the effect.
 */
const run =
  <A, E, R>(root: Command<A, E, R>, config: CliConfig) =>
  (args: ReadonlyArray<string>): Effect.Effect<A, E, R> =>
    Effect.suspend(() => {
      const outcome = evaluate(root as AnyCommand, args.slice(2), config);
      switch (outcome._tag) {
        case "help":
        case "version":
          return Console.log(outcome.text) as Effect.Effect<
            never,
            never,
            never
          >;
        case "error":
          return Console.error(outcome.message).pipe(
            Effect.andThen(Effect.fail(new Error(outcome.message))),
          ) as Effect.Effect<never, never, never>;
        case "execute":
          return outcome.run() as Effect.Effect<A, E, R>;
      }
    });

export const Command = {
  make,
  withDescription: withDescriptionCmd,
  withSubcommands,
  run,
} as const;
