# Hook-event probe notes

Date: 2026-08-25
Probe: Claude Code v2.1.2xx, fresh session in tmux pane %195, hooks from
`tmux-radar install-hooks` (probe superset), events logged by the `hook`
subcommand itself. Fixture: `internal/hookevents/testdata/probe-background-agent.jsonl`.

## Answers to the spec's three questions

**(a) Does `Stop` fire while background agents are still running? — YES.**
Observed order: `SubagentStart` 02:22:19 → main-turn `Stop` 02:22:21 while
the agent kept running. The live-set derivation is required, as designed.

**(b) Subagent payload fields — `agent_id` (+ `agent_type`).**
`SubagentStart`: `{session_id, prompt_id, agent_id, agent_type,
hook_event_name}`. `SubagentStop` carries the same `agent_id` for the
matching agent. The plan's guess (`agent_id`) holds.

**(c) Does an automatic resume fire `UserPromptSubmit`? — YES.**
When a background task finishes, the session auto-resumes with a
`UserPromptSubmit` whose `prompt` is the `<task-notification>` block.
Auto-resumed turns are indistinguishable from user turns — which is
exactly right for working detection.

## Surprises that change the derivation

1. **`SubagentStop` fires without a matching `SubagentStart`, routinely.**
   Stops arrived with unknown `agent_id`s and empty `agent_type` (e.g. one
   ~3 s after a turn's `Stop`) that never had a Start. The plan's
   "drain any live entry on unmatched stop" fallback would wrongly clear
   live agents. Derivation rule instead: remove only ids present in the
   live set; ignore stops for unknown ids. Anonymous counting remains only
   for the hypothetical start-without-id case (not observed; all observed
   Starts carry ids).
2. **`TaskCreated` / `TaskCompleted` never fired** — not for the Agent
   tool, not for a background shell. Removed from the installed event set.
3. **Background *shells* (`run_in_background` Bash) produce no hook events
   at all** ("1 shell still running" in the footer). Hook-derived working
   cannot see them; the scrape fallback's footer patterns remain the only
   signal. Acceptable: a lone background shell after turn end is closer to
   idle than working for radar's purposes.
4. **`StopFailure` did not fire** during the probe (no failed turn was
   produced). Kept in the installed set anyway: it is documented as the
   failure-side twin of `Stop`, and handling it in derivation is free.
   Turn-off on `Stop` alone would otherwise risk stuck-working after an
   errored turn.

## Event set decision (hooksetup.Events after trim)

Keep: `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `Stop`,
`StopFailure`, `SubagentStart`, `SubagentStop`.
Drop: `TaskCreated`, `TaskCompleted` (never fired; no signal value).

## Derivation adjustments for Task 5

- `subagentID` reads payload key `agent_id` (confirmed).
- `SubagentStop` with an id not in the live set is ignored (no drain-any).
- Add a per-entry age cap when evaluating liveness (entries older than 2 h
  vs. the evaluation time are ignored) as a drift bound: a missed
  `SubagentStop` must not pin a pane working for a whole session. This
  changes the signature to `Working(events []Event, now time.Time) bool`.
- `SessionStart` truncation verified live: `/clear` fired `SessionStart`
  and the pane log collapsed to that single line. `/exit` fired
  `SessionEnd` (logged well inside its 1.5 s budget).

## Other observations

- The unmatched empty-type `SubagentStop`s trailed the `Stop` of
  tool-using turns by ~2–3 s (turn 1, which used no tools, produced none).
  Whatever their internal meaning, the ignore-unknown-ids rule absorbs
  them.
- The background shell's completion produced no hook event and no
  auto-resume turn during the observation window; its "1 shell" footer
  count simply disappeared.
