package agenda

import (
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func titles(d Day) []string {
	var out []string
	for _, it := range d.Items {
		out = append(out, it.Event.Title)
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildGroupsAndOrdersByLocalDay(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, loc) }
	timed := func(title string, s, e time.Time) domain.Event {
		return domain.NewTimedEvent(title, "c", title, s, e, "", "")
	}
	allDay := func(title string, d, n int) domain.Event {
		s := domain.NewCivilDate(2026, 10, d)
		return domain.NewAllDayEvent(title, "c", title, s, s.AddDays(n), "", "")
	}

	events := []domain.Event{
		timed("Lunch", at(1, 12, 0), at(1, 13, 0)),
		timed("Standup", at(1, 9, 0), at(1, 9, 15)),
		allDay("Holiday", 1, 1),
		allDay("Conference", 2, 2),                         // Oct 2–3
		timed("Late", at(2, 23, 0), at(3, 1, 0)),           // crosses midnight
		timed("EarlyUTC", at(3, 1, 30), at(3, 2, 0)),       // Oct 2 22:30 UTC, but Oct 3 locally
		timed("Reminder", at(3, 8, 0), at(3, 8, 0)),        // zero-length
		timed("EndsAtMidnight", at(3, 23, 0), at(4, 0, 0)), // must not spill into Oct 4
	}

	days := Build(events, domain.NewCivilDate(2026, 10, 1), 4, loc)

	want := [][]string{
		{"Holiday", "Standup", "Lunch"},
		{"Conference", "Late"},
		{"Conference", "Late", "EarlyUTC", "Reminder", "EndsAtMidnight"},
		nil,
	}
	for i, w := range want {
		if got := titles(days[i]); !same(got, w) {
			t.Errorf("%s = %v, want %v", days[i].Date, got, w)
		}
	}

	if it := days[0].Items[1]; it.Start.Location() != loc || it.Start.Hour() != 9 {
		t.Errorf("Standup start = %v, want 09:00 in EEST", it.Start)
	}
}

func TestTimeLabel(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, loc) }
	oct1 := domain.NewCivilDate(2026, 10, 1)
	oct2 := oct1.AddDays(1)
	item := func(s, e time.Time) Item {
		return Item{Event: domain.NewTimedEvent("x", "c", "x", s, e, "", ""), Start: s, End: e}
	}

	cases := []struct {
		name string
		date domain.CivilDate
		it   Item
		want string
	}{
		{"same day", oct1, item(at(1, 9, 0), at(1, 9, 15)), "09:00–09:15"},
		{"into next day, first day", oct1, item(at(1, 23, 0), at(2, 1, 0)), "23:00–…"},
		{"into next day, second day", oct2, item(at(1, 23, 0), at(2, 1, 0)), "…–01:00"},
		{"ends at midnight", oct1, item(at(1, 23, 0), at(2, 0, 0)), "23:00–24:00"},
		{"zero length", oct1, item(at(1, 8, 0), at(1, 8, 0)), "08:00"},
		{"all day", oct1, Item{Event: domain.NewAllDayEvent("x", "c", "x", oct1, oct2, "", "")}, "all day"},
	}
	for _, c := range cases {
		if got := c.it.TimeLabel(c.date, loc); got != c.want {
			t.Errorf("%s: timeLabel() = %q, want %q", c.name, got, c.want)
		}
	}
}
