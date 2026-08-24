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

// Uninstall touches the user's global settings.json just like Install; it
// must take the same pre-write backup so an uninstall gone wrong (or a
// later regret) is recoverable.
func TestUninstallWritesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	pre := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/bin/tmux-radar hook Stop","timeout":5}]}]},"model":"opus"}`
	os.WriteFile(path, []byte(pre), 0o644)
	if err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var backups int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			backups++
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if string(b) != pre {
				t.Error("backup must hold the pre-uninstall content")
			}
		}
	}
	if backups != 1 {
		t.Fatalf("want 1 backup, got %d", backups)
	}
}

// save writes via a temp file + rename so a crash mid-write cannot leave
// the user's global settings.json truncated or corrupted. A successful
// write must leave no temp file behind.
func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"a":1}`), 0o644)
	if err := Install(path, "/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "settings.json" && !strings.HasPrefix(e.Name(), "settings.json.bak-") {
			t.Errorf("stray temp file left behind: %s", e.Name())
		}
	}
}

func TestDefaultSettingsPathEmptyWhenHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	if got := DefaultSettingsPath(); got != "" {
		t.Errorf("DefaultSettingsPath() = %q, want empty string when HOME is unset", got)
	}
}

// An empty settings path must not scatter a settings.json into whatever
// cwd the CLI happens to run in; Install must fail instead.
func TestInstallWithEmptyPathErrors(t *testing.T) {
	if err := Install("", "/bin/tmux-radar"); err == nil {
		t.Fatal("Install with empty settings path must error")
	}
}

func TestInstallHandlesMixedMatcherGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// Start with a mixed group: foreign + stale radar
	os.WriteFile(path, []byte(`{"hooks":{"Stop":[
		{"hooks":[
			{"type":"command","command":"afplay /done.aiff"},
			{"type":"command","command":"/old/tmux-radar hook Stop","timeout":5}
		]}
	]}}`), 0o644)
	if err := Install(path, "/new/tmux-radar"); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("want foreign group (filtered) + fresh radar entry, got %d groups", len(stop))
	}
	// First group should have only the foreign command.
	first := stop[0].(map[string]any)["hooks"].([]any)
	if len(first) != 1 {
		t.Fatalf("mixed group after install should have 1 command, got %d", len(first))
	}
	cmd := first[0].(map[string]any)["command"].(string)
	if cmd != "afplay /done.aiff" {
		t.Errorf("foreign command must survive, got %q", cmd)
	}
	// Second group should be the fresh radar entry.
	second := stop[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if second["command"] != "/new/tmux-radar hook Stop" {
		t.Errorf("fresh radar entry expected, got %v", second["command"])
	}
}

func TestInstallCreatesDistinctBackupsWithSubsecondPrecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"a":1}`), 0o644)

	// First install
	if err := Install(path, "/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}

	// Second install (back-to-back)
	if err := Install(path, "/bin/tmux-radar"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	backups := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			backups = append(backups, e.Name())
		}
	}

	if len(backups) != 2 {
		t.Fatalf("want 2 distinct backups, got %d: %v", len(backups), backups)
	}

	// First backup must contain original pre-install content.
	b, _ := os.ReadFile(filepath.Join(dir, backups[0]))
	if string(b) != `{"a":1}` {
		t.Errorf("first backup must hold original content, got %q", string(b))
	}
}

func TestInstallSweepsStaleRadarFromDroppedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// Pre-populate with radar entries under TaskCreated/TaskCompleted (dropped events)
	// plus a foreign entry under TaskCreated.
	os.WriteFile(path, []byte(`{"hooks":{"TaskCreated":[
		{"hooks":[{"type":"command","command":"afplay /done.aiff"}]},
		{"hooks":[{"type":"command","command":"/old/tmux-radar hook TaskCreated","timeout":5}]}
	],"TaskCompleted":[
		{"hooks":[{"type":"command","command":"/old/tmux-radar hook TaskCompleted","timeout":5}]}
	]},"model":"opus"}`), 0o644)

	if err := Install(path, "/new/tmux-radar"); err != nil {
		t.Fatal(err)
	}

	m := read(t, path)
	hooks := m["hooks"].(map[string]any)

	// TaskCreated should still exist with only the foreign entry (radar swept).
	taskCreated, ok := hooks["TaskCreated"].([]any)
	if !ok || len(taskCreated) != 1 {
		t.Fatalf("TaskCreated: want 1 entry (foreign), got %v", taskCreated)
	}
	foreign := taskCreated[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if foreign["command"] != "afplay /done.aiff" {
		t.Errorf("TaskCreated: foreign command lost, got %v", foreign["command"])
	}

	// TaskCompleted should be deleted entirely (was pure radar).
	if _, ok := hooks["TaskCompleted"]; ok {
		t.Error("TaskCompleted: pure radar entry should be deleted entirely")
	}

	// Every current Events member should exist with exactly one radar entry.
	for _, ev := range Events {
		matchers, ok := hooks[ev].([]any)
		if !ok || len(matchers) != 1 {
			t.Fatalf("%s: want 1 radar entry, got %v", ev, matchers)
		}
		cmd := matchers[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if cmd != "/new/tmux-radar hook "+ev {
			t.Errorf("%s: wrong radar command, got %q", ev, cmd)
		}
	}

	// Other keys must survive.
	if m["model"] != "opus" {
		t.Error("unrelated keys must survive")
	}
}
