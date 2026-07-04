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

func TestPopupArgsWrapSidebarInvocation(t *testing.T) {
	args := popupArgs("/path with space/tmux-radar", "60%", "60%")
	want := []string{"display-popup", "-E", "-w", "60%", "-h", "60%",
		`"/path with space/tmux-radar" sidebar --popup`}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestUsageMentionsPopup(t *testing.T) {
	var out strings.Builder
	if code := run([]string{"no-such-cmd"}, &out); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(out.String(), "popup") {
		t.Errorf("usage %q should mention popup", out.String())
	}
}
