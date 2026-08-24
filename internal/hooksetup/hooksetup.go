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

// Events is the hook set radar registers. It starts as the probe superset;
// the live probe (see the design spec) trims it to what actually fires.
var Events = []string{
	"SessionStart", "SessionEnd",
	"UserPromptSubmit", "Stop", "StopFailure",
	"SubagentStart", "SubagentStop",
	"TaskCreated", "TaskCompleted",
}

// marker identifies radar-owned entries regardless of the binary's path.
const marker = "tmux-radar hook "

// DefaultSettingsPath returns ~/.claude/settings.json.
func DefaultSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "settings.json"
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Install merges one radar entry per event into settingsPath, replacing any
// stale radar entries (old binary paths) and preserving foreign hooks and
// unknown keys. The pre-install file is backed up next to it.
func Install(settingsPath, binPath string) error {
	m, err := load(settingsPath)
	if err != nil {
		return err
	}
	if b, err := os.ReadFile(settingsPath); err == nil {
		backup := fmt.Sprintf("%s.bak-%s", settingsPath, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, b, 0o644); err != nil {
			return err
		}
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range Events {
		matchers, _ := hooks[ev].([]any)
		matchers = withoutRadar(matchers)
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
func Uninstall(settingsPath string) error {
	m, err := load(settingsPath)
	if err != nil {
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

// withoutRadar filters out matcher groups whose every command is radar's.
func withoutRadar(matchers []any) []any {
	var kept []any
	for _, raw := range matchers {
		group, _ := raw.(map[string]any)
		cmds, _ := group["hooks"].([]any)
		radar := len(cmds) > 0
		for _, c := range cmds {
			cm, _ := c.(map[string]any)
			s, _ := cm["command"].(string)
			if !strings.Contains(s, marker) {
				radar = false
			}
		}
		if !radar {
			kept = append(kept, raw)
		}
	}
	return kept
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

func save(path string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
