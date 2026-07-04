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
