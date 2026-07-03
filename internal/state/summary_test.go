package state

import (
	"testing"
	"time"

	"github.com/sngyo/tmux-agents/internal/detect"
)

func agent(st detect.State, doneUntil time.Time) Agent {
	return Agent{State: st, DoneUntil: doneUntil}
}

func TestSummaryCountsAndStyles(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := Snapshot{GeneratedAt: now, Agents: []Agent{
		agent(detect.Blocked, time.Time{}),
		agent(detect.Working, time.Time{}),
		agent(detect.Working, time.Time{}),
		agent(detect.Idle, now.Add(time.Minute)), // done -> counts as idle bucket
		agent(detect.Idle, time.Time{}),
	}}
	got := Summary(s, now, 3*time.Second)
	want := "#[fg=red,bold]◆1 #[fg=green]●2 #[fg=default]○2#[fg=default]"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSummaryOmitsBlockedWhenZero(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := Snapshot{GeneratedAt: now, Agents: []Agent{agent(detect.Working, time.Time{})}}
	got := Summary(s, now, 3*time.Second)
	want := "#[fg=green]●1 #[fg=default]○0#[fg=default]"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSummaryEmptyWhenStaleOrNoAgents(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	stale := Snapshot{GeneratedAt: now.Add(-10 * time.Second), Agents: []Agent{agent(detect.Working, time.Time{})}}
	if got := Summary(stale, now, 3*time.Second); got != "" {
		t.Errorf("stale: got %q, want empty", got)
	}
	if got := Summary(Snapshot{GeneratedAt: now}, now, 3*time.Second); got != "" {
		t.Errorf("no agents: got %q, want empty", got)
	}
}
