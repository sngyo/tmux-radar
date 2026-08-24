# Hook-Event Working Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make working⇔idle detection event-driven via Claude Code hooks, with the existing screen scraping kept as fallback.

**Architecture:** A logic-free `tmux-radar hook <event>` subcommand appends one JSON line per hook event to `~/.local/state/tmux-radar/events/<pane-id>.jsonl`. The poller derives hook-working from that log in Go and ORs it with the scrape result (scrape blocked still wins). `install-hooks`/`uninstall-hooks` manage the entries in `~/.claude/settings.json`.

**Tech Stack:** Go stdlib only (encoding/json, os). No new module dependencies.

**Spec:** `docs/superpowers/specs/2026-08-25-hook-events-design.md`

## Global Constraints

- The hook path NEVER writes to stdout and ALWAYS exits 0 — a radar bug must never block or pollute Claude Code (token cost must stay zero).
- `install-hooks` preserves every unrelated key/entry in `~/.claude/settings.json` verbatim (decode into `map[string]any`, mutate only `hooks.<Event>` arrays) and backs the file up first.
- Hook commands are registered with `"timeout": 5` (seconds) — never inherit the 10-minute default.
- Event log lines are written with a single `write()` (O_APPEND) so concurrent hooks interleave whole lines; payloads are compacted and capped at 32 KB.
- A session without hooks installed must behave exactly as today (scrape-only path).
- Commit style: lowercase conventional (`feat:`, `fix:`, `docs:`), matching `git log`.
- Execution preflight: if shell commands in the repo fail with "Operation not permitted", the session's shell lost the macOS TCC grant for ~/Documents — run commands via `tmux run-shell "/bin/sh <script> > <log> 2>&1; true"` (see memory note `shell-tcc-documents-denial`) or have the user re-grant access in System Settings.

---

### Task 1: Event log package (`internal/hookevents`)

**Files:**
- Create: `internal/hookevents/hookevents.go`
- Test: `internal/hookevents/hookevents_test.go`

**Interfaces:**
- Produces:
  - `type Event struct { Event string; Pane string; TS time.Time; Payload json.RawMessage }` (JSON tags `event`, `pane`, `ts`, `payload,omitempty`)
  - `func DefaultDir() string` — `~/.local/state/tmux-radar/events`
  - `func Append(dir, pane, event string, payload []byte, now time.Time) error`
  - `func ReadPane(dir, pane string) ([]Event, error)` — missing file → `(nil, nil)`
  - `func GC(dir string, now time.Time, maxAge time.Duration)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/hookevents/hookevents_test.go
package hookevents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

func TestAppendAndReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, "%20", "UserPromptSubmit", []byte(`{"prompt":"hi"}`), t0); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, "%20", "Stop", nil, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	evs, err := ReadPane(dir, "%20")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Event != "UserPromptSubmit" || evs[1].Event != "Stop" {
		t.Fatalf("got %+v", evs)
	}
	if evs[0].Pane != "%20" || !evs[0].TS.Equal(t0) {
		t.Errorf("metadata not preserved: %+v", evs[0])
	}
	if string(evs[0].Payload) != `{"prompt":"hi"}` {
		t.Errorf("payload = %s", evs[0].Payload)
	}
}

func TestReadPaneMissingFile(t *testing.T) {
	evs, err := ReadPane(t.TempDir(), "%99")
	if err != nil || evs != nil {
		t.Fatalf("missing file must be (nil, nil), got %v, %v", evs, err)
	}
}

// SessionStart marks a fresh session: the log restarts so drifted state
// (crashed turns, missed Stops) cannot outlive a restart.
func TestAppendSessionStartTruncates(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, "%20", "UserPromptSubmit", nil, t0)
	_ = Append(dir, "%20", "SessionStart", nil, t0.Add(time.Hour))
	evs, _ := ReadPane(dir, "%20")
	if len(evs) != 1 || evs[0].Event != "SessionStart" {
		t.Fatalf("SessionStart must truncate, got %+v", evs)
	}
}

// Pretty-printed or oversized payloads must not break the one-line format.
func TestAppendNormalizesPayload(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, "%20", "Stop", []byte("{\n  \"a\": 1\n}"), t0)
	_ = Append(dir, "%20", "Stop", []byte(`{"big":"`+strings.Repeat("x", 40<<10)+`"}`), t0)
	_ = Append(dir, "%20", "Stop", []byte(`not json`), t0)
	b, err := os.ReadFile(filepath.Join(dir, "%20.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 3 {
		t.Fatalf("want 3 lines, got %d", n)
	}
	evs, err := ReadPane(dir, "%20")
	if err != nil || len(evs) != 3 {
		t.Fatalf("all three lines must parse: %v, %v", evs, err)
	}
}

// A torn last line (reader raced a writer) must not fail the whole read.
func TestReadPaneToleratesPartialLastLine(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, "%20", "Stop", nil, t0)
	f, _ := os.OpenFile(filepath.Join(dir, "%20.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"event":"Sub`)
	f.Close()
	evs, err := ReadPane(dir, "%20")
	if err != nil || len(evs) != 1 {
		t.Fatalf("want the 1 whole line, got %v, %v", evs, err)
	}
}

func TestGCRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, "%old", "Stop", nil, t0)
	old := filepath.Join(dir, "%old.jsonl")
	past := time.Now().Add(-72 * time.Hour)
	os.Chtimes(old, past, past)
	_ = Append(dir, "%new", "Stop", nil, t0)
	GC(dir, time.Now(), 48*time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old file must be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "%new.jsonl")); err != nil {
		t.Error("fresh file must survive")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hookevents/`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Implement `internal/hookevents/hookevents.go`**

```go
// Package hookevents stores and reads the per-pane Claude Code hook event
// log that backs event-driven working detection.
package hookevents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Event is one hook firing, as appended by `tmux-radar hook <event>`.
type Event struct {
	Event   string          `json:"event"`
	Pane    string          `json:"pane"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// maxPayload bounds a stored payload. Larger payloads (huge prompts) are
// dropped: derivation only needs the event name; payloads are for debugging.
const maxPayload = 32 << 10

// DefaultDir returns ~/.local/state/tmux-radar/events.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "events"
	}
	return filepath.Join(home, ".local", "state", "tmux-radar", "events")
}

func paneFile(dir, pane string) string {
	return filepath.Join(dir, pane+".jsonl")
}

// Append writes one event as a single line. SessionStart truncates the log
// first: a fresh session must never inherit drifted state. Payloads are
// compacted to one line; invalid or oversized payloads are dropped, never
// an error — losing debug detail beats losing the event.
func Append(dir, pane, event string, payload []byte, now time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	e := Event{Event: event, Pane: pane, TS: now}
	var buf bytes.Buffer
	if len(payload) > 0 && len(payload) <= maxPayload && json.Compact(&buf, payload) == nil {
		e.Payload = json.RawMessage(buf.Bytes())
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if event == "SessionStart" {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(paneFile(dir, pane), flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadPane returns the pane's events in append order. A missing file is
// (nil, nil). Unparseable lines (a torn tail mid-write) are skipped.
func ReadPane(dir, pane string) ([]Event, error) {
	b, err := os.ReadFile(paneFile(dir, pane))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var evs []Event
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if json.Unmarshal(line, &e) == nil && e.Event != "" {
			evs = append(evs, e)
		}
	}
	return evs, nil
}

// GC removes event files untouched for maxAge. Closed panes stop appending,
// so their files age out; live panes keep fresh mtimes. Best-effort.
func GC(dir string, now time.Time, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		info, err := ent.Info()
		if err != nil || ent.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, ent.Name()))
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hookevents/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hookevents/
git commit -m "feat: per-pane hook event log with append, read, gc"
```

---

### Task 2: `hook` subcommand

**Files:**
- Modify: `cmd/tmux-radar/main.go` (switch in `run()` at line ~34; add `cmdHook`)
- Test: `cmd/tmux-radar/main_test.go` (append; keep existing tests untouched)

**Interfaces:**
- Consumes: `hookevents.Append`, `hookevents.DefaultDir` (Task 1)
- Produces: CLI `tmux-radar hook <event-name>` reading the payload from stdin and `$TMUX_PANE` from the environment. Testable core: `func cmdHook(dir, pane, event string, stdin io.Reader) int`.

- [ ] **Step 1: Write the failing tests**

```go
// append to cmd/tmux-radar/main_test.go
func TestCmdHookAppendsEvent(t *testing.T) {
	dir := t.TempDir()
	code := cmdHook(dir, "%20", "UserPromptSubmit", strings.NewReader(`{"prompt":"x"}`))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	evs, err := hookevents.ReadPane(dir, "%20")
	if err != nil || len(evs) != 1 || evs[0].Event != "UserPromptSubmit" {
		t.Fatalf("got %+v, %v", evs, err)
	}
}

// Outside tmux there is no pane to attribute the event to: exit 0, no file.
func TestCmdHookNoPaneIsSilentNoop(t *testing.T) {
	dir := t.TempDir()
	if code := cmdHook(dir, "", "Stop", strings.NewReader("{}")); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("no event file may be created without a pane")
	}
}

// The hook path must never block Claude Code: even an unwritable dir exits 0.
func TestCmdHookNeverFails(t *testing.T) {
	if code := cmdHook("/dev/null/nodir", "%20", "Stop", strings.NewReader("{}")); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// `hook` with no event name (misconfigured settings) exits 0 quietly.
func TestRunHookWithoutEventName(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"hook"}, &out); code != 0 || out.Len() != 0 {
		t.Fatalf("exit=%d out=%q, want 0 and empty", code, out.String())
	}
}
```

Add imports the test file lacks (`os`, `strings`, `bytes`, `github.com/sngyo/tmux-radar/internal/hookevents`) — check the existing import block first.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/tmux-radar/`
Expected: FAIL — `cmdHook` undefined.

- [ ] **Step 3: Implement in `cmd/tmux-radar/main.go`**

In the `run()` switch, before `default:`:

```go
case "hook":
	if len(args) < 2 {
		return 0 // misconfigured hook entry must not disturb Claude Code
	}
	return cmdHook(hookevents.DefaultDir(), os.Getenv("TMUX_PANE"), args[1], os.Stdin)
```

Do NOT add `hook` to the usage string's bracket list — it is not a
user-facing command; document it in README instead (Task 6).

```go
// cmdHook appends one hook event to the pane's event log. It never writes
// to stdout (hook stdout can be injected into Claude's context = token
// cost) and always exits 0 (radar must never block Claude Code).
func cmdHook(dir, pane, event string, stdin io.Reader) int {
	if pane == "" {
		return 0 // not inside tmux
	}
	payload, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		payload = nil
	}
	_ = hookevents.Append(dir, pane, event, payload, time.Now())
	return 0
}
```

Add `"github.com/sngyo/tmux-radar/internal/hookevents"` to main.go imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tmux-radar/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/tmux-radar/
git commit -m "feat: hook subcommand appending events to the pane log"
```

---

### Task 3: `install-hooks` / `uninstall-hooks`

**Files:**
- Create: `internal/hooksetup/hooksetup.go`
- Test: `internal/hooksetup/hooksetup_test.go`
- Modify: `cmd/tmux-radar/main.go` (two new switch cases + usage line)

**Interfaces:**
- Consumes: nothing from other tasks (pure settings.json surgery).
- Produces:
  - `var Events = []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "StopFailure", "SubagentStart", "SubagentStop", "TaskCreated", "TaskCompleted"}` — the probe superset; Task 5 trims it after the probe.
  - `func Install(settingsPath, binPath string) error` — backup + merge.
  - `func Uninstall(settingsPath string) error` — remove radar entries only.
  - CLI: `tmux-radar install-hooks`, `tmux-radar uninstall-hooks`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/hooksetup/hooksetup_test.go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hooksetup/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `internal/hooksetup/hooksetup.go`**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooksetup/`
Expected: PASS

- [ ] **Step 5: Wire the CLI in `cmd/tmux-radar/main.go`**

Switch cases (usage string gains `install-hooks|uninstall-hooks`):

```go
case "install-hooks":
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stdout, "install-hooks error: %v\n", err)
		return 1
	}
	path := hooksetup.DefaultSettingsPath()
	if err := hooksetup.Install(path, exe); err != nil {
		fmt.Fprintf(stdout, "install-hooks error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "hooks installed in %s (restart Claude Code sessions to pick them up)\n", path)
	return 0
case "uninstall-hooks":
	path := hooksetup.DefaultSettingsPath()
	if err := hooksetup.Uninstall(path); err != nil {
		fmt.Fprintf(stdout, "uninstall-hooks error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "hooks removed from %s\n", path)
	return 0
```

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/hooksetup/ cmd/tmux-radar/
git commit -m "feat: install-hooks and uninstall-hooks settings.json management"
```

---

### Task 4: Live probe — pin undocumented semantics, collect fixtures

Semi-manual: needs a restarted Claude Code session and the user's cooperation. The `hook` subcommand doubles as the probe because it logs full payloads.

**Files:**
- Create: `internal/hookevents/testdata/probe-*.jsonl` (captured logs)
- Create: `docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md`

**Interfaces:**
- Consumes: deployed binary with Tasks 1–3; `hooksetup.Events` superset.
- Produces: fixture files and three verdicts consumed by Task 5:
  (a) does `Stop` fire while background agents run, (b) which payload
  field identifies a subagent in `SubagentStart`/`SubagentStop` (and do
  `TaskCreated`/`TaskCompleted` fire for background Agent-tool tasks),
  (c) does an automatic resume after a task notification fire
  `UserPromptSubmit`.

- [ ] **Step 1: Deploy the probe binary and install hooks**

```bash
go build -o /tmp/tmux-radar-probe ./cmd/tmux-radar || exit 1
rm ~/.local/bin/tmux-radar && go build -o ~/.local/bin/tmux-radar ./cmd/tmux-radar
~/.local/bin/tmux-radar install-hooks
```

(Deploy rule from memory: never `cp` over the installed binary — remove
and rebuild to the path, or macOS kills it on launch.)

- [ ] **Step 2: Ask the user to restart one Claude Code session in a tmux pane**

Existing sessions do not see new hooks. One restarted session is enough.

- [ ] **Step 3: Exercise the scenarios in that session**

1. A plain turn: any short prompt → wait for it to finish.
2. A background subagent: "Spawn a background agent (Agent tool, run_in_background) that sleeps 60 seconds via Bash, then reply immediately." — the main turn must end while the agent runs.
3. Let the background agent finish and the session auto-resume.
4. `/clear` or restart to confirm SessionStart truncation.

- [ ] **Step 4: Collect the log and write the verdicts**

```bash
PANE=$(tmux display-message -p '#{pane_id}')   # run inside the probed pane
cp ~/.local/state/tmux-radar/events/$PANE.jsonl internal/hookevents/testdata/probe-background-agent.jsonl
```

Trim the fixture to the scenario windows; redact prompt text if any
(payloads may embed it). Write `docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md` answering (a), (b), (c) with the exact
event/field names seen, and list any superset events that never fired.

- [ ] **Step 5: Commit**

```bash
git add internal/hookevents/testdata/ docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md
git commit -m "docs: hook probe fixtures and semantics notes"
```

---

### Task 5: Derivation state machine

**Files:**
- Create: `internal/hookevents/derive.go`
- Test: `internal/hookevents/derive_test.go`
- Modify: `internal/hooksetup/hooksetup.go` (trim `Events` per probe notes)

**Interfaces:**
- Consumes: `Event` (Task 1), probe fixtures + notes (Task 4).
- Produces: `func Working(events []Event) bool` — used by Task 6.

**Adjust to the probe first:** the code below assumes the probe found a
subagent id at payload field `agent_id`. If the notes name a different
field, use that; if Stop turned out not to fire while agents run, the
live-set logic still holds (turn stays on longer, which is harmless).
Trim `hooksetup.Events` to the events that fired plus SessionStart/End.

- [ ] **Step 1: Write the failing tests**

```go
// internal/hookevents/derive_test.go
package hookevents

import "testing"

func ev(name string, payload string) Event {
	e := Event{Event: name, Pane: "%20", TS: t0}
	if payload != "" {
		e.Payload = []byte(payload)
	}
	return e
}

func TestWorkingTurnLifecycle(t *testing.T) {
	if Working([]Event{ev("SessionStart", "")}) {
		t.Error("fresh session is not working")
	}
	if !Working([]Event{ev("SessionStart", ""), ev("UserPromptSubmit", "")}) {
		t.Error("prompt submitted → working")
	}
	if Working([]Event{ev("UserPromptSubmit", ""), ev("Stop", "")}) {
		t.Error("turn ended → not working")
	}
	if Working([]Event{ev("UserPromptSubmit", ""), ev("StopFailure", "")}) {
		t.Error("failed turn ended → not working")
	}
}

// The case scraping keeps fumbling: main turn over, background agent live.
func TestWorkingSurvivesStopWhileSubagentRuns(t *testing.T) {
	evs := []Event{
		ev("UserPromptSubmit", ""),
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("Stop", ""),
	}
	if !Working(evs) {
		t.Fatal("live subagent must keep the pane working after Stop")
	}
	evs = append(evs, ev("SubagentStop", `{"agent_id":"a1"}`))
	if Working(evs) {
		t.Fatal("last subagent done → not working")
	}
}

func TestWorkingAnonymousSubagentCounting(t *testing.T) {
	evs := []Event{
		ev("SubagentStart", "{}"), ev("SubagentStart", "{}"), ev("Stop", ""),
		ev("SubagentStop", "{}"),
	}
	if !Working(evs) {
		t.Fatal("one of two anonymous subagents still live")
	}
	if Working(append(evs, ev("SubagentStop", "{}"))) {
		t.Fatal("both anonymous subagents done")
	}
}

// A Stop without an id must still drain an id-tracked entry: mixed payload
// shapes may not leak a stuck live set (stuck-working is the worst mode).
func TestWorkingMixedIdAndAnonymousStop(t *testing.T) {
	evs := []Event{
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("SubagentStop", "{}"),
	}
	if Working(evs) {
		t.Fatal("anonymous stop must drain the only live entry")
	}
}

func TestWorkingSessionEndResetsEverything(t *testing.T) {
	evs := []Event{
		ev("UserPromptSubmit", ""),
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("SessionEnd", ""),
	}
	if Working(evs) {
		t.Fatal("SessionEnd clears turn and live set")
	}
}

func TestWorkingEmptyLog(t *testing.T) {
	if Working(nil) {
		t.Error("no events → not working")
	}
}
```

Add a fixture-replay test once Task 4's files exist:

```go
func TestWorkingProbeFixtureEndsIdle(t *testing.T) {
	evs, err := ReadPane("testdata", "probe-background-agent")
	if err != nil || len(evs) == 0 {
		t.Skipf("fixture missing: %v", err)
	}
	if Working(evs) {
		t.Error("completed probe scenario must end not-working")
	}
}
```

(`ReadPane("testdata", "probe-background-agent")` reads the Task 4
fixture; additionally assert intermediate states per the probe notes if
the fixture has clear scenario boundaries.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hookevents/ -run TestWorking`
Expected: FAIL — `Working` undefined.

- [ ] **Step 3: Implement `internal/hookevents/derive.go`**

```go
package hookevents

import "encoding/json"

// subagentID extracts the identifier the probe found in Subagent* payloads.
// Field name pinned by docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md.
func subagentID(e Event) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if e.Payload == nil || json.Unmarshal(e.Payload, &p) != nil {
		return ""
	}
	return p.AgentID
}

// Working reduces a pane's event log to "is this agent busy": a turn is
// running, or background subagents are still live. Drift toward stuck-
// working is the worst failure mode, so unmatched stops drain aggressively
// and SessionStart truncation (Append) plus SessionEnd reset bound errors.
func Working(events []Event) bool {
	turn := false
	live := map[string]bool{}
	anon := 0
	for _, e := range events {
		switch e.Event {
		case "UserPromptSubmit":
			turn = true
		case "Stop", "StopFailure":
			turn = false
		case "SubagentStart":
			if id := subagentID(e); id != "" {
				live[id] = true
			} else {
				anon++
			}
		case "SubagentStop":
			id := subagentID(e)
			switch {
			case id != "" && live[id]:
				delete(live, id)
			case anon > 0:
				anon--
			case len(live) > 0: // mixed shapes: drain any entry over leaking
				for k := range live {
					delete(live, k)
					break
				}
			}
		case "SessionEnd":
			turn, live, anon = false, map[string]bool{}, 0
		}
	}
	return turn || len(live) > 0 || anon > 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hookevents/`
Expected: PASS

- [ ] **Step 5: Trim `hooksetup.Events` per the probe notes**

Remove events the probe proved useless (never fired / no signal value)
from `hooksetup.Events`; keep the `Working` switch handling only events
that remain. Update the hooksetup tests if the list changed, and re-run
`~/.local/bin/tmux-radar install-hooks` at deploy time (Task 6) so the
settings shrink to match.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/hookevents/ internal/hooksetup/
git commit -m "feat: derive hook-working from the pane event log"
```

---

### Task 6: Poller merge, GC wiring, README, deploy

**Files:**
- Modify: `internal/poller/poller.go` (Deps + RunOnce)
- Modify: `internal/poller/poller_test.go`
- Modify: `internal/config/config.go` (`PollerDeps` wires the reader)
- Modify: `cmd/tmux-radar/main.go` (`cmdSidebar`/`cmdWatch` call GC once at start; `DefaultDeps` callers unchanged)
- Modify: `README.md`

**Interfaces:**
- Consumes: `hookevents.ReadPane`, `hookevents.Working`, `hookevents.DefaultDir`, `hookevents.GC`.
- Produces: `poller.Deps.HookWorking func(paneID string) bool` (nil disables — existing tests keep passing).

- [ ] **Step 1: Write the failing test**

```go
// append to internal/poller/poller_test.go
// Hook events say working; the screen shows an idle prompt (e.g. the wait
// line scrolled away AND the subagent list failed to parse). Hook wins.
func TestHookWorkingUpgradesIdle(t *testing.T) {
	d := depsForScreen("❯ \n") // reuse this file's existing helper for a single-pane idle screen; check its actual name first
	d.HookWorking = func(paneID string) bool { return true }
	snap, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil || len(snap.Agents) != 1 {
		t.Fatalf("snapshot: %+v, %v", snap, err)
	}
	if snap.Agents[0].State != state.Working {
		t.Errorf("state = %s, want working via hook events", snap.Agents[0].State)
	}
}

// Blocked must still win: a permission prompt appears mid-turn while hook
// events legitimately say the turn is running.
func TestHookWorkingDoesNotMaskBlocked(t *testing.T) {
	d := depsForScreen("Do you want to proceed?\n❯ 1. Yes\n")
	d.HookWorking = func(paneID string) bool { return true }
	snap, _ := RunOnce(state.Snapshot{}, d, time.Now())
	if snap.Agents[0].State != state.Blocked {
		t.Errorf("state = %s, want blocked", snap.Agents[0].State)
	}
}
```

Before writing, open `internal/poller/poller_test.go` and reuse its
actual fake-deps helper (the existing tests build `Deps` with fake
`ListPanes`/`Capture`); mirror that construction instead of
`depsForScreen` if no such helper exists.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/poller/`
Expected: FAIL — `Deps` has no field `HookWorking`.

- [ ] **Step 3: Implement the merge**

In `internal/poller/poller.go`, add to `Deps`:

```go
	// HookWorking reports hook-event working state for a pane (nil: disabled).
	// It is the primary working signal for instrumented sessions; scraping
	// remains for blocked detection and uninstrumented sessions.
	HookWorking func(paneID string) bool
```

In `RunOnce`, after the existing `anySubagentWorking` upgrade:

```go
	if st == detect.Idle && d.HookWorking != nil && d.HookWorking(p.ID) {
		st = detect.Working
	}
```

In `DefaultDeps()` and `config.PollerDeps()`, wire the production reader:

```go
	HookWorking: func(paneID string) bool {
		evs, err := hookevents.ReadPane(hookevents.DefaultDir(), paneID)
		if err != nil {
			return false
		}
		return hookevents.Working(evs)
	},
```

(Import `github.com/sngyo/tmux-radar/internal/hookevents` in both files.)

In `cmd/tmux-radar/main.go`, first lines of `cmdSidebar` and `cmdWatch`:

```go
	hookevents.GC(hookevents.DefaultDir(), time.Now(), 48*time.Hour)
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS (existing poller tests pass with nil HookWorking).

- [ ] **Step 5: Update README.md**

Add a "Claude Code hooks (recommended)" section: what `install-hooks`
does, that sessions pick hooks up on restart, that scraping remains as
fallback, `uninstall-hooks` to revert, and the event log location for
debugging (`~/.local/state/tmux-radar/events/`).

- [ ] **Step 6: Deploy and verify end-to-end**

```bash
go vet ./... && go test ./...
rm ~/.local/bin/tmux-radar && go build -o ~/.local/bin/tmux-radar ./cmd/tmux-radar
~/.local/bin/tmux-radar install-hooks
```

Then: user restarts the sidebar (runs outside tmux) and one Claude
session; in that session start a long background agent; confirm the
sidebar keeps the pane working after the main turn ends, with the
`(+N)`-style footer irrelevant. `tmux-radar watch` output plus the
pane's event log are the evidence to check.

- [ ] **Step 7: Commit**

```bash
git add internal/poller/ internal/config/ cmd/tmux-radar/ README.md
git commit -m "feat: hook events as the primary working signal in the poller"
```
