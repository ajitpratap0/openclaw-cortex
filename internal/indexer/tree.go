package indexer

import (
	"strings"
)

// SectionNode represents a section of a markdown document.
type SectionNode struct {
	Title     string
	Depth     int            // 1=H1, 2=H2, 3=H3, 4=H4
	Path      string         // " / "-delimited: "Architecture / Store / Qdrant"
	Content   string         // raw body text (no sub-headings)
	WordCount int
	Children  []*SectionNode
}

// ParseMarkdownTree parses markdown into a forest of SectionNodes.
func ParseMarkdownTree(content string) []*SectionNode {
	lines := strings.Split(content, "\n")

	var roots []*SectionNode
	// stack holds the current ancestor chain: stack[0] is depth-1 node, etc.
	var stack []*SectionNode

	var current *SectionNode
	var bodyLines []string

	flush := func() {
		if current == nil {
			return
		}
		body := strings.TrimSpace(strings.Join(bodyLines, "\n"))
		current.Content = body
		current.WordCount = len(strings.Fields(body))
		bodyLines = nil
	}

	for _, line := range lines {
		depth, title, ok := parseHeader(line)
		if !ok {
			bodyLines = append(bodyLines, line)
			continue
		}

		// Flush previous node's body.
		flush()

		node := &SectionNode{
			Title: title,
			Depth: depth,
		}

		// Build path by popping stack to find parent.
		for len(stack) > 0 && stack[len(stack)-1].Depth >= depth {
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 {
			node.Path = title
			roots = append(roots, node)
		} else {
			parent := stack[len(stack)-1]
			node.Path = parent.Path + " / " + title
			parent.Children = append(parent.Children, node)
		}

		stack = append(stack, node)
		current = node
	}

	// Flush the last section.
	flush()

	return roots
}

// parseHeader returns the heading depth, trimmed title, and true when the line
// is a markdown ATX heading (up to ####). Returns (0, "", false) otherwise.
func parseHeader(line string) (depth int, title string, ok bool) {
	for d := 4; d >= 1; d-- {
		prefix := strings.Repeat("#", d) + " "
		if strings.HasPrefix(line, prefix) {
			return d, strings.TrimSpace(line[len(prefix):]), true
		}
	}
	return 0, "", false
}
