package ui

import (
	"sort"
	"time"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// The completion heatmap: one cell per day, a column per week, like GitHub's
// contribution graph. This file is the pure part (which days are shown, what
// is selected, how busy a day is); heatview.go draws it.

// heatUnit is what the selection moves and grows by.
type heatUnit int

const (
	heatDay heatUnit = iota
	heatWeek
	heatMonth
)

type heatDir int

const (
	heatUp heatDir = iota
	heatDown
	heatLeft
	heatRight
)

const (
	heatCellW  = 2 // columns per day
	heatLabelW = 2 // the weekday letter and a space
	heatRows   = 7

	// heatStart is the heatmap's first row, whatever the week-start setting:
	// Sunday on top and Saturday at the bottom keep the working days, where
	// the activity is, in the middle rows. The setting still decides what a
	// selected "week" is.
	heatStart = time.Sunday

	// heatReach is how far back the cursor can go through empty days: a year
	// and a week, further if there are older tasks.
	heatReach = 371
)

// A day with no tasks is a dot; a day with tasks is a flat square whose
// intensity is a text attribute (faint, plain, bold), never a shade glyph or a
// background color.
const (
	heatNone = "·"
	heatDone = "■"
	heatSel  = "▣" // a selected day: the square with a dot in it, in its own color
)

// heatState is the heatmap's data and where the user is in it.
type heatState struct {
	loaded bool
	err    error
	days   map[domain.CivilDate][]write.Archived
	cut    [2]int           // task counts at the top of intensity steps 1 and 2
	first  domain.CivilDate // the earliest and latest day with a task
	last   domain.CivilDate
	at     time.Time // when the data was read

	cursor, anchor domain.CivilDate // the selection is anchor..cursor
	unit           heatUnit
	left           domain.CivilDate // first visible week
	scroll         int              // first visible line of the tasks list
}

func isSet(d domain.CivilDate) bool { return d.Year != 0 }

func sameMonth(a, b domain.CivilDate) bool { return a.Year == b.Year && a.Month == b.Month }

func minDay(a, b domain.CivilDate) domain.CivilDate {
	if b.Before(a) {
		return b
	}
	return a
}

func maxDay(a, b domain.CivilDate) domain.CivilDate {
	if a.Before(b) {
		return b
	}
	return a
}

// weekFloor is the first day of the week that contains d.
func weekFloor(d domain.CivilDate, start time.Weekday) domain.CivilDate {
	return d.AddDays(-((int(d.Weekday()) - int(start) + 7) % 7))
}

// monthEnd is the last day of d's month.
func monthEnd(d domain.CivilDate) domain.CivilDate {
	return domain.NewCivilDate(d.Year, d.Month, d.DaysInMonth())
}

// withData replaces the archive's contents, keeping the user's place.
func (h heatState) withData(days map[domain.CivilDate][]write.Archived, at time.Time) heatState {
	h.days, h.at, h.loaded, h.err = days, at, true, nil
	h.cut = heatCutoffs(days)
	h.first, h.last = domain.CivilDate{}, domain.CivilDate{}
	for d := range days {
		if !isSet(h.first) || d.Before(h.first) {
			h.first = d
		}
		if !isSet(h.last) || h.last.Before(d) {
			h.last = d
		}
	}
	return h
}

// heatCutoffs are the terciles of the busy days' task counts: a day is as
// intense as the share of busy days it is busier than.
func heatCutoffs(days map[domain.CivilDate][]write.Archived) [2]int {
	var counts []int
	for _, tasks := range days {
		if len(tasks) > 0 {
			counts = append(counts, len(tasks))
		}
	}
	var cut [2]int
	if len(counts) == 0 {
		return cut
	}
	sort.Ints(counts)
	for k := range cut {
		cut[k] = counts[((k+1)*len(counts)+2)/3-1]
	}
	return cut
}

// heatLevel is a day's intensity step: 0 for none, then 1–3.
func heatLevel(count int, cut [2]int) int {
	if count <= 0 {
		return 0
	}
	level := 1
	for _, c := range cut {
		if count > c {
			level++
		}
	}
	return level
}

// bounds are the first and last day the cursor can reach, empty days
// included: a year back from today (or to an older task), up to today (or to
// a later task if the clock was ahead).
func (h heatState) bounds(today domain.CivilDate) (lo, hi domain.CivilDate) {
	hi = today
	if isSet(h.last) {
		hi = maxDay(hi, h.last)
	}
	if h.loaded {
		lo = today.AddDays(-heatReach)
		if isSet(h.first) {
			lo = minDay(lo, h.first)
		}
	}
	return lo, hi
}

func (h heatState) clamp(d, today domain.CivilDate) domain.CivilDate {
	lo, hi := h.bounds(today)
	d = minDay(d, hi)
	if isSet(lo) {
		d = maxDay(d, lo)
	}
	return d
}

// goTo puts the cursor, alone, on a day.
func (h heatState) goTo(day, today domain.CivilDate) heatState {
	h.cursor = h.clamp(day, today)
	h.anchor = h.cursor
	return h
}

// move moves the cursor by the current unit. Extending keeps the anchor
// where it was, so the selection grows; otherwise it is the cursor's day.
func (h heatState) move(dir heatDir, extend bool, today domain.CivilDate) heatState {
	sign := 1
	if dir == heatUp || dir == heatLeft {
		sign = -1
	}
	to := h.cursor
	switch h.unit {
	case heatMonth:
		to = h.cursor.AddMonths(sign)
	case heatWeek:
		to = h.cursor.AddDays(7 * sign)
	default:
		if dir == heatLeft || dir == heatRight {
			to = h.cursor.AddDays(7 * sign)
		} else {
			to = h.cursor.AddDays(sign)
		}
	}
	h.cursor = h.clamp(to, today)
	if !extend {
		h.anchor = h.cursor
	}
	return h
}

// cycleUnit goes day → week → month → day, and selects the cursor's day,
// week or month alone.
func (h heatState) cycleUnit() heatState {
	h.unit = (h.unit + 1) % 3
	h.anchor = h.cursor
	return h
}

// collapse shrinks the selection to the cursor's day.
func (h heatState) collapse() (heatState, bool) {
	if h.anchor == h.cursor && h.unit == heatDay {
		return h, false
	}
	h.anchor, h.unit = h.cursor, heatDay
	return h, true
}

// span is the selected days, whole weeks or months when the unit says so.
func (h heatState) span(start time.Weekday) (lo, hi domain.CivilDate) {
	a, b := h.anchor, h.cursor
	if b.Before(a) {
		a, b = b, a
	}
	switch h.unit {
	case heatWeek:
		return weekFloor(a, start), weekFloor(b, start).AddDays(6)
	case heatMonth:
		return domain.NewCivilDate(a.Year, a.Month, 1), monthEnd(b)
	}
	return a, b
}

// heatCol is one week's column.
type heatCol struct {
	start domain.CivilDate
	gap   bool // a blank column goes before it: a new month starts here
}

// heatColumns lays out week columns from left, as many as fit in width
// columns, and none after last. A blank column separates months, in place of
// month labels.
func heatColumns(left, last domain.CivilDate, width int) []heatCol {
	var cols []heatCol
	used := 0
	for w := left; !last.Before(w); w = w.AddDays(7) {
		gap := len(cols) > 0 && !sameMonth(w, cols[len(cols)-1].start)
		need := heatCellW
		if gap {
			need++
		}
		if used+need > width {
			break
		}
		cols = append(cols, heatCol{start: w, gap: gap})
		used += need
	}
	return cols
}

// heatLeftFor is the first week to show so that the week last is the
// rightmost one, with the window filled.
func heatLeftFor(last domain.CivilDate, width int) domain.CivilDate {
	left, used := last, heatCellW
	for {
		prev := left.AddDays(-7)
		need := heatCellW
		if !sameMonth(prev, left) {
			need++
		}
		if used+need > width {
			return left
		}
		left, used = prev, used+need
	}
}

// viewLeft is the first visible week for a window width columns wide: the
// stored one, moved just far enough to keep the cursor's week on screen, and
// pinned to the newest week when the window reaches it.
func (h heatState) viewLeft(width int, hi domain.CivilDate) domain.CivilDate {
	last := weekFloor(hi, heatStart)
	left := heatLeftFor(last, width)
	if isSet(h.left) {
		left = weekFloor(h.left, heatStart)
	}
	cw := weekFloor(h.cursor, heatStart)
	cols := heatColumns(left, last, width)
	switch {
	case cw.Before(left):
		left = cw
	case len(cols) > 0 && cols[len(cols)-1].start.Before(cw):
		left = heatLeftFor(cw, width)
	}
	if cols = heatColumns(left, last, width); len(cols) > 0 && cols[len(cols)-1].start == last {
		left = heatLeftFor(last, width)
	}
	return left
}

// weekdayLetter is the one-letter row label.
func weekdayLetter(d time.Weekday) string {
	return string("SMTWTFS"[d])
}
