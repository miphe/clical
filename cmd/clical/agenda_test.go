package main

import (
	"testing"
	"time"

	"github.com/rivo/uniseg"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

func TestTimeLabel(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, loc) }
	oct1 := domain.NewCivilDate(2026, 10, 1)
	oct2 := oct1.AddDays(1)
	item := func(s, e time.Time) agenda.Item {
		return agenda.Item{Event: domain.NewTimedEvent("x", "c", "x", s, e, "", ""), Start: s, End: e}
	}

	cases := []struct {
		name string
		date domain.CivilDate
		it   agenda.Item
		want string
	}{
		{"same day", oct1, item(at(1, 9, 0), at(1, 9, 15)), "09:00–09:15"},
		{"into next day, first day", oct1, item(at(1, 23, 0), at(2, 1, 0)), "23:00–…"},
		{"into next day, second day", oct2, item(at(1, 23, 0), at(2, 1, 0)), "…–01:00"},
		{"ends at midnight", oct1, item(at(1, 23, 0), at(2, 0, 0)), "23:00–24:00"},
		{"zero length", oct1, item(at(1, 8, 0), at(1, 8, 0)), "08:00"},
		{"all day", oct1, agenda.Item{Event: domain.NewAllDayEvent("x", "c", "x", oct1, oct2, "", "")}, "all day"},
	}
	for _, c := range cases {
		if got := timeLabel(c.date, c.it, loc); got != c.want {
			t.Errorf("%s: timeLabel() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPadRightUsesDisplayWidth(t *testing.T) {
	for _, s := range []string{"abc", "…–01:00", "会議", "🎉 party"} {
		got := padRight(s, 12)
		if width := uniseg.StringWidth(got); width != 12 {
			t.Errorf("padRight(%q) width = %d, want 12", s, width)
		}
	}
}
