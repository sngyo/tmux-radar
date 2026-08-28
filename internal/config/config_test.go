package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/poller"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file must not error, got %v", err)
	}
	if c.PollIntervalMS != 1000 || c.HiddenPrefix != "_" {
		t.Errorf("defaults wrong: %+v", c)
	}
}

func TestLoadPartialFileMergesDefaults(t *testing.T) {
	p := write(t, "poll_interval_ms = 2000\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.PollIntervalMS != 2000 {
		t.Errorf("override lost: %+v", c)
	}
	if c.HiddenPrefix != "_" {
		t.Errorf("default lost: %+v", c)
	}
}

func TestLoadMalformedFallsBackWithError(t *testing.T) {
	p := write(t, "this is not toml ===")
	c, err := Load(p)
	if err == nil {
		t.Error("malformed config must surface an error for the warning line")
	}
	if c.PollIntervalMS != 1000 {
		t.Errorf("fallback defaults wrong: %+v", c)
	}
}

func kindByName(t *testing.T, kinds []poller.Kind, name string) poller.Kind {
	t.Helper()
	for _, k := range kinds {
		if k.Name == name {
			return k
		}
	}
	t.Fatalf("kind %q not found in %+v", name, kinds)
	return poller.Kind{}
}

func TestKindsCompileCustomRules(t *testing.T) {
	p := write(t, `
[agents.claude]
process_names = ["claude"]
working = ['spinning']
blocked = ['approve\?']
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := c.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	claude := kindByName(t, kinds, "claude")
	if got := claude.Rules.Detect("please approve?"); got != detect.Blocked {
		t.Errorf("custom rule not applied, got %s", got)
	}
}

func TestKindsBadRegexErrors(t *testing.T) {
	p := write(t, `
[agents.claude]
process_names = ["claude"]
blocked = ['(unclosed']
`)
	c, _ := Load(p)
	if _, err := c.Kinds(); err == nil {
		t.Error("invalid regex must error")
	}
}

// Rules are per kind: a codex pane must not be judged by claude patterns.
// The claude blocked pattern "Would you like to" literally appears in the
// codex approval dialog, but only codex's own rules may fire there.
func TestKindsAreIsolatedPerAgent(t *testing.T) {
	kinds, err := Default().Kinds()
	if err != nil {
		t.Fatal(err)
	}
	codex := kindByName(t, kinds, "codex")
	if got := codex.Rules.Detect("✳ Compacting… (esc to interrupt)"); got != detect.Idle {
		t.Errorf("claude working screen under codex rules: got %s, want idle", got)
	}
	claude := kindByName(t, kinds, "claude")
	if got := claude.Rules.Detect("Would you like to run the following command?"); got != detect.Blocked {
		t.Errorf("claude blocked pattern gone: got %s", got)
	}
}

// A config that redeclares one agent table must not drop the other built-in
// kinds: [agents.claude] alone would otherwise silently disable codex.
func TestLoadKeepsMissingDefaultKinds(t *testing.T) {
	p := write(t, `
[agents.claude]
blocked = ['custom pattern']
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	codex, ok := c.Agents["codex"]
	if !ok {
		t.Fatal("codex defaults dropped by a claude-only config")
	}
	d := Default().Agents["codex"]
	if len(codex.ProcessNames) == 0 || codex.ProcessNames[0] != d.ProcessNames[0] {
		t.Errorf("codex process_names = %+v, want defaults", codex.ProcessNames)
	}
}

func TestDefaultConfigDetectsCodexStates(t *testing.T) {
	kinds, err := Default().Kinds()
	if err != nil {
		t.Fatal(err)
	}
	codex := kindByName(t, kinds, "codex")
	cases := []struct {
		screen string
		want   detect.State
	}{
		{"• Working (7s • esc to interrupt)", detect.Working},
		{"› 1. Yes, proceed (y)\n  Press enter to confirm or esc to cancel", detect.Blocked},
		{"› Ask Codex to do anything", detect.Idle},
	}
	for _, c := range cases {
		if got := codex.Rules.Detect(c.screen); got != c.want {
			t.Errorf("%q: got %s, want %s", c.screen, got, c.want)
		}
	}
}

func TestLoadPartialAgentOverrideInheritsDefaults(t *testing.T) {
	p := write(t, `
[agents.claude]
blocked = ['custom pattern']
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	a := c.Agents["claude"]
	d := Default().Agents["claude"]
	if len(a.ProcessNames) == 0 || a.ProcessNames[0] != d.ProcessNames[0] {
		t.Errorf("process_names not inherited: %+v", a.ProcessNames)
	}
	if len(a.Working) == 0 || a.Working[0] != d.Working[0] {
		t.Errorf("working not inherited: %+v", a.Working)
	}
	if len(a.Blocked) != 1 || a.Blocked[0] != "custom pattern" {
		t.Errorf("blocked override lost: %+v", a.Blocked)
	}
}

// The sidebar builds rules from config defaults, not detect.DefaultRules,
// so config defaults must classify the background-agent wait as working.
func TestDefaultConfigDetectsBackgroundAgentWait(t *testing.T) {
	kinds, err := Default().Kinds()
	if err != nil {
		t.Fatal(err)
	}
	screen := "✳ Waiting for 1 background agent to finish"
	if got := kindByName(t, kinds, "claude").Rules.Detect(screen); got != detect.Working {
		t.Errorf("got %s, want working", got)
	}
}

func TestPopupGeometryDefaultsAndOverride(t *testing.T) {
	c := Default()
	if c.PopupWidth != "60%" || c.PopupHeight != "60%" {
		t.Errorf("default popup geometry = %s x %s, want 60%% x 60%%", c.PopupWidth, c.PopupHeight)
	}
	loaded, err := Load(write(t, "popup_width = \"120\"\npopup_height = \"80%\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PopupWidth != "120" || loaded.PopupHeight != "80%" {
		t.Errorf("loaded popup geometry = %s x %s", loaded.PopupWidth, loaded.PopupHeight)
	}
	empty, err := Load(write(t, "poll_interval_ms = 500\n"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.PopupWidth != "60%" || empty.PopupHeight != "60%" {
		t.Errorf("omitted keys must fall back to defaults, got %s x %s", empty.PopupWidth, empty.PopupHeight)
	}
}
