package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/state"
	"github.com/sngyo/tmux-radar/internal/tmux"
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
			if !strings.Contains(r.Text, "└") && !strings.Contains(r.Text, "├") {
				t.Errorf("agent row %q must hang with a tree branch", r.Text)
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
		case r.Kind == RowAgent && strings.Contains(r.Text, "├ fixing tests"):
			hangFixing = true
		case r.Kind == RowAgent && strings.Contains(r.Text, "└ review"):
			hangReview = true
		}
	}
	if !anchor {
		t.Error(`expected a RowWindow anchor containing "5:api"`)
	}
	if !hangFixing {
		t.Error(`expected a RowAgent hang row containing "├ fixing tests" (first of two panes)`)
	}
	if !hangReview {
		t.Error(`expected a RowAgent hang row containing "└ review" (last pane)`)
	}
}

func TestRenderStructure(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	// header, alert (1 blocked), group main, window 12:api (1 agent),
	// window 14:worker (2 agents), group mon, window 1:claude (1 agent),
	// fold, footer — with one spacer between window blocks and before the fold
	want := []RowKind{RowHeader, RowAlert, RowGroup, RowWindow, RowAgent,
		RowSpacer, RowWindow, RowAgent, RowAgent,
		RowSpacer, RowGroup, RowWindow, RowAgent, RowSpacer, RowFold, RowFooter}
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

func TestRenderGroupRuleStretchesToPaneWidth(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 30})
	found := false
	for _, r := range rows {
		if r.Kind != RowGroup {
			continue
		}
		found = true
		if w := lipgloss.Width(r.Text); w != 30 {
			t.Errorf("group row %q width = %d, want 30", r.Text, w)
		}
		if !strings.HasSuffix(r.Text, "─") {
			t.Errorf("group row %q must end with a rule line", r.Text)
		}
	}
	if !found {
		t.Fatal("no RowGroup rows rendered")
	}
}

func TestRenderGroupRuleLongSessionStaysWithinWidth(t *testing.T) {
	agents := []state.Agent{
		mk("非常に長い日本語のセッション名がここにある", 1, "win", 1, "", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 20})
	for _, r := range rows {
		if r.Kind == RowGroup && lipgloss.Width(r.Text) > 20 {
			t.Errorf("group row wider than pane (%d cols): %q", lipgloss.Width(r.Text), r.Text)
		}
	}
}

func TestRenderMarksFocusedWindowRows(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0,
		Focus: tmux.Focus{Session: "main", WindowIndex: 14, PaneID: "%worker"}})
	var current, others []string
	for _, r := range rows {
		if r.Current {
			current = append(current, r.Text)
		} else {
			others = append(others, r.Text)
		}
	}
	// focused window 14 = anchor row + its two hang rows
	if len(current) != 3 {
		t.Fatalf("current rows = %d, want 3: %q", len(current), current)
	}
	if !strings.Contains(current[0], "14:worker") {
		t.Errorf("first current row %q should be the 14:worker anchor", current[0])
	}
	for _, text := range others {
		if strings.Contains(text, "12:api") && strings.Contains(text, "14:") {
			t.Errorf("row %q outside window 14 must not be current", text)
		}
	}
}

func TestRenderNoFocusMarksNothing(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for _, r := range rows {
		if r.Current {
			t.Errorf("row %q must not be current without focus", r.Text)
		}
	}
}

func TestRenderWorkingIconAnimatesAcrossFrames(t *testing.T) {
	agents := []state.Agent{
		mk("main", 1, "api", 1, "", detect.Working, t0),
		mk("main", 2, "web", 1, "", detect.Blocked, t0),
	}
	icon := func(frame int) (working, blocked string) {
		rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Frame: frame})
		for _, r := range rows {
			if r.Kind != RowAgent {
				continue
			}
			first := strings.Fields(r.Text)[0]
			if r.Display == state.DisplayWorking {
				working = first
			} else {
				blocked = first
			}
		}
		return
	}
	w0, b0 := icon(0)
	w1, b1 := icon(1)
	if w0 == w1 {
		t.Errorf("working icon must animate: frame0=%q frame1=%q", w0, w1)
	}
	if b0 != "◆" || b1 != "◆" {
		t.Errorf("blocked icon must stay ◆, got %q/%q", b0, b1)
	}
	// a full cycle returns to the first glyph
	wN, _ := icon(len(spinnerFrames))
	if wN != w0 {
		t.Errorf("frame len(spinnerFrames) should wrap to frame 0: %q vs %q", wN, w0)
	}
}

func TestRenderBlankLineBetweenWindowBlocks(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	for i, r := range rows {
		// a window anchor or session rule never sits directly under a hang
		// row: one blank spacer separates the blocks
		if (r.Kind == RowWindow || r.Kind == RowGroup) && i > 0 && rows[i-1].Kind == RowAgent {
			t.Errorf("row %d %q must be preceded by a spacer, got agent row %q", i, r.Text, rows[i-1].Text)
		}
		if r.Kind == RowSpacer {
			if r.Text != "" {
				t.Errorf("spacer row must be empty, got %q", r.Text)
			}
			if r.Current || r.PaneID != "" || r.ToggleFold {
				t.Errorf("spacer row must be inert: %+v", r)
			}
		}
	}
	// the first window of a group hugs its session rule — no spacer between
	for i, r := range rows {
		if r.Kind == RowGroup && i+1 < len(rows) && rows[i+1].Kind == RowSpacer {
			t.Errorf("no spacer allowed right after a session rule %q", r.Text)
		}
	}
}

func TestRenderMarksPendingTitles(t *testing.T) {
	agents := []state.Agent{
		mk("main", 1, "neo3", 1, "✳ [PENDING] Resume crashed session", detect.Working, t0),
		mk("main", 1, "neo3", 2, "✳ [pending] lower case too", detect.Idle, t0),
		mk("main", 1, "neo3", 3, "✳ Investigate JWT blocker", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 60})
	var pending, normal int
	for _, r := range rows {
		if r.Kind != RowAgent {
			if r.Pending {
				t.Errorf("non-agent row %q must not be pending", r.Text)
			}
			continue
		}
		if r.Pending {
			pending++
		} else {
			normal++
		}
	}
	if pending != 2 || normal != 1 {
		t.Errorf("pending=%d normal=%d, want 2/1", pending, normal)
	}
}

func TestRenderFooterOmitsReadOnly(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	footer := rows[len(rows)-1]
	if footer.Kind != RowFooter {
		t.Fatalf("last row kind = %v, want footer", footer.Kind)
	}
	if footer.Text != "C-t a jump · click jump" {
		t.Errorf("footer = %q, want %q", footer.Text, "C-t a jump · click jump")
	}
}

func TestRenderTreeConnectsSiblingPanes(t *testing.T) {
	agents := []state.Agent{
		mk("main", 5, "api", 1, "first", detect.Idle, t0),
		mk("main", 5, "api", 2, "middle", detect.Idle, t0),
		mk("main", 5, "api", 3, "last", detect.Idle, t0),
		mk("main", 6, "web", 1, "solo", detect.Idle, t0),
	}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 40})
	want := map[string]string{"first": "├", "middle": "├", "last": "└", "solo": "└"}
	for _, r := range rows {
		if r.Kind != RowAgent {
			continue
		}
		for title, branch := range want {
			if strings.Contains(r.Text, title) && !strings.Contains(r.Text, branch) {
				t.Errorf("row %q should use %q as its tree branch", r.Text, branch)
			}
		}
	}
}

func TestRenderAgentRowsHaveLeftPadding(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 40})
	seen := false
	for _, r := range rows {
		if r.Kind != RowAgent {
			continue
		}
		seen = true
		if !strings.HasPrefix(r.Text, " ") || strings.HasPrefix(r.Text, "  ") {
			t.Errorf("agent row %q must start with exactly one pad space", r.Text)
		}
		if lipgloss.Width(r.Text) > 40 {
			t.Errorf("agent row exceeds pane width 40: %q", r.Text)
		}
	}
	if !seen {
		t.Fatal("no agent rows rendered")
	}
}

func TestRenderSubagentRowsNestDeeper(t *testing.T) {
	a := mk("main", 5, "api", 1, "orchestrating the refactor", detect.Working, t0)
	a.Subagents = []detect.Subagent{
		{Type: "Explore", Title: "Map the config loaders", Done: true},
		{Type: "general-purpose", Title: "Refactor the billing report generator"},
	}
	rows := Render(ViewData{Agents: []state.Agent{a}, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 60})
	var subs []Row
	for _, r := range rows {
		if r.Kind == RowSubagent {
			subs = append(subs, r)
		}
	}
	if len(subs) != 2 {
		t.Fatalf("subagent rows = %d, want 2: %+v", len(subs), rows)
	}
	for _, s := range subs {
		if !strings.HasPrefix(s.Text, "     ") {
			t.Errorf("subagent row %q must indent deeper than its parent", s.Text)
		}
		if s.PaneID != "%api" {
			t.Errorf("subagent row must click-jump to the parent pane, got %q", s.PaneID)
		}
		if lipgloss.Width(s.Text) > 60 {
			t.Errorf("subagent row exceeds pane width: %q", s.Text)
		}
	}
	if !strings.Contains(subs[0].Text, "├") || !strings.Contains(subs[0].Text, "✓") ||
		!strings.Contains(subs[0].Text, "Explore") {
		t.Errorf("first subagent row should be a done ├ Explore entry: %q", subs[0].Text)
	}
	if !strings.Contains(subs[1].Text, "└") || !strings.Contains(subs[1].Text, "general-purpose") {
		t.Errorf("last subagent row should be a └ general-purpose entry: %q", subs[1].Text)
	}
}

func TestRenderSubagentWorkingIconAnimates(t *testing.T) {
	a := mk("main", 5, "api", 1, "orchestrating", detect.Working, t0)
	a.Subagents = []detect.Subagent{
		{Type: "general-purpose", Title: "running task", Working: true},
		{Type: "Explore", Title: "finished task", Done: true},
		{Type: "general-purpose", Title: "queued task"},
	}
	subs := func(frame int) []Row {
		rows := Render(ViewData{Agents: []state.Agent{a}, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 60, Frame: frame})
		var out []Row
		for _, r := range rows {
			if r.Kind == RowSubagent {
				out = append(out, r)
			}
		}
		return out
	}
	glyph := func(r Row) string { return strings.Fields(r.Text)[1] } // "├ <glyph> type · title"
	s0, s1 := subs(0), subs(1)
	if len(s0) != 3 || len(s1) != 3 {
		t.Fatalf("want 3 subagent rows, got %d/%d", len(s0), len(s1))
	}
	if glyph(s0[0]) == glyph(s1[0]) {
		t.Errorf("working subagent glyph must animate: %q vs %q", glyph(s0[0]), glyph(s1[0]))
	}
	if glyph(s0[1]) != "✓" || glyph(s1[1]) != "✓" {
		t.Errorf("done subagent must stay ✓, got %q/%q", glyph(s0[1]), glyph(s1[1]))
	}
	if glyph(s0[2]) != "○" || glyph(s1[2]) != "○" {
		t.Errorf("queued subagent must stay ○, got %q/%q", glyph(s0[2]), glyph(s1[2]))
	}
}

func TestRenderSpacerFollowsSubagentRows(t *testing.T) {
	withSubs := mk("main", 5, "api", 1, "orchestrating", detect.Working, t0)
	withSubs.Subagents = []detect.Subagent{{Type: "Explore", Title: "Map the loaders"}}
	agents := []state.Agent{withSubs, mk("main", 6, "web", 1, "idle prompt", detect.Idle, t0)}
	rows := Render(ViewData{Agents: agents, FoldHidden: true, HiddenPrefix: "_", Now: t0, Width: 60})
	for i, r := range rows {
		if r.Kind == RowWindow && strings.Contains(r.Text, "6:web") {
			if rows[i-1].Kind != RowSpacer {
				t.Errorf("window block after subagent rows must be preceded by a spacer, got %v %q",
					rows[i-1].Kind, rows[i-1].Text)
			}
		}
	}
}

func TestRenderPopupFooterShowsNavKeys(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0, Popup: true})
	footer := rows[len(rows)-1]
	if !strings.Contains(footer.Text, "esc") || !strings.Contains(footer.Text, "enter") {
		t.Errorf("popup footer %q should hint enter/esc navigation", footer.Text)
	}
	rows = Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0})
	if footer := rows[len(rows)-1]; strings.Contains(footer.Text, "enter") {
		t.Errorf("persistent footer %q must keep the jump hint", footer.Text)
	}
}

func TestRenderPopupAlertHintsPlainA(t *testing.T) {
	rows := Render(ViewData{Agents: testAgents(), FoldHidden: true, HiddenPrefix: "_", Now: t0, Popup: true})
	for _, r := range rows {
		if r.Kind == RowAlert {
			if strings.Contains(r.Text, "C-t") || !strings.Contains(r.Text, "a to jump") {
				t.Errorf("popup alert %q should hint plain 'a to jump'", r.Text)
			}
			return
		}
	}
	t.Fatal("no alert row")
}
