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

// toWeekPlus switches to Week+ and delivers the week load it asked for.
func toWeekPlus(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, press("5"))
	return deliverWeekPlus(t, m, events)
}

func deliverWeekPlus(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, weekPlusLoadedMsg{start: m.wp.start, events: events})
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

// wpAt selects event id on the selected day by stepping to it.
func wpAt(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 40 {
		if m.selectedWeekPlusEventID() == id {
			return m
		}
		m = keys(t, m, "j")
	}
	t.Fatalf("never reached %q; at %q", id, m.selectedWeekPlusEventID())
	return m
}

func wpSelected(m Model) string {
	date := m.wpDate().String()
	if it, ok := m.selectedWeekPlusItem(); ok {
		return date + " " + it.Event.ID
	}
	return fmt.Sprintf("%s nothing at %d", date, m.wp.at)
}

func TestKey5ShowsWeekPlusAndLoadsTheWeek(t *testing.T) {
	m := loaded(t, 120, 30)
	m, cmd := send(t, m, press("5"))
	if m.view != viewWeekPlus || cmd == nil || !m.wp.loading || m.wp.start != domain.NewCivilDate(2026, 9, 28) {
		t.Fatalf("view %v, loading %v, week %s, cmd %v", m.view, m.wp.loading, m.wp.start, cmd != nil)
	}
	if !strings.Contains(screen(m), "Loading…") {
		t.Error("no loading note")
	}
	m = deliverWeekPlus(t, m, testEvents())
	out := screen(m)
	for _, want := range []string{"[5] Week+", "Tue 29", "Dentist", "Name day", "28 Sep – 4 Oct 2026", "30 min/row"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if wpSelected(m) != "2026-09-29 dentist" {
		t.Errorf("selected %s, want the next event today", wpSelected(m))
	}
	// 2 stays the week list.
	m, _ = send(t, m, press("2"))
	if m.view != viewWeek {
		t.Errorf("2 went to view %v", m.view)
	}
}

func TestWeekPlusLinesFitTheWindow(t *testing.T) {
	for _, twelve := range []bool{false, true} {
		for _, size := range [][2]int{{140, 50}, {100, 30}, {56, 20}, {40, 12}, {34, 8}, {30, 6}} {
			m := loaded(t, size[0], size[1])
			m.clock.Twelve = twelve
			m = toWeekPlus(t, m, overlapEvents())
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

func TestGoldenWeekPlus(t *testing.T) {
	golden(t, "weekplus", screen(toWeekPlus(t, loaded(t, 124, 28), overlapEvents())))
	golden(t, "weekplus_narrow", screen(toWeekPlus(t, loaded(t, 56, 20), overlapEvents())))
	golden(t, "weekplus_too_small", screen(toWeekPlus(t, loaded(t, 34, 8), overlapEvents())))
}

func TestWeekPlusStepsThroughTheDaysEntriesInTimeOrder(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "g")
	var order []string
	for range 12 {
		order = append(order, m.selectedWeekPlusEventID())
		m = keys(t, m, "j")
	}
	got := strings.Join(order, " ")
	want := "nameday standup meetA meetC meetD meetE meetF meetB dentist train train train"
	if got != want {
		t.Errorf("order\n got %s\nwant %s", got, want)
	}
	if m = keys(t, m, "G"); m.selectedWeekPlusEventID() != "train" {
		t.Errorf("G on %s", m.selectedWeekPlusEventID())
	}
	if m = keys(t, m, "k"); m.selectedWeekPlusEventID() != "dentist" {
		t.Errorf("k on %s", m.selectedWeekPlusEventID())
	}
}

func TestWeekPlusChangingDayKeepsTheTime(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	if wpSelected(m) != "2026-09-29 dentist" {
		t.Fatalf("start %s", wpSelected(m))
	}
	// Wednesday has a reminder at 12:00, and the night train ending at 01:00.
	m = keys(t, m, "l")
	if wpSelected(m) != "2026-09-30 reminder" {
		t.Errorf("l: %s; want the event nearest 15:00", wpSelected(m))
	}
	// The time stays at 15:00, whatever was nearest to it, so back on
	// Tuesday it is the dentist again.
	m = keys(t, m, "h")
	if wpSelected(m) != "2026-09-29 dentist" {
		t.Errorf("h: %s; want what is at 15:00", wpSelected(m))
	}
	// A day with nothing keeps the time as a cursor, and j/k move it a row.
	m = keys(t, m, "h")
	if wpSelected(m) != "2026-09-28 nothing at 900" {
		t.Errorf("h to an empty day: %s", wpSelected(m))
	}
	m = keys(t, m, "j", "j")
	if m.wp.at != 960 || !strings.Contains(screen(m), "▸") {
		t.Errorf("the time cursor: at %d, want 960", m.wp.at)
	}
	if _, cmd := send(t, m, press("enter")); cmd != nil || m.detail != nil {
		t.Error("enter on a free time did something")
	}
}

func TestWeekPlusEdgesGoToTheAdjacentWeek(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	for range 5 {
		m = keys(t, m, "l")
	}
	if m.wp.start != domain.NewCivilDate(2026, 9, 28) || m.wpDate() != domain.NewCivilDate(2026, 10, 4) {
		t.Fatalf("week %s day %s", m.wp.start, m.wpDate())
	}
	m, cmd := send(t, m, press("l"))
	if cmd == nil || m.wp.start != domain.NewCivilDate(2026, 10, 5) || m.wpDate() != domain.NewCivilDate(2026, 10, 5) || m.wp.loaded {
		t.Fatalf("past the edge: week %s day %s loaded %v", m.wp.start, m.wpDate(), m.wp.loaded)
	}
	// The load of the week we left is stale.
	m, _ = send(t, m, weekPlusLoadedMsg{start: domain.NewCivilDate(2026, 9, 28), events: testEvents()})
	if m.wp.loaded {
		t.Error("a stale load was taken")
	}
	m = deliverWeekPlus(t, m, nil)
	if m = keys(t, m, "["); m.wp.start != domain.NewCivilDate(2026, 9, 28) || m.wpDate() != domain.NewCivilDate(2026, 9, 28) {
		t.Errorf("[: week %s day %s", m.wp.start, m.wpDate())
	}
}

func TestWeekPlusZoomKeepsTheSelectionAndStaysInBounds(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = wpAt(t, keys(t, m, "g"), "meetB")
	for _, c := range []struct {
		key  string
		slot int
	}{{"+", 15}, {"+", 15}, {"-", 30}, {"-", 60}, {"-", 60}, {"=", 30}} {
		m = keys(t, m, c.key)
		if m.wpSlot() != c.slot || m.selectedWeekPlusEventID() != "meetB" {
			t.Errorf("%s: slot %d, selected %s; want %d, meetB", c.key, m.wpSlot(), m.selectedWeekPlusEventID(), c.slot)
		}
		if !strings.Contains(screen(m), "Meeting B") {
			t.Errorf("%s: the selection scrolled out of view:\n%s", c.key, screen(m))
		}
	}
}

func TestWeekPlusWeekdaysOnly(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "D")
	if got := len(m.wpVisible()); got != 5 {
		t.Fatalf("%d days visible", got)
	}
	if out := screen(m); strings.Contains(out, "Sat 3") || strings.Contains(out, "Sun 4") {
		t.Errorf("a weekend in the weekdays:\n%s", out)
	}
	// From Friday, l goes to Monday of the next week.
	m = keys(t, m, "l", "l", "l")
	if m.wpDate() != domain.NewCivilDate(2026, 10, 2) {
		t.Fatalf("on %s", m.wpDate())
	}
	if m, _ = send(t, m, press("l")); m.wpDate() != domain.NewCivilDate(2026, 10, 5) {
		t.Errorf("l from Friday went to %s", m.wpDate())
	}
	// Turning the weekends off while on one moves to a weekday.
	m = toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "l", "l", "l", "l", "D")
	if m.wpHidden(m.wpDate()) {
		t.Errorf("selected %s, which is hidden", m.wpDate())
	}
}

func TestWeekPlusEnterOpensTheDetail(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
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

func TestWeekPlusOverlapsShareTheRowAndTheSelectionTakesTheColumn(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 140, 30), overlapEvents())
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
	if m.wp.top > 20 {
		t.Fatalf("not scrolled up: top %d", m.wp.top)
	}
	row := rowOf(m, "10:00 ")
	if strings.Count(row, "●") < 3 || !strings.Contains(row, "+") {
		t.Errorf("the 10:00 row should have two lanes and a count of the rest: %q", row)
	}
	// Select Meeting A: it takes the whole column, with its title in full.
	m = wpAt(t, keys(t, m, "l", "g"), "meetA")
	row = rowOf(m, "Meeting A")
	if !strings.Contains(row, "● Meeting A ") && !strings.Contains(row, "● Meeting A") {
		t.Errorf("the selected meeting isn't whole: %q", row)
	}
	if strings.Count(row, "Meeting") != 1 {
		t.Errorf("the selection should cover its neighbours: %q", row)
	}
}

func TestWeekPlusShowsAMidnightCrosserOnBothDays(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "ctrl+u", "ctrl+u", "ctrl+u", "ctrl+u")
	if out := screen(m); !strings.Contains(out, "▲ Night train") || m.wp.top != 0 {
		t.Errorf("the second day of the night train (top %d):\n%s", m.wp.top, out)
	}
	m = keys(t, m, "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d")
	if out := screen(m); !strings.Contains(out, "▼") || !strings.Contains(out, "Night train") {
		t.Errorf("the first day of the night train:\n%s", out)
	}
}

func TestWeekPlusNowLine(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	if !strings.Contains(screen(m), "14:30 ") {
		t.Errorf("no time now in the gutter:\n%s", screen(m))
	}
	// A week without today has none.
	m = keys(t, m, "]")
	m = deliverWeekPlus(t, m, nil)
	for _, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, "14:30") && !strings.Contains(l, "Synced") && !strings.Contains(l, "Idle") {
			t.Errorf("a now line in another week: %q", l)
		}
	}
}

func TestWeekPlusFilterAndHiddenCalendars(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
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

func TestWeekPlusLeavesTheWeekViewAlone(t *testing.T) {
	m := toWeek(t, loaded(t, 124, 28), testEvents())
	before := screen(m)
	m, _ = send(t, m, press("5"))
	m = deliverWeekPlus(t, m, testEvents())
	m, _ = send(t, m, press("2"))
	m = deliverWeek(t, m, testEvents())
	if screen(m) != before {
		t.Errorf("the week view changed after a visit to Week+:\n%s\nwas\n%s", screen(m), before)
	}
}

func TestWeekPlusKeepsTheDayAcrossViews(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 28), overlapEvents())
	m = keys(t, m, "l") // Wednesday
	m, _ = send(t, m, press("1"))
	if m.currentDay() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("the agenda is on %s", m.currentDay())
	}
	m, _ = send(t, m, press("5"))
	m = deliverWeekPlus(t, m, overlapEvents())
	if m.wpDate() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("Week+ is on %s", m.wpDate())
	}
}

func TestGoldenWeekPlusOverlaps(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 22), overlapEvents())
	m = keys(t, m, "h", "ctrl+u")
	golden(t, "weekplus_overlap", screen(m))
	m = wpAt(t, keys(t, m, "l", "g"), "meetB")
	golden(t, "weekplus_overlap_selected", screen(m))
	golden(t, "weekplus_zoom15", screen(keys(t, m, "+")))
	m12 := m
	m12.clock.Twelve = true
	golden(t, "weekplus_12h", screen(m12))
}

func TestWeekPlusResizeKeepsTheGridFilled(t *testing.T) {
	m := toWeekPlus(t, loaded(t, 124, 20), overlapEvents())
	m = keys(t, m, "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d", "ctrl+d") // to the end of the day
	if m.wp.top == 0 {
		t.Fatal("didn't scroll")
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 124, Height: 60})
	if rows, body := agenda.Rows(m.wpSlot()), m.wpGeometry().body; body <= rows && m.wp.top+body > rows || body > rows && m.wp.top != 0 {
		t.Errorf("top %d with %d of %d rows shown", m.wp.top, body, rows)
	}
	if strings.Contains(screen(m), "24:00") {
		t.Error("an hour past the end of the day")
	}
	if !strings.Contains(screen(m), "23:00") {
		t.Errorf("the end of the day isn't shown after growing:\n%s", screen(m))
	}
}
