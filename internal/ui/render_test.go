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

func TestRenderWindowAnchorAndHangRows(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	anchorFound := false
	for _, r := range rows {
		// pane-index refs like "12.1"/"14.2" must never appear
		if strings.Contains(r.Text, "12.1") || strings.Contains(r.Text, "14.2") {
			t.Errorf("row %q must not contain pane-index refs", r.Text)
		}
		if r.Kind == RowWindow && strings.Contains(r.Text, "12:api") && r.PaneID == "%api" {
			anchorFound = true
		}
		if r.Kind == RowAgent {
			if !strings.Contains(r.Text, "└") {
				t.Errorf("agent row %q must hang with └", r.Text)
			}
			// indexes live only on the window anchor, never on hang rows
			if strings.Contains(r.Text, "14:") {
				t.Errorf("agent row %q must not repeat the window index", r.Text)
			}
		}
	}
	if !anchorFound {
		t.Error(`window 12 should have a RowWindow anchor reading "12:api" with PaneID "%api"`)
	}
}

func TestRenderFirstRowShowsPaneTitle(t *testing.T) {
	agents := []state.Agent{
		mk("main", 5, "api", 1, "fixing tests", detect.Working, t0),
		mk("main", 5, "api", 2, "review", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 60})
	var anchor, hangFixing, hangReview bool
	for _, r := range rows {
		switch {
		case r.Kind == RowWindow && strings.Contains(r.Text, "5:api"):
			anchor = true
		case r.Kind == RowAgent && strings.Contains(r.Text, "└ fixing tests"):
			hangFixing = true
		case r.Kind == RowAgent && strings.Contains(r.Text, "└ review"):
			hangReview = true
		}
	}
	if !anchor {
		t.Error(`expected a RowWindow anchor containing "5:api"`)
	}
	if !hangFixing {
		t.Error(`expected a RowAgent hang row containing "└ fixing tests"`)
	}
	if !hangReview {
		t.Error(`expected a RowAgent hang row containing "└ review"`)
	}
}

func TestRenderStructure(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	// header, alert (1 blocked), group main, window 12:api (1 agent),
	// window 14:worker (2 agents), group mon, window 1:claude (1 agent), fold, footer
	want := []RowKind{RowHeader, RowAlert, RowGroup, RowWindow, RowAgent, RowWindow, RowAgent, RowAgent,
		RowGroup, RowWindow, RowAgent, RowFold, RowFooter}
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
	// expanded: the hidden agent becomes visible — its window name lives on
	// the RowWindow anchor now, not on RowAgent, so accept either kind.
	rows = Render(ViewData{Agents: testAgents(), FoldHidden: false, HiddenPrefix: "_", Now: t0})
	found := false
	for _, r := range rows {
		if strings.Contains(r.Text, "_archive") {
			found = true
		}
	}
	if !found {
		t.Error("expanded fold must show _archive agent")
	}
}

func TestRenderLabelFillsWidth(t *testing.T) {
	agents := []state.Agent{
		mk("main", 1, "win", 1, "a-very-long-title-that-should-not-be-cut-short", detect.Working, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 40})
	found := false
	for _, r := range rows {
		if r.Kind != RowAgent {
			continue
		}
		// with Width 40 the label budget is 36 cols — far past the old 16
		if strings.Contains(r.Text, "long-title-that-should-not") {
			found = true
		}
		if lipgloss.Width(r.Text) > 40 {
			t.Errorf("row exceeds pane width 40: %q", r.Text)
		}
	}
	if !found {
		t.Error("hang row title not expanded to pane width")
	}
}

func TestRenderNoAgeColumn(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for _, r := range rows {
		if r.PaneID == "%api" && strings.Contains(r.Text, "12m") {
			t.Errorf("row %q must not contain an age column", r.Text)
		}
	}
}

func TestRenderWideCharRowsStayWithinWidth(t *testing.T) {
	agents := []state.Agent{
		// A long CJK window name exercises the anchor row's width clamp;
		// a long CJK pane title exercises the hang row's.
		mk("main", 1, "日本語の長いウィンドウ名テストがここにある", 1, "日本語の長いタイトルテキストがここにある", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 30})
	for _, r := range rows {
		if (r.Kind == RowAgent || r.Kind == RowWindow) && lipgloss.Width(r.Text) > 30 {
			t.Errorf("row wider than pane (%d cols): %q", lipgloss.Width(r.Text), r.Text)
		}
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
