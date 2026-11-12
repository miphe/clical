package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

// plusEvents are the test events and, today (Tue 29 Sep, now 14:30), a long
// meeting on now whose title can't fit any panel, and one that overlaps the
// dentist.
func plusEvents() []domain.Event {
	return append(testEvents(),
		domain.NewTimedEvent("planning", "main", "Quarterly planning with the whole product organisation and the guests from design",
			at(29, 13, 0), at(29, 14, 45), "", ""),
		domain.NewTimedEvent("call", "hol", "Call with the bank", at(29, 15, 0), at(29, 15, 20), "", ""),
	)
}

type plusSource struct{ nopSource }

func (plusSource) Events(context.Context, time.Time, time.Time) ([]domain.Event, error) {
	return plusEvents(), nil
}

// plusModel is Focus+ (key 5) over plusEvents and a busy archive.
func plusModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(plusSource{}, loc, testNow)
	m.tick = 0
	m, _ = send(t, m,
		tea.WindowSizeMsg{Width: width, Height: height},
		calendarsLoadedMsg{rows: testCalendars},
		eventsLoadedMsg{from: m.from, days: m.days, events: plusEvents()},
		syncDoneMsg{at: at(29, 14, 29)},
		flashEndMsg{until: at(29, 14, 29).Add(flashDuration)},
	)
	src := newStoreSource(t)
	m = m.WithWrite(src)
	m.panelFlash = true
	m, _ = run(t, m, m.loadWrite(writeGlobal))
	seedArchive(t, src, busyArchive())
	return keys(t, m, "5")
}

func TestFocusPlusGolden(t *testing.T) {
	golden(t, "focusplus_wide", screen(plusModel(t, 110, 40)))
	golden(t, "focusplus_write_open", screen(keys(t, plusModel(t, 110, 40), "W")))
	golden(t, "focusplus_laptop", screen(plusModel(t, 80, 30)))
	golden(t, "focusplus_small", screen(plusModel(t, 56, 16)))
	golden(t, "focusplus_history", screen(keys(t, plusModel(t, 110, 40), "tab")))
}

func TestFocusPlusIsKey5AndLeavesFocusAlone(t *testing.T) {
	m := plusModel(t, 110, 40)
	if m.view != viewFocusPlus || m.focus != focusMain {
		t.Fatalf("5: view %v, focus %v; want Focus+ with Today focused", m.view, m.focus)
	}
	if !m.sidebarCollapsed {
		t.Error("Focus+ should collapse the calendar list")
	}
	s := screen(m)
	for _, want := range []string{"[5] Focus+", "now", "next", "Dentist", "Completed", "Heatmap"} {
		if !strings.Contains(s, want) {
			t.Errorf("Focus+ lacks %q:\n%s", want, s)
		}
	}

	// 4 is the focus view as it was: its own layout, and the heatmap focused.
	old := keys(t, m, "4")
	if old.view != viewHeat || old.focus != focusHeat || !strings.Contains(screen(old), "Next") {
		t.Errorf("4 from Focus+: view %v, focus %v\n%s", old.view, old.focus, screen(old))
	}
	// Another view gives the list back.
	if a := keys(t, m, "1"); a.sidebarCollapsed {
		t.Error("the list stays collapsed after leaving Focus+")
	}
}

func TestFocusPlusHeaderSaysWhatsOnAndNext(t *testing.T) {
	m := plusModel(t, 110, 40)
	head := strings.Join(m.todayHeader(m.todayContentW()), "\n")
	head = ansi.Strip(head)
	for _, want := range []string{"now", "Quarterly planning", "13:00–14:45 · 15 min left", "next", "Call with the bank", "in 30 min", "+1"} {
		if !strings.Contains(head, want) {
			t.Errorf("header lacks %q:\n%s", want, head)
		}
	}

	// After the last event of the day: nothing more today, and the first of
	// tomorrow named with its day.
	m.now = at(29, 23, 30)
	head = ansi.Strip(strings.Join(m.todayHeader(80), "\n"))
	if !strings.Contains(head, "Night train") || !strings.Contains(head, "left") {
		t.Errorf("at 23:30 the night train is on:\n%s", head)
	}
	m.now = at(30, 2, 0)
	head = ansi.Strip(strings.Join(m.todayHeader(80), "\n"))
	if !strings.Contains(head, "nothing more today") && !strings.Contains(head, "free until") {
		t.Errorf("at 02:00 nothing is on:\n%s", head)
	}
}

func TestFocusPlusListWrapsTitlesInsteadOfCuttingThem(t *testing.T) {
	m := plusModel(t, 80, 40)
	s := screen(m)
	title := "Quarterly planning with the whole product organisation and the guests from design"
	if strings.Contains(s, title) {
		t.Fatalf("the title fits on one line at 80 columns; the test needs a longer one")
	}
	// Every word of the title is in the list, the last one included.
	for _, w := range strings.Fields(title) {
		if !strings.Contains(s, w) {
			t.Errorf("the title's word %q is cut off:\n%s", w, s)
		}
	}
	if strings.Contains(s, "Quarterly planning with the whole product…") {
		t.Errorf("the list cut the title:\n%s", s)
	}
}

func TestFocusPlusStripMarksNowAndTheSelection(t *testing.T) {
	m := plusModel(t, 110, 40)
	strip := m.todayStrip(m.todayContentW())
	if len(strip) != todayStripH {
		t.Fatalf("strip rows = %d", len(strip))
	}
	bar, marks := ansi.Strip(strip[1]), ansi.Strip(strip[2])
	if !strings.Contains(bar, "━") || !strings.Contains(bar, "═") {
		t.Errorf("busy and overlapping cells missing: %q", bar)
	}
	if strings.Count(marks, "▲") != 1 {
		t.Errorf("one now marker: %q", marks)
	}
	if !strings.ContainsAny(marks, "└^") {
		t.Errorf("the selected event's span isn't marked: %q", marks)
	}
	// Moving the selection moves the bracket.
	before := marks
	m = keys(t, m, "j")
	if after := ansi.Strip(m.todayStrip(m.todayContentW())[2]); after == before {
		t.Errorf("the marker row didn't follow the selection: %q", after)
	}
}

func TestFocusPlusModesFollowTheFocus(t *testing.T) {
	m := plusModel(t, 110, 40)
	day := m.focusPlusLayout()
	if day.history || day.todayCompact || day.tasksH > plusTasksMaxH || day.tasksH == 0 {
		t.Errorf("day mode: %+v", day)
	}
	m = keys(t, m, "tab") // the heatmap
	hist := m.focusPlusLayout()
	if m.focus != focusHeat || !hist.history || !hist.todayCompact || hist.tasksH <= day.tasksH {
		t.Errorf("history mode (focus %v): %+v", m.focus, hist)
	}
	m = keys(t, m, "tab") // the completed tasks
	if m.focus != focusTasks || !m.focusPlusLayout().history {
		t.Errorf("tab from the heatmap: focus %v", m.focus)
	}
	m = keys(t, m, "esc", "5")
	if m.focus != focusMain || m.focusPlusLayout().history {
		t.Errorf("5 again: focus %v, want Today in day mode", m.focus)
	}
}

func TestFocusPlusWriteIsNarrower(t *testing.T) {
	m := keys(t, plusModel(t, 110, 40), "W")
	gw, _ := m.globalRect()
	if gw < plusWriteMinW || gw > plusWriteMaxW || m.mainWidth() < plusMainMinW {
		t.Errorf("Write %d, main %d at 110 columns", gw, m.mainWidth())
	}
	m = keys(t, plusModel(t, 80, 30), "W")
	if gw, _ := m.globalRect(); gw != stripW {
		t.Errorf("at 80 columns Write should stay a strip in Focus+, got %d", gw)
	}
}

func TestFocusPlusWriteWidensWhenTheDayHasRoom(t *testing.T) {
	prev := 0
	for _, width := range []int{110, 130, 160, 200, 260} {
		m := keys(t, plusModel(t, width, 40), "W")
		gw, _ := m.globalRect()
		if gw < prev {
			t.Errorf("at %d columns Write narrowed to %d from %d", width, gw, prev)
		}
		if gw > plusWriteMaxW && m.mainWidth() < plusMainRoomyW {
			t.Errorf("at %d columns Write is %d and leaves the day %d", width, gw, m.mainWidth())
		}
		if gw > plusWriteWideW {
			t.Errorf("at %d columns Write is %d, over %d", width, gw, plusWriteWideW)
		}
		prev = gw
	}
	if m := keys(t, plusModel(t, 160, 40), "W"); func() int { w, _ := m.globalRect(); return w }() <= plusWriteMaxW {
		t.Error("at 160 columns Write should be wider than its usual most")
	}
	if m := keys(t, plusModel(t, 260, 40), "W"); func() int { w, _ := m.globalRect(); return w }() != plusWriteWideW {
		t.Error("at 260 columns Write should be at its widest")
	}
}

func TestFocusPlusFitsEveryWindow(t *testing.T) {
	for _, size := range [][2]int{{160, 50}, {110, 40}, {80, 30}, {60, 20}, {56, 16}, {40, 12}, {30, 8}} {
		for _, k := range []string{"", "tab", "tab tab"} {
			m := plusModel(t, size[0], size[1])
			if k != "" {
				m = keys(t, m, strings.Fields(k)...)
			}
			lines := strings.Split(screen(m), "\n")
			if len(lines) != size[1] {
				t.Errorf("%v %q: %d lines", size, k, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%v %q: line %d is %d wide: %q", size, k, i, w, l)
				}
			}
		}
	}
}
