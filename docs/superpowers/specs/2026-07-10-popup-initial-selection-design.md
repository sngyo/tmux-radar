# Popup initial selection — design

Date: 2026-07-10
Status: approved

## Problem

`prefix-r` opens the radar popup with no selection cursor: the first
`n`/`j` press always starts from the top of the list. Users usually open
the popup while sitting next to one of the listed agents, so the cursor
should start at the agent row nearest to where the client currently is.

## Behavior

- When the popup starts, seed the selection cursor once, on the first
  successful poll. Later polls never move it.
- Nearest-row priority, evaluated against the attached client's focus
  (`Snapshot.Focus`):
  1. the agent whose `PaneID` equals `Focus.PaneID` (exact match)
  2. the first agent in the same session + window index
  3. the first agent in the same session
  4. the first visible agent (top of the list)
- "First" and "top" follow the rendered order: session asc → window
  index asc → pane index asc.
- While the hidden fold is closed (the default), agents in hidden
  windows (window name starting with the hidden prefix) are excluded
  from all tiers. If no visible agent exists, the popup opens with no
  selection, as today.
- Seeding only places the cursor. It does not trigger the live-preview
  jump and does not record `origin`; `esc` before any manual move
  closes the popup without jumping, exactly as today.

## Implementation

- `internal/ui/render.go`: extract the sort in `Render` into
  `sortAgents([]state.Agent)` so the render order and the seeding
  helper cannot drift apart.
- `internal/ui/app.go`:
  - add `nearestPane(agents []state.Agent, focus tmux.Focus,
    hiddenPrefix string, foldHidden bool) string` — a pure function
    implementing the priority above; returns `""` when no candidate.
  - add a `seeded bool` field to `App`. In the `snapMsg` branch of
    `Update`, when `a.popup && !a.seeded && m.err == nil`, set
    `a.selPane = nearestPane(...)` and `a.seeded = true`.
- `move()` needs no change: with `selPane` already set, the existing
  index lookup continues relative movement from the seeded row.

## Testing

- Unit-test `nearestPane`: each of the four tiers, the fold exclusion
  (hidden window skipped while folded, eligible when unfolded), and the
  zero-agent case.
- App-level test: a popup `App` receiving a first `snapMsg` seeds
  `selPane`; a second `snapMsg` with a different focus does not
  overwrite it; a non-popup `App` never seeds.

## Out of scope

- Auto-unfolding the hidden section when the current pane lives in a
  hidden window.
- Seeding from the stale on-disk snapshot before the first poll.
