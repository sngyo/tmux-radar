package poller

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/state"
	"github.com/sngyo/tmux-agents/internal/tmux"
)

var errPaneGone = errors.New("pane gone")

func TestRunOnceFiltersAndDetects(t *testing.T) {
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{
				{ID: "%1", Session: "main", WindowName: "api", Command: "claude"},
				{ID: "%2", Session: "main", WindowName: "web", Command: "zsh"},
			}, nil
		},
		Capture: func(paneID string) (string, error) {
			return "✶ Cerebrating… (esc to interrupt)", nil
		},
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentPane:     func() (string, error) { return "%elsewhere", nil },
	}
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s, err := RunOnce(state.Snapshot{}, d, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 {
		t.Fatalf("agents = %d, want 1 (zsh pane must be ignored)", len(s.Agents))
	}
	if s.Agents[0].State != detect.Working || s.Agents[0].Kind != "claude" {
		t.Errorf("got %+v", s.Agents[0])
	}
}

func TestRunOnceSkipsFailedCaptures(t *testing.T) {
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Command: "claude"}}, nil
		},
		Capture: func(string) (string, error) {
			return "", errPaneGone
		},
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentPane:     func() (string, error) { return "%elsewhere", nil },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 0 {
		t.Errorf("agents = %d, want 0", len(s.Agents))
	}
}

func TestRunOnceClearsDoneOnVisitedPane(t *testing.T) {
	prev := state.Snapshot{Agents: []state.Agent{
		{PaneID: "%1", State: detect.Idle, Done: true},
		{PaneID: "%2", State: detect.Idle, Done: true},
	}}
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Command: "claude"}, {ID: "%2", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return "idle prompt", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentPane:     func() (string, error) { return "%1", nil },
	}
	s, err := RunOnce(prev, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range s.Agents {
		switch a.PaneID {
		case "%1":
			if a.Done {
				t.Error("visited pane %1 must have Done cleared")
			}
		case "%2":
			if !a.Done {
				t.Error("unvisited pane %2 must keep Done")
			}
		}
	}
}

func TestDefaultPatternsMatchVersionedClaudeBinary(t *testing.T) {
	pats := DefaultDeps().ProcessPatterns
	for _, cmd := range []string{"claude", "2.1.185"} {
		if !matches(pats, cmd) {
			t.Errorf("%q should match default patterns", cmd)
		}
	}
	if matches(pats, "zsh") {
		t.Error("zsh must not match default patterns")
	}
}
