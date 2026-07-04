package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sngyo/tmux-radar/internal/detect"
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

func TestDetectRulesCompiles(t *testing.T) {
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
	r, err := c.DetectRules()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Detect("please approve?"); got != "blocked" {
		t.Errorf("custom rule not applied, got %s", got)
	}
}

func TestDetectRulesBadRegexErrors(t *testing.T) {
	p := write(t, `
[agents.claude]
process_names = ["claude"]
blocked = ['(unclosed']
`)
	c, _ := Load(p)
	if _, err := c.DetectRules(); err == nil {
		t.Error("invalid regex must error")
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
	rules, err := Default().DetectRules()
	if err != nil {
		t.Fatal(err)
	}
	screen := "✳ Waiting for 1 background agent to finish"
	if got := rules.Detect(screen); got != detect.Working {
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
