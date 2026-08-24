// Package hooksetup installs and removes tmux-radar's hook entries in
// Claude Code's user settings file, preserving everything else verbatim.
package hooksetup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Events is the hook set radar registers. Trimmed to what the live probe
// showed actually fires and carries signal: TaskCreated/TaskCompleted
// never fired for either the Agent tool or a background shell (see
// docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md).
var Events = []string{
	"SessionStart", "SessionEnd",
	"UserPromptSubmit", "Stop", "StopFailure",
	"SubagentStart", "SubagentStop",
}

// marker identifies radar-owned entries regardless of the binary's path.
const marker = "tmux-radar hook "

// DefaultSettingsPath returns ~/.claude/settings.json. An empty string
// (HOME unresolvable) is returned rather than a relative fallback: this is
// only ever used by the interactive install-hooks/uninstall-hooks
// subcommands, so surfacing an error there (instead of writing a stray
// settings.json into cwd) is the correct, visible failure.
func DefaultSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Install merges one radar entry per event into settingsPath, replacing any
// stale radar entries (old binary paths) and preserving foreign hooks and
// unknown keys. The pre-install file is backed up next to it. Stale radar
// entries from dropped events are swept away entirely.
func Install(settingsPath, binPath string) error {
	m, err := load(settingsPath)
	if err != nil {
		return err
	}
	if err := backup(settingsPath); err != nil {
		return err
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// First sweep: remove all radar entries from all event keys (including dropped events).
	for ev, v := range hooks {
		matchers, _ := v.([]any)
		if kept := withoutRadar(matchers); len(kept) > 0 {
			hooks[ev] = kept
		} else {
			delete(hooks, ev)
		}
	}
	// Second pass: add fresh radar entries for current Events.
	for _, ev := range Events {
		matchers, _ := hooks[ev].([]any)
		if matchers == nil {
			matchers = []any{}
		}
		matchers = append(matchers, map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": binPath + " hook " + ev,
				"timeout": 5,
			}},
		})
		hooks[ev] = matchers
	}
	m["hooks"] = hooks
	return save(settingsPath, m)
}

// Uninstall removes radar's entries; foreign hooks and other keys survive.
// Takes the same pre-write backup Install does: this edits the user's
// global settings.json too, and should be just as recoverable.
func Uninstall(settingsPath string) error {
	m, err := load(settingsPath)
	if err != nil {
		return err
	}
	if err := backup(settingsPath); err != nil {
		return err
	}
	hooks, _ := m["hooks"].(map[string]any)
	for ev, v := range hooks {
		matchers, _ := v.([]any)
		if kept := withoutRadar(matchers); len(kept) > 0 {
			hooks[ev] = kept
		} else {
			delete(hooks, ev)
		}
	}
	return save(settingsPath, m)
}

// withoutRadar removes radar commands from matcher groups, dropping groups
// that become empty.
func withoutRadar(matchers []any) []any {
	var kept []any
	for _, raw := range matchers {
		group, _ := raw.(map[string]any)
		cmds, _ := group["hooks"].([]any)
		var foreign []any
		for _, c := range cmds {
			cm, _ := c.(map[string]any)
			s, _ := cm["command"].(string)
			if !strings.Contains(s, marker) {
				foreign = append(foreign, c)
			}
		}
		if len(foreign) > 0 {
			// Keep the group with only foreign commands.
			group["hooks"] = foreign
			kept = append(kept, group)
		}
	}
	return kept
}

// backup writes a timestamped copy of settingsPath's current content next
// to it, if the file exists and is readable. A missing or unreadable file
// has nothing to preserve, so that case is silently skipped rather than
// treated as an error (mirrors load's not-exist handling).
func backup(settingsPath string) error {
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}
	dst := fmt.Sprintf("%s.bak-%s", settingsPath, time.Now().Format("20060102-150405.000000000"))
	return os.WriteFile(dst, b, 0o644)
}

func load(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// save writes m to path via a temp-file-then-rename so a crash mid-write
// cannot leave the user's global settings.json truncated or corrupted:
// os.WriteFile truncates in place first, so a process killed mid-write
// (or a full disk) would otherwise lose the file's content outright.
func save(path string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmux-radar-settings-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// A successful rename below makes this a no-op; any earlier return
	// leaves the temp file cleaned up instead of stranded.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
