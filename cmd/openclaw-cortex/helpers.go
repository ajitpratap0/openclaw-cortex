package main

import (
	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

// recallWeightsFromConfig converts the config weights struct into the recall
// package's Weights type, avoiding an 8-line struct literal duplicated across
// every command that creates a Recaller.
func recallWeightsFromConfig(c config.RecallWeightsConfig) recall.Weights {
	return recall.Weights{
		Similarity:    c.Similarity,
		Recency:       c.Recency,
		Frequency:     c.Frequency,
		TypeBoost:     c.TypeBoost,
		ScopeBoost:    c.ScopeBoost,
		Confidence:    c.Confidence,
		Reinforcement: c.Reinforcement,
		TagAffinity:   c.TagAffinity,
	}
}
