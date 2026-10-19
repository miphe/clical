package agenda

import (
	"sort"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// The time grid: one day laid out against the time of day, for a view whose
// vertical axis is time. It is pure layout, in rows of slot minutes, and knows
// nothing about the terminal.
//
// The axis is wall-clock time. A row is a stretch of the clock on the wall,
// not of elapsed time, so the hour labels always read right and the days of a
// week line up even when one of them has a daylight-saving shift. On the day
// the clocks go forward the missing hour is simply empty; on the day they go
// back, an event is as tall as it lasts, from where it starts.

// Block is a timed item placed in a day's column.
type Block struct {
	Item Item

	// Start and End are the item's part of this day, in minutes of the clock
	// from midnight: 0 to 1440, End after Start unless the item is Instant.
	Start, End int
	FromPrev   bool // it began on an earlier day
	ToNext     bool // it goes on into the next day
	Instant    bool // it has no length (a reminder)

	// Row0 and Row1 are the rows it covers, [Row0, Row1): at least one, and
	// whole rows, so what the eye sees overlap is what is laid out as overlap.
	Row0, Row1 int

	// Cluster is the group of blocks that overlap one another, directly or
	// through others, numbered by the position of its first block. Lane is the block's place in it, 0 first, of Lanes in
	// all; Span is how many lanes it may cover going right, itself included,
	// because the ones to its right are empty for as long as it lasts.
	Cluster, Lane, Lanes, Span int
}

// DayGrid is one day ready to draw.
type DayGrid struct {
	Date   domain.CivilDate
	Slot   int    // minutes per row
	AllDay []Item // all-day items, which have no place on the time axis
	Blocks []Block
}

// Rows is how many rows a day has at slot minutes a row.
func Rows(slot int) int { return 1440 / validSlot(slot) }

func validSlot(slot int) int {
	if slot <= 0 || 1440%slot != 0 {
		return 30
	}
	return slot
}

// MinutesOfDay is t's clock time as minutes from midnight, in t's own zone.
func MinutesOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }

// BuildGrid places d's timed items on a time axis of slot minutes a row.
// Blocks come in order of start, longer first, then by lane.
func BuildGrid(d Day, loc *time.Location, slot int) DayGrid {
	slot = validSlot(slot)
	g := DayGrid{Date: d.Date, Slot: slot}
	dayStart, dayEnd := d.Date.In(loc), d.Date.AddDays(1).In(loc)
	for _, it := range d.Items {
		if it.Event.AllDay {
			g.AllDay = append(g.AllDay, it)
			continue
		}
		b := Block{Item: it, Instant: it.Start.Equal(it.End)}
		b.FromPrev = it.Start.Before(dayStart)
		if !b.FromPrev {
			b.Start = MinutesOfDay(it.Start)
		}
		switch {
		case it.End.After(dayEnd):
			b.ToNext, b.End = true, 1440
		case it.End.Equal(dayEnd):
			b.End = 1440
		default:
			b.End = MinutesOfDay(it.End)
			if it.End.Second() > 0 || it.End.Nanosecond() > 0 {
				b.End++ // whole minutes, rounded up
			}
		}
		// Where the clocks went back, the end can read earlier than the start.
		// The item is as long as it lasts, from where it starts.
		if b.End <= b.Start && !b.Instant {
			b.End = min(b.Start+max(int((it.End.Sub(it.Start)+time.Minute-1)/time.Minute), 1), 1440)
		}
		if b.Instant {
			b.End = b.Start
		}
		b.Row0 = min(b.Start/slot, Rows(slot)-1)
		b.Row1 = max((b.End+slot-1)/slot, b.Row0+1)
		g.Blocks = append(g.Blocks, b)
	}
	g.layout()
	return g
}

// layout puts the blocks in lanes, cluster by cluster.
func (g *DayGrid) layout() {
	bs := g.Blocks
	sort.SliceStable(bs, func(a, b int) bool {
		x, y := bs[a], bs[b]
		switch {
		case x.Row0 != y.Row0:
			return x.Row0 < y.Row0
		case x.Row1 != y.Row1:
			return x.Row1 > y.Row1 // the longer first
		}
		return x.Item.Event.Title < y.Item.Event.Title
	})
	for i := 0; i < len(bs); {
		// A cluster runs while the next block starts before the rows so far end.
		j, end := i, 0
		for j < len(bs) && (j == i || bs[j].Row0 < end) {
			end = max(end, bs[j].Row1)
			j++
		}
		cluster := bs[i:j]
		var lanes []int // the row each lane is free from
		for k := range cluster {
			b := &cluster[k]
			lane := 0
			for lane < len(lanes) && lanes[lane] > b.Row0 {
				lane++
			}
			if lane == len(lanes) {
				lanes = append(lanes, 0)
			}
			lanes[lane] = b.Row1
			b.Lane = lane
		}
		for k := range cluster {
			b := &cluster[k]
			b.Cluster, b.Lanes, b.Span = i, len(lanes), 1
			// Going right while the lanes there are empty for as long as it lasts.
			for b.Lane+b.Span < len(lanes) && !laneBusy(cluster, b.Lane+b.Span, b.Row0, b.Row1) {
				b.Span++
			}
		}
		i = j
	}
	// Within a cluster, the order is by start and then lane, which the sort
	// above did except that a later block can sit in an earlier lane.
	sort.SliceStable(bs, func(a, b int) bool {
		x, y := bs[a], bs[b]
		if x.Cluster != y.Cluster {
			return x.Cluster < y.Cluster
		}
		if x.Row0 != y.Row0 {
			return x.Row0 < y.Row0
		}
		return x.Lane < y.Lane
	})
}

// laneBusy reports whether a block of the cluster in lane covers any of
// rows [r0, r1).
func laneBusy(cluster []Block, lane, r0, r1 int) bool {
	for _, b := range cluster {
		if b.Lane == lane && b.Row0 < r1 && r0 < b.Row1 {
			return true
		}
	}
	return false
}

// Hidden is how many blocks of a cluster sit in a lane beyond the first
// maxLanes, for a view with room for only that many side by side.
func (g DayGrid) Hidden(cluster, maxLanes int) int {
	n := 0
	for _, b := range g.Blocks {
		if b.Cluster == cluster && b.Lane >= maxLanes {
			n++
		}
	}
	return n
}

// Busy is the stretches of the day that something takes, in minutes of the
// clock, merged and in order. An instant takes none. It is what free time
// is found from.
func (g DayGrid) Busy() [][2]int {
	var spans [][2]int
	for _, b := range g.Blocks {
		if !b.Instant {
			spans = append(spans, [2]int{b.Start, b.End})
		}
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
	var out [][2]int
	for _, s := range spans {
		if n := len(out); n > 0 && s[0] <= out[n-1][1] {
			out[n-1][1] = max(out[n-1][1], s[1])
			continue
		}
		out = append(out, s)
	}
	return out
}
