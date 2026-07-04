package ui

import (
	"strings"
	"testing"

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
