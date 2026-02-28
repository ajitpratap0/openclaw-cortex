// Package hookinstall provides the core logic for `openclaw-cortex hook install`.
// It is a separate package so the cmd layer can delegate to it and the tests/
// package can exercise the logic without importing package main.
package hookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EventUserPromptSubmit is the Claude Code hook event fired when the user submits a prompt.
	EventUserPromptSubmit = "UserPromptSubmit"
	// EventStop is the Claude Code hook event fired when the assistant stops generating.
	EventStop = "Stop"
	// BinaryName is the name of the openclaw-cortex binary.
	BinaryName = "openclaw-cortex"
)

// HookEntry represents a single Claude Code hook command entry.
type HookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookMatcher represents a matcher+hooks pair in the Claude Code settings.
type HookMatcher struct {
	Matcher string      `json:"matcher"`
	Hooks   []HookEntry `json:"hooks"`
}

// DefaultHookConfig returns the hook matchers to inject for each event.
func DefaultHookConfig() map[string][]HookMatcher {
	return map[string][]HookMatcher{
		EventUserPromptSubmit: {
			{
				Matcher: "",
				Hooks: []HookEntry{
					{Type: "command", Command: BinaryName + " hook pre"},
				},
			},
		},
		EventStop: {
			{
				Matcher: "",
				Hooks: []HookEntry{
					{Type: "command", Command: BinaryName + " hook post"},
				},
			},
		},
	}
}

// ResolveSettingsPath returns the absolute path to the Claude Code settings.json
// given whether the global flag was set.
func ResolveSettingsPath(global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}
	return filepath.Join(cwd, ".claude", "settings.json"), nil
}

// Install reads the settings file at path (creating it if absent), merges the
// openclaw-cortex hook configuration, and writes the result back.
// It returns true when the file was modified and false when it was already up to date.
func Install(path string) (bool, error) {
	// Read existing settings.
	rawMap, hooksMap, err := readSettings(path)
	if err != nil {
		return false, err
	}

	// Merge.
	desired := DefaultHookConfig()
	changed := false
	for event, newMatchers := range desired {
		if !hasOCHook(hooksMap[event]) {
			hooksMap[event] = append(hooksMap[event], newMatchers...)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}

	// Serialize updated hooks back into raw map.
	hookBytes, marshalErr := json.MarshalIndent(hooksMap, "", "  ")
	if marshalErr != nil {
		return false, fmt.Errorf("marshaling hooks: %w", marshalErr)
	}
	rawMap["hooks"] = hookBytes

	// Write.
	data, marshalErr := json.MarshalIndent(rawMap, "", "  ")
	if marshalErr != nil {
		return false, fmt.Errorf("marshaling settings: %w", marshalErr)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
		return false, fmt.Errorf("creating directory %s: %w", dir, mkdirErr)
	}
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		return false, fmt.Errorf("writing %s: %w", path, writeErr)
	}
	return true, nil
}

// readSettings parses path into a raw map (for key preservation) and a typed
// hooks map. When the file does not exist both maps are empty and no error is returned.
func readSettings(path string) (map[string]json.RawMessage, map[string][]HookMatcher, error) {
	rawMap := map[string]json.RawMessage{}
	hooksMap := map[string][]HookMatcher{}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return rawMap, hooksMap, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if jsonErr := json.Unmarshal(data, &rawMap); jsonErr != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, jsonErr)
	}

	// Parse the hooks key only if present.
	if raw, ok := rawMap["hooks"]; ok {
		if jsonErr := json.Unmarshal(raw, &hooksMap); jsonErr != nil {
			// Hooks key exists but is malformed — treat as empty rather than error.
			hooksMap = map[string][]HookMatcher{}
		}
	}

	return rawMap, hooksMap, nil
}

// hasOCHook returns true when matchers already contain an openclaw-cortex command.
func hasOCHook(matchers []HookMatcher) bool {
	prefix := BinaryName
	for i := range matchers {
		for j := range matchers[i].Hooks {
			if strings.HasPrefix(matchers[i].Hooks[j].Command, prefix) {
				return true
			}
		}
	}
	return false
}
