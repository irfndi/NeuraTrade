// Type declarations for @sentry/bun
// This module extends @sentry/node with Bun-specific functionality

declare module "@sentry/bun" {
  import type { NodeOptions, NodeClient } from "@sentry/node";
  import type {
    Integration,
    Options,
    ScopeContext,
    SeverityLevel,
    User,
    Breadcrumb,
    Event,
    EventHint,
    Scope,
  } from "@sentry/types";

  export interface BunOptions extends NodeOptions {
    dsn?: string;
    environment?: string;
    release?: string;
    tracesSampleRate?: number;
    attachStacktrace?: boolean;
    integrations?:
      | Integration[]
      | ((integrations: Integration[]) => Integration[]);
    beforeSend?: (
      event: Event,
      hint?: EventHint,
    ) => Event | null | Promise<Event | null>;
  }

  export function init(options: BunOptions): NodeClient | undefined;
  export function captureException(
    exception: unknown,
    captureContext?: Partial<ScopeContext>,
  ): string;
  export function captureMessage(
    message: string,
    captureContext?: Partial<ScopeContext> | SeverityLevel,
  ): string;
  export function addBreadcrumb(breadcrumb: Breadcrumb): void;
  export function setUser(user: User | null): void;
  export function setTag(key: string, value: string): void;
  export function setExtra(key: string, value: unknown): void;
  export function withScope<T>(callback: (scope: Scope) => T): T;
  export function flush(timeout?: number): Promise<boolean>;
  export function close(timeout?: number): Promise<boolean>;

  export interface SpanOptions {
    name: string;
    op?: string;
    attributes?: Record<string, string | number | boolean>;
  }

  export function startSpan<T>(
    options: SpanOptions,
    callback: () => T | Promise<T>,
  ): T | Promise<T>;

  export * from "@sentry/node";
}
