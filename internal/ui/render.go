// Package ui renders the sidebar. Render is pure: it turns agents into
// rows; styling and mouse mapping happen in the bubbletea app.
package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/sngyo/tmux-agents/internal/state"
)

type RowKind int

const (
	RowHeader RowKind = iota
	RowAlert
	RowGroup
	RowWindow
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
	Width        int // pane width in columns; <=0 falls back to a sane default
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
	sort.SliceStable(agents, func(i, j int) bool {
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

	// Labels stretch to the pane width: agent rows consume 4 columns before
	// the title (icon, space, "└", space — e.g. "● └ ").
	labelW := v.Width - 4
	if v.Width <= 0 {
		labelW = 24
	}
	if labelW < 8 {
		labelW = 8
	}

	rows := []Row{{Text: fmt.Sprintf("AGENTS%14d agents", len(agents)), Kind: RowHeader}}
	if blocked > 0 {
		rows = append(rows, Row{
			Text: fmt.Sprintf("◆ %d blocked — C-t a to jump", blocked), Kind: RowAlert,
		})
	}
	rows = append(rows, agentRows(visible, v.Now, labelW)...)

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
		text := fmt.Sprintf("%s %shidden — %d %s", marker, sanitize(v.HiddenPrefix), len(hidden), plural)
		if hiddenBlocked > 0 {
			text += fmt.Sprintf(" ◆%d", hiddenBlocked)
		}
		rows = append(rows, Row{Text: text, Kind: RowFold, ToggleFold: true})
		if !v.FoldHidden {
			rows = append(rows, agentRows(hidden, v.Now, labelW)...)
		}
	}

	rows = append(rows, Row{Text: "C-t a jump · click jump · read-only", Kind: RowFooter})
	return rows
}

// agentRows emits group headers per session, one icon-less window anchor
// row per window ("  17:name"), and one hang row per agent pane
// ("● └ <pane title|pane N>"). The window and its panes are different
// hierarchy levels, so every pane hangs under its window's anchor —
// including a window's only pane — and agent count equals hang-row count.
func agentRows(agents []state.Agent, now time.Time, labelW int) []Row {
	var rows []Row
	lastSession := ""
	prevWindow := ""
	for _, a := range agents {
		session := sanitize(a.Session)
		if session != lastSession {
			rows = append(rows, Row{Text: "─ " + truncate(session, 24) + " ", Kind: RowGroup})
			lastSession = session
			prevWindow = ""
		}
		windowKey := fmt.Sprintf("%s:%d", session, a.WindowIndex)
		if windowKey != prevWindow {
			winLabel := fmt.Sprintf("%d:%s", a.WindowIndex, sanitize(a.WindowName))
			rows = append(rows, Row{
				Text: "  " + truncate(winLabel, labelW+2), Kind: RowWindow, PaneID: a.PaneID,
			})
			prevWindow = windowKey
		}
		title := sanitize(a.PaneTitle)
		if title == "" {
			title = fmt.Sprintf("pane %d", a.PaneIndex)
		}
		disp := a.Display(now)
		rows = append(rows, Row{
			Text: fmt.Sprintf("%s └ %s", icons[disp], truncate(title, labelW)),
			Kind: RowAgent, Display: disp, PaneID: a.PaneID,
		})
	}
	return rows
}

// sanitize removes control runes so a row can never span screen lines.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// truncate cuts s to at most w terminal columns, ellipsizing when needed.
func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)) > w-1 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}
