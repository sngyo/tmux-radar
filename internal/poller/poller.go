// Package poller performs one observation tick over all tmux panes.
package poller

import (
	"regexp"
	"time"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/hookevents"
	"github.com/sngyo/tmux-radar/internal/proc"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
)

// claudeKindName gates Claude Code-specific instrumentation (hook events,
// the subagent-list scrape) to panes of that kind.
const claudeKindName = "claude"

// Kind is one agent kind the poller can classify: its process-name patterns
// and the screen-content rules to detect its state.
type Kind struct {
	Name string
	// Process patterns are matched against pane_current_command first, then
	// against descendant process names for panes no kind claimed directly.
	Process []*regexp.Regexp
	Rules   detect.Rules
}

// Deps are the injectable dependencies of a poll tick.
type Deps struct {
	ListPanes func() ([]tmux.Pane, error)
	Capture   func(paneID string) (string, error)
	Kinds     []Kind
	// PaneDescendants resolves descendant process names for the given pane
	// root pids (nil: disabled). Wrapper launchers hide the agent from
	// pane_current_command — the npm Codex CLI shows up as "node" — so
	// unclaimed panes get a second chance via their process tree.
	PaneDescendants func(pids []int) (map[int][]string, error)
	// active pane of the attached client; clears unseen-done marks and
	// drives the sidebar's focused-window highlight
	CurrentFocus func() (tmux.Focus, error)
	// HookWorking reports hook-event working state for a pane (nil: disabled).
	// It is the primary working signal for instrumented sessions; scraping
	// remains for blocked detection and uninstrumented sessions.
	HookWorking func(paneID string) bool
	// GCHookEvents sweeps event-log files for panes that no longer exist
	// (nil: disabled). Called once per tick with every pane tmux currently
	// lists, not just claude-matching ones: a pane that switched away from
	// claude to a plain shell still exists and must not lose its log, but a
	// pane tmux no longer lists is gone and its log is dead weight that can
	// pin a reused pane id "working".
	GCHookEvents func(alive map[string]bool)
}

// DefaultProcessPatterns matches Claude Code binaries: the plain name plus
// the version-named binaries its auto-updater installs (e.g. "2.1.199").
// Config defaults reuse these exact strings.
func DefaultProcessPatterns() []string {
	return []string{`^claude$`, `^[0-9]+\.[0-9]+\.[0-9]+$`}
}

// DefaultCodexProcessPatterns matches the OpenAI Codex CLI. The native
// binary is named "codex"; the npm wrapper runs it under "node", which the
// descendant walk resolves against this same pattern.
func DefaultCodexProcessPatterns() []string {
	return []string{`^codex$`}
}

// DefaultKinds returns the built-in agent kinds.
func DefaultKinds() []Kind {
	return []Kind{
		{Name: "claude", Process: compile(DefaultProcessPatterns()), Rules: detect.DefaultRules()},
		{Name: "codex", Process: compile(DefaultCodexProcessPatterns()), Rules: detect.CodexRules()},
	}
}

func compile(patterns []string) []*regexp.Regexp {
	rs := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		rs = append(rs, regexp.MustCompile(p))
	}
	return rs
}

// DefaultDeps returns production dependencies (real tmux, default rules).
func DefaultDeps() Deps {
	return Deps{
		ListPanes:       tmux.ListPanes,
		Capture:         tmux.CapturePane,
		Kinds:           DefaultKinds(),
		PaneDescendants: proc.DescendantNames,
		CurrentFocus:    tmux.CurrentFocus,
		HookWorking: func(paneID string) bool {
			evs, err := hookevents.ReadPane(hookevents.DefaultDir(), paneID)
			if err != nil {
				return false
			}
			return hookevents.Working(evs, time.Now())
		},
		GCHookEvents: func(alive map[string]bool) {
			hookevents.GCDead(hookevents.DefaultDir(), alive)
		},
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
	if d.GCHookEvents != nil {
		alive := make(map[string]bool, len(panes))
		for _, p := range panes {
			alive[p.ID] = true
		}
		d.GCHookEvents(alive)
	}
	kinds := resolveKinds(panes, d)
	var obs []state.Observation
	for i, p := range panes {
		k := kinds[i]
		if k == nil {
			continue
		}
		screen, err := d.Capture(p.ID)
		if err != nil {
			continue
		}
		st := k.Rules.Detect(screen)
		var subs []detect.Subagent
		if k.Name == claudeKindName {
			subs = detect.Subagents(screen)
			// A tall pane can scroll the "Waiting for N background agents"
			// line above the detection tail (the input box is pinned to the
			// pane bottom), leaving no working pattern in view. The subagent
			// list next to the input box carries live runtime tails and is
			// the same signal, so it keeps the pane working. Blocked still
			// wins: a permission prompt can appear while subagents run.
			if st == detect.Idle && anySubagentWorking(subs) {
				st = detect.Working
			}
			// Hook events are the primary working signal for instrumented
			// sessions: they see UserPromptSubmit/Stop and background-agent
			// start/stop directly, so they catch a running turn or a live
			// subagent even when the screen text scrolled out of view or a
			// layout change broke the scrape. Blocked still wins above: a
			// permission prompt can appear while hook events say the turn
			// runs.
			if st == detect.Idle && d.HookWorking != nil && d.HookWorking(p.ID) {
				st = detect.Working
			}
		}
		obs = append(obs, state.Observation{
			Pane: p, Kind: k.Name, State: st,
			Subagents: subs,
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

// resolveKinds classifies each pane: pane_current_command first, then one
// batched descendant walk for the panes no kind claimed. A failed walk just
// leaves those panes unclassified for this tick.
func resolveKinds(panes []tmux.Pane, d Deps) []*Kind {
	res := make([]*Kind, len(panes))
	var pending []int
	var pids []int
	for i, p := range panes {
		if k := matchKind(d.Kinds, p.Command); k != nil {
			res[i] = k
			continue
		}
		if d.PaneDescendants != nil && p.PID > 0 {
			pending = append(pending, i)
			pids = append(pids, p.PID)
		}
	}
	if len(pending) == 0 {
		return res
	}
	names, err := d.PaneDescendants(pids)
	if err != nil {
		return res
	}
	for _, i := range pending {
		for _, name := range names[panes[i].PID] {
			if k := matchKind(d.Kinds, name); k != nil {
				res[i] = k
				break
			}
		}
	}
	return res
}

func matchKind(kinds []Kind, command string) *Kind {
	for i := range kinds {
		if matches(kinds[i].Process, command) {
			return &kinds[i]
		}
	}
	return nil
}

func anySubagentWorking(subs []detect.Subagent) bool {
	for _, s := range subs {
		if s.Working {
			return true
		}
	}
	return false
}

func matches(patterns []*regexp.Regexp, command string) bool {
	for _, re := range patterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}
