// Package attention decides which agent the user should visit next.
package attention

import (
	"sort"
	"time"

	"github.com/sngyo/tmux-radar/internal/state"
)

// Queue returns agents needing attention: blocked first (oldest Since
// first), then done (oldest first). Working/idle agents are excluded.
func Queue(agents []state.Agent, now time.Time) []state.Agent {
	var blocked, done []state.Agent
	for _, a := range agents {
		switch a.Display(now) {
		case state.DisplayBlocked:
			blocked = append(blocked, a)
		case state.DisplayDone:
			done = append(done, a)
		}
	}
	bySince := func(s []state.Agent) {
		sort.Slice(s, func(i, j int) bool { return s[i].Since.Before(s[j].Since) })
	}
	bySince(blocked)
	bySince(done)
	return append(blocked, done...)
}

// Working returns working agents oldest-first: the fallback "tour" for
// jump when nothing needs attention.
func Working(agents []state.Agent, now time.Time) []state.Agent {
	var working []state.Agent
	for _, a := range agents {
		if a.Display(now) == state.DisplayWorking {
			working = append(working, a)
		}
	}
	sort.Slice(working, func(i, j int) bool { return working[i].Since.Before(working[j].Since) })
	return working
}

// Next picks the jump target. If the current pane is in the queue, the
// following entry is chosen (wrapping), so repeated presses cycle.
func Next(queue []state.Agent, currentPaneID string) (state.Agent, bool) {
	if len(queue) == 0 {
		return state.Agent{}, false
	}
	for i, a := range queue {
		if a.PaneID == currentPaneID {
			return queue[(i+1)%len(queue)], true
		}
	}
	return queue[0], true
}
