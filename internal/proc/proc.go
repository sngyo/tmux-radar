// Package proc resolves pane process trees via a single ps invocation.
// Wrapper launchers hide the real agent from tmux: the npm-installed Codex
// CLI runs as "node <shim>" with the native codex binary as a child, so
// pane_current_command alone cannot identify the pane. Descendant process
// names can.
package proc

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Proc is one process as reported by ps.
type Proc struct {
	PID  int
	PPID int
	Name string
}

// DescendantNames returns, for each requested root pid, the names of every
// process below it (children, grandchildren, …), from one ps snapshot.
func DescendantNames(roots []int) (map[int][]string, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,ucomm=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return Descendants(ParsePS(string(out)), roots), nil
}

// ParsePS parses `ps -axo pid=,ppid=,ucomm=` output. Names may contain
// spaces; malformed lines are skipped.
func ParsePS(out string) []Proc {
	var procs []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, Proc{PID: pid, PPID: ppid, Name: strings.Join(f[2:], " ")})
	}
	return procs
}

// Descendants walks the process tree once and returns the names below each
// requested root, in depth-first order. A root with no descendants maps to nil.
func Descendants(procs []Proc, roots []int) map[int][]string {
	children := make(map[int][]Proc, len(procs))
	for _, p := range procs {
		children[p.PPID] = append(children[p.PPID], p)
	}
	res := make(map[int][]string, len(roots))
	for _, root := range roots {
		visited := map[int]bool{root: true}
		res[root] = walk(children, visited, root)
	}
	return res
}

func walk(children map[int][]Proc, visited map[int]bool, pid int) []string {
	var names []string
	for _, c := range children[pid] {
		if visited[c.PID] { // guard against pid-reuse cycles in the snapshot
			continue
		}
		visited[c.PID] = true
		names = append(names, c.Name)
		names = append(names, walk(children, visited, c.PID)...)
	}
	return names
}
