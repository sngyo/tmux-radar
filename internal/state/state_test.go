package state

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sngyo/tmux-radar/internal/detect"
	"github.com/sngyo/tmux-radar/internal/tmux"
)

var t0 = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

func obs(id string, st detect.State) Observation {
	return Observation{
		Pane: tmux.Pane{ID: id, Session: "main", WindowIndex: 14,
			WindowName: "api", PaneIndex: 1, Command: "claude"},
		Kind:  "claude",
		State: st,
	}
}

func TestApplyNewAgentSetsSince(t *testing.T) {
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)
	if len(s.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(s.Agents))
	}
	a := s.Agents[0]
	if a.State != detect.Working || !a.Since.Equal(t0) {
		t.Errorf("got %+v", a)
	}
}

func TestApplyWorkingToIdleProducesDone(t *testing.T) {
	prev := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)
	next := Apply(prev, []Observation{obs("%1", detect.Idle)}, t0.Add(time.Minute))
	a := next.Agents[0]
	if a.Display(t0.Add(2*time.Minute)) != DisplayDone {
		t.Errorf("display = %s, want done", a.Display(t0.Add(2*time.Minute)))
	}
	// done persists indefinitely: no TTL, still done far later.
	if a.Display(t0.Add(12*time.Hour)) != DisplayDone {
		t.Errorf("far later: display = %s, want done", a.Display(t0.Add(12*time.Hour)))
	}
}

func TestApplyBackToWorkingClearsDone(t *testing.T) {
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)
	s = Apply(s, []Observation{obs("%1", detect.Idle)}, t0.Add(time.Minute))
	s = Apply(s, []Observation{obs("%1", detect.Working)}, t0.Add(2*time.Minute))
	if got := s.Agents[0].Display(t0.Add(3 * time.Minute)); got != DisplayWorking {
		t.Errorf("display = %s, want working", got)
	}
}

func TestApplyDropsVanishedPanes(t *testing.T) {
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)
	s = Apply(s, nil, t0.Add(time.Minute))
	if len(s.Agents) != 0 {
		t.Errorf("agents = %d, want 0", len(s.Agents))
	}
}

func TestUnchangedStateKeepsSince(t *testing.T) {
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)
	s = Apply(s, []Observation{obs("%1", detect.Working)}, t0.Add(time.Minute))
	if !s.Agents[0].Since.Equal(t0) {
		t.Errorf("since = %v, want %v", s.Agents[0].Since, t0)
	}
}

func TestSaveLoadRoundTripAndStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Blocked)}, t0)
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents[0].State != detect.Blocked {
		t.Errorf("round trip lost data: %+v", got)
	}
	if got.Stale(t0.Add(time.Second), 3*time.Second) {
		t.Error("fresh snapshot reported stale")
	}
	if !got.Stale(t0.Add(10*time.Second), 3*time.Second) {
		t.Error("old snapshot not reported stale")
	}
}

// Two writers (watch + sidebar in production) saving the same path must
// never fail: a shared fixed temp name lets one writer rename the other's
// temp file away, so the loser's rename hits ENOENT.
func TestSaveConcurrentWritersDoNotCollide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := Apply(Snapshot{}, []Observation{obs("%1", detect.Working)}, t0)

	const writers, saves = 4, 300
	errs := make(chan error, writers*saves)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < saves; i++ {
				if err := Save(path, s); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Save failed: %v", err)
	}
}
