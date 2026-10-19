package agenda

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

var stockholm = func() *time.Location {
	l, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		panic(err)
	}
	return l
}()

// gridOf builds the grid of the one day the events are on.
func gridOf(t *testing.T, loc *time.Location, day domain.CivilDate, slot int, events ...domain.Event) DayGrid {
	t.Helper()
	days := Build(events, day, 1, loc)
	return BuildGrid(days[0], loc, slot)
}

func ev(title string, s, e time.Time) domain.Event {
	return domain.NewTimedEvent(title, "c", title, s, e, "", "")
}

func block(g DayGrid, title string) Block {
	for _, b := range g.Blocks {
		if b.Item.Event.Title == title {
			return b
		}
	}
	return Block{Row0: -1}
}

func TestGridClipsToTheDayAndMarksWhatContinues(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, loc) }
	events := []domain.Event{
		ev("Night", at(2, 23, 0), at(3, 1, 30)),
		ev("Standup", at(3, 9, 0), at(3, 9, 15)),
		ev("ToMidnight", at(3, 22, 0), at(4, 0, 0)),
		ev("Reminder", at(3, 8, 0), at(3, 8, 0)),
		ev("Seconds", at(3, 10, 0), at(3, 10, 0).Add(61*time.Second)),
		domain.NewAllDayEvent("Holiday", "c", "Holiday", domain.NewCivilDate(2026, 10, 3), domain.NewCivilDate(2026, 10, 4), "", ""),
	}
	d2 := gridOf(t, loc, domain.NewCivilDate(2026, 10, 2), 30, events...)
	if b := block(d2, "Night"); b.Start != 23*60 || b.End != 1440 || !b.ToNext || b.FromPrev {
		t.Errorf("first day of a midnight crosser: %+v", b)
	}
	g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), 30, events...)
	if b := block(g, "Night"); b.Start != 0 || b.End != 90 || !b.FromPrev || b.ToNext {
		t.Errorf("second day of a midnight crosser: %+v", b)
	}
	if b := block(g, "Standup"); b.Row0 != 18 || b.Row1 != 19 || b.Instant {
		t.Errorf("a quarter hour takes its one row: %+v", b)
	}
	if b := block(g, "ToMidnight"); b.End != 1440 || b.ToNext {
		t.Errorf("an end at midnight is the end of the day, not the next: %+v", b)
	}
	if b := block(g, "Reminder"); !b.Instant || b.Row1-b.Row0 != 1 || b.Start != b.End {
		t.Errorf("a reminder is one row: %+v", b)
	}
	if b := block(g, "Seconds"); b.End != 10*60+2 {
		t.Errorf("an end is rounded up to the minute: %+v", b)
	}
	if len(g.AllDay) != 1 || g.AllDay[0].Event.Title != "Holiday" || len(g.Blocks) != 5 {
		t.Errorf("all-day items belong to the band: %d all-day, %d blocks", len(g.AllDay), len(g.Blocks))
	}
}

func TestGridRowsFollowTheZoom(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	e := ev("Meeting", at(9, 15), at(10, 45))
	for _, c := range []struct{ slot, row0, row1 int }{{60, 9, 11}, {30, 18, 22}, {15, 37, 43}} {
		g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), c.slot, e)
		if b := g.Blocks[0]; b.Row0 != c.row0 || b.Row1 != c.row1 {
			t.Errorf("slot %d: rows %d–%d, want %d–%d", c.slot, b.Row0, b.Row1, c.row0, c.row1)
		}
	}
	if Rows(30) != 48 || Rows(60) != 24 || Rows(15) != 96 || Rows(7) != 48 || Rows(0) != 48 {
		t.Error("Rows")
	}
}

func TestEventsInTheSameRowAreSideBySideEvenIfTheyDontOverlapInTime(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	events := []domain.Event{ev("A", at(9, 0), at(9, 15)), ev("B", at(9, 30), at(9, 45))}
	day := domain.NewCivilDate(2026, 10, 3)
	if g := gridOf(t, loc, day, 60, events...); block(g, "A").Lanes != 2 || block(g, "A").Lane == block(g, "B").Lane {
		t.Errorf("one row each hour: %+v", g.Blocks)
	}
	if g := gridOf(t, loc, day, 15, events...); block(g, "A").Lanes != 1 || block(g, "B").Lanes != 1 {
		t.Errorf("a row each: %+v", g.Blocks)
	}
}

func TestLanesAndExpandRight(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), 30,
		ev("Long", at(9, 0), at(12, 0)),
		ev("Short1", at(9, 0), at(10, 0)),
		ev("Short2", at(10, 0), at(11, 0)),
		ev("Parallel", at(9, 30), at(10, 30)),
		ev("Alone", at(14, 0), at(15, 0)),
		ev("Touching", at(12, 0), at(13, 0)), // starts as Long ends: not an overlap
	)
	long := block(g, "Long")
	if long.Lane != 0 || long.Lanes != 3 {
		t.Errorf("Long should be in lane 0 of 3: %+v", long)
	}
	// Short2 is in lane 1 behind Short1 and has lane 2 free beside it
	// (Parallel is 09:30–10:30 in lane 2, which overlaps 10:00–11:00).
	if s := block(g, "Short2"); s.Lane != 1 || s.Span != 1 {
		t.Errorf("Short2: %+v", s)
	}
	if p := block(g, "Parallel"); p.Lane != 2 || p.Span != 1 {
		t.Errorf("Parallel: %+v", p)
	}
	if a := block(g, "Alone"); a.Lanes != 1 || a.Lane != 0 || a.Span != 1 {
		t.Errorf("Alone: %+v", a)
	}
	if tc := block(g, "Touching"); tc.Lanes != 1 || tc.Cluster == long.Cluster {
		t.Errorf("an event that starts as another ends is no overlap: %+v", tc)
	}
	// Short1 (09:00–10:00, lane 1) can go right past lane 2? Parallel is there
	// from 09:30, so no.
	if s := block(g, "Short1"); s.Span != 1 {
		t.Errorf("Short1 should not cover Parallel's lane: %+v", s)
	}
}

func TestExpandRightTakesTheFreeLanes(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	// Wide is in lane 0; A and B sit beside it in lanes 1 and 2 for an hour.
	// Wide can't expand (lanes 1 and 2 are busy), but C in lane 1 has lane 2
	// free for its whole span, so it can.
	g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), 30,
		ev("Wide", at(9, 0), at(12, 0)),
		ev("A", at(9, 0), at(10, 0)),
		ev("B", at(9, 0), at(10, 0)),
		ev("C", at(10, 0), at(11, 0)),
	)
	if c := block(g, "C"); c.Lane != 1 || c.Span != 2 {
		t.Errorf("C should take lanes 1 and 2: %+v", c)
	}
	if w := block(g, "Wide"); w.Span != 1 {
		t.Errorf("Wide has neighbours all along: %+v", w)
	}
}

// The lanes are correct whatever the events: no two blocks in a lane overlap,
// a cluster has as many lanes as its busiest row has blocks, and a block's
// span never covers a block.
func TestLanesProperty(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	rng := rand.New(rand.NewPCG(1, 2))
	for round := range 300 {
		var events []domain.Event
		for i := range 1 + rng.IntN(14) {
			s := time.Date(2026, 10, 3, 6+rng.IntN(14), rng.IntN(4)*15, 0, 0, loc)
			e := s.Add(time.Duration(rng.IntN(8)) * 30 * time.Minute)
			events = append(events, ev(fmt.Sprintf("e%d", i), s, e))
		}
		slot := []int{60, 30, 15}[rng.IntN(3)]
		g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), slot, events...)
		for i, a := range g.Blocks {
			for j, b := range g.Blocks {
				if i == j || a.Row0 >= b.Row1 || b.Row0 >= a.Row1 {
					continue
				}
				if a.Cluster != b.Cluster {
					t.Fatalf("round %d: overlapping blocks in two clusters", round)
				}
				if a.Lane == b.Lane {
					t.Fatalf("round %d: overlapping blocks share lane %d: %+v %+v", round, a.Lane, a, b)
				}
				if b.Lane > a.Lane && b.Lane < a.Lane+a.Span {
					t.Fatalf("round %d: %+v covers the lane of %+v", round, a, b)
				}
			}
		}
		peak := map[int]int{}
		for _, b := range g.Blocks {
			for r := b.Row0; r < b.Row1; r++ {
				n := 0
				for _, o := range g.Blocks {
					if o.Cluster == b.Cluster && o.Row0 <= r && r < o.Row1 {
						n++
					}
				}
				peak[b.Cluster] = max(peak[b.Cluster], n)
			}
		}
		for _, b := range g.Blocks {
			if b.Lanes != peak[b.Cluster] {
				t.Fatalf("round %d: a cluster has %d lanes but at most %d at once", round, b.Lanes, peak[b.Cluster])
			}
			if b.Lane+b.Span > b.Lanes || b.Span < 1 {
				t.Fatalf("round %d: bad span %+v", round, b)
			}
		}
	}
}

func TestHiddenCountsTheBlocksBeyondTheLanesThatFit(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	var events []domain.Event
	for i := range 5 {
		events = append(events, ev(fmt.Sprintf("e%d", i), at(9, 0), at(10, 0)))
	}
	g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), 30, events...)
	c := g.Blocks[0].Cluster
	if g.Hidden(c, 3) != 2 || g.Hidden(c, 1) != 4 || g.Hidden(c, 5) != 0 {
		t.Errorf("hidden: %d, %d, %d", g.Hidden(c, 3), g.Hidden(c, 1), g.Hidden(c, 5))
	}
}

func TestBusyMergesWhatOverlapsAndTouches(t *testing.T) {
	loc := time.FixedZone("EEST", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, loc) }
	g := gridOf(t, loc, domain.NewCivilDate(2026, 10, 3), 30,
		ev("A", at(9, 0), at(10, 0)), ev("B", at(9, 30), at(11, 0)), ev("C", at(11, 0), at(12, 0)),
		ev("D", at(14, 0), at(15, 0)), ev("Reminder", at(13, 0), at(13, 0)))
	got := g.Busy()
	if len(got) != 2 || got[0] != [2]int{540, 720} || got[1] != [2]int{840, 900} {
		t.Errorf("Busy() = %v", got)
	}
}

// The clocks go forward on 2026-03-29 in Stockholm: 02:00 becomes 03:00.
func TestGridOnTheDayTheClocksGoForward(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, stockholm) }
	day := domain.NewCivilDate(2026, 3, 29)
	g := gridOf(t, stockholm, day, 30,
		ev("Before", at(1, 0), at(1, 30)),
		ev("Across", at(1, 30), at(3, 30)), // an hour long, as the wall shows two
		ev("After", at(9, 0), at(10, 0)))
	if b := block(g, "Before"); b.Start != 60 || b.End != 90 {
		t.Errorf("Before: %+v", b)
	}
	if b := block(g, "Across"); b.Start != 90 || b.End != 210 {
		t.Errorf("Across should span the wall clock 01:30–03:30: %+v", b)
	}
	if b := block(g, "After"); b.Start != 540 || b.End != 600 {
		t.Errorf("After: %+v", b)
	}
}

// And back on 2026-10-25: 03:00 becomes 02:00, so 02:00–03:00 happens twice.
func TestGridOnTheDayTheClocksGoBack(t *testing.T) {
	day := domain.NewCivilDate(2026, 10, 25)
	start := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 02:30 CEST, the first time
	end := start.Add(time.Hour).In(stockholm)               // 02:30 CET, the second
	g := gridOf(t, stockholm, day, 30,
		ev("Repeated hour", start.In(stockholm), end),
		ev("Later", time.Date(2026, 10, 25, 9, 0, 0, 0, stockholm), time.Date(2026, 10, 25, 9, 30, 0, 0, stockholm)))
	if b := block(g, "Repeated hour"); b.Start != 150 || b.End != 210 || b.Instant {
		t.Errorf("an hour that reads 02:30 to 02:30 is still an hour tall: %+v", b)
	}
	if b := block(g, "Later"); b.Row0 != 18 {
		t.Errorf("Later: %+v", b)
	}
	// A whole day, from midnight to midnight, is 25 hours long here and fills
	// the 24 on the wall.
	long := gridOf(t, stockholm, day, 60, ev("All of it", day.In(stockholm), day.AddDays(1).In(stockholm)))
	if b := long.Blocks[0]; b.Start != 0 || b.End != 1440 || b.ToNext || b.FromPrev {
		t.Errorf("a whole day: %+v", b)
	}
}
