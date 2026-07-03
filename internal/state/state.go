// Package state holds the agent model, state transitions, and persistence.
package state

import (
	"time"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/tmux"
)

// Agent is one observed agent, keyed by the immutable tmux pane id.
type Agent struct {
	PaneID      string       `json:"pane_id"`
	Session     string       `json:"session"`
	WindowIndex int          `json:"window_index"`
	WindowName  string       `json:"window_name"`
	PaneIndex   int          `json:"pane_index"`
	PaneTitle   string       `json:"pane_title,omitempty"`
	Kind        string       `json:"kind"`
	State       detect.State `json:"state"`
	Since       time.Time    `json:"since"`
	DoneUntil   time.Time    `json:"done_until,omitempty"`
}

// Display is what the UI shows; it layers the "done" overlay on top of State.
type Display string

const (
	DisplayWorking Display = "working"
	DisplayBlocked Display = "blocked"
	DisplayDone    Display = "done"
	DisplayIdle    Display = "idle"
)

// Display resolves the effective display state at a point in time.
func (a Agent) Display(now time.Time) Display {
	switch a.State {
	case detect.Blocked:
		return DisplayBlocked
	case detect.Working:
		return DisplayWorking
	}
	if a.DoneUntil.After(now) {
		return DisplayDone
	}
	return DisplayIdle
}

// Observation is one pane observed during a poll tick.
type Observation struct {
	Pane  tmux.Pane
	Kind  string
	State detect.State
}

// Snapshot is the full state written to state.json each tick.
type Snapshot struct {
	GeneratedAt time.Time `json:"generated_at"`
	Agents      []Agent   `json:"agents"`
}

// Stale reports whether the snapshot is too old to trust.
func (s Snapshot) Stale(now time.Time, ttl time.Duration) bool {
	return now.Sub(s.GeneratedAt) > ttl
}

// Apply merges observations into the previous snapshot. Vanished panes are
// dropped; a working→idle transition arms the done overlay for doneTTL.
func Apply(prev Snapshot, obs []Observation, now time.Time, doneTTL time.Duration) Snapshot {
	prevByID := make(map[string]Agent, len(prev.Agents))
	for _, a := range prev.Agents {
		prevByID[a.PaneID] = a
	}
	next := Snapshot{GeneratedAt: now, Agents: make([]Agent, 0, len(obs))}
	for _, o := range obs {
		a := Agent{
			PaneID: o.Pane.ID, Session: o.Pane.Session,
			WindowIndex: o.Pane.WindowIndex, WindowName: o.Pane.WindowName,
			PaneIndex: o.Pane.PaneIndex, PaneTitle: o.Pane.Title,
			Kind: o.Kind, State: o.State, Since: now,
		}
		if p, ok := prevByID[o.Pane.ID]; ok {
			if p.State == o.State {
				a.Since = p.Since
			}
			a.DoneUntil = p.DoneUntil
			if p.State == detect.Working && o.State == detect.Idle {
				a.DoneUntil = now.Add(doneTTL)
			}
			if o.State == detect.Working {
				a.DoneUntil = time.Time{}
			}
		}
		next.Agents = append(next.Agents, a)
	}
	return next
}
