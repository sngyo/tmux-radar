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

// GCDead complements the mtime-based GC: a pane can close (and its id be
// reused) faster than 48h, so a stale log for a dead pane must be swept as
// soon as the poller notices the pane is gone, not 48h later.
func TestGCDeadRemovesFilesForPanesNotInAliveSet(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, "%dead", "Stop", nil, t0); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, "%live", "Stop", nil, t0); err != nil {
		t.Fatal(err)
	}
	// A non-jsonl file in the same directory must be left alone.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	GCDead(dir, map[string]bool{"%live": true})

	if _, err := os.Stat(filepath.Join(dir, "%dead.jsonl")); !os.IsNotExist(err) {
		t.Error("dead pane's file must be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "%live.jsonl")); err != nil {
		t.Error("live pane's file must survive")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "notes.txt")); err != nil || string(b) != "keep me" {
		t.Error("non-jsonl file must be untouched")
	}
}

func TestDefaultDirEmptyWhenHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	if got := DefaultDir(); got != "" {
		t.Errorf("DefaultDir() = %q, want empty string when HOME is unset (a relative fallback would scatter events/ into cwd)", got)
	}
}

// Append must fail rather than silently writing into a relative "" dir,
// which would scatter event files into whatever cwd the hook subcommand
// happens to run in.
func TestAppendWithEmptyDirErrors(t *testing.T) {
	if err := Append("", "%1", "Stop", nil, t0); err == nil {
		t.Fatal("Append with empty dir must error")
	}
}
