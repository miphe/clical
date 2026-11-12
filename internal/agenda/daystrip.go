package agenda

import (
	"time"
)

// The day strip: one day as a single row of cells, each a stretch of the
// clock, for a glance at the day's shape (where it's busy, where it's free)
// beside a list that has room for the titles. Like the time grid it is pure
// layout on the wall clock, and knows nothing about the terminal.

// StripSteps are the minutes a strip cell may cover, finest first. Each
// divides an hour, so cells line up with the hours.
var StripSteps = []int{5, 10, 15, 20, 30, 60}

// StripCell is one cell of a strip: Step minutes of the clock from Start.
type StripCell struct {
	Start int
	// Count is how many timed items take part of the cell. A reminder (an
	// item with no length) counts in the cell it falls in.
	Count int
	// First is the earliest of them (the longer first when two start
	// together), the one whose calendar the cell is drawn in.
	First Item
}

// StripRange is the stretch of the day a strip shows, in minutes of the
// clock: the working hours, widened to whole hours to take in every timed
// item of the day. Working hours that make no sense fall back to 08–18.
func StripRange(g DayGrid, workFrom, workTo int) (from, to int) {
	if workFrom < 0 || workTo > 1440 || workFrom >= workTo {
		workFrom, workTo = 8*60, 18*60
	}
	from, to = workFrom, workTo
	for _, b := range g.Blocks {
		from, to = min(from, b.Start), max(to, b.End)
	}
	from = from / 60 * 60
	to = min((to+59)/60*60, 1440)
	return from, to
}

// StripStep is the finest of StripSteps at which from..to fits in width
// cells, or the coarsest when none does.
func StripStep(from, to, width int) int {
	for _, s := range StripSteps {
		if (to-from+s-1)/s <= width {
			return s
		}
	}
	return StripSteps[len(StripSteps)-1]
}

// Strip lays g's timed items over cells of step minutes from from to to.
func Strip(g DayGrid, from, to, step int) []StripCell {
	if step <= 0 || to <= from {
		return nil
	}
	n := (to - from + step - 1) / step
	cells := make([]StripCell, n)
	for i := range cells {
		cells[i].Start = from + i*step
	}
	for _, b := range g.Blocks {
		for i := range cells {
			c0, c1 := cells[i].Start, cells[i].Start+step
			in := b.Start < c1 && b.End > c0
			if b.Instant {
				in = b.Start >= c0 && b.Start < c1
			}
			if !in {
				continue
			}
			if cells[i].Count == 0 {
				cells[i].First = b.Item
			}
			cells[i].Count++
		}
	}
	return cells
}

// Free is the stretches from..to that nothing takes and that last at least
// least minutes, in order.
func Free(g DayGrid, from, to, least int) [][2]int {
	var out [][2]int
	at := from
	add := func(end int) {
		if end-at >= max(least, 1) {
			out = append(out, [2]int{at, end})
		}
	}
	for _, s := range g.Busy() {
		if s[1] <= at {
			continue
		}
		if s[0] >= to {
			break
		}
		add(min(s[0], to))
		at = max(at, s[1])
	}
	if at < to {
		add(to)
	}
	return out
}

// NowNext finds, among the timed items of days, the ones on at now (started,
// not ended) and the next to start after now: every item that starts at
// that same moment. An item shown on several days counts once. All-day
// items are neither.
func NowNext(days []Day, now time.Time) (current, next []Item) {
	seen := map[string]bool{}
	for _, d := range days {
		for _, it := range d.Items {
			if it.Event.AllDay {
				continue
			}
			key := it.Event.ID + "|" + it.Start.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			switch {
			case !it.Start.After(now) && it.End.After(now):
				current = append(current, it)
			case it.Start.After(now):
				switch {
				case len(next) == 0 || it.Start.Before(next[0].Start):
					next = []Item{it}
				case it.Start.Equal(next[0].Start):
					next = append(next, it)
				}
			}
		}
	}
	return current, next
}
