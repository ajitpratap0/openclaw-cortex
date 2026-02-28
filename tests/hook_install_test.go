package tests

// hook_install_test.go exercises the hookinstall package that backs the
// `openclaw-cortex hook install` subcommand.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/hookinstall"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// readSettingsJSON reads the settings file at path and returns it as a generic map.
func readSettingsJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading settings file")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m), "parsing settings JSON")
	return m
}

// hooksSection extracts the "hooks" map from a generic settings map.
func hooksSection(t *testing.T, settings map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, ok := settings["hooks"]
	require.True(t, ok, "settings must have a 'hooks' key")
	hooks, ok := raw.(map[string]interface{})
	require.True(t, ok, "hooks value must be an object")
	return hooks
}

// hasOCCommand returns true when the event slice in hooks contains a command
// entry that starts with "openclaw-cortex".
func hasOCCommand(t *testing.T, hooks map[string]interface{}, event string) bool {
	t.Helper()
	raw, ok := hooks[event]
	if !ok {
		return false
	}
	matchers, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, m := range matchers {
		matcher, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		hookList, ok := matcher["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hookList {
			entry, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, ok := entry["command"].(string)
			if !ok {
				continue
			}
			prefix := "openclaw-cortex"
			if len(cmd) >= len(prefix) && cmd[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestHookInstall_CreatesNewFile verifies that Install creates .claude/settings.json
// when the file does not yet exist.
func TestHookInstall_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")

	changed, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)
	assert.True(t, changed, "expected changed=true on first install")

	// File must exist.
	_, statErr := os.Stat(settingsPath)
	require.NoError(t, statErr, "settings.json should have been created")

	// Contents must be valid JSON with the expected hook events.
	settings := readSettingsJSON(t, settingsPath)
	hooks := hooksSection(t, settings)
	assert.True(t, hasOCCommand(t, hooks, hookinstall.EventUserPromptSubmit),
		"UserPromptSubmit hook should be present")
	assert.True(t, hasOCCommand(t, hooks, hookinstall.EventStop),
		"Stop hook should be present")
}

// TestHookInstall_MergesIntoExisting verifies that Install merges hook entries
// into an existing settings.json without overwriting other keys.
func TestHookInstall_MergesIntoExisting(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")

	// Pre-create the file with existing keys that must be preserved.
	existing := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{"Bash"},
		},
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{},
		},
	}
	data, marshalErr := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, marshalErr)
	require.NoError(t, os.MkdirAll(filepath.Dir(settingsPath), 0o755))
	require.NoError(t, os.WriteFile(settingsPath, data, 0o644))

	// Install hooks.
	changed, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)
	assert.True(t, changed)

	// Read back.
	settings := readSettingsJSON(t, settingsPath)

	// Other keys must be preserved.
	perms, hasPerm := settings["permissions"]
	assert.True(t, hasPerm, "permissions key should be preserved")
	assert.NotNil(t, perms)

	// Hook events should be present.
	hooks := hooksSection(t, settings)
	assert.True(t, hasOCCommand(t, hooks, hookinstall.EventUserPromptSubmit))
	assert.True(t, hasOCCommand(t, hooks, hookinstall.EventStop))

	// Pre-existing PreToolUse key should still be there.
	_, hasPreToolUse := hooks["PreToolUse"]
	assert.True(t, hasPreToolUse, "PreToolUse event should be preserved")
}

// TestHookInstall_IdempotentWhenAlreadyInstalled verifies that a second call to
// Install is a no-op and returns changed=false.
func TestHookInstall_IdempotentWhenAlreadyInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")

	// First install.
	changed1, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)
	assert.True(t, changed1)

	// Second install — must be a no-op.
	changed2, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)
	assert.False(t, changed2, "expected changed=false on second install")
}

// TestHookInstall_GlobalTargetsHomeDir verifies that ResolveSettingsPath with
// global=true returns a path under the user home directory.
func TestHookInstall_GlobalTargetsHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	path, err := hookinstall.ResolveSettingsPath(true)
	require.NoError(t, err)

	expected := filepath.Join(home, ".claude", "settings.json")
	assert.Equal(t, expected, path)
}

// TestHookInstall_LocalTargetsCurrentDir verifies that ResolveSettingsPath with
// global=false returns .claude/settings.json under the current working directory.
func TestHookInstall_LocalTargetsCurrentDir(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	path, err := hookinstall.ResolveSettingsPath(false)
	require.NoError(t, err)

	expected := filepath.Join(cwd, ".claude", "settings.json")
	assert.Equal(t, expected, path)
}

// TestHookInstall_DefaultHookConfig verifies that DefaultHookConfig returns
// entries for both expected events.
func TestHookInstall_DefaultHookConfig(t *testing.T) {
	cfg := hookinstall.DefaultHookConfig()

	_, hasUPS := cfg[hookinstall.EventUserPromptSubmit]
	assert.True(t, hasUPS, "config must contain UserPromptSubmit")

	_, hasStop := cfg[hookinstall.EventStop]
	assert.True(t, hasStop, "config must contain Stop")

	// Verify command strings start with the binary name.
	for event, matchers := range cfg {
		for i := range matchers {
			for j := range matchers[i].Hooks {
				cmd := matchers[i].Hooks[j].Command
				prefix := hookinstall.BinaryName
				assert.True(t, len(cmd) >= len(prefix) && cmd[:len(prefix)] == prefix,
					"event %s hook command should start with %q", event, prefix)
			}
		}
	}
}

// TestHookInstall_WritesValidJSON verifies that the output file is valid JSON
// and has the correct hook structure.
func TestHookInstall_WritesValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")

	_, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)

	data, readErr := os.ReadFile(settingsPath)
	require.NoError(t, readErr)

	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &out), "output must be valid JSON")
	_, hasHooks := out["hooks"]
	assert.True(t, hasHooks, "output must contain a 'hooks' key")
}

// TestHookInstall_PreservesTrailingNewline verifies that the written file ends
// with a newline (POSIX convention).
func TestHookInstall_PreservesTrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")

	_, err := hookinstall.Install(settingsPath)
	require.NoError(t, err)

	data, readErr := os.ReadFile(settingsPath)
	require.NoError(t, readErr)
	require.NotEmpty(t, data)
	assert.Equal(t, byte('\n'), data[len(data)-1], "file should end with a newline")
}
