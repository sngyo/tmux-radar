package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/sngyo/tmux-radar/internal/hookevents"
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

func TestCmdHookAppendsEvent(t *testing.T) {
	dir := t.TempDir()
	code := cmdHook(dir, "%20", "UserPromptSubmit", strings.NewReader(`{"prompt":"x"}`))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	evs, err := hookevents.ReadPane(dir, "%20")
	if err != nil || len(evs) != 1 || evs[0].Event != "UserPromptSubmit" {
		t.Fatalf("got %+v, %v", evs, err)
	}
}

// Outside tmux there is no pane to attribute the event to: exit 0, no file.
func TestCmdHookNoPaneIsSilentNoop(t *testing.T) {
	dir := t.TempDir()
	if code := cmdHook(dir, "", "Stop", strings.NewReader("{}")); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("no event file may be created without a pane")
	}
}

// The hook path must never block Claude Code: even an unwritable dir exits 0.
func TestCmdHookNeverFails(t *testing.T) {
	if code := cmdHook("/dev/null/nodir", "%20", "Stop", strings.NewReader("{}")); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// `hook` with no event name (misconfigured settings) exits 0 quietly.
func TestRunHookWithoutEventName(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"hook"}, &out); code != 0 || out.Len() != 0 {
		t.Fatalf("exit=%d out=%q, want 0 and empty", code, out.String())
	}
}
