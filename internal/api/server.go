package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

// Server is an HTTP API server that exposes memory operations.
type Server struct {
	store     store.Store
	recall    *recall.Recaller
	embedder  embedder.Embedder
	logger    *slog.Logger
	authToken string // empty = no auth required
}

// NewServer creates a new Server with the given dependencies.
func NewServer(st store.Store, rec *recall.Recaller, emb embedder.Embedder, logger *slog.Logger, authToken string) *Server {
	return &Server{
		store:     st,
		recall:    rec,
		embedder:  emb,
		logger:    logger,
		authToken: authToken,
	}
}

// Handler returns an http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check — no auth required.
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Memory CRUD and search endpoints — wrapped with auth middleware.
	mux.HandleFunc("POST /v1/remember", s.auth(s.handleRemember))
	mux.HandleFunc("POST /v1/recall", s.auth(s.handleRecall))
	mux.HandleFunc("GET /v1/memories/{id}", s.auth(s.handleGetMemory))
	mux.HandleFunc("DELETE /v1/memories/{id}", s.auth(s.handleDeleteMemory))
	mux.HandleFunc("POST /v1/search", s.auth(s.handleSearch))
	mux.HandleFunc("GET /v1/stats", s.auth(s.handleStats))

	return mux
}

// --- middleware ---

// auth wraps a handler with Bearer token authentication when authToken is set.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			next(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
			s.writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// --- handlers ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// rememberRequest is the body accepted by POST /v1/remember.
type rememberRequest struct {
	Content    string             `json:"content"`
	Type       models.MemoryType  `json:"type"`
	Scope      models.MemoryScope `json:"scope"`
	Tags       []string           `json:"tags"`
	Project    string             `json:"project"`
	Confidence float64            `json:"confidence"`
}

// rememberResponse is returned by POST /v1/remember.
type rememberResponse struct {
	ID     string `json:"id"`
	Stored bool   `json:"stored"`
}

func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var req rememberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Content == "" {
		s.writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Type == "" {
		req.Type = models.MemoryTypeFact
	}
	if req.Scope == "" {
		req.Scope = models.ScopeSession
	}
	if req.Confidence == 0 {
		req.Confidence = 0.9
	}

	if !req.Type.IsValid() {
		s.writeError(w, http.StatusBadRequest, "invalid memory type")
		return
	}
	if !req.Scope.IsValid() {
		s.writeError(w, http.StatusBadRequest, "invalid memory scope")
		return
	}

	vec, err := s.embedder.Embed(r.Context(), req.Content)
	if err != nil {
		s.logger.Error("failed to embed memory", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to generate embedding")
		return
	}

	now := time.Now().UTC()
	mem := models.Memory{
		ID:           uuid.NewString(),
		Type:         req.Type,
		Scope:        req.Scope,
		Visibility:   models.VisibilityPrivate,
		Content:      req.Content,
		Confidence:   req.Confidence,
		Source:       "api",
		Tags:         req.Tags,
		Project:      req.Project,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}

	if err = s.store.Upsert(r.Context(), mem, vec); err != nil {
		s.logger.Error("failed to store memory", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to store memory")
		return
	}

	s.writeJSON(w, http.StatusOK, rememberResponse{ID: mem.ID, Stored: true})
}

// recallRequest is the body accepted by POST /v1/recall.
type recallRequest struct {
	Message string `json:"message"`
	Project string `json:"project"`
	Budget  int    `json:"budget"`
}

// recallResponse is returned by POST /v1/recall.
type recallResponse struct {
	Context     string `json:"context"`
	MemoryCount int    `json:"memory_count"`
	TokensUsed  int    `json:"tokens_used"`
}

func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var req recallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		s.writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.Budget <= 0 {
		req.Budget = 2000
	}

	vec, err := s.embedder.Embed(r.Context(), req.Message)
	if err != nil {
		s.logger.Error("failed to embed recall query", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to generate embedding")
		return
	}

	var filters *store.SearchFilters
	if req.Project != "" {
		proj := req.Project
		filters = &store.SearchFilters{Project: &proj}
	}

	results, err := s.store.Search(r.Context(), vec, 50, filters)
	if err != nil {
		s.logger.Error("failed to search store", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to search memories")
		return
	}

	ranked := s.recall.Rank(results, req.Project)

	var contents []string
	for i := range ranked {
		contents = append(contents, ranked[i].Memory.Content)
	}

	formattedCtx, count := tokenizer.FormatMemoriesWithBudget(contents, req.Budget)
	tokensUsed := tokenizer.EstimateTokens(formattedCtx)

	// Update access metadata for returned memories.
	for i := 0; i < count && i < len(ranked); i++ {
		if err := s.store.UpdateAccessMetadata(r.Context(), ranked[i].Memory.ID); err != nil {
			s.logger.Warn("handleRecall: UpdateAccessMetadata", "id", ranked[i].Memory.ID, "error", err)
		}
	}

	s.writeJSON(w, http.StatusOK, recallResponse{
		Context:     formattedCtx,
		MemoryCount: count,
		TokensUsed:  tokensUsed,
	})
}

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	mem, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "memory not found")
			return
		}
		s.logger.Error("failed to get memory", "id", id, "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to get memory")
		return
	}

	s.writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "memory not found")
			return
		}
		s.logger.Error("failed to delete memory", "id", id, "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to delete memory")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// searchRequest is the body accepted by POST /v1/search.
type searchRequest struct {
	Message string `json:"message"`
	Limit   int    `json:"limit"`
	Project string `json:"project"`
}

// searchResponse is returned by POST /v1/search.
type searchResponse struct {
	Results []models.SearchResult `json:"results"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		s.writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	vec, err := s.embedder.Embed(r.Context(), req.Message)
	if err != nil {
		s.logger.Error("failed to embed search query", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to generate embedding")
		return
	}

	var filters *store.SearchFilters
	if req.Project != "" {
		proj := req.Project
		filters = &store.SearchFilters{Project: &proj}
	}

	results, err := s.store.Search(r.Context(), vec, uint64(req.Limit), filters)
	if err != nil {
		s.logger.Error("failed to search store", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to search memories")
		return
	}

	s.writeJSON(w, http.StatusOK, searchResponse{Results: results})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.logger.Error("failed to get stats", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// --- helpers ---

// writeJSON encodes v as JSON and writes it to w with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if encErr := json.NewEncoder(w).Encode(v); encErr != nil {
		s.logger.Error("failed to encode response", "error", encErr)
	}
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// Shutdown gracefully shuts down an http.Server with the given timeout.
// This is a convenience helper used by the serve command.
func Shutdown(srv *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return srv.Shutdown(ctx)
}
