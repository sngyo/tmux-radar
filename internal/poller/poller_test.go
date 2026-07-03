package poller

import (
	"errors"
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
		Rules:        detect.DefaultRules(),
		ProcessNames: []string{"claude"},
		DoneTTL:      10 * time.Minute,
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
		Rules:        detect.DefaultRules(),
		ProcessNames: []string{"claude"},
		DoneTTL:      10 * time.Minute,
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 0 {
		t.Errorf("agents = %d, want 0", len(s.Agents))
	}
}
