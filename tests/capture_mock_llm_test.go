package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// ---- helpers ---------------------------------------------------------------

func newCapturer(llm *mockLLMClient) *capture.ClaudeCapturer {
	return capture.NewCapturer(llm, "claude-haiku", slog.Default())
}

func newEntityExtractor(llm *mockLLMClient) *capture.EntityExtractor {
	return capture.NewEntityExtractor(llm, "claude-haiku", slog.Default())
}

// ---- ClaudeCapturer.Extract ------------------------------------------------

func TestClaudeCapturerExtract_Success(t *testing.T) {
	resp := `[{"content":"Go uses goroutines for concurrency","type":"fact","confidence":0.9,"tags":["go","concurrency"]}]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	mems, err := c.Extract(context.Background(), "Tell me about Go", "Go uses goroutines")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
	if mems[0].Type != models.MemoryTypeFact {
		t.Errorf("expected type fact, got %q", mems[0].Type)
	}
	if mems[0].Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", mems[0].Confidence)
	}
}

func TestClaudeCapturerExtract_LLMError(t *testing.T) {
	c := newCapturer(&mockLLMClient{Err: errors.New("api failure")})

	_, err := c.Extract(context.Background(), "user", "assistant")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClaudeCapturerExtract_InvalidJSON(t *testing.T) {
	c := newCapturer(&mockLLMClient{Resp: "not json at all"})

	_, err := c.Extract(context.Background(), "user", "assistant")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestClaudeCapturerExtract_LowConfidenceFiltered(t *testing.T) {
	// One memory above threshold, one below (0.3 < 0.5)
	resp := `[
		{"content":"high confidence fact","type":"fact","confidence":0.8,"tags":[]},
		{"content":"low confidence fact","type":"fact","confidence":0.3,"tags":[]}
	]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	mems, err := c.Extract(context.Background(), "u", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("expected 1 memory after low-confidence filter, got %d", len(mems))
	}
	if len(mems) > 0 && mems[0].Content != "high confidence fact" {
		t.Errorf("wrong memory kept: %q", mems[0].Content)
	}
}

func TestClaudeCapturerExtract_CodeFenceWrappedJSON(t *testing.T) {
	resp := "```json\n[{\"content\":\"fence test\",\"type\":\"rule\",\"confidence\":0.7,\"tags\":[]}]\n```"
	c := newCapturer(&mockLLMClient{Resp: resp})

	mems, err := c.Extract(context.Background(), "u", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
}

func TestClaudeCapturerExtract_XMLSpecialCharsInInput(t *testing.T) {
	// Inputs containing XML special characters must not break the prompt
	resp := `[{"content":"fact","type":"fact","confidence":0.9,"tags":[]}]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	userMsg := `<script>alert("xss")</script> & "quotes" 'single'`
	assistantMsg := `<b>bold</b> & more`

	mems, err := c.Extract(context.Background(), userMsg, assistantMsg)
	if err != nil {
		t.Fatalf("unexpected error with XML chars in input: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("expected 1 memory, got %d", len(mems))
	}
}

func TestClaudeCapturerExtract_EmptyResponse(t *testing.T) {
	c := newCapturer(&mockLLMClient{Resp: ""})

	_, err := c.Extract(context.Background(), "u", "a")
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}

func TestClaudeCapturerExtract_EmptyArrayResponse(t *testing.T) {
	c := newCapturer(&mockLLMClient{Resp: "[]"})

	mems, err := c.Extract(context.Background(), "u", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 0 {
		t.Errorf("expected 0 memories, got %d", len(mems))
	}
}

func TestClaudeCapturerExtract_WrappedFormat(t *testing.T) {
	// Some models return {"memories": [...]} rather than bare array
	resp := `{"memories":[{"content":"wrapped fact","type":"fact","confidence":0.75,"tags":["test"]}]}`
	c := newCapturer(&mockLLMClient{Resp: resp})

	mems, err := c.Extract(context.Background(), "u", "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
	if mems[0].Content != "wrapped fact" {
		t.Errorf("unexpected content: %q", mems[0].Content)
	}
}

// ---- ClaudeCapturer.ExtractWithContext -------------------------------------

func TestClaudeCapturerExtractWithContext_NoPriorTurns(t *testing.T) {
	resp := `[{"content":"no prior turns fact","type":"fact","confidence":0.85,"tags":[]}]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	// Empty priorTurns delegates to Extract
	mems, err := c.ExtractWithContext(context.Background(), "u", "a", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
}

func TestClaudeCapturerExtractWithContext_WithPriorTurns(t *testing.T) {
	resp := `[{"content":"context-aware fact","type":"fact","confidence":0.8,"tags":[]}]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	priorTurns := []capture.ConversationTurn{
		{Role: "user", Content: "What is Go?"},
		{Role: "assistant", Content: "Go is a statically typed language."},
	}

	mems, err := c.ExtractWithContext(context.Background(), "Tell me more", "Go has goroutines", priorTurns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
}

func TestClaudeCapturerExtractWithContext_XMLInPriorTurns(t *testing.T) {
	resp := `[{"content":"xml-safe fact","type":"fact","confidence":0.9,"tags":[]}]`
	c := newCapturer(&mockLLMClient{Resp: resp})

	priorTurns := []capture.ConversationTurn{
		{Role: "user", Content: `<injection> & "quoted" 'single'`},
		{Role: "assistant", Content: `<b>response</b>`},
	}

	mems, err := c.ExtractWithContext(context.Background(), "current user", "current assistant", priorTurns)
	if err != nil {
		t.Fatalf("unexpected error with XML in prior turns: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("expected 1 memory, got %d", len(mems))
	}
}

func TestClaudeCapturerExtractWithContext_LLMError(t *testing.T) {
	c := newCapturer(&mockLLMClient{Err: errors.New("llm down")})

	priorTurns := []capture.ConversationTurn{
		{Role: "user", Content: "hello"},
	}

	_, err := c.ExtractWithContext(context.Background(), "u", "a", priorTurns)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---- EntityExtractor -------------------------------------------------------

func TestEntityExtractorExtract_Success(t *testing.T) {
	resp := `[{"name":"Alice","type":"person","aliases":["Al"],"description":"A software engineer"}]`
	e := newEntityExtractor(&mockLLMClient{Resp: resp})

	entities, err := e.Extract(context.Background(), "Alice is a software engineer.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Type != models.EntityTypePerson {
		t.Errorf("expected type person, got %q", entities[0].Type)
	}
	if entities[0].Name != "Alice" {
		t.Errorf("expected name Alice, got %q", entities[0].Name)
	}
	if entities[0].ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestEntityExtractorExtract_LLMErrorDegraces(t *testing.T) {
	e := newEntityExtractor(&mockLLMClient{Err: errors.New("api error")})

	// On LLM error, EntityExtractor must return nil, nil (graceful degradation)
	entities, err := e.Extract(context.Background(), "some content")
	if err != nil {
		t.Fatalf("expected graceful degradation (nil error), got: %v", err)
	}
	if entities != nil {
		t.Errorf("expected nil entities on LLM error, got %v", entities)
	}
}

func TestEntityExtractorExtract_EmptyResponseDegrades(t *testing.T) {
	e := newEntityExtractor(&mockLLMClient{Resp: ""})

	entities, err := e.Extract(context.Background(), "some content")
	if err != nil {
		t.Fatalf("expected graceful degradation (nil error), got: %v", err)
	}
	if entities != nil {
		t.Errorf("expected nil entities on empty response, got %v", entities)
	}
}

func TestEntityExtractorExtract_InvalidEntityTypeDefaultsToConcept(t *testing.T) {
	resp := `[{"name":"Kubernetes","type":"unknown_type","aliases":[],"description":"Container orchestration"}]`
	e := newEntityExtractor(&mockLLMClient{Resp: resp})

	entities, err := e.Extract(context.Background(), "We use Kubernetes.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Type != models.EntityTypeConcept {
		t.Errorf("expected type concept (fallback), got %q", entities[0].Type)
	}
}

func TestEntityExtractorExtract_InvalidJSON(t *testing.T) {
	e := newEntityExtractor(&mockLLMClient{Resp: "not valid json"})

	_, err := e.Extract(context.Background(), "some content")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestEntityExtractorExtract_EmptyArrayResponse(t *testing.T) {
	e := newEntityExtractor(&mockLLMClient{Resp: "[]"})

	entities, err := e.Extract(context.Background(), "no notable entities here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestEntityExtractorExtract_MultipleEntities(t *testing.T) {
	resp := `[
		{"name":"Alice","type":"person","aliases":[],"description":"Engineer"},
		{"name":"ProjectX","type":"project","aliases":["PX"],"description":"Main project"}
	]`
	e := newEntityExtractor(&mockLLMClient{Resp: resp})

	entities, err := e.Extract(context.Background(), "Alice works on ProjectX.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	// Verify distinct IDs
	if entities[0].ID == entities[1].ID {
		t.Error("expected distinct IDs for different entities")
	}
}

func TestEntityExtractorExtract_CodeFenceWrapped(t *testing.T) {
	resp := "```json\n[{\"name\":\"Bob\",\"type\":\"person\",\"aliases\":[],\"description\":\"A developer\"}]\n```"
	e := newEntityExtractor(&mockLLMClient{Resp: resp})

	entities, err := e.Extract(context.Background(), "Bob is a developer.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	if entities[0].Name != "Bob" {
		t.Errorf("expected name Bob, got %q", entities[0].Name)
	}
}
