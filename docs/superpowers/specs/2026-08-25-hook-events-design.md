# Hook-event working detection — design

Date: 2026-08-25
Status: approved

## Problem

Radar classifies a pane as working by scraping Claude Code's TUI text
(spinner line, "Waiting for N background agents", the subagent task
list). Every Claude Code release that redraws those surfaces breaks
detection; the last five fix commits are all pattern chasing, most
recently the "(+N)" collapsed-workflow suffix. Scraping is inherently
reactive: it models a UI that is not a contract.

Claude Code hooks are a contract. They fire on turn and subagent
lifecycle events, cost ~5 ms per event (measured), add zero tokens when
the hook prints nothing, and are configured once in
`~/.claude/settings.json` for every project and session.

## Goals / non-goals

- Goal: make working⇔idle detection event-driven for sessions that have
  the hooks installed, with screen scraping kept as fallback so an
  uninstrumented or mid-migration session behaves exactly as today.
- Goal: keep all classification logic in Go where it is unit-testable;
  hook commands stay logic-free.
- Non-goal: blocked detection stays scrape-based (`Do you want`,
  AskUserQuestion footer, etc.). Historical breakage has been on the
  working side; permission UIs churn less.
- Non-goal: no daemon/socket. The poller keeps its pull model.

## Architecture

```
Claude Code session
  hooks: UserPromptSubmit / Stop / SubagentStart / SubagentStop /
         SessionStart / SessionEnd            (set tuned by verification)
    └─ tmux-radar hook <event>
         appends one JSON line to ~/.local/state/tmux-radar/events/<pane-id>.jsonl

radar poller, each tick
  ├─ scrape (unchanged): capture-pane → detect.Rules / detect.Subagents
  ├─ hook state: read events/<pane>.jsonl → derive working / not-working
  └─ merge: blocked(scrape) > working(hook OR scrape) > idle
```

## Hook subcommand

`tmux-radar hook <event-name>`:

- Resolves the pane from `$TMUX_PANE`. Outside tmux, or with the
  variable unset, exit 0 silently.
- Reads the hook payload JSON from stdin, flattens it to one line, and
  appends `{"event": <argv>, "pane": <pane>, "ts": <now>, "payload":
  {...}}` to `~/.local/state/tmux-radar/events/<pane-id>.jsonl`
  (O_APPEND; a single line well under PIPE_BUF, so concurrent hooks
  interleave whole lines).
- Never writes to stdout (token cost stays zero) and always exits 0 —
  a radar bug must never block Claude Code.

## Poller derivation (Go, unit-tested)

Per pane, reduce the event log in order:

- `UserPromptSubmit` → turn on; `Stop` → turn off.
- `SubagentStart` adds the subagent id to a live set; `SubagentStop`
  removes it. If payloads carry no usable id (verification will tell),
  degrade to a counter.
- hook-working = turn on OR live set non-empty. This is the exact case
  scraping keeps fumbling: main turn over, background agents running.
- `SessionStart` → truncate that pane's log: every restart resets any
  drifted state (crashed turns, missed Stop events). `SessionEnd` →
  clear turn and live set: the pane reads not-working immediately.
- The hook state for a pane is consulted only if the pane's process
  already passes the existing claude-process filter; the poller GCs
  event files whose pane no longer exists or whose mtime is older
  than 48 h.

Merge in `poller.RunOnce`: scrape blocked wins (permission prompts
appear mid-turn); otherwise working if either source says working;
else idle. A session without hooks produces no log and follows the
scrape-only path — behavior today, so migration cannot regress.

## install-hooks / uninstall-hooks

- `tmux-radar install-hooks`: back up `~/.claude/settings.json` to
  `settings.json.bak-<ts>`, then merge one entry per event invoking
  `tmux-radar hook <event>` (absolute path of the current binary) with
  a 5-second timeout — a wedged binary must never hold a turn for the
  10-minute default — preserving unrelated hooks. Idempotent:
  re-running replaces radar's entries in place.
- `tmux-radar uninstall-hooks`: remove entries whose command contains
  the `tmux-radar hook` marker; leave everything else untouched.
- README documents both plus the migration note (running sessions pick
  hooks up only after restart).

## Verification phase (before implementation)

The docs leave three behaviors unspecified; pin them empirically with a
temporary probe hook that logs every event and payload:

1. Does `Stop` fire while background agents/tasks are still running?
2. What do `SubagentStart`/`SubagentStop` payloads contain (ids?), and
   do `TaskCreated`/`TaskCompleted` fire for background Agent-tool
   tasks?
3. Does an automatic resume after a task notification fire
   `UserPromptSubmit`?

The event set above is adjusted to whatever the probe shows; captured
payloads become test fixtures.

## Testing

- Derivation, merge, and settings.json merge logic: table-driven unit
  tests, fixtures from the probe (existing `testdata` pattern).
- `hook` subcommand: unit test append/atomicity/no-stdout behavior with
  a fake `$TMUX_PANE`.
- End-to-end sanity: one instrumented live session compared against the
  sidebar before the scrape patterns are ever touched again.

## Rollback

`uninstall-hooks` plus binary rollback returns radar to pure scraping;
event logs are inert data and can be deleted at any time.
