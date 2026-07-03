// Package detect classifies an agent pane from its visible screen content.
package detect

import "regexp"

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

// Detect classifies a screen. Blocked wins over working: a permission
// prompt can appear while the spinner footer is still visible.
func (r Rules) Detect(screen string) State {
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
