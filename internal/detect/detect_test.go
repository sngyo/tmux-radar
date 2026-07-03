package detect

import (
	"os"
	"path/filepath"
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
