// Package config loads ~/.config/tmux-radar/config.toml with embedded defaults.
package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/poller"
)

// AgentRules is the per-agent-kind detection config.
type AgentRules struct {
	ProcessNames []string `toml:"process_names"`
	Working      []string `toml:"working"`
	Blocked      []string `toml:"blocked"`
}

// Config mirrors config.toml. Zero values are replaced by Default().
type Config struct {
	PollIntervalMS int `toml:"poll_interval_ms"`
	// HiddenPrefix folds windows whose name starts with it; explicitly
	// setting "" disables folding (kept verbatim, not defaulted).
	HiddenPrefix   string                `toml:"hidden_prefix"`
	FocusReturnCmd string                `toml:"focus_return_cmd"`
	Agents         map[string]AgentRules `toml:"agents"`
	// popup geometry, passed to tmux display-popup -w/-h (cells or "N%")
	PopupWidth  string `toml:"popup_width"`
	PopupHeight string `toml:"popup_height"`
}

// Default returns the compiled-in configuration.
func Default() Config {
	return Config{
		PollIntervalMS: 1000,
		HiddenPrefix:   "_",
		// landscape: wide enough for full pane titles, short enough to float
		PopupWidth:  "60%",
		PopupHeight: "60%",
		Agents: map[string]AgentRules{
			// Single source of truth: pattern strings live in detect/poller.
			"claude": {
				ProcessNames: poller.DefaultProcessPatterns(),
				Working:      detect.DefaultWorkingPatterns(),
				Blocked:      detect.DefaultBlockedPatterns(),
			},
			"codex": {
				ProcessNames: poller.DefaultCodexProcessPatterns(),
				Working:      detect.DefaultCodexWorkingPatterns(),
				Blocked:      detect.DefaultCodexBlockedPatterns(),
			},
		},
	}
}

// DefaultConfigPath returns ~/.config/tmux-radar/config.toml.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "tmux-radar", "config.toml")
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
	if c.PopupWidth == "" {
		c.PopupWidth = Default().PopupWidth
	}
	if c.PopupHeight == "" {
		c.PopupHeight = Default().PopupHeight
	}
	if len(c.Agents) == 0 {
		c.Agents = Default().Agents
	}
	// Field-level defaulting: TOML decoding replaces a re-declared agent
	// table wholesale, so an entry like [agents.claude] overriding only
	// `blocked` would otherwise silently drop the default process_names and
	// working patterns (and with them, all pane matching). A built-in kind
	// the file never mentions is kept whole: declaring [agents.claude] must
	// not silently disable codex detection.
	for k, d := range Default().Agents {
		a, ok := c.Agents[k]
		if !ok {
			c.Agents[k] = d
			continue
		}
		if len(a.ProcessNames) == 0 {
			a.ProcessNames = d.ProcessNames
		}
		if len(a.Working) == 0 {
			a.Working = d.Working
		}
		if len(a.Blocked) == 0 {
			a.Blocked = d.Blocked
		}
		c.Agents[k] = a
	}
	return c, nil
}

// Kinds compiles the configured agent tables into poller kinds, one per
// agent, sorted by name for deterministic match order.
func (c Config) Kinds() ([]poller.Kind, error) {
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	kinds := make([]poller.Kind, 0, len(names))
	for _, name := range names {
		a := c.Agents[name]
		k := poller.Kind{Name: name}
		for _, p := range a.ProcessNames {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, err
			}
			k.Process = append(k.Process, re)
		}
		for _, p := range a.Working {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, err
			}
			k.Rules.Working = append(k.Rules.Working, re)
		}
		for _, p := range a.Blocked {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, err
			}
			k.Rules.Blocked = append(k.Rules.Blocked, re)
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// PollerDeps builds poller dependencies from the config.
// process_names entries are regexes: Claude Code's auto-updater installs
// version-named binaries ("2.1.199"), so exact matching would find nothing.
func (c Config) PollerDeps() (poller.Deps, error) {
	kinds, err := c.Kinds()
	if err != nil {
		return poller.Deps{}, err
	}
	deps := poller.DefaultDeps()
	deps.Kinds = kinds
	return deps, nil
}
