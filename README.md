# tmux-radar

`tmux-radar` is a lightweight companion tool for tmux users who run multiple
AI coding agents (Claude Code first; others later) across many windows and
panes. It gives you a persistent agent sidebar that lives *outside* tmux
(e.g. in a terminal split next to it), so you can always see which agents are
working, blocked, or done without switching windows; a compact counter in the
tmux status line that surfaces blocked agents even when the sidebar is
hidden; and a single tmux keybind that jumps straight to whichever agent
needs your attention, cycling through the queue on repeated presses. Prefer
on-demand over always-on? The same sidebar also runs as a tmux popup with
keyboard browsing that live-previews each agent behind it.

## Screenshot

```
AGENTS                8 agents
◆ 1 blocked — C-t a to jump      ← full-width alert band, only when blocked > 0
─ main ───────────────────────
  11:web
 ○ └ ✳ waiting for input

  12:api                         ← the active window is highlighted
 ⠸ └ ✳ fixing tests              ← working rows spin
     └ ○ general-purpose · adding coverage   ← background subagents nest deeper

  14:worker
 ⠴ ├ ✳ building
 ◆ └ reviewing PR
─ monitor ────────────────────
  1:claude
 ○ └ ✳ [PENDING] parked task     ← [PENDING] titles are grayed out
▸ _hidden — 8 agents             ← fold; click to expand; shows ◆n if any
C-t a jump · click jump
```

## Install

```bash
go install github.com/sngyo/tmux-radar/cmd/tmux-radar@latest
```

Or build from source:

```bash
git clone https://github.com/sngyo/tmux-radar.git
cd tmux-radar
go build -o tmux-radar ./cmd/tmux-radar
```

## Quick start

Add the following to your host configuration, then start `tmux-radar
sidebar` in a terminal split outside tmux (e.g. a Ghostty split next to your
tmux pane).

**`.tmux.conf`** — jump keybind and status-line summary:

```tmux
bind a run-shell "tmux-radar jump"
set -g status-right '#(tmux-radar summary) …'
```

> The tmux server often runs with a minimal `PATH`; if the binds do
> nothing, spell out the full binary path (e.g.
> `~/go/bin/tmux-radar` or `~/.local/bin/tmux-radar`).

**Popup mode** (optional) — prefer on-demand over always-on? Bind a key to
open the same sidebar in a tmux popup (tmux ≥ 3.2):

```tmux
bind r run-shell -b "tmux-radar popup"
```

Inside the popup, `n`/`p` (also `C-n`/`C-p`, `j`/`k`, arrows) move a
selection cursor — and the client *behind* the popup switches to each
selected agent as you browse, like a live preview. `a` jumps straight to
the next agent needing attention (same queue as `jump` — handy because a
focused popup swallows the tmux prefix, so the usual prefix+`a` reflex
still lands on this key). `enter` keeps the current selection and closes;
`esc` returns to where you started and closes; a mouse click jumps and
closes; `q` just closes.

**Ghostty** (optional) — toggle the sidebar split's zoom with one key:

```ini
# ghostty config — hide/show the sidebar split with one key
keybind = super+shift+enter=toggle_split_zoom
```

**Hammerspoon** (optional) — return keyboard focus to the terminal split
running tmux after a mouse-click jump in the sidebar:

```lua
-- hammerspoon (optional): return focus to the tmux split after a click jump.
-- Exposed via hs.ipc so the sidebar can call: hs -c "focusTmuxSplit()"
function focusTmuxSplit()
  -- send the Ghostty "goto right split" keybind to the frontmost window
  hs.eventtap.keyStroke({ "cmd", "alt" }, "right", 0,
    hs.application.get("Ghostty"))
end
```

## Subcommands

| Command | Description |
|---|---|
| `sidebar` | Runs the bubbletea sidebar TUI in the current terminal (the default when no subcommand is given). Polls tmux panes, renders the agent list, and supports mouse-click jump. `--popup` switches to the one-shot popup behavior described above. |
| `popup` | Opens `sidebar --popup` inside a `tmux display-popup` sized by `popup_width`/`popup_height`; the popup closes when the sidebar exits. |
| `summary` | Prints a ready-to-render tmux status-line string (e.g. `◆1 ●3 ○2`) by reading the cached state; intended for `status-right`. Prints nothing if the state is stale or missing. |
| `jump` | Switches the tmux client to the next agent needing attention (blocked agents first, then done agents, oldest first); repeated presses cycle through the queue. When nothing needs attention it falls back to touring the working agents (oldest first); only when nothing is working either does it show a status message and stay put. |
| `watch` | Runs the poller headlessly in the foreground, writing state to disk on every tick — useful for status-line/jump support without running the sidebar TUI. |
| `version` | Prints the `tmux-radar` version string. |

## Configuration

`~/.config/tmux-radar/config.toml` (all keys optional; compiled-in defaults
are used for anything omitted):

```toml
poll_interval_ms = 1000
hidden_prefix    = "_"
focus_return_cmd = ""   # e.g. "hs -c 'focusTmuxSplit()'"
popup_width      = "60%" # tmux display-popup geometry: cells or "N%"
popup_height     = "60%"

[agents.claude]
# Regexes matched against pane_current_command. Claude Code's auto-updater
# installs version-named binaries ("2.1.199"), hence the version pattern.
process_names = ['^claude$', '^[0-9]+\.[0-9]+\.[0-9]+$']
working  = ['esc to interrupt', 'Waiting for \d+ background agents? to finish']
blocked  = ['Do you want', '❯ 1\.', 'Would you like to']
```

- `poll_interval_ms` — how often, in milliseconds, the poller re-scans tmux
  panes and re-evaluates each agent's state. The staleness window used by
  `summary` and `jump` scales with this value (3× the interval, minimum 3s).
- `hidden_prefix` — window-name prefix that gets folded into a single
  collapsible row in the sidebar; setting it to `""` disables folding
  entirely, so every window is always shown expanded.
- `focus_return_cmd` — an optional shell command run after a mouse-click
  jump, to return keyboard focus to the terminal split hosting the sidebar
  (e.g. a Hammerspoon call); left empty (the default), nothing runs.
- `popup_width` / `popup_height` — the `tmux-radar popup` window geometry,
  passed straight to `display-popup -w/-h`: a cell count (`"120"`) or a
  percentage of the client (`"60%"`). Defaults to a landscape 60% × 60%.
- `[agents.claude]` — per-agent detection rules. A partial override (e.g. a
  config that only sets `blocked`) inherits the remaining fields
  (`process_names`, `working`) from the compiled-in defaults for that agent,
  so you never have to restate patterns you don't want to change.
  - `process_names` — regexes matched against tmux's `pane_current_command`
    to recognize the agent's process.
  - `working` — regexes matched against captured pane output to detect the
    `working` state.
  - `blocked` — regexes matched against captured pane output to detect the
    `blocked` (permission/question) state.

## State semantics

| State | Rule (defaults; regex list in config) |
|---|---|
| `working` | Footer contains `esc to interrupt`, or the pane is waiting for background agents to finish |
| `blocked` | Permission / question UI detected: `Do you want to proceed?`, `❯ 1. Yes`, plan-approval prompts, AskUserQuestion chrome |
| `idle` | Agent process alive, none of the above |
| `done` (overlay) | On a `working → idle` transition, armed as an unseen-completion marker. Displayed as `✓` until you visit the pane (C-t a / click), then plain `idle`. |

Precedence: `blocked` > `working` > `done` > `idle`.

Extras layered on top:

- **Background subagents** — while a Claude Code pane shows its agents list
  (`⏺ main` + task rows), those tasks are scraped and nested one level below
  the pane's row. An unrecognized layout simply shows no subagents.
- **`[PENDING]` marker** — put `[PENDING]` anywhere in a pane title to gray
  the row out (parked on purpose; still tracked, just visually muted).

## License

MIT — see [LICENSE](LICENSE).
