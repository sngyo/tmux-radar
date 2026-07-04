package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sngyo/tmux-radar/internal/detect"
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
