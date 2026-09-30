package ui

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sngyo/tmux-radar/internal/attention"
	"github.com/sngyo/tmux-radar/internal/poller"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
)

// Faint is avoided on purpose: with dozens of idle agents it makes the
// whole sidebar unreadably dim. Structure rows use mid greys from the
// 256-color palette (stable across themes; Solarized remaps bright ANSI
// 8-15 to dark tones, so those are avoided too). Idle rows use the
// terminal's default foreground.
var styles = map[RowKind]lipgloss.Style{
	RowHeader: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	// full-width band: light pink on dark red, padded to the pane in View
	RowAlert:  lipgloss.NewStyle().Foreground(lipgloss.Color("217")).Background(lipgloss.Color("52")).Bold(true),
	RowGroup:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	RowWindow: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	RowFold:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	RowFooter: lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
	// scraped background tasks read as secondary detail
	RowSubagent: lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
}

// currentBg marks the attached client's active window: a band that keeps
// each row's own foreground color readable on top. Dark themes sit close to
// the low greys, so the band alone is easy to lose — currentBar carries the
// mark as a glyph in the gutter column, which no background can swallow.
var currentBg = lipgloss.Color("238")

const currentBar = "▎"

var currentBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Background(currentBg)

// pendingStyle grays out agents whose pane title carries the [PENDING]
// marker: parked on purpose, so no working green and no bold.
var pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

// dropMarkerStyle paints the insertion line shown while dragging a window
// block: amber so it reads against every row color.
var dropMarkerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

// selectionBg marks the popup's keyboard selection; brighter than the
// focused-window band so the cursor reads on top of it.
var selectionBg = lipgloss.Color("240")

var displayStyles = map[state.Display]lipgloss.Style{
	state.DisplayWorking: lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
	state.DisplayBlocked: lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
	state.DisplayDone:    lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
	state.DisplayIdle:    lipgloss.NewStyle(), // terminal default foreground
}

type tickMsg time.Time
type animMsg struct{}
type snapMsg struct {
	snap state.Snapshot
	err  error
}
type focusTickMsg time.Time
type focusMsg struct {
	focus tmux.Focus
	err   error
}

// animInterval paces the working-glyph spinner. Fast enough to read as
// motion, slow enough that a full redraw of a ~40-col pane costs nothing.
const animInterval = 200 * time.Millisecond

// focusInterval paces the focus-only refresh that keeps the focused-window
// highlight tracking the client between full polls. A full poll captures
// every agent pane and lands at most once per interval (default 1s), which
// made the highlight lag by up to two polls; focus itself is a single cheap
// tmux exec, so it can run an order of magnitude faster. The next fetch is
// armed only after the previous one returns, so fetches never overlap.
const focusInterval = 150 * time.Millisecond

// App is the bubbletea model of the sidebar.
type App struct {
	deps           poller.Deps
	focusReturnCmd string
	hiddenPrefix   string
	interval       time.Duration
	snap           state.Snapshot
	rows           []Row // last rendered rows; index = screen line for mouse
	fold           bool
	width          int // pane width from the last WindowSizeMsg
	height         int // pane height from the last WindowSizeMsg
	frame          int // animation frame for the working-glyph spinner
	err            error
	inFlight       bool       // a poll cmd is outstanding; skip re-issuing until it returns
	popup          bool       // one-shot popup: esc closes, click jump closes after jumping
	selPane        string     // popup selection: pane id of the highlighted agent row
	seeded         bool       // popup: initial selection was placed after the first good poll
	origin         tmux.Focus // where the client was before popup browsing began
	jump           func(session string, windowIndex int, paneID string) error
	moveWindow     func(session string, src, dst int, after bool) error
	drag           *dragState // sidebar: a left button is held on a window block
}

// dragState tracks a window block being dragged with the mouse.
type dragState struct {
	session string
	window  int        // source window index
	target  dropTarget // where a release would put it; refreshed on motion
}

// dropTarget is a gap between window blocks: "after window `window`", or
// "before it" when after is false (the session's first window). row is the
// screen line the marker is drawn on. ok is false when the cursor is over
// no usable gap (another session, or a gap adjacent to the source).
type dropTarget struct {
	session string
	window  int
	after   bool
	row     int
	ok      bool
}

// NewApp builds the sidebar model. focusReturnCmd, when non-empty, runs
// via `sh -c` after a click jump to hand focus back to the tmux split.
// popup makes the app one-shot for tmux display-popup: esc quits and a
// click jump quits right after jumping (the popup would cover the target).
func NewApp(deps poller.Deps, focusReturnCmd, hiddenPrefix string, interval time.Duration, popup bool) *App {
	if interval <= 0 {
		interval = time.Second // zero/negative would spin or stall the tick loop
	}
	// Seed the poll baseline from the last saved snapshot so a fresh process
	// inherits the "done" overlays and Since history the long-lived sidebar
	// accumulated. The popup is short-lived and would otherwise start with an
	// empty prev, unable to reproduce any working->idle completion that
	// happened before it opened — and its first save would clobber those
	// marks in state.json for the status line and jump. A missing/corrupt
	// file yields a zero snapshot: the first poll then rebuilds from scratch.
	snap, _ := state.Load(state.DefaultPath())
	return &App{deps: deps, focusReturnCmd: focusReturnCmd, hiddenPrefix: hiddenPrefix,
		interval: interval, fold: true, popup: popup, snap: snap}
}

// moveWindowTo reorders a window via the injected hook (tests) or the real tmux.
func (a *App) moveWindowTo(session string, src, dst int, after bool) error {
	if a.moveWindow != nil {
		return a.moveWindow(session, src, dst, after)
	}
	return tmux.MoveWindow(session, src, dst, after)
}

// jumpTo focuses a pane via the injected hook (tests) or the real tmux.
func (a *App) jumpTo(session string, windowIndex int, paneID string) error {
	if a.jump != nil {
		return a.jump(session, windowIndex, paneID)
	}
	return tmux.JumpTo(session, windowIndex, paneID)
}

func (a *App) Init() tea.Cmd {
	a.inFlight = true
	// fetchFocus (not focusTick) so the first highlight lands immediately
	return tea.Batch(a.poll(), a.tick(), a.animTick(), a.fetchFocus())
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(a.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a *App) animTick() tea.Cmd {
	return tea.Tick(animInterval, func(time.Time) tea.Msg { return animMsg{} })
}

func (a *App) focusTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return focusTickMsg(t) })
}

// fetchFocus resolves the attached client's focus off the update loop.
// CurrentFocus is captured here so the closure never races the model.
func (a *App) fetchFocus() tea.Cmd {
	fetch := a.deps.CurrentFocus
	return func() tea.Msg {
		if fetch == nil {
			return focusMsg{err: errors.New("no focus source configured")}
		}
		f, err := fetch()
		return focusMsg{focus: f, err: err}
	}
}

func (a *App) poll() tea.Cmd {
	prev := a.snap
	deps := a.deps
	return func() tea.Msg {
		next, err := poller.RunOnce(prev, deps, time.Now())
		if err == nil {
			_ = state.Save(state.DefaultPath(), next)
		}
		return snapMsg{snap: next, err: err}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tickMsg:
		cmds := []tea.Cmd{a.tick()}
		if !a.inFlight {
			a.inFlight = true
			cmds = append(cmds, a.poll())
		}
		return a, tea.Batch(cmds...)
	case animMsg:
		a.frame++
		return a, a.animTick()
	case focusTickMsg:
		return a, a.fetchFocus()
	case focusMsg:
		if m.err != nil {
			// tmux gone: back off to the slow poll cadence instead of
			// hammering a dead server several times a second.
			return a, a.focusTick(a.interval)
		}
		a.snap.Focus = m.focus
		return a, a.focusTick(focusInterval)
	case snapMsg:
		a.inFlight = false
		a.err = m.err
		// GeneratedAt check is belt-and-braces against reordering if the
		// in-flight guard is ever removed.
		if m.err == nil && !m.snap.GeneratedAt.Before(a.snap.GeneratedAt) {
			a.snap = m.snap
		}
		// Seed the popup's cursor once, on the first successful poll: start
		// browsing from where the client already is instead of the list top.
		// Cursor only — no live-preview jump, and origin stays unset so esc
		// before any manual move keeps doing nothing.
		if a.popup && !a.seeded && m.err == nil {
			a.selPane = nearestPane(a.snap.Agents, a.snap.Focus, a.hiddenPrefix, a.fold)
			a.seeded = true
		}
		return a, nil
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		return a, nil
	case tea.KeyMsg:
		if m.String() == "q" || m.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if a.popup {
			switch m.String() {
			case "ctrl+n", "down", "j", "n":
				a.move(1)
			case "ctrl+p", "up", "k", "p":
				a.move(-1)
			case "a":
				// attention jump, same queue as `tmux-radar jump`. Bound here
				// because a focused popup swallows the tmux prefix — so the
				// C-t a muscle memory still lands: C-t is ignored, a jumps.
				now := time.Now()
				queue := attention.Queue(a.snap.Agents, now)
				if len(queue) == 0 {
					queue = attention.Working(a.snap.Agents, now)
				}
				if target, ok := attention.Next(queue, a.snap.Focus.PaneID); ok {
					_ = a.jumpTo(target.Session, target.WindowIndex, target.PaneID)
					return a, tea.Quit
				}
			case "enter":
				return a, tea.Quit
			case "esc":
				// cancel browsing: put the client back where it started
				if a.origin.PaneID != "" {
					_ = a.jumpTo(a.origin.Session, a.origin.WindowIndex, a.origin.PaneID)
				}
				return a, tea.Quit
			}
		}
	case tea.MouseMsg:
		if m.Button != tea.MouseButtonLeft {
			return a, nil
		}
		switch m.Action {
		case tea.MouseActionPress:
			a.drag = a.startDrag(m.Y)
		case tea.MouseActionMotion:
			if a.drag != nil {
				a.drag.target = a.dropTargetAt(m.Y)
			}
		case tea.MouseActionRelease:
			drag := a.drag
			if drag == nil || (a.rowWindow(m.Y) == drag.session+":"+strconv.Itoa(drag.window)) {
				a.drag = nil
				return a, a.click(m.Y) // no drag, or released on the source block
			}
			t := a.dropTargetAt(m.Y) // needs a.drag for the session/adjacency checks
			a.drag = nil
			if t.ok {
				if err := a.moveWindowTo(t.session, drag.window, t.window, t.after); err == nil && !a.inFlight {
					a.inFlight = true
					return a, a.poll() // show the new order without waiting for the tick
				}
			}
		}
	}
	return a, nil
}

// rowWindow keys the window a screen line belongs to ("session:index"),
// or "" for rows outside any window block.
func (a *App) rowWindow(y int) string {
	if y < 0 || y >= len(a.rows) {
		return ""
	}
	r := a.rows[y]
	if r.Kind != RowWindow && r.Kind != RowAgent && r.Kind != RowSubagent {
		return ""
	}
	return r.Session + ":" + strconv.Itoa(r.WindowIndex)
}

// startDrag arms a drag when the press lands on a window block. The popup
// never drags: it is one-shot and a click there jumps and closes.
func (a *App) startDrag(y int) *dragState {
	if a.popup || a.rowWindow(y) == "" {
		return nil
	}
	r := a.rows[y]
	return &dragState{session: r.Session, window: r.WindowIndex}
}

// dropTargetAt maps a cursor line to the nearest gap between window blocks
// (ties go to the gap above). Gaps are: the session rule ("before the
// session's first window") and the row right after each block's last
// row — a spacer, the next session rule, the fold row or the footer
// ("after that window"). Only gaps in the source's session count, and the
// two gaps hugging the source block are no-ops.
func (a *App) dropTargetAt(y int) dropTarget {
	if a.drag == nil {
		return dropTarget{}
	}
	var gaps []dropTarget
	for i, r := range a.rows {
		switch r.Kind {
		case RowGroup:
			// resolved to the first window of the session once seen
			if j := i + 1; j < len(a.rows) && a.rows[j].Kind == RowWindow {
				gaps = append(gaps, dropTarget{session: r.Session, window: a.rows[j].WindowIndex, after: false, row: i})
			}
		case RowWindow, RowAgent, RowSubagent:
			last := i+1 >= len(a.rows) || a.rows[i+1].Kind != RowAgent && a.rows[i+1].Kind != RowSubagent
			if last {
				gaps = append(gaps, dropTarget{session: r.Session, window: r.WindowIndex, after: true, row: i + 1})
			}
		}
	}
	best, bestDist := dropTarget{}, -1
	for _, g := range gaps {
		d := y - g.row
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist {
			best, bestDist = g, d
		}
	}
	if bestDist < 0 || best.session != a.drag.session {
		return dropTarget{}
	}
	// the gap right above or below the source leaves the order unchanged
	if best.row == a.sourceTop()-1 || best.row == a.sourceBottom()+1 {
		return dropTarget{}
	}
	best.ok = true
	return best
}

// sourceTop / sourceBottom are the first and last screen lines of the
// dragged block, or -2 (never adjacent to anything) when it is not on screen.
func (a *App) sourceTop() int {
	for i := range a.rows {
		if a.rowWindow(i) == a.drag.session+":"+strconv.Itoa(a.drag.window) {
			return i
		}
	}
	return -2
}

func (a *App) sourceBottom() int {
	for i := len(a.rows) - 1; i >= 0; i-- {
		if a.rowWindow(i) == a.drag.session+":"+strconv.Itoa(a.drag.window) {
			return i
		}
	}
	return -2
}

// nearestPane picks the popup's initial selection: the visible agent row
// nearest to the attached client's focus, in display order — the exact
// focused pane, else the focused window's first agent, else the focused
// session's first agent, else the top of the list. Agents in hidden
// windows are no candidates while the fold is closed (their rows aren't
// on screen). Empty when nothing is visible.
func nearestPane(agents []state.Agent, focus tmux.Focus, hiddenPrefix string, foldHidden bool) string {
	windowPane, sessionPane, topPane := "", "", ""
	for _, a := range sortAgents(agents) {
		if foldHidden && hiddenPrefix != "" && strings.HasPrefix(a.WindowName, hiddenPrefix) {
			continue
		}
		if a.PaneID == focus.PaneID {
			return a.PaneID
		}
		if windowPane == "" && a.Session == focus.Session && a.WindowIndex == focus.WindowIndex {
			windowPane = a.PaneID
		}
		if sessionPane == "" && a.Session == focus.Session {
			sessionPane = a.PaneID
		}
		if topPane == "" {
			topPane = a.PaneID
		}
	}
	if windowPane != "" {
		return windowPane
	}
	if sessionPane != "" {
		return sessionPane
	}
	return topPane
}

// move shifts the popup selection to the next/previous agent row (wrapping)
// and switches the client behind the popup to that agent, so browsing
// live-previews each pane. The pre-browse window is recorded once so esc
// can put the client back.
func (a *App) move(dir int) {
	var panes []string
	for _, r := range a.rows {
		if r.Kind == RowAgent && r.PaneID != "" {
			panes = append(panes, r.PaneID)
		}
	}
	if len(panes) == 0 {
		return
	}
	idx := -1
	for i, p := range panes {
		if p == a.selPane {
			idx = i
			break
		}
	}
	if idx < 0 { // no selection yet: start from the top (or bottom going up)
		idx = -dir // 1 → 0 after +dir; -1 → len-1 after wrap
	}
	idx = ((idx+dir)%len(panes) + len(panes)) % len(panes)
	if a.origin.PaneID == "" {
		a.origin = a.snap.Focus
	}
	a.selPane = panes[idx]
	for _, ag := range a.snap.Agents {
		if ag.PaneID == a.selPane {
			_ = a.jumpTo(ag.Session, ag.WindowIndex, ag.PaneID)
			return
		}
	}
}

// click resolves a screen line to a row and acts on it. The returned
// command is non-nil only for a popup-mode jump, which closes the popup.
func (a *App) click(y int) tea.Cmd {
	if y < 0 || y >= len(a.rows) {
		return nil
	}
	row := a.rows[y]
	switch {
	case row.ToggleFold:
		a.fold = !a.fold
	case row.PaneID != "":
		for _, ag := range a.snap.Agents {
			if ag.PaneID == row.PaneID {
				_ = a.jumpTo(ag.Session, ag.WindowIndex, ag.PaneID)
				if a.popup {
					// the popup floats over the jump target: get out of the way
					return tea.Quit
				}
				if a.focusReturnCmd != "" {
					// focusReturnCmd is a user-authored hook from the user's own
					// config file, executed verbatim (same trust model as tmux
					// run-shell / git hooks). SECURITY: never interpolate runtime
					// data (pane ids, window names) into this string; if the hook
					// ever needs context, pass it via environment variables.
					_ = exec.Command("sh", "-c", a.focusReturnCmd).Start()
				}
				return nil
			}
		}
	}
	return nil
}

// dropMarker draws the insertion line: "┄┄┄ ▾ ┄┄┄" stretched to the pane.
func dropMarker(width int) string {
	if width < 8 {
		width = 24
	}
	side := strings.Repeat("┄", (width-3)/2)
	return side + " ▾ " + side
}

func (a *App) View() string {
	if a.err != nil {
		return "tmux server not running…\nretrying every second (q to quit)\n"
	}
	rows := Render(ViewData{
		Agents: a.snap.Agents, FoldHidden: a.fold, HiddenPrefix: a.hiddenPrefix,
		Now: time.Now(), Width: a.width, Focus: a.snap.Focus, Frame: a.frame, Popup: a.popup,
	})
	// The view must never be taller than the pane: bubbletea drops overflow
	// lines from the TOP, which would shift every row up on screen and break
	// the rows-index == screen-line mouse mapping (e.g. the fold row stops
	// being clickable right after unfolding). Cut from the bottom instead,
	// and skip the trailing newline — the renderer counts it as one more
	// line, which would start the top-dropping one row early.
	if a.height > 0 && len(rows) > a.height {
		rows = rows[:a.height]
	}
	a.rows = rows
	out := ""
	for i, r := range a.rows {
		st := styles[r.Kind]
		if r.Kind == RowAgent {
			st = displayStyles[r.Display]
			if r.Pending {
				st = pendingStyle
			}
		}
		if r.Current {
			st = st.Background(currentBg)
			if r.Kind == RowWindow {
				st = st.Bold(true)
			}
		}
		text := r.Text
		selected := a.popup && r.Kind == RowAgent && r.PaneID != "" && r.PaneID == a.selPane
		if selected {
			text = "❯" + strings.TrimPrefix(text, " ")
			st = st.Background(selectionBg)
		}
		// the gutter column is free on every row (all text starts with a
		// space); the focused window claims it for the bar unless the
		// keyboard cursor already sits there
		prefix := ""
		if r.Current && !selected && strings.HasPrefix(text, " ") {
			prefix, text = currentBarStyle.Render(currentBar), text[1:]
		}
		// background rows stretch their band across the pane
		if (r.Kind == RowAlert || r.Current || selected) && a.width > 0 {
			st = st.Width(a.width - lipgloss.Width(prefix))
		}
		if a.drag != nil && a.drag.target.ok && a.drag.target.row == i {
			// the insertion line replaces the gap row's own text; the row
			// count stays put so the mouse mapping holds during the drag
			prefix, text, st = "", dropMarker(a.width), dropMarkerStyle
		}
		if i > 0 {
			out += "\n"
		}
		out += prefix + st.Render(text)
	}
	return out
}
