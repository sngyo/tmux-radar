package hooksetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInstallIntoEmptySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus"}`), 0o644)
	if err := Install(path, "/usr/local/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	if m["model"] != "opus" {
		t.Error("unrelated keys must survive verbatim")
	}
	hooks := m["hooks"].(map[string]any)
	for _, ev := range Events {
		matchers, ok := hooks[ev].([]any)
		if !ok || len(matchers) != 1 {
			t.Fatalf("%s: want 1 radar matcher, got %v", ev, hooks[ev])
		}
		h := matchers[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		if h["command"] != "/usr/local/bin/tmux-radar hook "+ev {
			t.Errorf("%s command = %v", ev, h["command"])
		}
		if h["timeout"] != float64(5) {
			t.Errorf("%s timeout = %v, want 5", ev, h["timeout"])
		}
	}
}

func TestInstallPreservesForeignHooksAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"hooks":{"Stop":[
		{"hooks":[{"type":"command","command":"afplay /done.aiff"}]},
		{"hooks":[{"type":"command","command":"/old/tmux-radar hook Stop"}]}
	]}}`), 0o644)
	if err := Install(path, "/new/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	if err := Install(path, "/new/tmux-radar"); err != nil { // run twice
		t.Fatal(err)
	}
	stop := read(t, path)["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("want foreign + 1 radar entry, got %d: %v", len(stop), stop)
	}
	first := stop[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if first["command"] != "afplay /done.aiff" {
		t.Error("foreign hook must survive in place")
	}
	last := stop[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if last["command"] != "/new/tmux-radar hook Stop" {
		t.Errorf("stale radar entry must be replaced, got %v", last["command"])
	}
}

func TestInstallMissingFileCreatesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Install(path, "/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	if _, ok := read(t, path)["hooks"]; !ok {
		t.Fatal("settings.json must be created with hooks")
	}
}

func TestInstallWritesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"a":1}`), 0o644)
	if err := Install(path, "/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var backups int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			backups++
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if string(b) != `{"a":1}` {
				t.Error("backup must hold the pre-install content")
			}
		}
	}
	if backups != 1 {
		t.Fatalf("want 1 backup, got %d", backups)
	}
}

func TestUninstallRemovesOnlyRadarEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"hooks":{"Stop":[
		{"hooks":[{"type":"command","command":"afplay /done.aiff"}]},
		{"hooks":[{"type":"command","command":"/bin/tmux-radar hook Stop","timeout":5}]}
	]},"model":"opus"}`), 0o644)
	if err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("radar entry must be gone, foreign kept: %v", stop)
	}
	if m["model"] != "opus" {
		t.Error("unrelated keys must survive")
	}
}
