package attention

import (
	"testing"
	"time"

	"github.com/sngyo/tmux-agents/internal/detect"
	"github.com/sngyo/tmux-agents/internal/state"
)

var t0 = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

func ag(id string, st detect.State, since time.Time, doneUntil time.Time) state.Agent {
	return state.Agent{PaneID: id, State: st, Since: since, DoneUntil: doneUntil}
}

func TestQueueOrdersBlockedThenDoneOldestFirst(t *testing.T) {
	agents := []state.Agent{
		ag("%idle", detect.Idle, t0, time.Time{}),
		ag("%done-old", detect.Idle, t0.Add(-2*time.Minute), t0.Add(time.Hour)),
		ag("%blocked-new", detect.Blocked, t0.Add(-time.Minute), time.Time{}),
		ag("%working", detect.Working, t0, time.Time{}),
		ag("%blocked-old", detect.Blocked, t0.Add(-5*time.Minute), time.Time{}),
		ag("%done-new", detect.Idle, t0.Add(-time.Minute), t0.Add(time.Hour)),
	}
	q := Queue(agents, t0)
	want := []string{"%blocked-old", "%blocked-new", "%done-old", "%done-new"}
	if len(q) != len(want) {
		t.Fatalf("queue len = %d, want %d", len(q), len(want))
	}
	for i, id := range want {
		if q[i].PaneID != id {
			t.Errorf("q[%d] = %s, want %s", i, q[i].PaneID, id)
		}
	}
}

func TestNextFromOutsideQueueReturnsHead(t *testing.T) {
	q := []state.Agent{ag("%a", detect.Blocked, t0, time.Time{}), ag("%b", detect.Blocked, t0, time.Time{})}
	got, ok := Next(q, "%elsewhere")
	if !ok || got.PaneID != "%a" {
		t.Errorf("got %v %v, want %%a", got.PaneID, ok)
	}
}

func TestNextCyclesFromCurrent(t *testing.T) {
	q := []state.Agent{ag("%a", detect.Blocked, t0, time.Time{}), ag("%b", detect.Blocked, t0, time.Time{})}
	if got, _ := Next(q, "%a"); got.PaneID != "%b" {
		t.Errorf("from %%a got %s, want %%b", got.PaneID)
	}
	if got, _ := Next(q, "%b"); got.PaneID != "%a" {
		t.Errorf("from %%b got %s, want %%a (wrap)", got.PaneID)
	}
}

func TestNextEmptyQueue(t *testing.T) {
	if _, ok := Next(nil, "%a"); ok {
		t.Error("empty queue must return ok=false")
	}
}
