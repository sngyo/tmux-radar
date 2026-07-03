package state

import (
	"fmt"
	"strings"
	"time"
)

// Summary renders the tmux status-line counter, e.g.
// "#[fg=red,bold]◆1 #[fg=green]●2 #[fg=default]○2#[fg=default]".
// It returns "" when the snapshot is stale (sidebar not running) or empty,
// so the status segment disappears instead of lying.
func Summary(s Snapshot, now time.Time, staleTTL time.Duration) string {
	if s.Stale(now, staleTTL) || len(s.Agents) == 0 {
		return ""
	}
	var blocked, working, rest int
	for _, a := range s.Agents {
		switch a.Display(now) {
		case DisplayBlocked:
			blocked++
		case DisplayWorking:
			working++
		default: // done and idle share the quiet bucket
			rest++
		}
	}
	var b strings.Builder
	if blocked > 0 {
		fmt.Fprintf(&b, "#[fg=red,bold]◆%d ", blocked)
	}
	fmt.Fprintf(&b, "#[fg=green]●%d #[fg=default]○%d#[fg=default]", working, rest)
	return b.String()
}
