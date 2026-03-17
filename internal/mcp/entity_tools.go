package mcp

import (
	"context"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// buildEntitySearchTool returns the MCP tool definition for entity search.
func buildEntitySearchTool() mcpgo.Tool {
	return mcpgo.NewTool("entity_search",
		mcpgo.WithDescription("Search for entities by name. Returns matching entities from the knowledge graph."),
		mcpgo.WithString("query",
			mcpgo.Required(),
			mcpgo.Description("Name or partial name to search for"),
		),
	)
}

// buildEntityGetTool returns the MCP tool definition for entity get.
func buildEntityGetTool() mcpgo.Tool {
	return mcpgo.NewTool("entity_get",
		mcpgo.WithDescription("Get a single entity by ID."),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The entity ID to retrieve"),
		),
	)
}

// handleEntitySearch searches for entities by name.
func (s *Server) handleEntitySearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}

	query := req.GetString("query", "")
	if strings.TrimSpace(query) == "" {
		return mcpgo.NewToolResultError("query is required and must not be empty"), nil
	}

	entities, searchErr := s.st.SearchEntities(ctx, query, "", 100)
	if searchErr != nil {
		return mcpgo.NewToolResultErrorf("entity search failed: %s", searchErr.Error()), nil
	}

	return toolResultJSON(entities)
}

// handleEntityGet retrieves a single entity by ID.
func (s *Server) handleEntityGet(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.st == nil {
		return mcpgo.NewToolResultError("store is unavailable"), nil
	}

	id := req.GetString("id", "")
	if strings.TrimSpace(id) == "" {
		return mcpgo.NewToolResultError("id is required and must not be empty"), nil
	}

	entity, getErr := s.st.GetEntity(ctx, id)
	if getErr != nil {
		return mcpgo.NewToolResultErrorf("entity not found: %s", getErr.Error()), nil
	}

	return toolResultJSON(entity)
}
