package tests

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
)

func TestFactExtractor_NewDoesNotPanic(t *testing.T) {
	// Verify that construction works with any inputs (including empty).
	fe := graph.NewFactExtractor("", "claude-3-haiku-20240307", nil)
	require.NotNil(t, fe)

	fe2 := graph.NewFactExtractor("sk-test", "claude-3-haiku-20240307", slog.Default())
	require.NotNil(t, fe2)
}

func TestFactExtractor_EmptyEntities(t *testing.T) {
	fe := graph.NewFactExtractor("", "claude-3-haiku-20240307", nil)

	// When no entity names are provided, extraction should short-circuit
	// and return nil, nil without calling the API.
	facts, err := fe.Extract(context.Background(), "Alice works at Acme Corp.", nil)
	assert.NoError(t, err)
	assert.Nil(t, facts)

	facts2, err2 := fe.Extract(context.Background(), "Alice works at Acme Corp.", []string{})
	assert.NoError(t, err2)
	assert.Nil(t, facts2)
}

func TestFactExtractor_NilOnAPIError(t *testing.T) {
	// With an empty API key the Claude call will fail. The extractor should
	// log a warning and return (nil, nil) rather than propagating an error.
	fe := graph.NewFactExtractor("", "claude-3-haiku-20240307", nil)

	facts, err := fe.Extract(context.Background(), "Bob depends on the Auth Service.", []string{"Bob", "Auth Service"})
	assert.NoError(t, err)
	assert.Nil(t, facts)
}
