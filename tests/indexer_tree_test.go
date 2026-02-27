package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
)

func TestParseMarkdownTree_EmptyDocument(t *testing.T) {
	nodes := indexer.ParseMarkdownTree("")
	assert.Empty(t, nodes)
}

func TestParseMarkdownTree_NoHeadings(t *testing.T) {
	nodes := indexer.ParseMarkdownTree("just some plain text\nwith no headings")
	// Content without headings produces no nodes (or an empty root).
	// The important invariant: no panic, result is consistent.
	_ = nodes
}

func TestParseMarkdownTree_SingleH1(t *testing.T) {
	nodes := indexer.ParseMarkdownTree("# Title\nSome body content here.\n")
	require.Len(t, nodes, 1)
	assert.Equal(t, "Title", nodes[0].Title)
	assert.Equal(t, 1, nodes[0].Depth)
	assert.Equal(t, "Title", nodes[0].Path)
	assert.Equal(t, "Some body content here.", nodes[0].Content)
	assert.Equal(t, 4, nodes[0].WordCount)
	assert.Empty(t, nodes[0].Children)
}

func TestParseMarkdownTree_MultipleRoots(t *testing.T) {
	content := "# First\nfirst body\n# Second\nsecond body\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 2)
	assert.Equal(t, "First", nodes[0].Title)
	assert.Equal(t, "Second", nodes[1].Title)
	assert.Equal(t, "first body", nodes[0].Content)
	assert.Equal(t, "second body", nodes[1].Content)
}

func TestParseMarkdownTree_NestedH2(t *testing.T) {
	content := "# Parent\nparent body\n## Child\nchild body\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 1)
	require.Len(t, nodes[0].Children, 1)

	child := nodes[0].Children[0]
	assert.Equal(t, "Child", child.Title)
	assert.Equal(t, 2, child.Depth)
	assert.Equal(t, "Parent / Child", child.Path)
	assert.Equal(t, "child body", child.Content)
}

func TestParseMarkdownTree_DeepNesting(t *testing.T) {
	content := "# H1\n## H2\n### H3\ndeep content here\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 1)
	require.Len(t, nodes[0].Children, 1)
	require.Len(t, nodes[0].Children[0].Children, 1)

	h3 := nodes[0].Children[0].Children[0]
	assert.Equal(t, "H3", h3.Title)
	assert.Equal(t, 3, h3.Depth)
	assert.Equal(t, "H1 / H2 / H3", h3.Path)
	assert.Contains(t, h3.Content, "deep content")
}

func TestParseMarkdownTree_SiblingH2s(t *testing.T) {
	content := "# Root\n## Alpha\nalpha content\n## Beta\nbeta content\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 1)
	require.Len(t, nodes[0].Children, 2)

	assert.Equal(t, "Alpha", nodes[0].Children[0].Title)
	assert.Equal(t, "Beta", nodes[0].Children[1].Title)
	assert.Equal(t, "Root / Alpha", nodes[0].Children[0].Path)
	assert.Equal(t, "Root / Beta", nodes[0].Children[1].Path)
}

func TestParseMarkdownTree_WordCount(t *testing.T) {
	content := "# Title\none two three four five six\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 1)
	assert.Equal(t, 6, nodes[0].WordCount)
}

func TestParseMarkdownTree_PathIsSlashDelimited(t *testing.T) {
	content := "# A\n## B\n### C\ncontent\n"
	nodes := indexer.ParseMarkdownTree(content)
	h3 := nodes[0].Children[0].Children[0]
	assert.True(t, strings.Contains(h3.Path, " / "), "path should use ' / ' delimiter: %q", h3.Path)
	assert.Equal(t, "A / B / C", h3.Path)
}

func TestParseMarkdownTree_ContentStripped(t *testing.T) {
	// Heading text should not appear in Content; only body lines should.
	content := "# MySection\nline one\nline two\n"
	nodes := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes, 1)
	assert.NotContains(t, nodes[0].Content, "# MySection")
	assert.Contains(t, nodes[0].Content, "line one")
	assert.Contains(t, nodes[0].Content, "line two")
}

func TestParseMarkdownTree_H4Support(t *testing.T) {
	content := "# A\n## B\n### C\n#### D\nd content\n"
	nodes := indexer.ParseMarkdownTree(content)
	h4 := nodes[0].Children[0].Children[0].Children[0]
	assert.Equal(t, "D", h4.Title)
	assert.Equal(t, 4, h4.Depth)
	assert.Equal(t, "A / B / C / D", h4.Path)
}

func TestParseMarkdownTree_MultipleFiles_Idempotent(t *testing.T) {
	// Calling ParseMarkdownTree twice on the same input should return the same structure.
	content := "# Title\nbody\n## Sub\nsub body\n"
	nodes1 := indexer.ParseMarkdownTree(content)
	nodes2 := indexer.ParseMarkdownTree(content)
	require.Len(t, nodes1, len(nodes2))
	assert.Equal(t, nodes1[0].Title, nodes2[0].Title)
	assert.Equal(t, nodes1[0].Children[0].Path, nodes2[0].Children[0].Path)
}
