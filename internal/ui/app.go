package ui

import (
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// currentBg marks the attached client's active window: a subtle band that
// keeps each row's own foreground color readable on top.
var currentBg = lipgloss.Color("236")

// pendingStyle grays out agents whose pane title carries the [PENDING]
// marker: parked on purpose, so no working green and no bold.
var pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

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

// animInterval paces the working-glyph spinner. Fast enough to read as
// motion, slow enough that a full redraw of a ~40-col pane costs nothing.
const animInterval = 200 * time.Millisecond

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
	frame          int // animation frame for the working-glyph spinner
	err            error
	inFlight       bool // a poll cmd is outstanding; skip re-issuing until it returns
}

// NewApp builds the sidebar model. focusReturnCmd, when non-empty, runs
// via `sh -c` after a click jump to hand focus back to the tmux split.
func NewApp(deps poller.Deps, focusReturnCmd, hiddenPrefix string, interval time.Duration) *App {
	if interval <= 0 {
		interval = time.Second // zero/negative would spin or stall the tick loop
	}
	return &App{deps: deps, focusReturnCmd: focusReturnCmd, hiddenPrefix: hiddenPrefix, interval: interval, fold: true}
}

func (a *App) Init() tea.Cmd {
	a.inFlight = true
	return tea.Batch(a.poll(), a.tick(), a.animTick())
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(a.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a *App) animTick() tea.Cmd {
	return tea.Tick(animInterval, func(time.Time) tea.Msg { return animMsg{} })
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
	case snapMsg:
		a.inFlight = false
		a.err = m.err
		// GeneratedAt check is belt-and-braces against reordering if the
		// in-flight guard is ever removed.
		if m.err == nil && !m.snap.GeneratedAt.Before(a.snap.GeneratedAt) {
			a.snap = m.snap
		}
		return a, nil
	case tea.WindowSizeMsg:
		a.width = m.Width
		return a, nil
	case tea.KeyMsg:
		if m.String() == "q" || m.String() == "ctrl+c" {
			return a, tea.Quit
		}
	case tea.MouseMsg:
		if m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft {
			a.click(m.Y)
		}
	}
	return a, nil
}

// click resolves a screen line to a row and acts on it.
func (a *App) click(y int) {
	if y < 0 || y >= len(a.rows) {
		return
	}
	row := a.rows[y]
	switch {
	case row.ToggleFold:
		a.fold = !a.fold
	case row.PaneID != "":
		for _, ag := range a.snap.Agents {
			if ag.PaneID == row.PaneID {
				_ = tmux.JumpTo(ag.Session, ag.WindowIndex, ag.PaneID)
				if a.focusReturnCmd != "" {
					// focusReturnCmd is a user-authored hook from the user's own
					// config file, executed verbatim (same trust model as tmux
					// run-shell / git hooks). SECURITY: never interpolate runtime
					// data (pane ids, window names) into this string; if the hook
					// ever needs context, pass it via environment variables.
					_ = exec.Command("sh", "-c", a.focusReturnCmd).Start()
				}
				return
			}
		}
	}
}

func (a *App) View() string {
	if a.err != nil {
		return "tmux server not running…\nretrying every second (q to quit)\n"
	}
	a.rows = Render(ViewData{
		Agents: a.snap.Agents, FoldHidden: a.fold, HiddenPrefix: a.hiddenPrefix,
		Now: time.Now(), Width: a.width, Focus: a.snap.Focus, Frame: a.frame,
	})
	out := ""
	for _, r := range a.rows {
		st := styles[r.Kind]
		if r.Kind == RowAgent {
			st = displayStyles[r.Display]
			if r.Pending {
				st = pendingStyle
			}
		}
		if r.Current {
			st = st.Background(currentBg)
		}
		// background rows stretch their band across the pane
		if (r.Kind == RowAlert || r.Current) && a.width > 0 {
			st = st.Width(a.width)
		}
		out += st.Render(r.Text) + "\n"
	}
	return out
}
