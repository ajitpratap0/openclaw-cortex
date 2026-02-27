package classifier

import (
	"log/slog"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// Classifier determines the type of a memory.
type Classifier interface {
	Classify(content string) models.MemoryType
}

// HeuristicClassifier uses keyword-based rules for classification.
type HeuristicClassifier struct {
	logger *slog.Logger
}

// NewClassifier creates a new heuristic-based classifier.
func NewClassifier(logger *slog.Logger) *HeuristicClassifier {
	return &HeuristicClassifier{logger: logger}
}

// rulePatterns match text that describes rules/constraints.
var rulePatterns = []string{
	"must", "always", "never", "required", "shall", "constraint",
	"invariant", "rule:", "important:", "critical:", "forbidden",
	"mandatory", "do not", "don't", "ensure that", "make sure",
}

// procedurePatterns match text that describes procedures/steps.
var procedurePatterns = []string{
	"step 1", "step 2", "first,", "then,", "finally,",
	"how to", "to do this", "process:", "workflow:", "steps:",
	"run the", "execute", "install", "configure", "setup",
	"create a", "deploy", "build the",
}

// preferencePatterns match text about preferences.
var preferencePatterns = []string{
	"prefer", "like", "dislike", "favorite", "rather",
	"style:", "prefer to", "would rather", "choose",
	"instead of", "better than", "worse than",
}

// episodePatterns match text about specific events.
var episodePatterns = []string{
	"yesterday", "today", "last week", "on monday",
	"happened", "occurred", "we did", "we found",
	"discovered", "resolved", "fixed the", "broke",
	"incident", "session", "meeting",
}

// Classify determines the memory type from content using heuristics.
func (c *HeuristicClassifier) Classify(content string) models.MemoryType {
	lower := strings.ToLower(content)

	scores := map[models.MemoryType]int{
		models.MemoryTypeRule:       0,
		models.MemoryTypeFact:       0,
		models.MemoryTypeEpisode:    0,
		models.MemoryTypeProcedure:  0,
		models.MemoryTypePreference: 0,
	}

	for _, p := range rulePatterns {
		if strings.Contains(lower, p) {
			scores[models.MemoryTypeRule]++
		}
	}

	for _, p := range procedurePatterns {
		if strings.Contains(lower, p) {
			scores[models.MemoryTypeProcedure]++
		}
	}

	for _, p := range preferencePatterns {
		if strings.Contains(lower, p) {
			scores[models.MemoryTypePreference]++
		}
	}

	for _, p := range episodePatterns {
		if strings.Contains(lower, p) {
			scores[models.MemoryTypeEpisode]++
		}
	}

	// Find the highest scoring type
	bestType := models.MemoryTypeFact
	bestScore := 0
	for mt, score := range scores {
		if score > bestScore {
			bestScore = score
			bestType = mt
		}
	}

	// Default to fact if no strong signal
	if bestScore == 0 {
		bestType = models.MemoryTypeFact
	}

	c.logger.Debug("classified memory", "type", bestType, "score", bestScore, "content_prefix", truncate(content, 60))
	return bestType
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}
