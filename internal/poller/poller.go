// Package poller performs one observation tick over all tmux panes.
package poller

import (
	"regexp"
	"time"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/state"
	"github.com/sngyo/tmux-agents/internal/tmux"
)

// Deps are the injectable dependencies of a poll tick.
type Deps struct {
	ListPanes       func() ([]tmux.Pane, error)
	Capture         func(paneID string) (string, error)
	Rules           detect.Rules
	ProcessPatterns []*regexp.Regexp // matched against pane_current_command
	DoneTTL         time.Duration
}

// DefaultDeps returns production dependencies (real tmux, default rules).
// Claude Code's auto-updater installs version-named binaries (e.g. "2.1.199"),
// so the defaults match both the plain name and a bare version string.
func DefaultDeps() Deps {
	return Deps{
		ListPanes: tmux.ListPanes,
		Capture:   tmux.CapturePane,
		Rules:     detect.DefaultRules(),
		ProcessPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^claude$`),
			regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`),
		},
		DoneTTL: 10 * time.Minute,
	}
}

// RunOnce observes every agent pane and merges the result into prev.
// A pane that fails to capture (e.g. it died mid-tick) is skipped.
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
		})
	}
	return state.Apply(prev, obs, now, d.DoneTTL), nil
}

func matches(patterns []*regexp.Regexp, command string) bool {
	for _, re := range patterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}
