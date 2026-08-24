package hookevents

import (
	"testing"
	"time"
)

func ev(name string, payload string) Event {
	e := Event{Event: name, Pane: "%20", TS: t0}
	if payload != "" {
		e.Payload = []byte(payload)
	}
	return e
}

// wnow is a fixed evaluation time for tests that don't exercise the age
// cap: well within maxAgentAge of t0, so it never masks a live entry.
var wnow = t0.Add(time.Minute)

func TestWorkingTurnLifecycle(t *testing.T) {
	if Working([]Event{ev("SessionStart", "")}, wnow) {
		t.Error("fresh session is not working")
	}
	if !Working([]Event{ev("SessionStart", ""), ev("UserPromptSubmit", "")}, wnow) {
		t.Error("prompt submitted → working")
	}
	if Working([]Event{ev("UserPromptSubmit", ""), ev("Stop", "")}, wnow) {
		t.Error("turn ended → not working")
	}
	if Working([]Event{ev("UserPromptSubmit", ""), ev("StopFailure", "")}, wnow) {
		t.Error("failed turn ended → not working")
	}
}

// The case scraping keeps fumbling: main turn over, background agent live.
func TestWorkingSurvivesStopWhileSubagentRuns(t *testing.T) {
	evs := []Event{
		ev("UserPromptSubmit", ""),
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("Stop", ""),
	}
	if !Working(evs, wnow) {
		t.Fatal("live subagent must keep the pane working after Stop")
	}
	evs = append(evs, ev("SubagentStop", `{"agent_id":"a1"}`))
	if Working(evs, wnow) {
		t.Fatal("last subagent done → not working")
	}
}

func TestWorkingAnonymousSubagentCounting(t *testing.T) {
	evs := []Event{
		ev("SubagentStart", "{}"), ev("SubagentStart", "{}"), ev("Stop", ""),
		ev("SubagentStop", "{}"),
	}
	if !Working(evs, wnow) {
		t.Fatal("one of two anonymous subagents still live")
	}
	if Working(append(evs, ev("SubagentStop", "{}")), wnow) {
		t.Fatal("both anonymous subagents done")
	}
}

// The probe found SubagentStop firing routinely with unknown, non-empty
// agent_ids and no matching Start (probe notes, surprise 1). A stop whose
// id is empty must only ever drain the anonymous counter, never an
// id-tracked live entry — otherwise an unrelated anonymous-shaped stop
// would wrongly clear a live, identified agent. This supersedes the
// pre-probe design's "drain any entry" fallback.
func TestWorkingAnonymousStopDoesNotDrainIDTrackedEntry(t *testing.T) {
	evs := []Event{
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("SubagentStop", "{}"),
	}
	if !Working(evs, wnow) {
		t.Fatal("anonymous stop must not drain an id-tracked live entry; anon counter is already 0")
	}
}

// A stop for an id that never had a matching Start must be ignored outright,
// not drained against some unrelated live entry (probe notes, surprise 1).
func TestWorkingUnknownIDStopIgnored(t *testing.T) {
	evs := []Event{
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("SubagentStop", `{"agent_id":"unknown"}`),
	}
	if !Working(evs, wnow) {
		t.Fatal("an unknown-id stop must not clear the live a1 entry")
	}
}

func TestWorkingSessionEndResetsEverything(t *testing.T) {
	evs := []Event{
		ev("UserPromptSubmit", ""),
		ev("SubagentStart", `{"agent_id":"a1"}`),
		ev("SessionEnd", ""),
	}
	if Working(evs, wnow) {
		t.Fatal("SessionEnd clears turn and live set")
	}
}

func TestWorkingEmptyLog(t *testing.T) {
	if Working(nil, wnow) {
		t.Error("no events → not working")
	}
}

// A missed SubagentStop (crashed agent, dropped hook) must not pin a pane
// working for a whole session: live entries older than maxAgentAge are
// ignored relative to the evaluation time, regardless of whether a stop
// ever fires. Turn state has no such cap (Stop is reliable per the probe).
func TestWorkingAgeCapExpiresStaleLiveEntry(t *testing.T) {
	now := t0.Add(3 * time.Hour)

	stale := []Event{{Event: "SubagentStart", Pane: "%20", TS: t0, Payload: []byte(`{"agent_id":"a1"}`)}}
	if Working(stale, now) {
		t.Fatal("a subagent started 3h before now (> maxAgentAge) must be treated as stale")
	}

	fresh := []Event{{Event: "SubagentStart", Pane: "%20", TS: now.Add(-time.Hour), Payload: []byte(`{"agent_id":"a1"}`)}}
	if !Working(fresh, now) {
		t.Fatal("a subagent started 1h before now (< maxAgentAge) must still be working")
	}
}

// A missed Stop (crashed process, dropped hook, or a stale tail left behind
// by uninstall-hooks) must not pin a pane working forever: a turn whose
// UserPromptSubmit is older than maxTurnAge relative to now is treated as
// stale, even with no Stop in the log.
func TestWorkingTurnAgeCapExpiresStaleTurn(t *testing.T) {
	now := t0.Add(13 * time.Hour)

	stale := []Event{{Event: "UserPromptSubmit", Pane: "%20", TS: t0}}
	if Working(stale, now) {
		t.Fatal("a turn started 13h before now (> maxTurnAge) must be treated as stale")
	}

	fresh := []Event{{Event: "UserPromptSubmit", Pane: "%20", TS: now.Add(-time.Hour)}}
	if !Working(fresh, now) {
		t.Fatal("a turn started 1h before now (< maxTurnAge) must still be working")
	}
}

// Replays the Task 4 probe fixture and asserts the exact intermediate
// states the probe notes describe: the main-turn Stop that fires while a
// subagent is still running, the unknown-id SubagentStop that must NOT
// clear it, the matching SubagentStop that does, the auto-resumed turn,
// and the final idle state.
func TestWorkingProbeFixtureReplay(t *testing.T) {
	evs, err := ReadPane("testdata", "probe-background-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 11 {
		t.Fatalf("fixture must have exactly 11 events, got %d: %+v", len(evs), evs)
	}
	now := evs[len(evs)-1].TS.Add(time.Minute)

	if !Working(evs[:6], now) {
		t.Error("main turn stopped but subagent live → still working")
	}
	if !Working(evs[:7], now) {
		t.Error("unknown-id SubagentStop must not clear the live agent")
	}
	if Working(evs[:8], now) {
		t.Error("matching SubagentStop drains the live agent → not working")
	}
	if !Working(evs[:9], now) {
		t.Error("auto-resumed UserPromptSubmit → working again")
	}
	if Working(evs, now) {
		t.Error("final state: turn stopped, stray unknown-id stop ignored → idle")
	}
}
