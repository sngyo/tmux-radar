package proc

import (
	"reflect"
	"testing"
)

func TestParsePS(t *testing.T) {
	// ps -axo pid=,ppid=,ucomm= output: right-aligned numeric columns, names
	// possibly containing spaces and padded with trailing blanks.
	out := " 2917  2562 zsh\n" +
		"82578  2917 node\n" +
		"82579 82578 codex           \n" +
		"  310     1 Codex Computer Use\n" +
		"garbage line without pids\n"
	got := ParsePS(out)
	want := []Proc{
		{PID: 2917, PPID: 2562, Name: "zsh"},
		{PID: 82578, PPID: 2917, Name: "node"},
		{PID: 82579, PPID: 82578, Name: "codex"},
		{PID: 310, PPID: 1, Name: "Codex Computer Use"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestDescendants(t *testing.T) {
	// pane shell (1) → node wrapper (2) → codex (3) → repl helper (4) with
	// further codex workers (5, 6); an unrelated tree (7 → 8) must not leak in.
	procs := []Proc{
		{PID: 1, PPID: 0, Name: "zsh"},
		{PID: 2, PPID: 1, Name: "node"},
		{PID: 3, PPID: 2, Name: "codex"},
		{PID: 4, PPID: 3, Name: "node_repl"},
		{PID: 5, PPID: 4, Name: "codex"},
		{PID: 6, PPID: 3, Name: "codex-code-mode"},
		{PID: 7, PPID: 0, Name: "zsh"},
		{PID: 8, PPID: 7, Name: "vim"},
	}
	got := Descendants(procs, []int{1, 99})
	want := map[int][]string{
		1:  {"node", "codex", "node_repl", "codex", "codex-code-mode"},
		99: nil,
	}
	if !reflect.DeepEqual(got[1], want[1]) {
		t.Errorf("root 1: got %v, want %v", got[1], want[1])
	}
	if len(got[99]) != 0 {
		t.Errorf("unknown root must have no descendants, got %v", got[99])
	}
	if names, ok := got[7]; ok {
		t.Errorf("unrequested root 7 must be absent, got %v", names)
	}
}
