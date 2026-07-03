// Package config loads ~/.config/tmux-agents/config.toml with embedded defaults.
package config

import (
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/poller"
	"github.com/sngyo/tmux-agents/internal/tmux"
)

// AgentRules is the per-agent-kind detection config.
type AgentRules struct {
	ProcessNames []string `toml:"process_names"`
	Working      []string `toml:"working"`
	Blocked      []string `toml:"blocked"`
}

// Config mirrors config.toml. Zero values are replaced by Default().
type Config struct {
	PollIntervalMS int                   `toml:"poll_interval_ms"`
	DoneTTLMin     int                   `toml:"done_ttl_min"`
	HiddenPrefix   string                `toml:"hidden_prefix"`
	FocusReturnCmd string                `toml:"focus_return_cmd"`
	Agents         map[string]AgentRules `toml:"agents"`
}

// Default returns the compiled-in configuration.
func Default() Config {
	return Config{
		PollIntervalMS: 1000,
		DoneTTLMin:     10,
		HiddenPrefix:   "_",
		Agents: map[string]AgentRules{
			"claude": {
				// Regexes; the version pattern matches Claude Code's
				// auto-updated binaries named after the version.
				ProcessNames: []string{`^claude$`, `^[0-9]+\.[0-9]+\.[0-9]+$`},
				Working:      []string{`esc to interrupt`},
				Blocked:      []string{`Do you want`, `❯ 1\.`, `Would you like to`},
			},
		},
	}
}

// DefaultConfigPath returns ~/.config/tmux-agents/config.toml.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "tmux-agents", "config.toml")
}

// Load reads the config file. A missing file is not an error (defaults).
// A malformed file returns defaults plus the parse error so callers can warn.
func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return Default(), err
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return Default(), err
	}
	if c.PollIntervalMS <= 0 {
		c.PollIntervalMS = 1000
	}
	if c.DoneTTLMin <= 0 {
		c.DoneTTLMin = 10
	}
	if len(c.Agents) == 0 {
		c.Agents = Default().Agents
	}
	return c, nil
}

// DetectRules compiles the configured patterns (all agent kinds merged).
func (c Config) DetectRules() (detect.Rules, error) {
	var r detect.Rules
	for _, a := range c.Agents {
		for _, p := range a.Working {
			re, err := regexp.Compile(p)
			if err != nil {
				return r, err
			}
			r.Working = append(r.Working, re)
		}
		for _, p := range a.Blocked {
			re, err := regexp.Compile(p)
			if err != nil {
				return r, err
			}
			r.Blocked = append(r.Blocked, re)
		}
	}
	return r, nil
}

// PollerDeps builds poller dependencies from the config.
// process_names entries are regexes: Claude Code's auto-updater installs
// version-named binaries ("2.1.199"), so exact matching would find nothing.
func (c Config) PollerDeps() (poller.Deps, error) {
	rules, err := c.DetectRules()
	if err != nil {
		return poller.Deps{}, err
	}
	var pats []*regexp.Regexp
	for _, a := range c.Agents {
		for _, p := range a.ProcessNames {
			re, err := regexp.Compile(p)
			if err != nil {
				return poller.Deps{}, err
			}
			pats = append(pats, re)
		}
	}
	return poller.Deps{
		ListPanes:       tmux.ListPanes,
		Capture:         tmux.CapturePane,
		Rules:           rules,
		ProcessPatterns: pats,
		DoneTTL:         time.Duration(c.DoneTTLMin) * time.Minute,
	}, nil
}
