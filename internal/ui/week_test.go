package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"

	"github.com/miphe/clical/internal/domain"
)

// toWeek switches to the week view and delivers the week load it asked for.
func toWeek(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, press("2"))
	return deliverWeek(t, m, events)
}

func deliverWeek(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, weekLoadedMsg{start: m.week.start, events: events})
	return m
}

// overlapEvents is the usual test week with six meetings on Tuesday 29 Sep
// that overlap each other between 10:00 and 12:00.
func overlapEvents() []domain.Event {
	ev := func(id string, s, e [2]int) domain.Event {
		return domain.NewTimedEvent("meet"+id, "main", "Meeting "+id, at(29, s[0], s[1]), at(29, e[0], e[1]), "", "")
	}
	return append(testEvents(),
		ev("A", [2]int{10, 0}, [2]int{11, 0}), ev("B", [2]int{10, 30}, [2]int{12, 0}), ev("C", [2]int{10, 0}, [2]int{10, 30}),
		ev("D", [2]int{10, 0}, [2]int{10, 30}), ev("E", [2]int{10, 0}, [2]int{10, 30}), ev("F", [2]int{10, 0}, [2]int{10, 30}))
}

// weekAt selects event id on the selected day by stepping to it.
func weekAt(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 40 {
		if m.selectedWeekEventID() == id {
			return m
		}
		m = keys(t, m, "j")
	}
	t.Fatalf("never reached %q; at %q", id, m.selectedWeekEventID())
	return m
}

func weekSelected(m Model) string {
	date := m.weekDate().String()
	if it, ok := m.selectedWeekItem(); ok {
		return date + " " + it.Event.ID
	}
	return fmt.Sprintf("%s nothing at %d", date, m.week.at)
}

func TestKey2ShowsTheWeekAndLoadsIt(t *testing.T) {
	m := loaded(t, 120, 30)
	m, cmd := send(t, m, press("2"))
	if m.view != viewWeek || cmd == nil || !m.week.loading || m.week.start != domain.NewCivilDate(2026, 9, 28) {
		t.Fatalf("view %v, loading %v, week %s, cmd %v", m.view, m.week.loading, m.week.start, cmd != nil)
	}
	if !strings.Contains(screen(m), "Loading…") {
		t.Error("no loading note")
	}
	m = deliverWeek(t, m, testEvents())
	out := screen(m)
	for _, want := range []string{"[2] Week 40", "Tue 29", "Dentist", "Name day", "28 Sep – 4 Oct 2026", "30 min/row"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if weekSelected(m) != "2026-09-29 dentist" {
		t.Errorf("selected %s, want the next event today", weekSelected(m))
	}
	// 5 is not a view any more.
	if m, _ = send(t, m, press("5")); m.view != viewWeek {
		t.Errorf("5 went to view %v", m.view)
	}
}

func TestWeekLinesFitTheWindow(t *testing.T) {
	for _, twelve := range []bool{false, true} {
		for _, size := range [][2]int{{140, 50}, {100, 30}, {56, 20}, {40, 12}, {34, 8}, {30, 6}} {
			m := loaded(t, size[0], size[1])
			m.clock.Twelve = twelve
			m = toWeek(t, m, overlapEvents())
			for _, keysPressed := range [][]string{nil, {"h"}, {"+"}, {"D"}, {"l", "l", "l", "l", "l"}} {
				m2 := keys(t, m, keysPressed...)
				lines := strings.Split(m2.View().Content, "\n")
				if len(lines) != size[1] {
					t.Errorf("%dx%d %v 12h=%v: %d lines, want %d", size[0], size[1], keysPressed, twelve, len(lines), size[1])
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != size[0] {
						t.Errorf("%dx%d %v 12h=%v line %d width %d: %q", size[0], size[1], keysPressed, twelve, i, w, ansi.Strip(l))
					}
				}
			}
		}
	}
}

func TestGoldenWeek(t *testing.T) {
	golden(t, "week", screen(toWeek(t, loaded(t, 124, 28), overlapEvents())))
	golden(t, "week_narrow", screen(toWeek(t, loaded(t, 56, 20), overlapEvents())))
	golden(t, "week_too_small", screen(toWeek(t, loaded(t, 34, 8), overlapEvents())))
}

func TestWeekStepsThroughTheDaysEntriesInTimeOrder(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "g")
	var order []string
	for range 12 {
		order = append(order, m.selectedWeekEventID())
		m = keys(t, m, "j")
	}
	got := strings.Join(order, " ")
	want := "nameday standup meetA meetC meetD meetE meetF meetB dentist train train train"
	if got != want {
		t.Errorf("order\n got %s\nwant %s", got, want)
	}
	if m = keys(t, m, "G"); m.selectedWeekEventID() != "train" {
		t.Errorf("G on %s", m.selectedWeekEventID())
	}
	if m = keys(t, m, "k"); m.selectedWeekEventID() != "dentist" {
		t.Errorf("k on %s", m.selectedWeekEventID())
	}
}

func TestWeekChangingDayKeepsTheTime(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	if weekSelected(m) != "2026-09-29 dentist" {
		t.Fatalf("start %s", weekSelected(m))
	}
	// Wednesday has a reminder at 12:00, and the night train ending at 01:00.
	m = keys(t, m, "l")
	if weekSelected(m) != "2026-09-30 reminder" {
		t.Errorf("l: %s; want the event nearest 15:00", weekSelected(m))
	}
	// The time stays at 15:00, whatever was nearest to it, so back on
	// Tuesday it is the dentist again.
	m = keys(t, m, "h")
	if weekSelected(m) != "2026-09-29 dentist" {
		t.Errorf("h: %s; want what is at 15:00", weekSelected(m))
	}
	// A day with nothing keeps the time as a cursor, and j/k move it a row.
	m = keys(t, m, "h")
	if weekSelected(m) != "2026-09-28 nothing at 900" {
		t.Errorf("h to an empty day: %s", weekSelected(m))
	}
	m = keys(t, m, "j", "j")
	if m.week.at != 960 || !strings.Contains(screen(m), "▸") {
		t.Errorf("the time cursor: at %d, want 960", m.week.at)
	}
	if _, cmd := send(t, m, press("enter")); cmd != nil || m.detail != nil {
		t.Error("enter on a free time did something")
	}
}

func TestWeekEdgesGoToTheAdjacentWeek(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	for range 5 {
		m = keys(t, m, "l")
	}
	if m.week.start != domain.NewCivilDate(2026, 9, 28) || m.weekDate() != domain.NewCivilDate(2026, 10, 4) {
		t.Fatalf("week %s day %s", m.week.start, m.weekDate())
	}
	m, cmd := send(t, m, press("l"))
	if cmd == nil || m.week.start != domain.NewCivilDate(2026, 10, 5) || m.weekDate() != domain.NewCivilDate(2026, 10, 5) || m.week.loaded {
		t.Fatalf("past the edge: week %s day %s loaded %v", m.week.start, m.weekDate(), m.week.loaded)
	}
	// The load of the week we left is stale.
	m, _ = send(t, m, weekLoadedMsg{start: domain.NewCivilDate(2026, 9, 28), events: testEvents()})
	if m.week.loaded {
		t.Error("a stale load was taken")
	}
	m = deliverWeek(t, m, nil)
	if m = keys(t, m, "["); m.week.start != domain.NewCivilDate(2026, 9, 28) || m.weekDate() != domain.NewCivilDate(2026, 9, 28) {
		t.Errorf("[: week %s day %s", m.week.start, m.weekDate())
	}
}

func TestWeekZoomKeepsTheSelectionAndStaysInBounds(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = weekAt(t, keys(t, m, "g"), "meetB")
	for _, c := range []struct {
		key  string
		slot int
	}{{"+", 15}, {"+", 15}, {"-", 30}, {"-", 60}, {"-", 60}, {"=", 30}} {
		m = keys(t, m, c.key)
		if m.weekSlot() != c.slot || m.selectedWeekEventID() != "meetB" {
			t.Errorf("%s: slot %d, selected %s; want %d, meetB", c.key, m.weekSlot(), m.selectedWeekEventID(), c.slot)
		}
		if !strings.Contains(screen(m), "Meeting B") {
			t.Errorf("%s: the selection scrolled out of view:\n%s", c.key, screen(m))
		}
	}
}

func TestWeekWeekdaysOnly(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "D")
	if got := len(m.weekVisible()); got != 5 {
		t.Fatalf("%d days visible", got)
	}
	if out := screen(m); strings.Contains(out, "Sat 3") || strings.Contains(out, "Sun 4") {
		t.Errorf("a weekend in the weekdays:\n%s", out)
	}
	// From Friday, l goes to Monday of the next week.
	m = keys(t, m, "l", "l", "l")
	if m.weekDate() != domain.NewCivilDate(2026, 10, 2) {
		t.Fatalf("on %s", m.weekDate())
	}
	if m, _ = send(t, m, press("l")); m.weekDate() != domain.NewCivilDate(2026, 10, 5) {
		t.Errorf("l from Friday went to %s", m.weekDate())
	}
	// Turning the weekends off while on one moves to a weekday.
	m = toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "l", "l", "l", "l", "D")
	if m.weekHidden(m.weekDate()) {
		t.Errorf("selected %s, which is hidden", m.weekDate())
	}
}

func TestWeekEnterOpensTheDetail(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m, _ = send(t, m, press("enter"))
	if m.detail == nil || m.detail.Event.ID != "dentist" {
		t.Fatalf("enter opened %v", m.detail)
	}
	m, _ = send(t, m, press("esc"))
	m = keys(t, m, "g")
	if m, _ = send(t, m, press("enter")); m.detail == nil || m.detail.Event.ID != "nameday" {
		t.Errorf("an all-day item didn't open")
	}
}

func TestWeekOverlapsShareTheRowAndTheSelectionTakesTheColumn(t *testing.T) {
	m := toWeek(t, loaded(t, 140, 30), overlapEvents())
	// On Monday, so that nothing of Tuesday is selected; scroll up to 10:00.
	m = keys(t, m, "h", "ctrl+u")
	rowOf := func(m Model, text string) string {
		for _, l := range strings.Split(screen(m), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		return ""
	}
	if m.week.top > 20 {
		t.Fatalf("not scrolled up: top %d", m.week.top)
	}
	row := rowOf(m, "10:00 ")
	if strings.Count(row, "●") < 3 || !strings.Contains(row, "+") {
		t.Errorf("the 10:00 row should have two lanes and a count of the rest: %q", row)
	}
	// Select Meeting A: it takes the whole column, with its title in full.
	m = weekAt(t, keys(t, m, "l", "g"), "meetA")
	row = rowOf(m, "Meeting A")
	if !strings.Contains(row, "● Meeting A ") && !strings.Contains(row, "● Meeting A") {
		t.Errorf("the selected meeting isn't whole: %q", row)
	}
	if strings.Count(row, "Meeting") != 1 {
		t.Errorf("the selection should cover its neighbours: %q", row)
	}
}

func TestWeekShowsAMidnightCrosserOnBothDays(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "ctrl+u", "ctrl+u", "ctrl+u", "ctrl+u")
	if out := screen(m); !strings.Contains(out, "▲ Night train") || m.week.top != 0 {
		t.Errorf("the second day of the night train (top %d):\n%s", m.week.top, out)
	}
	m = keys(t, m, "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d")
	if out := screen(m); !strings.Contains(out, "▼") || !strings.Contains(out, "Night train") {
		t.Errorf("the first day of the night train:\n%s", out)
	}
}

func TestWeekNowLine(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	if !strings.Contains(screen(m), "14:30 ") {
		t.Errorf("no time now in the gutter:\n%s", screen(m))
	}
	// A week without today has none.
	m = keys(t, m, "]")
	m = deliverWeek(t, m, nil)
	for _, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, "14:30") && !strings.Contains(l, "Synced") && !strings.Contains(l, "Idle") {
			t.Errorf("a now line in another week: %q", l)
		}
	}
}

func TestWeekFilterAndHiddenCalendars(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "/")
	m = typed(t, m, "dentist")
	if out := screen(m); !strings.Contains(out, "Dentist") || strings.Contains(out, "Meeting") {
		t.Errorf("the filter doesn't narrow the grid:\n%s", out)
	}
	m = keys(t, m, "enter", "esc")
	if !strings.Contains(screen(m), "Name day") {
		t.Error("clearing the filter didn't bring the events back")
	}
}

func TestWeekKeepsTheDayAcrossViews(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "l") // Wednesday
	m, _ = send(t, m, press("1"))
	if m.currentDay() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("the agenda is on %s", m.currentDay())
	}
	m, _ = send(t, m, press("2"))
	m = deliverWeek(t, m, overlapEvents())
	if m.weekDate() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("the week is on %s", m.weekDate())
	}
}

func TestGoldenWeekOverlaps(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 22), overlapEvents())
	m = keys(t, m, "h", "ctrl+u")
	golden(t, "week_overlap", screen(m))
	m = weekAt(t, keys(t, m, "l", "g"), "meetB")
	golden(t, "week_overlap_selected", screen(m))
	golden(t, "week_zoom15", screen(keys(t, m, "+")))
	m12 := m
	m12.clock.Twelve = true
	golden(t, "week_12h", screen(m12))
}

func TestWeekResizeKeepsTheGridFilled(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 20), overlapEvents())
	m = keys(t, m, "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d") // to the end of the day
	if m.week.top == 0 {
		t.Fatal("didn't scroll")
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 124, Height: 60})
	if rows, body := agenda.Rows(m.weekSlot()), m.weekGeometry().body; body <= rows && m.week.top+body > rows || body > rows && m.week.top != 0 {
		t.Errorf("top %d with %d of %d rows shown", m.week.top, body, rows)
	}
	if strings.Contains(screen(m), "24:00") {
		t.Error("an hour past the end of the day")
	}
	if !strings.Contains(screen(m), "23:00") {
		t.Errorf("the end of the day isn't shown after growing:\n%s", screen(m))
	}
}
