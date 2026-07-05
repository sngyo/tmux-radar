package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDetect(t *testing.T) {
	r := DefaultRules()
	cases := []struct {
		file string
		want State
	}{
		{"working.txt", Working},
		{"working_background_agent.txt", Working},
		{"working_monitor.txt", Working},
		{"blocked_permission.txt", Blocked},
		{"blocked_question.txt", Blocked},
		{"idle.txt", Idle},
	}
	for _, c := range cases {
		if got := r.Detect(fixture(t, c.file)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.file, got, c.want)
		}
	}
}

// A permission prompt shown while a spinner line is still on screen must win.
func TestBlockedBeatsWorking(t *testing.T) {
	screen := fixture(t, "working.txt") + "\n" + fixture(t, "blocked_permission.txt")
	if got := DefaultRules().Detect(screen); got != Blocked {
		t.Errorf("got %s, want blocked", got)
	}
}

// Conversation history above the bottom tail can quote dialog-like text
// (e.g. a past numbered user message rendered with "❯ 1."); it must not
// keep the agent looking blocked once the actual dialog is gone.
func TestDetectIgnoresHistoryAboveTail(t *testing.T) {
	history := "❯ 1. an old quoted option list\n" + strings.Repeat("plain output line\n", 40)
	screen := history + fixture(t, "idle.txt")
	if got := DefaultRules().Detect(screen); got != Idle {
		t.Errorf("got %s, want idle (history above the tail must not match)", got)
	}
}

func TestSubagentsScrapesTaskList(t *testing.T) {
	subs := Subagents(fixture(t, "working_background_agent.txt"))
	if len(subs) != 1 {
		t.Fatalf("subagents = %d, want 1: %+v", len(subs), subs)
	}
	want := Subagent{Type: "general-purpose", Title: "Refactor the billing report generator"}
	if subs[0] != want {
		t.Errorf("got %+v, want %+v", subs[0], want)
	}
}

func TestSubagentsIgnoresPlainConversation(t *testing.T) {
	screens := []string{
		"we discussed the general-purpose agent yesterday\n",
		fixture(t, "idle.txt"),
		fixture(t, "working.txt"),
	}
	for _, s := range screens {
		if subs := Subagents(s); len(subs) != 0 {
			t.Errorf("screen %q: unexpected subagents %+v", s[:40], subs)
		}
	}
}

func TestSubagentsMarksDoneEntries(t *testing.T) {
	screen := "  ● main\n  ✓ Explore  Map the config loaders\n  ○ general-purpose  Fix the flaky test\n"
	subs := Subagents(screen)
	if len(subs) != 2 {
		t.Fatalf("subagents = %d, want 2: %+v", len(subs), subs)
	}
	if !subs[0].Done || subs[0].Type != "Explore" {
		t.Errorf("first entry should be a done Explore agent: %+v", subs[0])
	}
	if subs[1].Done {
		t.Errorf("second entry must not be done: %+v", subs[1])
	}
}
