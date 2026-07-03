package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultPath returns ~/.cache/tmux-agents/state.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "state.json"
	}
	return filepath.Join(home, ".cache", "tmux-agents", "state.json")
}

// Save writes the snapshot atomically (temp file + rename) so concurrent
// readers never observe partial JSON.
func Save(path string, s Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a snapshot; a missing file returns an empty snapshot and the error.
func Load(path string) (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}
