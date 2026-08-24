// Package hookevents stores and reads the per-pane Claude Code hook event
// log that backs event-driven working detection.
package hookevents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event is one hook firing, as appended by `tmux-radar hook <event>`.
type Event struct {
	Event   string          `json:"event"`
	Pane    string          `json:"pane"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// maxPayload bounds a stored payload. Larger payloads (huge prompts) are
// dropped: derivation only needs the event name; payloads are for debugging.
const maxPayload = 32 << 10

// DefaultDir returns ~/.local/state/tmux-radar/events. An empty string
// (HOME unresolvable) is returned rather than a relative fallback: a
// relative "events" dir would scatter event files into whatever cwd the
// hook subcommand happens to run in (a different one per Claude Code
// session). Callers must treat "" as "give up", not as a valid directory.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "tmux-radar", "events")
}

func paneFile(dir, pane string) string {
	return filepath.Join(dir, pane+".jsonl")
}

// Append writes one event as a single line. SessionStart truncates the log
// first: a fresh session must never inherit drifted state. Payloads are
// compacted to one line; invalid or oversized payloads are dropped, never
// an error — losing debug detail beats losing the event.
func Append(dir, pane, event string, payload []byte, now time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	e := Event{Event: event, Pane: pane, TS: now}
	var buf bytes.Buffer
	if len(payload) > 0 && len(payload) <= maxPayload && json.Compact(&buf, payload) == nil {
		e.Payload = json.RawMessage(buf.Bytes())
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if event == "SessionStart" {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(paneFile(dir, pane), flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadPane returns the pane's events in append order. A missing file is
// (nil, nil). Unparseable lines (a torn tail mid-write) are skipped.
func ReadPane(dir, pane string) ([]Event, error) {
	b, err := os.ReadFile(paneFile(dir, pane))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var evs []Event
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if json.Unmarshal(line, &e) == nil && e.Event != "" {
			evs = append(evs, e)
		}
	}
	return evs, nil
}

// GC removes event files untouched for maxAge. Closed panes stop appending,
// so their files age out; live panes keep fresh mtimes. Best-effort.
func GC(dir string, now time.Time, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		info, err := ent.Info()
		if err != nil || ent.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, ent.Name()))
		}
	}
}

// GCDead removes event files whose pane is not in alive. It complements
// the mtime-based GC above: a pane can close (and tmux can reuse its id)
// well within 48h, and a stale log left pinned "working" for a reused pane
// id is the worst failure mode — most acutely right after uninstall-hooks,
// where a turn=true tail with a fresh mtime has no future SessionStart to
// truncate it. Only files named "<pane>.jsonl" are considered; subdirs and
// foreign files are left untouched. Best-effort, like GC.
func GCDead(dir string, alive map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		pane, ok := strings.CutSuffix(name, ".jsonl")
		if !ok {
			continue
		}
		if !alive[pane] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
