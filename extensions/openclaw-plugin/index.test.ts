import { describe, it, expect } from "vitest";
import { resolveEnv, parseStoreOutput } from "./index.ts";

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

describe("parseStoreOutput", () => {
  const uuid = "a1b2c3d4-e5f6-7890-abcd-ef1234567890";

  it('returns action "created" for "Stored memory <UUID> [type/scope]"', () => {
    const result = parseStoreOutput(`Stored memory ${uuid} [fact/permanent]`);
    expect(result).toEqual({ id: uuid, action: "created" });
  });

  it('returns action "created" and extracts UUID without the trailing [type/scope] annotation', () => {
    const result = parseStoreOutput(`Stored memory ${uuid} [rule/project]`);
    expect(result?.id).toBe(uuid);
    expect(result?.action).toBe("created");
  });

  it('returns action "updated" for dedup richer-content message', () => {
    const result = parseStoreOutput(
      `duplicate detected: updated existing memory ${uuid} with richer content (note: --tags/--confidence/--scope flags were not applied; use --skip-dedup to replace fully)`,
    );
    expect(result).toEqual({ id: uuid, action: "updated" });
  });

  it('returns action "updated" even when the trailing note is absent', () => {
    const result = parseStoreOutput(
      `duplicate detected: updated existing memory ${uuid} with richer content`,
    );
    expect(result).toEqual({ id: uuid, action: "updated" });
  });

  it('returns action "skipped" for dedup already-covers message', () => {
    const result = parseStoreOutput(
      `duplicate detected: memory ${uuid} already covers this content (skipped)`,
    );
    expect(result).toEqual({ id: uuid, action: "skipped" });
  });

  it("returns null for unrecognised output (true failure)", () => {
    expect(parseStoreOutput("")).toBeNull();
    expect(parseStoreOutput("some unexpected binary error")).toBeNull();
    expect(parseStoreOutput("stored memory abc123")).toBeNull(); // wrong case
  });

  it("does not match 'Stored memory' mid-line (anchored to start)", () => {
    // A line that has 'Stored memory' but NOT at the start should not match
    const result = parseStoreOutput(`prefix Stored memory ${uuid} [fact/permanent]`);
    expect(result).toBeNull();
  });
});
