// Package poller performs one observation tick over all tmux panes.
package poller

import (
	"regexp"
	"time"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
)

// Deps are the injectable dependencies of a poll tick.
type Deps struct {
	ListPanes       func() ([]tmux.Pane, error)
	Capture         func(paneID string) (string, error)
	Rules           detect.Rules
	ProcessPatterns []*regexp.Regexp // matched against pane_current_command
	// active pane of the attached client; clears unseen-done marks and
	// drives the sidebar's focused-window highlight
	CurrentFocus func() (tmux.Focus, error)
}

// DefaultProcessPatterns matches Claude Code binaries: the plain name plus
// the version-named binaries its auto-updater installs (e.g. "2.1.199").
// Config defaults reuse these exact strings.
func DefaultProcessPatterns() []string {
	return []string{`^claude$`, `^[0-9]+\.[0-9]+\.[0-9]+$`}
}

// DefaultDeps returns production dependencies (real tmux, default rules).
func DefaultDeps() Deps {
	pats := make([]*regexp.Regexp, 0, len(DefaultProcessPatterns()))
	for _, p := range DefaultProcessPatterns() {
		pats = append(pats, regexp.MustCompile(p))
	}
	return Deps{
		ListPanes:       tmux.ListPanes,
		Capture:         tmux.CapturePane,
		Rules:           detect.DefaultRules(),
		ProcessPatterns: pats,
		CurrentFocus:    tmux.CurrentFocus,
	}
}

// RunOnce observes every agent pane and merges the result into prev.
// A pane that fails to capture (e.g. it died mid-tick) is skipped. Visiting
// a pane marks its completion as seen: if the attached client's active pane
// is one of the observed agents, its Done mark is cleared this tick.
func RunOnce(prev state.Snapshot, d Deps, now time.Time) (state.Snapshot, error) {
	panes, err := d.ListPanes()
	if err != nil {
		return prev, err
	}
	var obs []state.Observation
	for _, p := range panes {
		if !matches(d.ProcessPatterns, p.Command) {
			continue
		}
		screen, err := d.Capture(p.ID)
		if err != nil {
			continue
		}
		obs = append(obs, state.Observation{
			Pane: p, Kind: p.Command, State: d.Rules.Detect(screen),
			Subagents: detect.Subagents(screen),
		})
	}
	next := state.Apply(prev, obs, now)
	if d.CurrentFocus != nil {
		if focus, err := d.CurrentFocus(); err == nil {
			next.Focus = focus
			for i := range next.Agents {
				if next.Agents[i].PaneID == focus.PaneID {
					next.Agents[i].Done = false
				}
			}
		}
	}
	return next, nil
}

func matches(patterns []*regexp.Regexp, command string) bool {
	for _, re := range patterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}
