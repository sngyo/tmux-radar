// Command tmux-radar watches AI coding agents running in tmux panes.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sngyo/tmux-radar/internal/attention"
	"github.com/sngyo/tmux-radar/internal/config"
	"github.com/sngyo/tmux-radar/internal/poller"
	"github.com/sngyo/tmux-radar/internal/state"
	tmuxpkg "github.com/sngyo/tmux-radar/internal/tmux"
	"github.com/sngyo/tmux-radar/internal/ui"
)

const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run dispatches subcommands. Later tasks replace the stub cases.
func run(args []string, stdout io.Writer) int {
	cmd := "sidebar"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		fmt.Fprintf(stdout, "tmux-radar %s\n", version)
		return 0
	case "summary":
		return cmdSummary(stdout)
	case "watch":
		return cmdWatch(stdout)
	case "jump":
		return cmdJump()
	case "sidebar":
		popup := len(args) > 1 && args[1] == "--popup"
		return cmdSidebar(stdout, popup)
	case "popup":
		return cmdPopup(stdout)
	default:
		fmt.Fprintf(stdout, "usage: tmux-radar [sidebar|popup|summary|jump|watch|version]\n")
		return 2
	}
}

// cmdPopup opens the sidebar inside a tmux display-popup. The popup closes
// when the sidebar exits: esc/q/enter, or automatically after a click jump.
func cmdPopup(stdout io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stdout, "popup error: %v\n", err)
		return 1
	}
	cfg, _ := config.Load(config.DefaultConfigPath()) // bad config: defaults still give a usable popup
	if err := exec.Command("tmux", popupArgs(exe, cfg.PopupWidth, cfg.PopupHeight)...).Run(); err != nil {
		fmt.Fprintf(stdout, "popup error: %v\n", err)
		return 1
	}
	return 0
}

// popupArgs builds the display-popup invocation wrapping `sidebar --popup`.
func popupArgs(exe, width, height string) []string {
	return []string{"display-popup", "-E", "-w", width, "-h", height,
		fmt.Sprintf("%q sidebar --popup", exe)}
}

// staleFor derives the snapshot staleness window from the poll interval:
// at least 3s, and wide enough to span three missed polls so a slow
// configured cadence does not starve summary/jump readers.
func staleFor(pollIntervalMS int) time.Duration {
	ttl := 3 * time.Duration(pollIntervalMS) * time.Millisecond
	if ttl < 3*time.Second {
		ttl = 3 * time.Second
	}
	return ttl
}

func cmdSummary(stdout io.Writer) int {
	s, err := state.Load(state.DefaultPath())
	if err != nil {
		return 0 // no state yet -> empty segment, success for status bar
	}
	cfg, _ := config.Load(config.DefaultConfigPath()) // errors intentionally ignored: status line must never print warnings
	fmt.Fprint(stdout, state.Summary(s, time.Now(), staleFor(cfg.PollIntervalMS)))
	return 0
}

// cmdWatch runs the poller headlessly (P1 usage and debugging).
func cmdWatch(stdout io.Writer) int {
	cfg, cfgErr := config.Load(config.DefaultConfigPath())
	deps, err := cfg.PollerDeps()
	if err != nil {
		deps = poller.DefaultDeps() // bad regex in config: fall back
	}
	if w := firstNonNil(cfgErr, err); w != nil {
		fmt.Fprintf(stdout, "config warning: %v (using defaults)\n", w)
	}
	path := state.DefaultPath()
	var snap state.Snapshot
	interval := time.Duration(cfg.PollIntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = time.Second // zero/negative would spin or stall the poll loop
	}
	fmt.Fprintf(stdout, "watching; writing %s (ctrl-c to stop)\n", path)
	for {
		next, err := poller.RunOnce(snap, deps, time.Now())
		if err != nil {
			fmt.Fprintf(stdout, "poll error: %v\n", err)
		} else {
			snap = next
			if err := state.Save(path, snap); err != nil {
				fmt.Fprintf(stdout, "save error: %v\n", err)
			}
		}
		time.Sleep(interval)
	}
}

// cmdJump moves the tmux client to the next agent needing attention.
// Stale state (sidebar/watch not running) triggers one inline poll.
func cmdJump() int {
	now := time.Now()
	cfg, cfgErr := config.Load(config.DefaultConfigPath())
	deps, err := cfg.PollerDeps()
	if err != nil {
		deps = poller.DefaultDeps() // bad regex in config: fall back
	}
	if w := firstNonNil(cfgErr, err); w != nil {
		_ = tmuxpkg.DisplayMessage("tmux-radar: config warning: " + w.Error())
	}
	snap, err := state.Load(state.DefaultPath())
	if err != nil || snap.Stale(now, staleFor(cfg.PollIntervalMS)) {
		// Reuse the (possibly stale) snapshot as prev so working->idle
		// transitions can still arm the done overlay on this one-shot poll.
		snap, err = poller.RunOnce(snap, deps, now)
		if err != nil {
			_ = tmuxpkg.DisplayMessage("tmux-radar: " + err.Error())
			return 1
		}
	}
	queue := attention.Queue(snap.Agents, now)
	if len(queue) == 0 {
		// Nothing needs attention: fall back to touring working agents.
		queue = attention.Working(snap.Agents, now)
	}
	current, _ := tmuxpkg.CurrentPaneID()
	target, ok := attention.Next(queue, current)
	if !ok {
		_ = tmuxpkg.DisplayMessage("tmux-radar: nothing needs attention")
		return 0
	}
	// Same-window pane hops are visually subtle and a one-entry queue can
	// target the pane we are already on — always say what happened.
	where := fmt.Sprintf("%d:%s (%s)", target.WindowIndex,
		escapeFormat(target.WindowName), target.Display(now))
	if target.PaneID == current {
		_ = tmuxpkg.DisplayMessage("tmux-radar: already at " + where)
		return 0
	}
	if err := tmuxpkg.JumpTo(target.Session, target.WindowIndex, target.PaneID); err != nil {
		_ = tmuxpkg.DisplayMessage("tmux-radar: jump failed: " + err.Error())
		return 1
	}
	_ = tmuxpkg.DisplayMessage("tmux-radar: → " + where)
	return 0
}

// escapeFormat doubles '#' so window names cannot inject tmux format
// expansions into display-message output.
func escapeFormat(s string) string {
	return strings.ReplaceAll(s, "#", "##")
}

// cmdSidebar runs the bubbletea sidebar app in the current terminal.
func cmdSidebar(stdout io.Writer, popup bool) int {
	cfg, cfgErr := config.Load(config.DefaultConfigPath())
	deps, err := cfg.PollerDeps()
	if err != nil {
		deps = poller.DefaultDeps() // bad regex in config: fall back
	}
	if cfgErr != nil || err != nil {
		fmt.Fprintf(stdout, "config warning: %v\n", firstNonNil(cfgErr, err))
	}
	interval := time.Duration(cfg.PollIntervalMS) * time.Millisecond
	app := ui.NewApp(deps, cfg.FocusReturnCmd, cfg.HiddenPrefix, interval, popup)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stdout, "sidebar error: %v\n", err)
		return 1
	}
	return 0
}

// firstNonNil returns the first non-nil error, or nil if all are nil.
func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
