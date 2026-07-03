package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if c.PollIntervalMS != 1000 || c.DoneTTLMin != 10 || c.HiddenPrefix != "_" {
		t.Errorf("defaults wrong: %+v", c)
	}
}

func TestLoadPartialFileMergesDefaults(t *testing.T) {
	p := write(t, "done_ttl_min = 30\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.DoneTTLMin != 30 {
		t.Errorf("override lost: %+v", c)
	}
	if c.PollIntervalMS != 1000 {
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
