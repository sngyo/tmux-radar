package tmux

import (
	"reflect"
	"testing"
)

func TestParsePanes(t *testing.T) {
	out := "%37\tmain\t14\tapi\t2\treviewer\tclaude\n" +
		"%12\tmain\t11\tweb\t1\t\tzsh\n"
	got, err := ParsePanes(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pane{
		{ID: "%37", Session: "main", WindowIndex: 14, WindowName: "api",
			PaneIndex: 2, Title: "reviewer", Command: "claude"},
		{ID: "%12", Session: "main", WindowIndex: 11, WindowName: "web",
			PaneIndex: 1, Title: "", Command: "zsh"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParsePanesSkipsMalformedLines(t *testing.T) {
	got, err := ParsePanes("garbage-without-tabs\n%1\tmain\t1\tapi\t1\t\tclaude\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "%1" {
		t.Errorf("got %+v, want single %%1 pane", got)
	}
}

func TestParseFocusPicksActivePaneOfAttachedSession(t *testing.T) {
	out := "0\t900\tpreview\t1\t%9\t1\t1\n" + // detached session must be ignored
		"1\t200\tmain\t14\t%37\t1\t1\n" + // attached + active window + active pane
		"1\t200\tmain\t14\t%38\t1\t0\n" + // inactive pane
		"1\t200\tmain\t13\t%30\t0\t1\n" // inactive window
	f, err := ParseFocus(out)
	if err != nil {
		t.Fatal(err)
	}
	want := Focus{Session: "main", WindowIndex: 14, PaneID: "%37"}
	if f != want {
		t.Errorf("got %+v, want %+v", f, want)
	}
}

func TestParseFocusPrefersMostRecentlyActiveSession(t *testing.T) {
	out := "1\t100\tolder\t1\t%1\t1\t1\n" +
		"1\t300\tnewer\t2\t%2\t1\t1\n"
	f, err := ParseFocus(out)
	if err != nil {
		t.Fatal(err)
	}
	if f.Session != "newer" || f.PaneID != "%2" {
		t.Errorf("got %+v, want the newer session's active pane", f)
	}
}

func TestParseFocusNoAttachedClientFails(t *testing.T) {
	for _, out := range []string{"", "0\t100\tdetached\t1\t%1\t1\t1\n", "garbage line"} {
		if _, err := ParseFocus(out); err == nil {
			t.Errorf("ParseFocus(%q) should fail", out)
		}
	}
}
