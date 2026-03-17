package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestEntitySearchEndpoint(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Bob", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e3", Name: "Acme", Type: models.EntityTypeProject})

	srv := api.NewServer(ms, nil, nil, nil, "test-token", "")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities?query=Alice", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Entities []models.Entity `json:"entities"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	if result.Entities[0].Name != "Alice" {
		t.Errorf("expected 'Alice', got %q", result.Entities[0].Name)
	}
}

func TestEntitySearchWithTypeFilter(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Alice Project", Type: models.EntityTypeProject})

	srv := api.NewServer(ms, nil, nil, nil, "test-token", "")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities?query=Alice&type=person", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result struct {
		Entities []models.Entity `json:"entities"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity (type-filtered), got %d", len(result.Entities))
	}
}

func TestEntityGetEndpoint(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})

	srv := api.NewServer(ms, nil, nil, nil, "test-token", "")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities/e1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entity models.Entity
	if err := json.NewDecoder(rec.Body).Decode(&entity); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assert.Equal(t, "Alice", entity.Name)
	assert.Equal(t, models.EntityTypePerson, entity.Type)
	assert.NotEmpty(t, entity.ID)
}

func TestEntityGetNotFound(t *testing.T) {
	ms := store.NewMockStore()
	srv := api.NewServer(ms, nil, nil, nil, "test-token", "")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
