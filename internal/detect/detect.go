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

// DefaultRules returns the built-in Claude Code detection rules.
func DefaultRules() Rules {
	return Rules{
		Working: compile(`esc to interrupt`),
		Blocked: compile(
			`Do you want`,
			`❯ 1\.`,
			`Would you like to`,
		),
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

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
