// Package tmux is a thin wrapper over the tmux CLI.
package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Pane describes one tmux pane as reported by list-panes.
type Pane struct {
	ID          string // immutable pane id, e.g. "%37" — the tracking key
	Session     string
	WindowIndex int
	WindowName  string
	PaneIndex   int
	Title       string // user- or app-set pane title ("" if unset)
	Command     string // pane_current_command, e.g. "claude"
	PID         int    // pane_pid: the pane's root process (usually the shell)
	// DimStyle is the style radar last applied to this pane for the
	// [PENDING] dim ("" if none), read back from the @radar_dim pane option
	// so reconciliation stays correct across radar restarts.
	DimStyle string
}

// PendingMarker parks a pane: a title containing it (case-insensitively)
// grays the sidebar row out and dims the pane itself.
const PendingMarker = "[PENDING]"

// IsPending reports whether a pane title carries the PendingMarker.
func IsPending(title string) bool {
	return strings.Contains(strings.ToUpper(title), PendingMarker)
}

// panesFormat uses tabs as field separators; tmux names never sanely contain tabs.
const panesFormat = "#{pane_id}\t#{session_name}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_title}\t#{pane_current_command}\t#{pane_pid}\t#{@radar_dim}"

// ListPanes returns every pane on the local tmux server.
func ListPanes() ([]Pane, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", panesFormat).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	return ParsePanes(string(out))
}

// ParsePanes parses list-panes output. Malformed lines are skipped.
func ParsePanes(out string) ([]Pane, error) {
	var panes []Pane
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			continue
		}
		wi, err1 := strconv.Atoi(f[2])
		pi, err2 := strconv.Atoi(f[4])
		pid, err3 := strconv.Atoi(f[7])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		panes = append(panes, Pane{
			ID: f[0], Session: f[1], WindowIndex: wi, WindowName: f[3],
			PaneIndex: pi, Title: f[5], Command: f[6], PID: pid, DimStyle: f[8],
		})
	}
	return panes, nil
}

// SetPaneDim applies style to a pane's background/default colours (both
// window-style and window-active-style, so the pane stays dimmed while
// focused) and records it in @radar_dim; an empty style restores the
// inherited styles. One tmux invocation per change.
func SetPaneDim(paneID, style string) error {
	return exec.Command("tmux", paneDimArgs(paneID, style)...).Run()
}

func paneDimArgs(paneID, style string) []string {
	var args []string
	for i, opt := range []string{"window-style", "window-active-style", "@radar_dim"} {
		if i > 0 {
			args = append(args, ";")
		}
		if style == "" {
			args = append(args, "set-option", "-pu", "-t", paneID, opt)
		} else {
			args = append(args, "set-option", "-p", "-t", paneID, opt, style)
		}
	}
	return args
}

// CapturePane returns the visible screen content of a pane as plain text.
func CapturePane(paneID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", paneID).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane %s: %w", paneID, err)
	}
	return string(out), nil
}

// Focus identifies the attached client's active pane and the window it
// lives in; the sidebar highlights that window's rows.
type Focus struct {
	Session     string `json:"session"`
	WindowIndex int    `json:"window_index"`
	PaneID      string `json:"pane_id"`
}

// focusFormat feeds ParseFocus via list-panes. display-message is avoided:
// without a client context it resolves to "the most recently used session",
// which can be a detached one; list-panes lets us require an attached session.
const focusFormat = "#{session_attached}\t#{session_activity}\t#{session_name}\t#{window_index}\t#{pane_id}\t#{window_active}\t#{pane_active}"

// CurrentFocus returns the active pane of the most recently active
// attached session.
func CurrentFocus() (Focus, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", focusFormat).Output()
	if err != nil {
		return Focus{}, fmt.Errorf("tmux list-panes: %w", err)
	}
	return ParseFocus(string(out))
}

// ParseFocus picks the focused pane from list-panes output in focusFormat:
// the active pane of the active window of an attached session, preferring
// the session with the most recent activity. Malformed lines are skipped.
func ParseFocus(out string) (Focus, error) {
	best := Focus{}
	bestActivity := int64(-1)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 7 || f[0] == "0" || f[5] != "1" || f[6] != "1" {
			continue
		}
		activity, err1 := strconv.ParseInt(f[1], 10, 64)
		wi, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		if activity > bestActivity {
			bestActivity = activity
			best = Focus{Session: f[2], WindowIndex: wi, PaneID: f[4]}
		}
	}
	if bestActivity < 0 {
		return Focus{}, fmt.Errorf("no attached session in list-panes output")
	}
	return best, nil
}

// CurrentPaneID returns the active pane id of the current client.
func CurrentPaneID() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// JumpTo focuses a pane across sessions: switch client, select window,
// select pane — chained in a single tmux invocation.
func JumpTo(session string, windowIndex int, paneID string) error {
	target := fmt.Sprintf("%s:%d", session, windowIndex)
	return exec.Command("tmux",
		"switch-client", "-t", session, ";",
		"select-window", "-t", target, ";",
		"select-pane", "-t", paneID,
	).Run()
}

// MoveWindow reorders a window within its session: after=true inserts it
// right after dst, otherwise right before. tmux shifts the windows in
// between (and compacts indexes when renumber-windows is on).
func MoveWindow(session string, src, dst int, after bool) error {
	return exec.Command("tmux", moveWindowArgs(session, src, dst, after)...).Run()
}

func moveWindowArgs(session string, src, dst int, after bool) []string {
	flag := "-b"
	if after {
		flag = "-a"
	}
	return []string{"move-window", flag,
		"-s", fmt.Sprintf("%s:%d", session, src),
		"-t", fmt.Sprintf("%s:%d", session, dst)}
}

// DisplayMessage shows a transient message in the tmux status line.
func DisplayMessage(msg string) error {
	return exec.Command("tmux", "display-message", msg).Run()
}
