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

	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
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
	RowSpacer   // blank separator between window blocks; inert to clicks
	RowSubagent // background task nested under its agent; clicks jump to the parent pane
)

// Row is one sidebar line plus the metadata the app needs for styling
// and mouse-click resolution.
type Row struct {
	Text       string
	Kind       RowKind
	Display    state.Display
	PaneID     string // set on RowAgent
	ToggleFold bool   // set on RowFold
	Current    bool   // row belongs to the attached client's active window
	Pending    bool   // pane title carries the [PENDING] marker: gray the row out
}

// ViewData is everything Render needs. Now is injected for testability.
type ViewData struct {
	Agents       []state.Agent
	FoldHidden   bool
	HiddenPrefix string
	Now          time.Time
	Width        int        // pane width in columns; <=0 falls back to a sane default
	Focus        tmux.Focus // active window; its rows get the current-row highlight
	Frame        int        // animation frame; spins the working glyph
	Popup        bool       // popup mode: the footer hints keyboard navigation
}

var icons = map[state.Display]string{
	state.DisplayWorking: "●", // fallback; animated via spinnerFrames when rendering
	state.DisplayBlocked: "◆",
	state.DisplayDone:    "✓",
	state.DisplayIdle:    "○",
}

// spinnerFrames animate the working glyph so running agents read as alive.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// sortAgents copies agents into display order: session → window index →
// pane index. Shared by Render and the popup's initial-selection seeding
// so the two notions of "first" cannot drift apart.
func sortAgents(agents []state.Agent) []state.Agent {
	sorted := append([]state.Agent(nil), agents...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Session != b.Session {
			return a.Session < b.Session
		}
		if a.WindowIndex != b.WindowIndex {
			return a.WindowIndex < b.WindowIndex
		}
		return a.PaneIndex < b.PaneIndex
	})
	return sorted
}

// Render produces the full sidebar as rows, ordered
// session → window index → pane index, hidden windows folded at the bottom.
func Render(v ViewData) []Row {
	agents := sortAgents(v.Agents)

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

	// Labels stretch to the pane width: agent rows consume 5 columns before
	// the title (pad, icon, space, branch, space — e.g. " ● └ ").
	labelW := v.Width - 5
	if v.Width <= 0 {
		labelW = 24
	}
	if labelW < 8 {
		labelW = 8
	}

	rows := []Row{{Text: fmt.Sprintf("AGENTS%14d agents", len(agents)), Kind: RowHeader}}
	if blocked > 0 {
		hint := "C-t a to jump"
		if v.Popup {
			hint = "a to jump" // the popup handles the key itself; no prefix
		}
		rows = append(rows, Row{
			Text: fmt.Sprintf("◆ %d blocked — %s", blocked, hint), Kind: RowAlert,
		})
	}
	rows = append(rows, agentRows(visible, v, labelW)...)

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
		rows = appendSpaced(rows, Row{Text: text, Kind: RowFold, ToggleFold: true})
		if !v.FoldHidden {
			rows = append(rows, agentRows(hidden, v, labelW)...)
		}
	}

	footer := "C-t a jump · click jump"
	if v.Popup {
		footer = "n/p move · enter keep · esc back"
	}
	rows = append(rows, Row{Text: footer, Kind: RowFooter})
	return rows
}

// agentRows emits group headers per session, one icon-less window anchor
// row per window ("  17:name"), and one hang row per agent pane
// ("● └ <pane title|pane N>"). The window and its panes are different
// hierarchy levels, so every pane hangs under its window's anchor —
// including a window's only pane — and agent count equals hang-row count.
func agentRows(agents []state.Agent, v ViewData, labelW int) []Row {
	var rows []Row
	lastSession := ""
	prevWindow := ""
	for i, a := range agents {
		session := sanitize(a.Session)
		if session != lastSession {
			rows = appendSpaced(rows, Row{Text: groupRule(session, labelW+5), Kind: RowGroup})
			lastSession = session
			prevWindow = ""
		}
		current := v.Focus.PaneID != "" && a.Session == v.Focus.Session && a.WindowIndex == v.Focus.WindowIndex
		windowKey := fmt.Sprintf("%s:%d", session, a.WindowIndex)
		if windowKey != prevWindow {
			winLabel := fmt.Sprintf("%d:%s", a.WindowIndex, sanitize(a.WindowName))
			rows = appendSpaced(rows, Row{
				Text: "  " + truncate(winLabel, labelW+3), Kind: RowWindow, PaneID: a.PaneID,
				Current: current,
			})
			prevWindow = windowKey
		}
		title := sanitize(a.PaneTitle)
		if title == "" {
			title = fmt.Sprintf("pane %d", a.PaneIndex)
		}
		disp := a.Display(v.Now)
		icon := icons[disp]
		if disp == state.DisplayWorking {
			icon = spinnerFrames[((v.Frame%len(spinnerFrames))+len(spinnerFrames))%len(spinnerFrames)]
		}
		// tree branch: ├ while siblings follow in the same window, └ on the last
		branch := "└"
		if i+1 < len(agents) && agents[i+1].Session == a.Session && agents[i+1].WindowIndex == a.WindowIndex {
			branch = "├"
		}
		rows = append(rows, Row{
			Text: fmt.Sprintf(" %s %s %s", icon, branch, truncate(title, labelW)),
			Kind: RowAgent, Display: disp, PaneID: a.PaneID, Current: current,
			Pending: strings.Contains(strings.ToUpper(title), "[PENDING]"),
		})
		rows = append(rows, subagentRows(a, labelW, current, v.Frame)...)
	}
	return rows
}

// subagentRows nests an agent's scraped background tasks one level deeper
// than its hang row: "     ├ ○ general-purpose · task title". A running task
// spins like its parent; a finished one shows ✓; a queued one stays ○.
func subagentRows(a state.Agent, labelW int, current bool, frame int) []Row {
	var rows []Row
	for i, s := range a.Subagents {
		branch := "└"
		if i+1 < len(a.Subagents) {
			branch = "├"
		}
		icon := "○"
		switch {
		case s.Done:
			icon = "✓"
		case s.Working:
			icon = spinnerFrames[((frame%len(spinnerFrames))+len(spinnerFrames))%len(spinnerFrames)]
		}
		budget := labelW - 4 // the child prefix is 4 columns wider than the parent's
		if budget < 8 {
			budget = 8
		}
		label := sanitize(s.Type + " · " + s.Title)
		rows = append(rows, Row{
			Text: fmt.Sprintf("     %s %s %s", branch, icon, truncate(label, budget)),
			Kind: RowSubagent, PaneID: a.PaneID, Current: current,
		})
	}
	return rows
}

// appendSpaced starts a new block: a blank spacer first when the previous
// row ends an agent block (hang or subagent row), so window blocks and
// session rules breathe.
func appendSpaced(rows []Row, r Row) []Row {
	if n := len(rows); n > 0 && (rows[n-1].Kind == RowAgent || rows[n-1].Kind == RowSubagent) {
		rows = append(rows, Row{Kind: RowSpacer})
	}
	return append(rows, r)
}

// groupRule builds a session divider whose rule line stretches to the
// pane width: "─ main ───────". The name keeps at least one trailing dash.
func groupRule(session string, width int) string {
	text := "─ " + truncate(session, width-4) + " "
	if fill := width - lipgloss.Width(text); fill > 0 {
		text += strings.Repeat("─", fill)
	}
	return text
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
