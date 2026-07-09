package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/poller"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
)

func TestViewFocusedWindowRowsSpanPaneWidth(t *testing.T) {
	a := &App{width: 30, snap: state.Snapshot{
		Agents: []state.Agent{
			mk("main", 5, "api", 1, "", detect.Working, t0),
			mk("main", 6, "web", 1, "", detect.Idle, t0),
		},
		Focus: tmux.Focus{Session: "main", WindowIndex: 5, PaneID: "%api"},
	}}
	var focusedAnchor, otherAnchor string
	for _, line := range strings.Split(a.View(), "\n") {
		if strings.Contains(line, "5:api") {
			focusedAnchor = line
		}
		if strings.Contains(line, "6:web") {
			otherAnchor = line
		}
	}
	if focusedAnchor == "" || otherAnchor == "" {
		t.Fatal("anchor rows missing from view")
	}
	if w := lipgloss.Width(focusedAnchor); w != 30 {
		t.Errorf("focused anchor width = %d, want 30 (highlight band): %q", w, focusedAnchor)
	}
	if w := lipgloss.Width(otherAnchor); w == 30 {
		t.Errorf("unfocused anchor must keep its natural width: %q", otherAnchor)
	}
}

func TestViewAlertBandSpansPaneWidth(t *testing.T) {
	a := &App{width: 30, snap: state.Snapshot{Agents: []state.Agent{
		mk("main", 1, "api", 1, "", detect.Blocked, t0),
	}}}
	for _, line := range strings.Split(a.View(), "\n") {
		if strings.Contains(line, "blocked") && strings.Contains(line, "◆") {
			if w := lipgloss.Width(line); w != 30 {
				t.Errorf("alert band width = %d, want 30: %q", w, line)
			}
			return
		}
	}
	t.Fatal("no alert row in view")
}

func TestAnimTickAdvancesFrameAndReschedules(t *testing.T) {
	a := &App{}
	m, cmd := a.Update(animMsg{})
	if m.(*App).frame != 1 {
		t.Errorf("frame = %d, want 1", m.(*App).frame)
	}
	if cmd == nil {
		t.Error("anim tick must reschedule itself")
	}
}

func TestViewWiresFrameIntoWorkingGlyph(t *testing.T) {
	a := &App{width: 30, snap: state.Snapshot{Agents: []state.Agent{
		mk("main", 1, "api", 1, "", detect.Working, t0),
	}}}
	v0 := a.View()
	a.frame = 1
	if v1 := a.View(); v0 == v1 {
		t.Error("view must change with the animation frame while an agent works")
	}
}

func TestViewGraysOutPendingRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	a := &App{width: 60, snap: state.Snapshot{Agents: []state.Agent{
		mk("main", 1, "neo3", 1, "✳ [PENDING] Resume crashed session", detect.Working, t0),
		mk("main", 1, "neo3", 2, "✳ fixing tests", detect.Working, t0),
	}}}
	var pendingLine, workingLine string
	for _, line := range strings.Split(a.View(), "\n") {
		if strings.Contains(line, "PENDING") {
			pendingLine = line
		}
		if strings.Contains(line, "fixing tests") {
			workingLine = line
		}
	}
	if pendingLine == "" || workingLine == "" {
		t.Fatal("expected both a pending and a working hang row")
	}
	if !strings.Contains(pendingLine, "38;5;242") {
		t.Errorf("pending row must be gray (fg 242): %q", pendingLine)
	}
	if strings.Contains(pendingLine, ";32m") || strings.Contains(pendingLine, "[32") {
		t.Errorf("pending row must not keep the working green: %q", pendingLine)
	}
	if !strings.Contains(workingLine, "32") {
		t.Errorf("working row should stay green: %q", workingLine)
	}
}

// A freshly spawned app (notably the popup) must inherit the last saved
// snapshot as its poll baseline. Without it, its first tick has no prev, so
// working->idle "done" overlays accumulated by the long-lived sidebar cannot
// be reproduced and the two panes diverge (a done ✓ shows as a plain idle ○).
func TestNewAppSeedsPrevFromDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seed := state.Snapshot{GeneratedAt: t0, Agents: []state.Agent{
		{PaneID: "%api", Session: "s", WindowIndex: 1, WindowName: "api", State: detect.Idle, Done: true},
	}}
	if err := state.Save(state.DefaultPath(), seed); err != nil {
		t.Fatal(err)
	}
	a := NewApp(poller.Deps{}, "", "", time.Second, true)
	if len(a.snap.Agents) != 1 || !a.snap.Agents[0].Done {
		t.Fatalf("NewApp must seed snap from disk with the done mark, got %+v", a.snap)
	}
}

// Unfolding many hidden agents can make the view taller than the pane.
// bubbletea's renderer drops overflow lines from the TOP, which would shift
// every row up on screen and break the rows-index == screen-line mouse
// mapping — the fold row the user just clicked could no longer be clicked
// to fold back. The view must clamp to the pane height instead, cutting
// overflow from the bottom.
func TestClickFoldTogglesClosedWhenUnfoldedOverflowsPane(t *testing.T) {
	agents := []state.Agent{mk("main", 1, "api", 1, "", detect.Idle, t0)}
	for i := 0; i < 20; i++ {
		agents = append(agents, mk("main", 10+i, fmt.Sprintf("_w%d", i), 1, "", detect.Idle, t0))
	}
	a := &App{hiddenPrefix: "_", fold: true, snap: state.Snapshot{Agents: agents}}
	a.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	a.View()
	foldY := -1
	for i, r := range a.rows {
		if r.ToggleFold {
			foldY = i
		}
	}
	if foldY < 0 {
		t.Fatal("no fold row rendered")
	}
	click := tea.MouseMsg{Y: foldY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	a.Update(click)
	if a.fold {
		t.Fatal("first click must unfold the hidden section")
	}
	view := a.View()
	if got := len(strings.Split(view, "\n")); got > 10 {
		t.Errorf("unfolded view is %d lines, must fit the 10-line pane so screen lines keep matching row indices", got)
	}
	if !a.rows[foldY].ToggleFold {
		t.Fatalf("row at y=%d is no longer the fold row after unfolding", foldY)
	}
	a.Update(click) // the user clicks the same spot again
	if !a.fold {
		t.Error("second click on the fold row must fold the hidden section back")
	}
}

func TestPopupModeEscQuits(t *testing.T) {
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	popup := &App{popup: true}
	if _, cmd := popup.Update(esc); cmd == nil {
		t.Error("esc must quit in popup mode")
	}
	normal := &App{}
	if _, cmd := normal.Update(esc); cmd != nil {
		t.Error("esc must be ignored in the persistent sidebar")
	}
}

func TestPopupModeClickJumpQuits(t *testing.T) {
	a := &App{popup: true, width: 40, snap: state.Snapshot{Agents: []state.Agent{
		mk("no-such-session", 1, "api", 1, "", detect.Idle, t0),
	}}}
	a.View() // populate rows for mouse mapping
	y := -1
	for i, r := range a.rows {
		if r.Kind == RowAgent {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("no agent row rendered")
	}
	click := tea.MouseMsg{Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	_, cmd := a.Update(click)
	if cmd == nil {
		t.Fatal("click jump in popup mode must return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("returned command should quit, got %T", cmd())
	}
}

func recordingApp() (*App, *[]string) {
	var jumps []string
	a := &App{popup: true, width: 40, snap: state.Snapshot{
		Agents: []state.Agent{
			mk("s", 1, "api", 1, "first", detect.Idle, t0),
			mk("s", 2, "web", 1, "second", detect.Idle, t0),
		},
		Focus: tmux.Focus{Session: "s", WindowIndex: 9, PaneID: "%origin"},
	}}
	a.jump = func(session string, window int, pane string) error {
		jumps = append(jumps, fmt.Sprintf("%s:%d:%s", session, window, pane))
		return nil
	}
	a.View() // populate rows for navigation
	return a, &jumps
}

func TestPopupMoveSelectsAndPeeksBehind(t *testing.T) {
	a, jumps := recordingApp()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if a.selPane != "%api" {
		t.Fatalf("selPane = %q, want %%api", a.selPane)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}) // wraps to first
	if a.selPane != "%api" {
		t.Fatalf("selection should wrap, selPane = %q", a.selPane)
	}
	want := []string{"s:1:%api", "s:2:%web", "s:1:%api"}
	if fmt.Sprint(*jumps) != fmt.Sprint(want) {
		t.Errorf("jumps = %v, want %v", *jumps, want)
	}
}

func TestPopupEnterConfirmsAndQuits(t *testing.T) {
	a, _ := recordingApp()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must quit the popup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("enter should quit, got %T", cmd())
	}
}

func TestPopupEscReturnsToOrigin(t *testing.T) {
	a, jumps := recordingApp()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc must quit the popup")
	}
	last := (*jumps)[len(*jumps)-1]
	if last != "s:9:%origin" {
		t.Errorf("esc should jump back to the origin, last jump = %q", last)
	}
}

func TestViewMarksPopupSelection(t *testing.T) {
	a, _ := recordingApp()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	marked := false
	for _, line := range strings.Split(a.View(), "\n") {
		if strings.HasPrefix(line, "❯") && strings.Contains(line, "first") {
			marked = true
		}
	}
	if !marked {
		t.Error("selected row must carry the ❯ marker")
	}
}

// ag builds an agent with an explicit pane id, for tests where several
// panes share a window (mk derives the id from the window name).
func ag(pane, sess string, win int, name string, paneIdx int) state.Agent {
	return state.Agent{PaneID: pane, Session: sess, WindowIndex: win,
		WindowName: name, PaneIndex: paneIdx, Kind: "claude", State: detect.Idle, Since: t0}
}

func TestNearestPaneExactMatchWins(t *testing.T) {
	agents := []state.Agent{
		ag("%a", "s", 1, "api", 1),
		ag("%b", "s", 1, "api", 2),
	}
	focus := tmux.Focus{Session: "s", WindowIndex: 1, PaneID: "%b"}
	if got := nearestPane(agents, focus, "_", true); got != "%b" {
		t.Errorf("nearestPane = %q, want %%b (exact pane match beats window order)", got)
	}
}

func TestNearestPaneFallsBackToFirstAgentInWindow(t *testing.T) {
	// the focused pane itself is not an agent; agents listed out of order to
	// prove the helper follows rendered order, not slice order
	agents := []state.Agent{
		ag("%a", "s", 1, "api", 1),
		ag("%c", "s", 2, "web", 2),
		ag("%b", "s", 2, "web", 1),
	}
	focus := tmux.Focus{Session: "s", WindowIndex: 2, PaneID: "%shell"}
	if got := nearestPane(agents, focus, "_", true); got != "%b" {
		t.Errorf("nearestPane = %q, want %%b (first agent in the focused window)", got)
	}
}

func TestNearestPaneFallsBackToFirstAgentInSession(t *testing.T) {
	agents := []state.Agent{
		ag("%z", "s", 9, "worker", 1),
		ag("%a", "s", 3, "api", 1),
		ag("%m", "mon", 1, "claude", 1),
	}
	focus := tmux.Focus{Session: "s", WindowIndex: 5, PaneID: "%shell"}
	if got := nearestPane(agents, focus, "_", true); got != "%a" {
		t.Errorf("nearestPane = %q, want %%a (first agent in the focused session)", got)
	}
}

func TestNearestPaneFallsBackToTop(t *testing.T) {
	agents := []state.Agent{
		ag("%z", "zzz", 1, "api", 1),
		ag("%m", "mon", 1, "claude", 1),
	}
	focus := tmux.Focus{Session: "elsewhere", WindowIndex: 1, PaneID: "%shell"}
	if got := nearestPane(agents, focus, "_", true); got != "%m" {
		t.Errorf("nearestPane = %q, want %%m (top of the rendered list)", got)
	}
}

func TestNearestPaneSkipsHiddenWhileFolded(t *testing.T) {
	agents := []state.Agent{
		ag("%h", "s", 1, "_bg", 1),
		ag("%a", "s", 2, "api", 1),
	}
	focus := tmux.Focus{Session: "s", WindowIndex: 1, PaneID: "%h"}
	if got := nearestPane(agents, focus, "_", true); got != "%a" {
		t.Errorf("nearestPane = %q, want %%a (hidden window excluded while folded)", got)
	}
	if got := nearestPane(agents, focus, "_", false); got != "%h" {
		t.Errorf("nearestPane = %q, want %%h (hidden window eligible once unfolded)", got)
	}
	if got := nearestPane(agents, focus, "", true); got != "%h" {
		t.Errorf("nearestPane = %q, want %%h (empty prefix hides nothing)", got)
	}
}

func TestNearestPaneNoVisibleAgents(t *testing.T) {
	focus := tmux.Focus{Session: "s", WindowIndex: 1, PaneID: "%shell"}
	if got := nearestPane(nil, focus, "_", true); got != "" {
		t.Errorf("nearestPane = %q, want empty for no agents", got)
	}
	hidden := []state.Agent{ag("%h", "s", 1, "_bg", 1)}
	if got := nearestPane(hidden, focus, "_", true); got != "" {
		t.Errorf("nearestPane = %q, want empty when every agent is folded away", got)
	}
}

func TestPopupSeedsSelectionFromFocusOnFirstPoll(t *testing.T) {
	a, jumps := recordingApp()
	snap := a.snap
	snap.Focus = tmux.Focus{Session: "s", WindowIndex: 2, PaneID: "%web"}
	a.Update(snapMsg{snap: snap})
	if a.selPane != "%web" {
		t.Fatalf("selPane = %q, want %%web (seeded from focus)", a.selPane)
	}
	if len(*jumps) != 0 {
		t.Fatalf("seeding must not live-preview jump, got %v", *jumps)
	}
	// a later poll with a different focus must not move the cursor
	moved := snap
	moved.Focus = tmux.Focus{Session: "s", WindowIndex: 1, PaneID: "%api"}
	a.Update(snapMsg{snap: moved})
	if a.selPane != "%web" {
		t.Fatalf("selPane = %q, second poll must not re-seed", a.selPane)
	}
	// movement continues relative to the seeded row (wraps to the first)
	a.View()
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if a.selPane != "%api" {
		t.Errorf("selPane = %q, want %%api (move starts from the seeded row)", a.selPane)
	}
}

func TestPopupSeedWaitsForSuccessfulPoll(t *testing.T) {
	a, _ := recordingApp()
	snap := a.snap
	snap.Focus = tmux.Focus{Session: "s", WindowIndex: 2, PaneID: "%web"}
	a.Update(snapMsg{snap: snap, err: errors.New("tmux gone")})
	if a.selPane != "" {
		t.Fatalf("selPane = %q, a failed poll must not seed", a.selPane)
	}
	a.Update(snapMsg{snap: snap})
	if a.selPane != "%web" {
		t.Errorf("selPane = %q, want %%web after the first successful poll", a.selPane)
	}
}

func TestSidebarNeverSeedsSelection(t *testing.T) {
	a, _ := recordingApp()
	a.popup = false
	snap := a.snap
	snap.Focus = tmux.Focus{Session: "s", WindowIndex: 2, PaneID: "%web"}
	a.Update(snapMsg{snap: snap})
	if a.selPane != "" {
		t.Errorf("selPane = %q, the persistent sidebar has no selection cursor", a.selPane)
	}
}

func TestPopupAttentionJumpOnA(t *testing.T) {
	var jumps []string
	a := &App{popup: true, width: 40, snap: state.Snapshot{
		Agents: []state.Agent{
			mk("s", 1, "api", 1, "working away", detect.Working, t0),
			mk("s", 2, "web", 1, "needs approval", detect.Blocked, t0),
		},
		Focus: tmux.Focus{Session: "s", WindowIndex: 1, PaneID: "%api"},
	}}
	a.jump = func(session string, window int, pane string) error {
		jumps = append(jumps, fmt.Sprintf("%s:%d:%s", session, window, pane))
		return nil
	}
	a.View()
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if len(jumps) != 1 || jumps[0] != "s:2:%web" {
		t.Fatalf("a should jump to the blocked agent, jumps = %v", jumps)
	}
	if cmd == nil {
		t.Fatal("attention jump must close the popup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected quit, got %T", cmd())
	}
}
