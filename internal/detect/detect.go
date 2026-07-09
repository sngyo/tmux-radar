// Package detect classifies an agent pane from its visible screen content.
package detect

import (
	"regexp"
	"strings"
)

// State is the raw detected state of an agent pane.
type State string

const (
	Working State = "working"
	Blocked State = "blocked"
	Idle    State = "idle"
)

// Rules holds the screen-content patterns for one agent kind.
// Defaults target Claude Code; overridable via config (Task 10).
type Rules struct {
	Working []*regexp.Regexp
	Blocked []*regexp.Regexp
}

// DefaultWorkingPatterns and DefaultBlockedPatterns are the canonical
// Claude Code screen patterns. Config defaults reuse these exact strings,
// so pattern changes land in one place and cannot drift.
func DefaultWorkingPatterns() []string {
	return []string{
		`esc to interrupt`,
		// main turn is over but spawned subagents are still running
		`Waiting for \d+ background agents? to finish`,
		// main turn is over but a dynamic (self-paced) workflow is still
		// running; its wait line names "dynamic workflow", not "background
		// agents", so the pattern above never matches it.
		`Waiting for \d+ dynamic workflows? to finish`,
		// main turn is over but a background monitor is still running; the
		// footer counts it ("· 1 monitor ·") whenever one is live. A monitor
		// is not a background agent, so the wait line above never matches it.
		`· \d+ monitors? ·`,
	}
}

func DefaultBlockedPatterns() []string {
	return []string{
		`Do you want`,
		`❯ 1\.`,
		`Would you like to`,
	}
}

// askQuestionFooterRe matches the interactive footer Claude Code renders at the
// very bottom of a live AskUserQuestion card ("Enter to select · ↑/↓ to
// navigate · … · Esc to cancel"). The card carries an arbitrary, often
// non-English question and numbered options with no "❯ 1." caret, so this
// footer is the only stable, language-independent signal. It is matched ONLY
// against the last non-blank line (see Detect): once the card is answered — or
// merely quoted in the conversation — a live input box sits below it, so the
// footer is no longer last and the agent is not blocked.
var askQuestionFooterRe = regexp.MustCompile(`^\s*Enter to select\b`)

// DefaultRules returns the built-in Claude Code detection rules.
func DefaultRules() Rules {
	return Rules{
		Working: compile(DefaultWorkingPatterns()...),
		Blocked: compile(DefaultBlockedPatterns()...),
	}
}

func compile(patterns ...string) []*regexp.Regexp {
	rs := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		rs = append(rs, regexp.MustCompile(p))
	}
	return rs
}

// tailLines bounds detection to the bottom of the screen: dialogs and the
// running footer render near the input box, while conversation history
// higher up can echo the same phrases (e.g. a quoted "❯ 1." option list)
// and must not keep an agent looking blocked after the dialog is answered.
const tailLines = 30

// Detect classifies a screen from its bottom tailLines lines. Blocked wins
// over working: a permission prompt can appear while the spinner footer is
// still visible.
func (r Rules) Detect(screen string) State {
	screen = tail(screen, tailLines)
	// A live AskUserQuestion card owns the input region: its footer is the
	// last non-blank line. Answered or quoted cards sit above a live input box.
	if askQuestionFooterRe.MatchString(lastNonBlankLine(screen)) {
		return Blocked
	}
	for _, re := range r.Blocked {
		if re.MatchString(screen) {
			return Blocked
		}
	}
	for _, re := range r.Working {
		if re.MatchString(screen) {
			return Working
		}
	}
	return Idle
}

// lastNonBlankLine returns the final line of s that has non-whitespace content
// (capture-pane pads the screen with trailing blank lines and trailing spaces).
func lastNonBlankLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Subagent is one background task scraped from the agents list Claude Code
// renders under its input box while background agents exist.
type Subagent struct {
	Type    string `json:"type"`              // e.g. "general-purpose"
	Title   string `json:"title"`             // task description
	Working bool   `json:"working,omitempty"` // has a live runtime tail (elapsed/tokens)
	Done    bool   `json:"done,omitempty"`    // list glyph was ✓
}

var (
	// anchor row: "⏺ main" (glyph is U+23FA; older builds may use ●),
	// possibly behind a selection caret like ")" or "❯"
	subagentAnchorRe = regexp.MustCompile(`^[\s)❯>]*[⏺●○◯]\s+main\s*$`)
	// entry rows: "◯ general-purpose  Refactor the billing report generator"
	// (glyph is U+25EF; ✓ marks a finished task)
	subagentEntryRe = regexp.MustCompile(`^\s*([○◯●⏺✓])\s+(\S+)\s{2,}(.+?)\s*$`)
	// right-aligned runtime status ("1m 20s · ↓ 76.2k tokens") is separated
	// from the title by a wide space run
	subagentStatusRe = regexp.MustCompile(`\s{3,}`)
)

// Subagents scrapes the background-agents list from a screen. The list is
// anchored by a "● main" row; the indented rows that follow are subagents.
// Screen scraping is inherently version-sensitive: an unrecognized layout
// degrades to "no subagents", never to a wrong state.
func Subagents(screen string) []Subagent {
	lines := strings.Split(tail(screen, tailLines), "\n")
	start := -1
	for i, l := range lines {
		if subagentAnchorRe.MatchString(l) {
			start = i // last anchor wins: it sits closest to the input box
		}
	}
	if start < 0 {
		return nil
	}
	var subs []Subagent
	for _, l := range lines[start+1:] {
		m := subagentEntryRe.FindStringSubmatch(l)
		if m == nil {
			break
		}
		parts := subagentStatusRe.Split(m[3], 2)
		done := m[1] == "✓"
		// A running task carries a live runtime tail ("5m 39s · ↓ 105k
		// tokens") after the title; ✓ (done) takes precedence over a tail
		// that may linger on a just-finished row.
		working := !done && len(parts) == 2 && strings.TrimSpace(parts[1]) != ""
		subs = append(subs, Subagent{Type: m[2], Title: parts[0], Working: working, Done: done})
	}
	return subs
}
