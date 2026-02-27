package classifier

import (
	"log/slog"
	"regexp"

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

// compileBoundaryPatterns compiles a list of literal strings into case-insensitive
// word-boundary regexps to avoid false positives from substring matching.
func compileBoundaryPatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		compiled[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(p) + `\b`)
	}
	return compiled
}

// rulePatterns match text that describes rules/constraints.
var rulePatterns = []string{
	"must", "always", "never", "required", "shall", "constraint",
	"invariant", "rule:", "important:", "critical:", "forbidden",
	"mandatory", "do not", "don't", "ensure that", "make sure",
}

// procedurePatterns match text that describes procedures/steps.
var procedurePatterns = []string{
	"step 1", "step 2", "first", "then", "finally",
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

var (
	compiledRulePatterns       = compileBoundaryPatterns(rulePatterns)
	compiledProcedurePatterns  = compileBoundaryPatterns(procedurePatterns)
	compiledPreferencePatterns = compileBoundaryPatterns(preferencePatterns)
	compiledEpisodePatterns    = compileBoundaryPatterns(episodePatterns)
)

// Classify determines the memory type from content using heuristics.
func (c *HeuristicClassifier) Classify(content string) models.MemoryType {
	scores := map[models.MemoryType]int{
		models.MemoryTypeRule:       0,
		models.MemoryTypeFact:       0,
		models.MemoryTypeEpisode:    0,
		models.MemoryTypeProcedure:  0,
		models.MemoryTypePreference: 0,
	}

	for _, re := range compiledRulePatterns {
		if re.MatchString(content) {
			scores[models.MemoryTypeRule]++
		}
	}

	for _, re := range compiledProcedurePatterns {
		if re.MatchString(content) {
			scores[models.MemoryTypeProcedure]++
		}
	}

	for _, re := range compiledPreferencePatterns {
		if re.MatchString(content) {
			scores[models.MemoryTypePreference]++
		}
	}

	for _, re := range compiledEpisodePatterns {
		if re.MatchString(content) {
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
