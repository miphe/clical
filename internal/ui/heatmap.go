package ui

import (
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
	heatGapW   = 1 // the blank column between months
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

// A day with no tasks is a dot; a day with tasks is the same solid square
// whatever the count, and only its color says how many (see heatStyles). A
// selected day is a square with a dot in it, in its own color. A day with
// notes but no tasks is a hollow dot: notes aren't activity, so it keeps the
// quietest color, but it isn't empty.
const (
	heatNone = "·"
	heatNote = "∘"
	// heatNoteMark leads a note in the day's list.
	heatNoteMark = "¶"
	heatDone     = "■"
	heatSel      = "▣"
)

// heatState is the heatmap's data and where the user is in it.
type heatState struct {
	loaded bool
	err    error
	days   map[domain.CivilDate]write.DayArchive
	first  domain.CivilDate // the earliest and latest day with a task or a note
	last   domain.CivilDate
	at     time.Time // when the data was read

	cursor, anchor domain.CivilDate // the selection is anchor..cursor
	unit           heatUnit
	left           domain.CivilDate // first visible week
	pinned         bool             // the window ends at the newest column, and follows it as days pass
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
func (h heatState) withData(days map[domain.CivilDate]write.DayArchive, at time.Time) heatState {
	h.days, h.at, h.loaded, h.err = days, at, true, nil
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

// heatLevel is a day's intensity step, by how many tasks were completed: 0
// for none, 1 for one to three, 2 for four to six, 3 for seven or more. The
// steps are fixed, so a day means the same thing whatever else is in the
// archive.
func heatLevel(count int) int {
	switch {
	case count <= 0:
		return 0
	case count <= 3:
		return 1
	case count <= 6:
		return 2
	}
	return 3
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

// A month is a block of columns of its own, so each month has exactly its
// days: the first column starts on the weekday of the 1st, the last ends on
// the last day, and a blank column separates it from the next month. A week
// that spans two months is drawn in both, each with its own days. The cells of
// a block that aren't a day (the rows above the 1st and below the last day)
// are dots, so each month is a square block; they can't be reached.

// colStart is the first day of the column that holds d: the Sunday of d's
// week, or the 1st of d's month when that comes later.
func colStart(d domain.CivilDate) domain.CivilDate {
	return maxDay(weekFloor(d, heatStart), domain.NewCivilDate(d.Year, d.Month, 1))
}

// colEnd is the last day of the column that starts on s: the Saturday, or the
// month's last day when that comes first.
func colEnd(s domain.CivilDate) domain.CivilDate {
	return minDay(weekFloor(s, heatStart).AddDays(6), monthEnd(s))
}

// heatCol is one column: the days from start to end, down from the row of
// start's weekday.
type heatCol struct {
	start, end domain.CivilDate
	gap        bool // a blank column goes before it: a new month starts here
}

// day is the day in a row (0 is Sunday), if the column has one there.
func (c heatCol) day(row int) (domain.CivilDate, bool) {
	first, last := int(c.start.Weekday()), int(c.end.Weekday())
	if row < first || row > last {
		return domain.CivilDate{}, false
	}
	return c.start.AddDays(row - first), true
}

// heatColumns lays out columns from the one holding left, as many as fit in
// width columns, and none for days after hi.
func heatColumns(left, hi domain.CivilDate, width int) []heatCol {
	var cols []heatCol
	used := 0
	for s := colStart(left); !hi.Before(s); s = colEnd(s).AddDays(1) {
		gap := len(cols) > 0 && !sameMonth(s, cols[len(cols)-1].start)
		need := heatCellW
		if gap {
			need += heatGapW
		}
		if used+need > width {
			break
		}
		cols = append(cols, heatCol{start: s, end: colEnd(s), gap: gap})
		used += need
	}
	return cols
}

// heatLeftFor is the first column to show so that the one holding day is the
// rightmost, with the window filled.
func heatLeftFor(day domain.CivilDate, width int) domain.CivilDate {
	left, used := colStart(day), heatCellW
	for {
		prev := colStart(left.AddDays(-1))
		need := heatCellW
		if !sameMonth(prev, left) {
			need += heatGapW
		}
		if used+need > width {
			return left
		}
		left, used = prev, used+need
	}
}

// viewLeft is the first visible column for a window width columns wide: the
// stored one, moved just far enough to keep the cursor's column on screen, and
// pinned to the newest column when the window reaches it.
func (h heatState) viewLeft(width int, hi domain.CivilDate) domain.CivilDate {
	left := heatLeftFor(hi, width)
	if isSet(h.left) && !h.pinned {
		left = colStart(h.left)
	}
	cc := colStart(h.cursor)
	cols := heatColumns(left, hi, width)
	switch {
	case cc.Before(left):
		left = cc
	case len(cols) > 0 && cols[len(cols)-1].start.Before(cc):
		left = heatLeftFor(cc, width)
	}
	if cols = heatColumns(left, hi, width); len(cols) > 0 && cols[len(cols)-1].start == colStart(hi) {
		left = heatLeftFor(hi, width)
	}
	return left
}

// weekdayLetter is the one-letter row label.
func weekdayLetter(d time.Weekday) string {
	return string("SMTWTFS"[d])
}
