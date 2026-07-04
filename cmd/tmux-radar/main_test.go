package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if got := run([]string{"version"}, &out); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if !strings.Contains(out.String(), "tmux-radar") {
		t.Errorf("output %q does not contain binary name", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if got := run([]string{"bogus"}, &out); got != 2 {
		t.Fatalf("exit = %d, want 2", got)
	}
}
