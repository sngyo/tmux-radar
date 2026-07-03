// Command tmux-agents watches AI coding agents running in tmux panes.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sngyo/tmux-agents/internal/attention"
	"github.com/sngyo/tmux-agents/internal/poller"
	"github.com/sngyo/tmux-agents/internal/state"
	tmuxpkg "github.com/sngyo/tmux-agents/internal/tmux"
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
		fmt.Fprintf(stdout, "tmux-agents %s\n", version)
		return 0
	case "summary":
		return cmdSummary(stdout)
	case "watch":
		return cmdWatch(stdout)
	case "jump":
		return cmdJump()
	case "sidebar":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", cmd)
		return 1
	default:
		fmt.Fprintf(stdout, "usage: tmux-agents [sidebar|summary|jump|watch|version]\n")
		return 2
	}
}

func cmdSummary(stdout io.Writer) int {
	s, err := state.Load(state.DefaultPath())
	if err != nil {
		return 0 // no state yet -> empty segment, success for status bar
	}
	fmt.Fprint(stdout, state.Summary(s, time.Now(), 3*time.Second))
	return 0
}

// cmdWatch runs the poller headlessly (P1 usage and debugging).
func cmdWatch(stdout io.Writer) int {
	deps := poller.DefaultDeps()
	path := state.DefaultPath()
	var snap state.Snapshot
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
		time.Sleep(time.Second)
	}
}

// cmdJump moves the tmux client to the next agent needing attention.
// Stale state (sidebar/watch not running) triggers one inline poll.
func cmdJump() int {
	now := time.Now()
	snap, err := state.Load(state.DefaultPath())
	if err != nil || snap.Stale(now, 3*time.Second) {
		// Reuse the (possibly stale) snapshot as prev so working->idle
		// transitions can still arm the done overlay on this one-shot poll.
		snap, err = poller.RunOnce(snap, poller.DefaultDeps(), now)
		if err != nil {
			_ = tmuxpkg.DisplayMessage("tmux-agents: " + err.Error())
			return 1
		}
	}
	queue := attention.Queue(snap.Agents, now)
	current, _ := tmuxpkg.CurrentPaneID()
	target, ok := attention.Next(queue, current)
	if !ok {
		_ = tmuxpkg.DisplayMessage("tmux-agents: nothing needs attention")
		return 0
	}
	if err := tmuxpkg.JumpTo(target.Session, target.WindowIndex, target.PaneID); err != nil {
		_ = tmuxpkg.DisplayMessage("tmux-agents: jump failed: " + err.Error())
		return 1
	}
	return 0
}
