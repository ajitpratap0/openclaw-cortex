import { describe, it, expect } from "vitest";
import { resolveEnv } from "./index.ts";

describe("resolveEnv", () => {
  const empty: Record<string, string | undefined> = {};

  it("sets gateway vars when both url+token given and env is empty", () => {
    const env = resolveEnv(empty, "http://127.0.0.1:18789", "tok", undefined);
    expect(env.OPENCLAW_GATEWAY_URL).toBe("http://127.0.0.1:18789");
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBe("tok");
    expect(env.ANTHROPIC_API_KEY).toBeUndefined();
  });

  it("does not overwrite pre-set gateway env vars (env wins over auto-wired config)", () => {
    const base = {
      OPENCLAW_GATEWAY_URL: "http://127.0.0.1:9999",
      OPENCLAW_GATEWAY_TOKEN: "existing-token",
    };
    const env = resolveEnv(base, "http://127.0.0.1:18789", "new-token", undefined);
    expect(env.OPENCLAW_GATEWAY_URL).toBe("http://127.0.0.1:9999");
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBe("existing-token");
  });

  it("leaves both gateway vars untouched when one is already pre-set (atomic pair guard)", () => {
    const base = { OPENCLAW_GATEWAY_TOKEN: "pre-set-token" };
    const env = resolveEnv(base, "http://127.0.0.1:18789", "new-token", undefined);
    // One var pre-set → treat as user's deliberate partial config; inject nothing.
    expect(env.OPENCLAW_GATEWAY_URL).toBeUndefined();
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBe("pre-set-token");
  });

  it("does not mix a pre-set URL with an auto-detected token from params", () => {
    const base = { OPENCLAW_GATEWAY_URL: "http://127.0.0.1:9999" };
    const env = resolveEnv(base, "http://127.0.0.1:18789", "new-token", undefined);
    // URL pre-set, no token → inject nothing (don't pair shell URL with plugin token).
    expect(env.OPENCLAW_GATEWAY_URL).toBe("http://127.0.0.1:9999");
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBeUndefined();
  });

  it("falls through to anthropicApiKey when only gatewayUrl given (no token)", () => {
    const env = resolveEnv(empty, "http://127.0.0.1:18789", undefined, "sk-ant-test");
    expect(env.OPENCLAW_GATEWAY_URL).toBeUndefined();
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-test");
  });

  it("falls through to anthropicApiKey when only gatewayToken given (no url)", () => {
    const env = resolveEnv(empty, undefined, "tok", "sk-ant-test");
    expect(env.OPENCLAW_GATEWAY_URL).toBeUndefined();
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBeUndefined();
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-test");
  });

  it("sets ANTHROPIC_API_KEY and explicit plugin config overrides ambient env", () => {
    const base = { ANTHROPIC_API_KEY: "old-key" };
    const env = resolveEnv(base, undefined, undefined, "sk-ant-new");
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-new");
  });

  it("leaves env unchanged when no credentials provided", () => {
    const base = { FOO: "bar" };
    const env = resolveEnv(base, undefined, undefined, undefined);
    expect(env).toEqual(base);
  });

  it("sets both gateway vars and anthropicApiKey when both provided (gateway wins at binary runtime)", () => {
    const env = resolveEnv(empty, "http://127.0.0.1:18789", "tok", "sk-ant-test");
    expect(env.OPENCLAW_GATEWAY_URL).toBe("http://127.0.0.1:18789");
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBe("tok");
    // anthropicApiKey is also written — provides a fallback if gateway becomes unavailable
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-test");
  });

  it("documents partial-gateway state when OPENCLAW_GATEWAY_URL is in base but params incomplete", () => {
    const base = { OPENCLAW_GATEWAY_URL: "http://127.0.0.1:9999" };
    const env = resolveEnv(base, undefined, undefined, "sk-ant-fallback");
    // Gateway URL from base is preserved (not stripped — user's shell choice)
    expect(env.OPENCLAW_GATEWAY_URL).toBe("http://127.0.0.1:9999");
    // No token from params (gateway incomplete) → not written
    expect(env.OPENCLAW_GATEWAY_TOKEN).toBeUndefined();
    // anthropicApiKey provides the active LLM path
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-fallback");
  });
});
