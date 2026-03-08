package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

const rerankCacheTTL = 5 * time.Minute

// RerankCacheEntry holds pre-ranked results for a session.
type RerankCacheEntry struct {
	SessionID string                `json:"session_id"`
	RankedAt  time.Time             `json:"ranked_at"`
	Results   []models.RecallResult `json:"results"`
}

// WriteRerankCache writes pre-ranked results for a session to disk.
func WriteRerankCache(homeDir, sessionID string, results []models.RecallResult) {
	dir := filepath.Join(homeDir, ".cortex", "rerank_cache")
	_ = os.MkdirAll(dir, 0o700)
	entry := RerankCacheEntry{SessionID: sessionID, RankedAt: time.Now(), Results: results}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, sessionID+".json"), data, 0o600)
}

// ReadRerankCache reads pre-ranked results if the cache is fresh (< 5 min).
// Returns nil if missing, expired, or corrupt.
func ReadRerankCache(homeDir, sessionID string) []models.RecallResult {
	path := filepath.Join(homeDir, ".cortex", "rerank_cache", sessionID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry RerankCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return nil
	}
	if time.Since(entry.RankedAt) > rerankCacheTTL {
		_ = os.Remove(path)
		return nil
	}
	return entry.Results
}
