package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/state"
)

var t0 = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

func mk(sess string, win int, name string, pane int, title string, st detect.State, since time.Time) state.Agent {
	return state.Agent{PaneID: "%" + name, Session: sess, WindowIndex: win,
		WindowName: name, PaneIndex: pane, PaneTitle: title, Kind: "claude",
		State: st, Since: since}
}

func testAgents() []state.Agent {
	return []state.Agent{
		mk("main", 12, "api", 1, "", detect.Working, t0.Add(-12*time.Minute)),
		mk("main", 14, "worker", 1, "", detect.Working, t0.Add(-5*time.Minute)),
		mk("main", 14, "worker", 2, "reviewer", detect.Blocked, t0.Add(-3*time.Minute)),
		mk("main", 20, "_archive", 1, "", detect.Idle, t0.Add(-time.Hour)),
		mk("mon", 1, "claude", 1, "", detect.Working, t0.Add(-51*time.Minute)),
	}
}

func kinds(rows []Row) []RowKind {
	ks := make([]RowKind, len(rows))
	for i, r := range rows {
		ks[i] = r.Kind
	}
	return ks
}

func TestRenderStructure(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	// header, alert (1 blocked), group main, 3 agents, group mon, 1 agent, fold, footer
	want := []RowKind{RowHeader, RowAlert, RowGroup, RowAgent, RowAgent, RowAgent,
		RowGroup, RowAgent, RowFold, RowFooter}
	got := kinds(rows)
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(got), len(want), rows)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d kind = %v, want %v (%q)", i, got[i], want[i], rows[i].Text)
		}
	}
}

func TestRenderSecondPaneHangsWithTitle(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	var sub *Row
	for i := range rows {
		if rows[i].PaneID == "%worker" && strings.Contains(rows[i].Text, "└") {
			sub = &rows[i]
		}
	}
	if sub == nil {
		t.Fatal("no hanging sub-row found for second pane of window 'worker'")
	}
	if !strings.Contains(sub.Text, "reviewer") {
		t.Errorf("sub-row %q should show the pane title", sub.Text)
	}
	if sub.Display != state.DisplayBlocked {
		t.Errorf("sub-row display = %s, want blocked", sub.Display)
	}
}

func TestRenderFoldHidesUnderscoreWindows(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for _, r := range rows {
		if r.Kind == RowAgent && strings.Contains(r.Text, "_archive") {
			t.Error("_archive agent must be folded")
		}
		if r.Kind == RowFold && !strings.Contains(r.Text, "1 agent") {
			t.Errorf("fold row %q must show the hidden count", r.Text)
		}
	}
	// expanded: the hidden agent becomes a visible row
	rows = Render(ViewData{Agents: testAgents(), FoldHidden: false, HiddenPrefix: "_", Now: t0})
	found := false
	for _, r := range rows {
		if r.Kind == RowAgent && strings.Contains(r.Text, "_archive") {
			found = true
		}
	}
	if !found {
		t.Error("expanded fold must show _archive agent")
	}
}

func TestRenderAgeColumn(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for _, r := range rows {
		if r.PaneID == "%api" && !strings.Contains(r.Text, "12m") {
			t.Errorf("row %q should contain age 12m", r.Text)
		}
	}
}

func TestRenderWideCharColumnsAlign(t *testing.T) {
	agents := []state.Agent{
		mk("main", 1, "api", 1, "", detect.Working, t0),
		mk("main", 2, "日本語の長いウィンドウ名テスト", 1, "", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0})
	var widths []int
	for _, r := range rows {
		if r.Kind == RowAgent {
			widths = append(widths, lipgloss.Width(r.Text))
		}
	}
	if len(widths) != 2 || widths[0] != widths[1] {
		t.Errorf("agent row display widths differ: %v", widths)
	}
}

func TestRenderSanitizesControlChars(t *testing.T) {
	// mk's 5th arg (pane title) only surfaces on a window's 2nd+ pane, so a
	// title on the first pane wouldn't exercise the sanitizer. Put both
	// control chars in the window name instead so they both go through the
	// window-name path.
	agents := []state.Agent{mk("main", 1, "bad\tname\n", 1, "", detect.Working, t0)}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for _, r := range rows {
		if strings.ContainsAny(r.Text, "\n\t\r") {
			t.Errorf("row text contains control chars: %q", r.Text)
		}
	}
}
