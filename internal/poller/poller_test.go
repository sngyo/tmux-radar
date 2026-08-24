package poller

import (
	"errors"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
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
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
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
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
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
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%1"}, nil },
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

func TestRunOncePromotesIdleToWorkingWhileSubagentsRun(t *testing.T) {
	// In a tall pane the "Waiting for N background agents to finish" line can
	// sit above the detection tail (the input box is pinned to the pane
	// bottom), so no working pattern is visible. The subagent list rendered
	// next to the input box still carries live runtime tails and must keep
	// the pane working.
	screen := "  kosuke.s@example.com\n" +
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent\n" +
		"\n" +
		"  ⏺ main\n" +
		"  ◯ claude   investigate the branch                 5m 24s · ↓ 125.8k tokens\n" +
		"  ◯ Explore  explore the MCP surface                 5m 9s · ↓ 220.6k tokens\n"
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return screen, nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 {
		t.Fatalf("want 1 agent, got %+v", s.Agents)
	}
	if s.Agents[0].State != detect.Working {
		t.Errorf("agent state = %s, want working while subagents run", s.Agents[0].State)
	}
}

func TestRunOnceKeepsIdleWhenAllSubagentsDone(t *testing.T) {
	// Finished subagents (✓ rows) linger in the list; they must not keep the
	// pane working, or the unseen-done marker would never arm.
	screen := "  ⏺ main\n" +
		"  ✓ claude   investigate the branch                 5m 24s · ↓ 125.8k tokens\n"
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return screen, nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 {
		t.Fatalf("want 1 agent, got %+v", s.Agents)
	}
	if s.Agents[0].State != detect.Idle {
		t.Errorf("agent state = %s, want idle when all subagents are done", s.Agents[0].State)
	}
}

func TestHookWorkingUpgradesIdle(t *testing.T) {
	// Hook events say working; the screen shows an idle prompt (e.g. the
	// wait line scrolled away AND the subagent list failed to parse). Hook
	// wins.
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return "❯ \n", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
		HookWorking:     func(paneID string) bool { return true },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil || len(s.Agents) != 1 {
		t.Fatalf("snapshot: %+v, %v", s, err)
	}
	if s.Agents[0].State != detect.Working {
		t.Errorf("state = %s, want working via hook events", s.Agents[0].State)
	}
}

func TestHookWorkingDoesNotMaskBlocked(t *testing.T) {
	// Blocked must still win: a permission prompt appears mid-turn while
	// hook events legitimately say the turn is running.
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return "Do you want to proceed?\n❯ 1. Yes\n", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
		HookWorking:     func(paneID string) bool { return true },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil || len(s.Agents) != 1 {
		t.Fatalf("snapshot: %+v, %v", s, err)
	}
	if s.Agents[0].State != detect.Blocked {
		t.Errorf("state = %s, want blocked", s.Agents[0].State)
	}
}

// GCHookEvents must see every listed pane (not just claude-matching ones):
// a pane that switched away from claude to a plain shell still exists, but
// a pane tmux no longer lists is gone and its log is dead weight that must
// be swept regardless of what command last ran there.
func TestRunOnceCallsGCHookEventsWithAllListedPaneIDs(t *testing.T) {
	var gotAlive map[string]bool
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{
				{ID: "%1", Command: "claude"},
				{ID: "%2", Command: "zsh"},
			}, nil
		},
		Capture:         func(string) (string, error) { return "idle prompt", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
		GCHookEvents:    func(alive map[string]bool) { gotAlive = alive },
	}
	if _, err := RunOnce(state.Snapshot{}, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"%1": true, "%2": true}
	if !reflect.DeepEqual(gotAlive, want) {
		t.Errorf("alive set = %v, want %v (every listed pane, not just claude ones)", gotAlive, want)
	}
}

// A nil GCHookEvents (the zero value used throughout the other tests here)
// must not panic RunOnce; it simply disables the sweep.
func TestRunOnceToleratesNilGCHookEvents(t *testing.T) {
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return "idle prompt", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
	}
	if _, err := RunOnce(state.Snapshot{}, d, time.Now()); err != nil {
		t.Fatal(err)
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

func TestRunOnceRecordsFocus(t *testing.T) {
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", WindowIndex: 14, Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return "idle prompt", nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus: func() (tmux.Focus, error) {
			return tmux.Focus{Session: "main", WindowIndex: 14, PaneID: "%1"}, nil
		},
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := tmux.Focus{Session: "main", WindowIndex: 14, PaneID: "%1"}
	if s.Focus != want {
		t.Errorf("snapshot focus = %+v, want %+v", s.Focus, want)
	}
}

func TestRunOnceScrapesSubagents(t *testing.T) {
	screen := "✳ Waiting for 1 background agent to finish\n" +
		"  ● main\n" +
		"  ○ general-purpose  Refactor the billing report generator\n"
	d := Deps{
		ListPanes: func() ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", Session: "main", Command: "claude"}}, nil
		},
		Capture:         func(string) (string, error) { return screen, nil },
		Rules:           detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{regexp.MustCompile("^claude$")},
		CurrentFocus:    func() (tmux.Focus, error) { return tmux.Focus{PaneID: "%elsewhere"}, nil },
	}
	s, err := RunOnce(state.Snapshot{}, d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 || len(s.Agents[0].Subagents) != 1 {
		t.Fatalf("want 1 agent with 1 subagent, got %+v", s.Agents)
	}
	sub := s.Agents[0].Subagents[0]
	if sub.Type != "general-purpose" || sub.Title != "Refactor the billing report generator" {
		t.Errorf("got %+v", sub)
	}
	if s.Agents[0].State != detect.Working {
		t.Errorf("agent state = %s, want working", s.Agents[0].State)
	}
}
