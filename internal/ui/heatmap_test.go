package ui

import (
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

func cd(y, m, d int) domain.CivilDate { return domain.NewCivilDate(y, m, d) }

func daysWith(counts map[domain.CivilDate]int) map[domain.CivilDate][]write.Archived {
	out := map[domain.CivilDate][]write.Archived{}
	for d, n := range counts {
		out[d] = make([]write.Archived, n)
	}
	return out
}

func TestWeekFloor(t *testing.T) {
	wed := cd(2026, 9, 30)
	for _, c := range []struct {
		start time.Weekday
		want  domain.CivilDate
	}{
		{time.Monday, cd(2026, 9, 28)},
		{time.Sunday, cd(2026, 9, 27)},
		{time.Wednesday, wed},
	} {
		if got := weekFloor(wed, c.start); got != c.want {
			t.Errorf("weekFloor(Wed, %v) = %v, want %v", c.start, got, c.want)
		}
	}
	if got := weekFloor(cd(2026, 9, 27), time.Monday); got != cd(2026, 9, 21) {
		t.Errorf("a Sunday belongs to the week before: %v", got)
	}
}

func TestHeatLevels(t *testing.T) {
	for count, want := range map[int]int{-1: 0, 0: 0, 1: 1, 3: 1, 4: 2, 6: 2, 7: 3, 8: 3, 40: 3} {
		if got := heatLevel(count); got != want {
			t.Errorf("level(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestHeatColumnsSeparateMonths(t *testing.T) {
	last := cd(2026, 10, 4) // a Sunday: columns are Sunday-first weeks
	left := cd(2026, 9, 13)
	cols := heatColumns(left, last, 100)
	var starts []string
	var gaps []bool
	for _, c := range cols {
		starts = append(starts, c.start.String())
		gaps = append(gaps, c.gap)
	}
	// 13, 20, 27 Sep; then 4 Oct starts October: a blank column before it.
	if len(cols) != 4 || cols[3].start != last || starts[0] != "2026-09-13" {
		t.Fatalf("columns = %v", starts)
	}
	if want := []bool{false, false, false, true}; gaps[0] != want[0] || gaps[1] != want[1] || gaps[2] != want[2] || gaps[3] != want[3] {
		t.Errorf("gaps = %v, want %v", gaps, want)
	}

	// Width counts the gap: 4 weeks and 1 gap need 9 columns, not 8.
	if got := len(heatColumns(left, last, 8)); got != 3 {
		t.Errorf("8 columns hold %d weeks, want 3", got)
	}
	if got := len(heatColumns(left, last, 9)); got != 4 {
		t.Errorf("9 columns hold %d weeks, want 4", got)
	}
}

func TestHeatLeftForFillsTheWindow(t *testing.T) {
	last := cd(2026, 9, 27)
	for _, width := range []int{2, 6, 21, 40, 119} {
		left := heatLeftFor(last, width)
		cols := heatColumns(left, last, width)
		if len(cols) == 0 || cols[len(cols)-1].start != last {
			t.Errorf("width %d: the newest week isn't the rightmost: %v", width, cols)
		}
		// One more week to the left wouldn't fit.
		more := heatColumns(left.AddDays(-7), last, width)
		if len(more) > 0 && more[len(more)-1].start == last {
			t.Errorf("width %d: left isn't as far left as it can be", width)
		}
	}
}

func TestViewLeftKeepsTheCursorOnScreen(t *testing.T) {
	today := cd(2026, 9, 29) // Tue; its Sunday-first week starts 27 Sep
	hi := today
	width := 20
	h := heatState{loaded: true, cursor: today}

	left := h.viewLeft(width, hi)
	if cols := heatColumns(left, weekFloor(hi, heatStart), width); cols[len(cols)-1].start != cd(2026, 9, 27) {
		t.Fatal("not pinned to the newest week")
	}

	// Scroll back far: the window moves just enough, cursor at the left edge.
	h.cursor = cd(2026, 3, 2) // its week starts Sun 1 Mar
	h.left = left
	left2 := h.viewLeft(width, hi)
	if left2 != cd(2026, 3, 1) {
		t.Errorf("left = %v, want the cursor's week", left2)
	}

	// Moving forward again shows the cursor at the right edge, not the left.
	h.left = left2
	h.cursor = cd(2026, 6, 1) // week of Sun 31 May
	left3 := h.viewLeft(width, hi)
	cols := heatColumns(left3, weekFloor(hi, heatStart), width)
	if cols[len(cols)-1].start != cd(2026, 5, 31) {
		t.Errorf("cursor week isn't the rightmost: left %v", left3)
	}

	// Back at the newest week it pins again.
	h.left = left3
	h.cursor = today
	if got := h.viewLeft(width, hi); got != left {
		t.Errorf("left = %v, want %v", got, left)
	}
}

func TestHeatMoveAndExtend(t *testing.T) {
	today := cd(2026, 9, 29) // a Tuesday
	h := heatState{loaded: true, cursor: today, anchor: today}
	h = h.withData(daysWith(map[domain.CivilDate]int{cd(2026, 8, 10): 1, today: 1}), time.Time{})
	h.cursor, h.anchor = today, today

	h = h.move(heatDown, false, today)
	if h.cursor != today { // nothing after today
		t.Errorf("moved past today: %v", h.cursor)
	}
	h = h.move(heatLeft, false, today)
	if h.cursor != cd(2026, 9, 22) || h.anchor != h.cursor {
		t.Errorf("left = %v anchor %v", h.cursor, h.anchor)
	}
	h = h.move(heatUp, false, today)
	if h.cursor != cd(2026, 9, 21) {
		t.Errorf("up = %v", h.cursor)
	}

	// Extending keeps the anchor.
	h = h.move(heatDown, true, today).move(heatDown, true, today)
	if h.anchor != cd(2026, 9, 21) || h.cursor != cd(2026, 9, 23) {
		t.Errorf("extend: anchor %v cursor %v", h.anchor, h.cursor)
	}
	lo, hi := h.span(time.Monday)
	if lo != cd(2026, 9, 21) || hi != cd(2026, 9, 23) {
		t.Errorf("span = %v–%v", lo, hi)
	}
	// Extending backwards past the anchor still gives lo <= hi.
	h = h.move(heatUp, true, today).move(heatUp, true, today).move(heatUp, true, today).move(heatUp, true, today)
	lo, hi = h.span(time.Monday)
	if lo != cd(2026, 9, 19) || hi != cd(2026, 9, 21) {
		t.Errorf("backwards span = %v–%v", lo, hi)
	}
	// A plain move afterwards selects one day again.
	h = h.move(heatDown, false, today)
	if lo, hi = h.span(time.Monday); lo != hi {
		t.Errorf("plain move kept a range: %v–%v", lo, hi)
	}
}

func TestHeatCursorCrossesEmptyDaysForAYear(t *testing.T) {
	today := cd(2026, 9, 29)
	h := heatState{cursor: today, anchor: today}.withData(daysWith(map[domain.CivilDate]int{cd(2026, 9, 10): 2}), time.Time{})
	for i := 0; i < 80; i++ {
		h = h.move(heatLeft, false, today)
	}
	if want := today.AddDays(-heatReach); h.cursor != want {
		t.Errorf("cursor = %v, want a year and a week back, %v", h.cursor, want)
	}
	// Older tasks extend the reach.
	old := heatState{cursor: today, anchor: today}.withData(daysWith(map[domain.CivilDate]int{cd(2024, 5, 1): 1}), time.Time{})
	for i := 0; i < 200; i++ {
		old = old.move(heatLeft, false, today)
	}
	if old.cursor != cd(2024, 5, 1) {
		t.Errorf("cursor = %v, want the oldest task's day", old.cursor)
	}
	// Before the data arrives, any past day is fine.
	pre := heatState{}.goTo(cd(2020, 1, 1), today)
	if pre.cursor != cd(2020, 1, 1) {
		t.Errorf("before load: %v", pre.cursor)
	}
}

func TestHeatUnits(t *testing.T) {
	today := cd(2026, 9, 29)
	h := heatState{cursor: cd(2026, 6, 10), anchor: cd(2026, 6, 10)}.withData(
		daysWith(map[domain.CivilDate]int{cd(2026, 1, 5): 1, today: 1}), time.Time{})
	h.cursor, h.anchor = cd(2026, 6, 10), cd(2026, 6, 10) // a Wednesday

	h = h.cycleUnit() // week
	if lo, hi := h.span(time.Monday); lo != cd(2026, 6, 8) || hi != cd(2026, 6, 14) {
		t.Errorf("week = %v–%v", lo, hi)
	}
	h = h.move(heatRight, false, today)
	if lo, hi := h.span(time.Monday); lo != cd(2026, 6, 15) || hi != cd(2026, 6, 21) {
		t.Errorf("next week = %v–%v", lo, hi)
	}
	// Up and down move by the week too.
	h = h.move(heatUp, false, today).move(heatUp, false, today)
	if lo, _ := h.span(time.Monday); lo != cd(2026, 6, 1) {
		t.Errorf("two weeks back = %v", lo)
	}
	// Extending by weeks.
	h = h.move(heatDown, true, today).move(heatDown, true, today)
	if lo, hi := h.span(time.Monday); lo != cd(2026, 6, 1) || hi != cd(2026, 6, 21) {
		t.Errorf("three weeks = %v–%v", lo, hi)
	}
	// Sunday-first weeks.
	if lo, hi := h.span(time.Sunday); lo != cd(2026, 5, 31) || hi != cd(2026, 6, 20) {
		t.Errorf("Sunday-first = %v–%v", lo, hi)
	}

	h = h.cycleUnit() // month, alone
	if lo, hi := h.span(time.Monday); lo != cd(2026, 6, 1) || hi != cd(2026, 6, 30) {
		t.Errorf("month = %v–%v", lo, hi)
	}
	h = h.move(heatLeft, false, today)
	if lo, hi := h.span(time.Monday); lo != cd(2026, 5, 1) || hi != cd(2026, 5, 31) {
		t.Errorf("previous month = %v–%v", lo, hi)
	}
	// Moving by month from the 31st clamps to a shorter month.
	h.cursor, h.anchor = cd(2026, 8, 31), cd(2026, 8, 31)
	h = h.move(heatRight, false, today)
	if h.cursor != cd(2026, 9, 29) { // September has 30 days, but today is the 29th
		t.Errorf("cursor = %v", h.cursor)
	}

	h = h.cycleUnit() // back to days
	if h.unit != heatDay || h.anchor != h.cursor {
		t.Errorf("unit %v anchor %v", h.unit, h.anchor)
	}

	h, ok := h.collapse()
	if ok {
		t.Error("collapse changed a single day")
	}
	h = h.move(heatUp, true, today)
	if _, ok = h.collapse(); !ok {
		t.Error("collapse did nothing to a range")
	}
}

func TestWeekdayLetters(t *testing.T) {
	var got string
	for r := 0; r < heatRows; r++ {
		got += weekdayLetter(time.Weekday(r))
	}
	if got != "SMTWTFS" { // Sunday on top, Saturday at the bottom
		t.Errorf("letters = %q", got)
	}
}
