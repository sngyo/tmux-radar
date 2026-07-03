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
