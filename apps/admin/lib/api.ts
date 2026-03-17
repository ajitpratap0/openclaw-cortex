import type {
  Memory,
  Entity,
  ListMemoriesResponse,
  CollectionStats,
  UpdateMemoryRequest,
  RememberRequest,
  SearchEntitiesResponse,
} from "./types";

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

function getBaseURL(): string {
  if (typeof window !== "undefined") {
    const stored = localStorage.getItem("cortex_api_url");
    if (stored) return stored.replace(/\/$/, "");
  }
  return (
    process.env.NEXT_PUBLIC_CORTEX_API_URL ?? "http://localhost:8080"
  ).replace(/\/$/, "");
}

function getToken(): string {
  if (typeof window !== "undefined") {
    const stored = localStorage.getItem("cortex_api_token");
    if (stored) return stored;
  }
  return process.env.NEXT_PUBLIC_CORTEX_API_TOKEN ?? "";
}

// ---------------------------------------------------------------------------
// Core fetch wrapper
// ---------------------------------------------------------------------------

export class APIError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message);
    this.name = "APIError";
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const baseURL = getBaseURL();
  const token = getToken();

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const errBody = (await res.json()) as { error?: string };
      if (typeof errBody.error === "string") message = errBody.error;
    } catch {
      // ignore — use status text as fallback
    }
    throw new APIError(res.status, message);
  }

  // DELETE /v1/memories/{id} returns { deleted: true } — callers that
  // use void can safely cast the result.
  const text = await res.text();
  return text ? (JSON.parse(text) as T) : (undefined as unknown as T);
}

// ---------------------------------------------------------------------------
// Typed client
// ---------------------------------------------------------------------------

export const cortex = {
  // Health — GET /healthz (no /v1/ prefix, no auth required)
  health(): Promise<{ status: string }> {
    return request<{ status: string }>("GET", "/healthz");
  },

  // Stats — GET /v1/stats
  stats(): Promise<CollectionStats> {
    return request<CollectionStats>("GET", "/v1/stats");
  },

  // Memories — GET /v1/memories with optional query params
  listMemories(params?: {
    type?: string;
    scope?: string;
    project?: string;
    tags?: string;   // comma-separated
    limit?: number;
    cursor?: string;
  }): Promise<ListMemoriesResponse> {
    const q = new URLSearchParams();
    if (params?.type)    q.set("type",    params.type);
    if (params?.scope)   q.set("scope",   params.scope);
    if (params?.project) q.set("project", params.project);
    if (params?.tags)    q.set("tags",    params.tags);
    if (params?.limit)   q.set("limit",   String(params.limit));
    if (params?.cursor)  q.set("cursor",  params.cursor);
    const qs = q.toString();
    return request<ListMemoriesResponse>(
      "GET",
      `/v1/memories${qs ? `?${qs}` : ""}`
    );
  },

  getMemory(id: string): Promise<Memory> {
    return request<Memory>("GET", `/v1/memories/${encodeURIComponent(id)}`);
  },

  // Server uses PUT (not PATCH) — see handleUpdate in internal/api/server.go
  updateMemory(id: string, body: UpdateMemoryRequest): Promise<Memory> {
    return request<Memory>(
      "PUT",
      `/v1/memories/${encodeURIComponent(id)}`,
      body
    );
  },

  deleteMemory(id: string): Promise<void> {
    return request<void>(
      "DELETE",
      `/v1/memories/${encodeURIComponent(id)}`
    );
  },

  rememberMemory(
    body: RememberRequest
  ): Promise<{ id: string; stored: boolean }> {
    return request<{ id: string; stored: boolean }>(
      "POST",
      "/v1/remember",
      body
    );
  },

  // Entities — GET /v1/entities?query=...&type=...&limit=...
  searchEntities(params?: {
    query?: string;
    type?: string;
    limit?: number;
  }): Promise<SearchEntitiesResponse> {
    const q = new URLSearchParams();
    if (params?.query) q.set("query", params.query);
    if (params?.type)  q.set("type",  params.type);
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return request<SearchEntitiesResponse>(
      "GET",
      `/v1/entities${qs ? `?${qs}` : ""}`
    );
  },

  // GET /v1/entities/{id}
  getEntity(id: string): Promise<Entity> {
    return request<Entity>(
      "GET",
      `/v1/entities/${encodeURIComponent(id)}`
    );
  },
};

// ---------------------------------------------------------------------------
// localStorage helpers used by the Settings page
// ---------------------------------------------------------------------------

export function saveSettings(apiURL: string, apiToken: string): void {
  if (typeof window === "undefined") return;
  localStorage.setItem("cortex_api_url",   apiURL);
  localStorage.setItem("cortex_api_token", apiToken);
}

export function loadSettings(): { apiURL: string; apiToken: string } {
  if (typeof window === "undefined") {
    return {
      apiURL:   process.env.NEXT_PUBLIC_CORTEX_API_URL   ?? "http://localhost:8080",
      apiToken: process.env.NEXT_PUBLIC_CORTEX_API_TOKEN ?? "",
    };
  }
  return {
    apiURL:
      localStorage.getItem("cortex_api_url") ??
      process.env.NEXT_PUBLIC_CORTEX_API_URL ??
      "http://localhost:8080",
    apiToken:
      localStorage.getItem("cortex_api_token") ??
      process.env.NEXT_PUBLIC_CORTEX_API_TOKEN ??
      "",
  };
}
