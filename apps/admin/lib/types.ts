// ---------------------------------------------------------------------------
// Memory types — mirrors internal/models/memory.go
// ---------------------------------------------------------------------------

export type MemoryType =
  | "rule"
  | "fact"
  | "episode"
  | "procedure"
  | "preference";

export type MemoryScope = "permanent" | "project" | "session" | "ttl";

export type ConflictStatus = "" | "active" | "resolved";

export type MemoryVisibility = "private" | "shared" | "sensitive";

export interface Memory {
  id: string;
  type: MemoryType;
  scope: MemoryScope;
  visibility: MemoryVisibility;
  content: string;
  confidence: number;
  source: string;
  tags: string[] | null;
  project?: string;
  user_id?: string;
  ttl_seconds?: number;
  created_at: string;      // RFC3339
  updated_at: string;
  last_accessed: string;
  access_count: number;
  reinforced_at?: string;
  reinforced_count?: number;
  metadata?: Record<string, unknown>;
  supersedes_id?: string;
  valid_until?: string;
  valid_from?: string;
  valid_to?: string | null;
  is_current_version?: boolean;
  conflict_group_id?: string;
  conflict_status?: ConflictStatus;
}

export interface SearchResult {
  memory: Memory;
  score: number;
}

// ---------------------------------------------------------------------------
// Entity types — mirrors internal/models/entity.go
// ---------------------------------------------------------------------------

export type EntityType =
  | "person"
  | "project"
  | "system"
  | "decision"
  | "concept";

export interface Entity {
  id: string;
  name: string;
  type: EntityType;
  aliases?: string[];
  memory_ids?: string[];
  created_at: string;
  updated_at: string;
  metadata?: Record<string, unknown>;
  project?: string;
  summary?: string;
  community_id?: string;
}

// ---------------------------------------------------------------------------
// API response shapes — mirrors internal/api/server.go
// ---------------------------------------------------------------------------

export interface ListMemoriesResponse {
  memories: Memory[];
  next_cursor: string;
}

export interface MemoryPreview {
  id: string;
  content: string;
  access_count: number;
}

export interface CollectionStats {
  total_memories: number;
  by_type: Record<string, number>;
  by_scope: Record<string, number>;
  oldest_memory?: string;
  newest_memory?: string;
  top_accessed?: MemoryPreview[];
  reinforcement_tiers?: Record<string, number>;
  active_conflicts: number;
  pending_ttl_expiry: number;
  storage_estimate_bytes: number;
}

export interface SearchEntitiesResponse {
  entities: Entity[];
}

// ---------------------------------------------------------------------------
// Request body shapes — mirrors internal/api/server.go updateRequest
// ---------------------------------------------------------------------------

export interface UpdateMemoryRequest {
  content?: string;
  type?: MemoryType;
  scope?: MemoryScope;
  tags?: string[];
  project?: string;
  confidence?: number;
}

export interface RememberRequest {
  content: string;
  type?: MemoryType;
  scope?: MemoryScope;
  tags?: string[];
  project?: string;
  confidence?: number;
}
