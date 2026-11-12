package agenda

import (
	"reflect"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func TestStripRangeIsWorkHoursWidenedToWholeHours(t *testing.T) {
	loc := time.FixedZone("EET", 2*3600)
	day := domain.NewCivilDate(2026, 11, 12)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 12, h, m, 0, 0, loc) }
	for _, c := range []struct {
		name             string
		events           []domain.Event
		workFrom, workTo int
		from, to         int
	}{
		{"an empty day is the working hours", nil, 8 * 60, 18 * 60, 8 * 60, 18 * 60},
		{"working hours off the hour widen to it", nil, 8*60 + 30, 17*60 + 15, 8 * 60, 18 * 60},
		{"events outside widen it", []domain.Event{
			ev("Coffee", at(6, 14), at(6, 44)),
			ev("Late", at(20, 0), at(22, 10)),
		}, 8 * 60, 18 * 60, 6 * 60, 23 * 60},
		{"to midnight stops at midnight", []domain.Event{ev("Late", at(23, 30), at(23, 59).Add(time.Minute))}, 8 * 60, 18 * 60, 8 * 60, 1440},
		{"working hours that make no sense", nil, 18 * 60, 8 * 60, 8 * 60, 18 * 60},
	} {
		g := gridOf(t, loc, day, 30, c.events...)
		from, to := StripRange(g, c.workFrom, c.workTo)
		if from != c.from || to != c.to {
			t.Errorf("%s: %d–%d, want %d–%d", c.name, from, to, c.from, c.to)
		}
	}
}

func TestStripRangeTakesInWhatCrossesMidnight(t *testing.T) {
	loc := time.FixedZone("EET", 2*3600)
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, loc) }
	g := gridOf(t, loc, domain.NewCivilDate(2026, 11, 12), 30, ev("Night shift", at(11, 22), at(12, 6)))
	if from, to := StripRange(g, 8*60, 18*60); from != 0 || to != 18*60 {
		t.Errorf("the part of the night on this day starts it at midnight: %d–%d", from, to)
	}
}

func TestStripStepIsTheFinestThatFits(t *testing.T) {
	for _, c := range []struct{ from, to, width, step int }{
		{8 * 60, 18 * 60, 120, 5},
		{8 * 60, 18 * 60, 119, 10},
		{8 * 60, 18 * 60, 40, 15},
		{8 * 60, 18 * 60, 30, 20},
		{8 * 60, 18 * 60, 29, 30},
		{6 * 60, 22 * 60, 32, 30},
		{6 * 60, 22 * 60, 31, 60},
		{0, 1440, 10, 60}, // nothing fits: the coarsest
	} {
		if got := StripStep(c.from, c.to, c.width); got != c.step {
			t.Errorf("%d–%d in %d cells: step %d, want %d", c.from, c.to, c.width, got, c.step)
		}
	}
}

func TestStripCountsAndColorsTheCells(t *testing.T) {
	loc := time.FixedZone("EET", 2*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 12, h, m, 0, 0, loc) }
	g := gridOf(t, loc, domain.NewCivilDate(2026, 11, 12), 30,
		ev("Deep work", at(8, 49), at(10, 19)),
		ev("Quick sync", at(9, 22), at(9, 52)),
		ev("Standup", at(9, 30), at(9, 45)),
		ev("Reminder", at(11, 10), at(11, 10)),
	)
	cells := Strip(g, 8*60, 12*60, 30)
	if len(cells) != 8 {
		t.Fatalf("4 hours at 30 minutes: %d cells", len(cells))
	}
	type cell struct {
		start, count int
		first        string
	}
	var got []cell
	for _, c := range cells {
		got = append(got, cell{c.Start, c.Count, c.First.Event.Title})
	}
	want := []cell{
		{480, 0, ""},
		{510, 1, "Deep work"}, // 08:30, from 08:49
		{540, 2, "Deep work"}, // 09:00, the sync from 09:22
		{570, 3, "Deep work"}, // 09:30, all three
		{600, 1, "Deep work"}, // 10:00, until 10:19
		{630, 0, ""},          // 10:30
		{660, 1, "Reminder"},  // 11:00, a reminder counts where it falls
		{690, 0, ""},          // 11:30
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cells:\n got %v\nwant %v", got, want)
	}
}

func TestStripOnTheDaysTheClocksChange(t *testing.T) {
	// Spring: 02:00–03:00 doesn't exist. An event across it ends where the
	// clock says, and the cells of the missing hour are as empty as the axis.
	spring := domain.NewCivilDate(2026, 3, 29)
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, stockholm) }
	g := gridOf(t, stockholm, spring, 30, ev("Across", at(1, 0), at(4, 0)))
	cells := Strip(g, 0, 6*60, 60)
	for i, want := range []int{0, 1, 1, 1, 0, 0} {
		if cells[i].Count != want {
			t.Errorf("spring, hour %d: count %d, want %d", i, cells[i].Count, want)
		}
	}
	// Autumn: 02:00–03:00 happens twice. An hour from the first 02:30 is
	// as long as it lasts, from where it starts: 02:30–03:30 on the strip.
	autumn := domain.NewCivilDate(2026, 10, 25)
	start := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC).In(stockholm)
	g = gridOf(t, stockholm, autumn, 30, ev("Repeated hour", start, start.Add(time.Hour)))
	cells = Strip(g, 2*60, 4*60, 30)
	for i, want := range []int{0, 1, 1, 0} {
		if cells[i].Count != want {
			t.Errorf("autumn, cell %d: count %d, want %d", i, cells[i].Count, want)
		}
	}
}

func TestStripOfNothing(t *testing.T) {
	g := gridOf(t, time.UTC, domain.NewCivilDate(2026, 11, 12), 30)
	if cells := Strip(g, 8*60, 10*60, 60); len(cells) != 2 || cells[0].Count+cells[1].Count != 0 {
		t.Errorf("an empty day is empty cells: %+v", cells)
	}
	if cells := Strip(g, 10*60, 8*60, 60); cells != nil {
		t.Errorf("a range the wrong way round has no cells: %+v", cells)
	}
}

func TestFreeIsTheGapsLongEnough(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 11, 12, h, m, 0, 0, loc) }
	g := gridOf(t, loc, domain.NewCivilDate(2026, 11, 12), 30,
		ev("A", at(8, 49), at(10, 19)),
		ev("B", at(9, 22), at(9, 52)), // inside A
		ev("C", at(10, 30), at(11, 15)),
		ev("D", at(12, 14), at(13, 14)),
		ev("Reminder", at(16, 0), at(16, 0)), // takes no time
	)
	got := Free(g, 8*60, 18*60, 30)
	want := [][2]int{{480, 529}, {675, 734}, {794, 1080}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("free: got %v, want %v", got, want)
	}
	if got := Free(g, 9*60, 12*60, 15); !reflect.DeepEqual(got, [][2]int{{675, 720}}) {
		t.Errorf("free within 09–12: %v", got)
	}
	empty := gridOf(t, loc, domain.NewCivilDate(2026, 11, 12), 30)
	if got := Free(empty, 8*60, 18*60, 30); !reflect.DeepEqual(got, [][2]int{{480, 1080}}) {
		t.Errorf("an empty day is free throughout: %v", got)
	}
}

func TestNowNext(t *testing.T) {
	loc := time.UTC
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	events := []domain.Event{
		ev("Deep work", at(12, 8, 49), at(12, 10, 19)),
		ev("Quick sync", at(12, 9, 22), at(12, 9, 52)),
		ev("Twin", at(12, 9, 22), at(12, 9, 40)),
		ev("Standup", at(12, 9, 30), at(12, 9, 45)),
		ev("Overnight", at(11, 22, 0), at(12, 9, 30)),
		ev("Tomorrow", at(13, 9, 30), at(13, 9, 45)),
		domain.NewAllDayEvent("Holiday", "c", "Holiday", domain.NewCivilDate(2026, 11, 12), domain.NewCivilDate(2026, 11, 13), "", ""),
	}
	days := Build(events, domain.NewCivilDate(2026, 11, 11), 3, loc)
	titles := func(items []Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Event.Title)
		}
		return out
	}

	cur, next := NowNext(days, at(12, 9, 14))
	if got := titles(cur); !reflect.DeepEqual(got, []string{"Overnight", "Deep work"}) {
		t.Errorf("on now (once each, though Overnight is on two days): %v", got)
	}
	if got := titles(next); !reflect.DeepEqual(got, []string{"Quick sync", "Twin"}) {
		t.Errorf("next, both that start then: %v", got)
	}

	cur, next = NowNext(days, at(12, 9, 22))
	if got := titles(cur); !reflect.DeepEqual(got, []string{"Overnight", "Deep work", "Quick sync", "Twin"}) {
		t.Errorf("at a start the event is on, not next: %v", got)
	}
	if got := titles(next); !reflect.DeepEqual(got, []string{"Standup"}) {
		t.Errorf("next: %v", got)
	}

	cur, next = NowNext(days, at(12, 20, 0))
	if len(cur) != 0 || !reflect.DeepEqual(titles(next), []string{"Tomorrow"}) {
		t.Errorf("in the evening: on %v, next %v", titles(cur), titles(next))
	}
	if cur, next = NowNext(days, at(13, 23, 0)); len(cur)+len(next) != 0 {
		t.Errorf("after everything: on %v, next %v", titles(cur), titles(next))
	}
}
