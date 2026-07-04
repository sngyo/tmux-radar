package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultPath returns ~/.cache/tmux-radar/state.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "state.json"
	}
	return filepath.Join(home, ".cache", "tmux-radar", "state.json")
}

// Save writes the snapshot atomically (temp file + rename) so concurrent
// readers never observe partial JSON. The temp name is unique per call:
// watch and sidebar save the same path concurrently, and a shared temp
// name would let one writer rename the other's file away mid-save.
func Save(path string, s Snapshot) error {
	// State may reveal user activity (session/window names): owner-only perms.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
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
