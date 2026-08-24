package hookevents

import (
	"encoding/json"
	"time"
)

// maxAgentAge bounds how long a SubagentStart can keep a pane "working"
// without a matching SubagentStop. A crashed agent or a dropped hook must
// not pin a pane working for the rest of the session, so live entries
// older than this relative to the evaluation time are ignored. Turn state
// has no such cap: the probe showed Stop firing reliably.
const maxAgentAge = 2 * time.Hour

// maxTurnAge is a defensive, last-resort cap on how long a running turn
// (UserPromptSubmit with no Stop/StopFailure yet) can keep a pane
// "working". The probe showed Stop firing reliably, so this is not a
// normal expiry path — it exists because a missed Stop (crashed process,
// dropped hook, or a stale tail left behind by uninstall-hooks with no
// future SessionStart to truncate it) must not pin a pane working for the
// rest of the session. Set far above any plausible legitimate turn length
// (long agentic loops can run for hours) so it only fires on drift.
const maxTurnAge = 12 * time.Hour

// subagentID extracts the identifier the probe found in Subagent* payloads.
// Field name pinned by docs/superpowers/specs/2026-08-25-hook-events-probe-notes.md.
func subagentID(e Event) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if e.Payload == nil || json.Unmarshal(e.Payload, &p) != nil {
		return ""
	}
	return p.AgentID
}

// Working reduces a pane's event log to "is this agent busy": a turn is
// running, or background subagents are still live, as of now. Drift toward
// stuck-working is the worst failure mode, so every unmatched signal drains
// rather than accumulates: SessionEnd resets all state, and the age cap
// bounds a missed SubagentStop. The one exception, forced by the probe, is
// SubagentStop for an unrecognized non-empty id: those fired routinely with
// no matching Start (probe notes, surprise 1), so draining on them would
// wrongly clear an unrelated live agent — such stops are ignored instead.
func Working(events []Event, now time.Time) bool {
	turn := false
	var turnStart time.Time
	live := map[string]time.Time{}
	var anon []time.Time

	for _, e := range events {
		switch e.Event {
		case "UserPromptSubmit":
			turn = true
			turnStart = e.TS
		case "Stop", "StopFailure":
			turn = false
		case "SubagentStart":
			if id := subagentID(e); id != "" {
				live[id] = e.TS
			} else {
				anon = append(anon, e.TS)
			}
		case "SubagentStop":
			switch id := subagentID(e); {
			case id != "":
				// Only drain a known live entry; an unrecognized id is a
				// routine probe surprise, not evidence the agent is gone.
				if _, ok := live[id]; ok {
					delete(live, id)
				}
			case len(anon) > 0:
				anon = anon[1:]
			}
		case "SessionEnd":
			turn, live, anon = false, map[string]time.Time{}, nil
			turnStart = time.Time{}
		}
	}

	if turn && now.Sub(turnStart) <= maxTurnAge {
		return true
	}
	cutoff := now.Add(-maxAgentAge)
	for _, ts := range live {
		if !ts.Before(cutoff) {
			return true
		}
	}
	for _, ts := range anon {
		if !ts.Before(cutoff) {
			return true
		}
	}
	return false
}
