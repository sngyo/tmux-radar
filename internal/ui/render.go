// Package ui renders the sidebar. Render is pure: it turns agents into
// rows; styling and mouse mapping happen in the bubbletea app.
package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sngyo/tmux-agents/internal/state"
)

type RowKind int

const (
	RowHeader RowKind = iota
	RowAlert
	RowGroup
	RowAgent
	RowFold
	RowFooter
)

// Row is one sidebar line plus the metadata the app needs for styling
// and mouse-click resolution.
type Row struct {
	Text       string
	Kind       RowKind
	Display    state.Display
	PaneID     string // set on RowAgent
	ToggleFold bool   // set on RowFold
}

// ViewData is everything Render needs. Now is injected for testability.
type ViewData struct {
	Agents       []state.Agent
	FoldHidden   bool
	HiddenPrefix string
	Now          time.Time
}

var icons = map[state.Display]string{
	state.DisplayWorking: "●",
	state.DisplayBlocked: "◆",
	state.DisplayDone:    "✓",
	state.DisplayIdle:    "○",
}

// Render produces the full sidebar as rows, ordered
// session → window index → pane index, hidden windows folded at the bottom.
func Render(v ViewData) []Row {
	agents := append([]state.Agent(nil), v.Agents...)
	sort.Slice(agents, func(i, j int) bool {
		a, b := agents[i], agents[j]
		if a.Session != b.Session {
			return a.Session < b.Session
		}
		if a.WindowIndex != b.WindowIndex {
			return a.WindowIndex < b.WindowIndex
		}
		return a.PaneIndex < b.PaneIndex
	})

	var visible, hidden []state.Agent
	for _, a := range agents {
		if v.HiddenPrefix != "" && strings.HasPrefix(a.WindowName, v.HiddenPrefix) {
			hidden = append(hidden, a)
		} else {
			visible = append(visible, a)
		}
	}

	blocked := 0
	for _, a := range agents {
		if a.Display(v.Now) == state.DisplayBlocked {
			blocked++
		}
	}

	rows := []Row{{Text: fmt.Sprintf("AGENTS%14d agents", len(agents)), Kind: RowHeader}}
	if blocked > 0 {
		rows = append(rows, Row{
			Text: fmt.Sprintf("◆ %d blocked — C-t a to jump", blocked), Kind: RowAlert,
		})
	}
	rows = append(rows, agentRows(visible, v.Now)...)

	if len(hidden) > 0 {
		hiddenBlocked := 0
		for _, a := range hidden {
			if a.Display(v.Now) == state.DisplayBlocked {
				hiddenBlocked++
			}
		}
		marker, plural := "▸", "agents"
		if len(hidden) == 1 {
			plural = "agent"
		}
		if !v.FoldHidden {
			marker = "▾"
		}
		text := fmt.Sprintf("%s %shidden — %d %s", marker, v.HiddenPrefix, len(hidden), plural)
		if hiddenBlocked > 0 {
			text += fmt.Sprintf(" ◆%d", hiddenBlocked)
		}
		rows = append(rows, Row{Text: text, Kind: RowFold, ToggleFold: true})
		if !v.FoldHidden {
			rows = append(rows, agentRows(hidden, v.Now)...)
		}
	}

	rows = append(rows, Row{Text: "C-t a jump · click jump · read-only", Kind: RowFooter})
	return rows
}

// agentRows emits group headers per session and one row per agent;
// a window's 2nd+ pane hangs with "└" and its pane title when set.
func agentRows(agents []state.Agent, now time.Time) []Row {
	var rows []Row
	lastSession := ""
	prevWindow := ""
	for _, a := range agents {
		if a.Session != lastSession {
			rows = append(rows, Row{Text: "─ " + a.Session + " ", Kind: RowGroup})
			lastSession = a.Session
			prevWindow = ""
		}
		windowKey := fmt.Sprintf("%s:%d", a.Session, a.WindowIndex)
		label := a.WindowName
		if windowKey == prevWindow {
			title := a.PaneTitle
			if title == "" {
				title = fmt.Sprintf("pane %d", a.PaneIndex)
			}
			label = "└ " + title
		}
		prevWindow = windowKey
		disp := a.Display(now)
		rows = append(rows, Row{
			Text: fmt.Sprintf("%s %-14s %2d.%d %5s",
				icons[disp], truncate(label, 14), a.WindowIndex, a.PaneIndex, age(now.Sub(a.Since))),
			Kind: RowAgent, Display: disp, PaneID: a.PaneID,
		})
	}
	return rows
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// age formats a duration compactly: 45s, 12m, 3h.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}
