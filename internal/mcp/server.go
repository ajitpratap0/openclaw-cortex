// Package mcp implements the Model Context Protocol server for openclaw-cortex.
package mcp

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

const (
	// defaultRecallBudget is the default token budget for recall responses.
	defaultRecallBudget = 2000

	// defaultSearchLimit is the default number of results for search.
	defaultSearchLimit = 10

	// recallSearchLimit is how many vector results to fetch before re-ranking.
	recallSearchLimit = 50
)

// Server wraps an MCPServer with openclaw-cortex dependencies.
type Server struct {
	mcp      *mcpserver.MCPServer
	st       store.Store
	emb      embedder.Embedder
	recaller *recall.Recaller
	logger   *slog.Logger
}

// NewServer creates a new MCP server. If st or emb are nil,
// the corresponding tool calls will return an error response instead of panicking.
func NewServer(st store.Store, emb embedder.Embedder, logger *slog.Logger) *Server {
	s := &Server{
		st:       st,
		emb:      emb,
		recaller: recall.NewRecaller(recall.DefaultWeights(), logger),
		logger:   logger,
	}

	mcpSrv := mcpserver.NewMCPServer(
		"openclaw-cortex",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	mcpSrv.AddTool(buildRememberTool(), s.handleRemember)
	mcpSrv.AddTool(buildRecallTool(), s.handleRecall)
	mcpSrv.AddTool(buildForgetTool(), s.handleForget)
	mcpSrv.AddTool(buildSearchTool(), s.handleSearch)
	mcpSrv.AddTool(buildStatsTool(), s.handleStats)

	s.mcp = mcpSrv
	return s
}

// MCPServer returns the underlying mcp-go MCPServer for use with ServeStdio.
func (s *Server) MCPServer() *mcpserver.MCPServer {
	return s.mcp
}

// HandleRemember is the exported handler for the "remember" tool.
// It is exposed for direct testing without the mcp-go transport layer.
func (s *Server) HandleRemember(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.handleRemember(ctx, req)
}

// HandleRecall is the exported handler for the "recall" tool.
func (s *Server) HandleRecall(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.handleRecall(ctx, req)
}

// HandleForget is the exported handler for the "forget" tool.
func (s *Server) HandleForget(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.handleForget(ctx, req)
}

// HandleSearch is the exported handler for the "search" tool.
func (s *Server) HandleSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.handleSearch(ctx, req)
}

// HandleStats is the exported handler for the "stats" tool.
func (s *Server) HandleStats(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.handleStats(ctx, req)
}

// --- helpers ---

// xmlEscape replaces characters that have special meaning in XML to prevent
// prompt injection when embedding user content in XML-delimited templates.
func xmlEscape(s string) string {
	var buf strings.Builder
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

// toolResultJSON marshals v to JSON and returns it as a tool text result.
func toolResultJSON(v any) (*mcpgo.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshaling result: %w", err)
	}
	return mcpgo.NewToolResultText(string(b)), nil
}

// --- tool definitions ---

func buildRememberTool() mcpgo.Tool {
	return mcpgo.NewTool("remember",
		mcpgo.WithDescription("Store a memory in openclaw-cortex. Embeds the content and upserts it to the vector store."),
		mcpgo.WithString("content",
			mcpgo.Required(),
			mcpgo.Description("The text content to remember"),
		),
		mcpgo.WithString("type",
			mcpgo.Description("Memory type: rule, fact, episode, procedure, or preference (default: fact)"),
		),
		mcpgo.WithString("scope",
			mcpgo.Description("Memory scope: permanent, project, session, or ttl (default: permanent)"),
		),
		mcpgo.WithString("project",
			mcpgo.Description("Project name for project-scoped memories"),
		),
		mcpgo.WithNumber("confidence",
			mcpgo.Description("Confidence score 0.0-1.0 (default: 1.0)"),
		),
	)
}

func buildRecallTool() mcpgo.Tool {
	return mcpgo.NewTool("recall",
		mcpgo.WithDescription("Retrieve relevant memories using semantic search and multi-factor ranking."),
		mcpgo.WithString("message",
			mcpgo.Required(),
			mcpgo.Description("The query to recall memories for"),
		),
		mcpgo.WithString("project",
			mcpgo.Description("Project context for scope boosting"),
		),
		mcpgo.WithNumber("budget",
			mcpgo.Description("Token budget for returned context (default: 2000)"),
		),
	)
}

func buildForgetTool() mcpgo.Tool {
	return mcpgo.NewTool("forget",
		mcpgo.WithDescription("Delete a memory by ID."),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The ID of the memory to delete"),
		),
	)
}

func buildSearchTool() mcpgo.Tool {
	return mcpgo.NewTool("search",
		mcpgo.WithDescription("Semantic search over memories. Returns raw search results with similarity scores."),
		mcpgo.WithString("message",
			mcpgo.Required(),
			mcpgo.Description("The query to search for"),
		),
		mcpgo.WithNumber("limit",
			mcpgo.Description("Maximum number of results (default: 10)"),
		),
		mcpgo.WithString("project",
			mcpgo.Description("Filter results by project"),
		),
	)
}

func buildStatsTool() mcpgo.Tool {
	return mcpgo.NewTool("stats",
		mcpgo.WithDescription("Get collection statistics: total memories, breakdown by type and scope."),
	)
}

// --- tool handlers ---

// handleRemember embeds content and upserts a new memory.
func (s *Server) handleRemember(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}
	if s.emb == nil {
		return mcpgo.NewToolResultError("embedder is unavailable"), nil
	}

	content := req.GetString("content", "")
	if strings.TrimSpace(content) == "" {
		return mcpgo.NewToolResultError("content is required and must not be empty"), nil
	}

	memType := models.MemoryTypeFact
	if t := req.GetString("type", ""); t != "" {
		candidate := models.MemoryType(t)
		if !candidate.IsValid() {
			return mcpgo.NewToolResultErrorf("invalid type %q: must be one of rule, fact, episode, procedure, preference", t), nil
		}
		memType = candidate
	}

	memScope := models.ScopePermanent
	if sc := req.GetString("scope", ""); sc != "" {
		candidate := models.MemoryScope(sc)
		if !candidate.IsValid() {
			return mcpgo.NewToolResultErrorf("invalid scope %q: must be one of permanent, project, session, ttl", sc), nil
		}
		memScope = candidate
	}

	confidence := req.GetFloat("confidence", 1.0)
	if confidence < 0.0 || confidence > 1.0 {
		return mcpgo.NewToolResultError("confidence must be between 0.0 and 1.0"), nil
	}

	project := req.GetString("project", "")

	content = xmlEscape(content)

	vec, err := s.emb.Embed(ctx, content)
	if err != nil {
		return mcpgo.NewToolResultErrorf("embedding failed: %s", err.Error()), nil
	}

	now := time.Now().UTC()
	mem := models.Memory{
		ID:           uuid.New().String(),
		Type:         memType,
		Scope:        memScope,
		Visibility:   models.VisibilityPrivate,
		Content:      content,
		Confidence:   confidence,
		Source:       "mcp",
		Project:      project,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}

	if err := s.st.Upsert(ctx, mem, vec); err != nil {
		return mcpgo.NewToolResultErrorf("store upsert failed: %s", err.Error()), nil
	}

	s.logger.Info("mcp: remember stored memory", "id", mem.ID, "type", mem.Type, "scope", mem.Scope)

	result := map[string]any{
		"id":     mem.ID,
		"stored": true,
	}
	return toolResultJSON(result)
}

// handleRecall embeds the query, searches, re-ranks, and formats results within the token budget.
func (s *Server) handleRecall(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}
	if s.emb == nil {
		return mcpgo.NewToolResultError("embedder is unavailable"), nil
	}

	message := req.GetString("message", "")
	if strings.TrimSpace(message) == "" {
		return mcpgo.NewToolResultError("message is required and must not be empty"), nil
	}

	project := req.GetString("project", "")
	budget := req.GetInt("budget", defaultRecallBudget)
	if budget <= 0 {
		budget = defaultRecallBudget
	}

	vec, err := s.emb.Embed(ctx, message)
	if err != nil {
		return mcpgo.NewToolResultErrorf("embedding failed: %s", err.Error()), nil
	}

	var filters *store.SearchFilters
	if project != "" {
		filters = &store.SearchFilters{Project: &project}
	}

	results, err := s.st.Search(ctx, vec, recallSearchLimit, filters)
	if err != nil {
		return mcpgo.NewToolResultErrorf("search failed: %s", err.Error()), nil
	}

	ranked := s.recaller.Rank(results, project)

	var contents []string
	for i := range ranked {
		contents = append(contents, ranked[i].Memory.Content)
	}

	output, count := tokenizer.FormatMemoriesWithBudget(contents, budget)

	// Update access metadata for returned memories.
	for i := 0; i < count && i < len(ranked); i++ {
		if updateErr := s.st.UpdateAccessMetadata(ctx, ranked[i].Memory.ID); updateErr != nil {
			s.logger.Warn("mcp: recall: failed to update access metadata", "id", ranked[i].Memory.ID, "error", updateErr)
		}
	}

	result := map[string]any{
		"context":      output,
		"memory_count": count,
	}
	return toolResultJSON(result)
}

// handleForget deletes a memory by ID.
func (s *Server) handleForget(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}

	id := req.GetString("id", "")
	if strings.TrimSpace(id) == "" {
		return mcpgo.NewToolResultError("id is required and must not be empty"), nil
	}

	if err := s.st.Delete(ctx, id); err != nil {
		return mcpgo.NewToolResultErrorf("delete failed: %s", err.Error()), nil
	}

	s.logger.Info("mcp: forget deleted memory", "id", id)

	result := map[string]any{
		"deleted": true,
	}
	return toolResultJSON(result)
}

// handleSearch performs a raw semantic search and returns scored results.
func (s *Server) handleSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}
	if s.emb == nil {
		return mcpgo.NewToolResultError("embedder is unavailable"), nil
	}

	message := req.GetString("message", "")
	if strings.TrimSpace(message) == "" {
		return mcpgo.NewToolResultError("message is required and must not be empty"), nil
	}

	limit := req.GetInt("limit", defaultSearchLimit)
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	project := req.GetString("project", "")

	vec, err := s.emb.Embed(ctx, message)
	if err != nil {
		return mcpgo.NewToolResultErrorf("embedding failed: %s", err.Error()), nil
	}

	var filters *store.SearchFilters
	if project != "" {
		filters = &store.SearchFilters{Project: &project}
	}

	results, err := s.st.Search(ctx, vec, uint64(limit), filters) //nolint:gosec // limit validated above
	if err != nil {
		return mcpgo.NewToolResultErrorf("search failed: %s", err.Error()), nil
	}

	result := map[string]any{
		"results": results,
	}
	return toolResultJSON(result)
}

// handleStats returns collection statistics.
func (s *Server) handleStats(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}

	stats, err := s.st.Stats(ctx)
	if err != nil {
		return mcpgo.NewToolResultErrorf("stats failed: %s", err.Error()), nil
	}
	return toolResultJSON(stats)
}
