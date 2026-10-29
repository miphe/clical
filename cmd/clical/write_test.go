package main

import (
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/write"
)

var (
	weekly = write.Ref{Calendar: "c", Key: "k1", Title: "Weekly sync"}
	dental = write.Ref{Calendar: "c", Key: "k2", Title: "Dentist"}
)

func TestItemLine(t *testing.T) {
	done := time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC)
	cases := []struct {
		name  string
		item  write.Item
		scope write.Ref
		want  string
	}{
		{"plain", write.Item{ID: "ab12", Todo: true, Text: "call", Origin: write.Ref{}}, write.Ref{}, "[ ] ab12  call"},
		{"moved to global", write.Item{ID: "ab12", Todo: true, Text: "call", Origin: weekly}, write.Ref{}, "[ ] ab12  call  (from Weekly sync)"},
		{"done", write.Item{ID: "ab12", Todo: true, Done: true, Completed: done, Text: "call"}, write.Ref{}, "[x] ab12  call  (done 5 Oct 12:30)"},
		{"began global, now in an event", write.Item{ID: "ab12", Todo: true, Text: "call", Last: weekly}, weekly, "[ ] ab12  call  (from global)"},
		{"another event on the way", write.Item{ID: "ab12", Todo: true, Text: "call", Origin: weekly, Last: dental}, write.Ref{}, "[ ] ab12  call  (from Weekly sync; last in Dentist)"},
		{"priority", write.Item{ID: "ab12", Todo: true, Priority: true, Text: "call"}, write.Ref{}, "[!] ab12  call"},
		{"note", write.Item{Text: "agenda"}, write.Ref{}, "    agenda"},
		{"control characters", write.Item{ID: "ab12", Todo: true, Text: "a\x1b[2Jb"}, write.Ref{}, "[ ] ab12  a[2Jb"},
	}
	for _, c := range cases {
		if got := itemLine(c.item, c.scope); got != c.want {
			t.Errorf("%s: %q; want %q", c.name, got, c.want)
		}
	}
}

func TestPrintEntries(t *testing.T) {
	var b strings.Builder
	global := write.List{Items: []write.Item{{ID: "g1", Todo: true, Text: "water"}, {Text: ""}}}
	ev := write.List{Header: &write.Header{Event: weekly, Calendar: "Work", Start: "2026-10-05T09:00:00Z"}}
	printEntries(&b, []write.Entry{{List: global}, {Scope: weekly, List: ev}}, true)
	got := b.String()
	if !strings.Contains(got, "Global\n  [ ] g1  water\n") || strings.Contains(got, "Weekly sync") {
		t.Errorf("--all output:\n%s", got)
	}
	b.Reset()
	printEntries(&b, []write.Entry{{List: write.List{}}}, false)
	if !strings.Contains(b.String(), "(nothing yet)") {
		t.Errorf("empty list:\n%s", b.String())
	}
}

func TestEventWindow(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	from, to, err := (eventFlags{}).window(now)
	if err != nil || !from.Before(now.Add(-13*24*time.Hour)) || !to.After(now.Add(59*24*time.Hour)) {
		t.Errorf("default window = %v – %v, %v", from, to, err)
	}
	from, to, err = (eventFlags{On: "2026-10-07"}).window(now)
	if err != nil || to.Sub(from) < 23*time.Hour || from.Day() != 7 {
		t.Errorf("--on window = %v – %v, %v", from, to, err)
	}
	if _, _, err := (eventFlags{On: "tomorrow"}).window(now); err == nil {
		t.Error("--on tomorrow accepted")
	}
}
