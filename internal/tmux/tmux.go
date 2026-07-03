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
}

// panesFormat uses tabs as field separators; tmux names never sanely contain tabs.
const panesFormat = "#{pane_id}\t#{session_name}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_title}\t#{pane_current_command}"

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
		if len(f) != 7 {
			continue
		}
		wi, err1 := strconv.Atoi(f[2])
		pi, err2 := strconv.Atoi(f[4])
		if err1 != nil || err2 != nil {
			continue
		}
		panes = append(panes, Pane{
			ID: f[0], Session: f[1], WindowIndex: wi, WindowName: f[3],
			PaneIndex: pi, Title: f[5], Command: f[6],
		})
	}
	return panes, nil
}

// CapturePane returns the visible screen content of a pane as plain text.
func CapturePane(paneID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", paneID).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane %s: %w", paneID, err)
	}
	return string(out), nil
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

// DisplayMessage shows a transient message in the tmux status line.
func DisplayMessage(msg string) error {
	return exec.Command("tmux", "display-message", msg).Run()
}
