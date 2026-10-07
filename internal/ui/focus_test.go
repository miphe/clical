package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// seedArchive writes archive records the way `clical write archive` does.
func seedArchive(t *testing.T, src storeSource, recs []write.Archived) {
	t.Helper()
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(src.st.Dir(), "archive.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// done is a task completed on a day (the loc zone), archived a day later.
func done(id, text string, y int, mo time.Month, d, hour int) write.Archived {
	at := time.Date(y, mo, d, hour, 0, 0, 0, loc)
	return write.Archived{ID: id, Text: text, Completed: at, Archived: at.AddDate(0, 0, 1)}
}

// busyArchive is five months of varied days before testNow (Tue 29 Sep 2026).
func busyArchive() []write.Archived {
	var recs []write.Archived
	today := time.Date(2026, 9, 29, 0, 0, 0, 0, loc)
	for i := 1; i < 150; i++ {
		day := today.AddDate(0, 0, -i)
		n := int(uint32(i)*2654435761>>28) % 5 // a fixed scatter of 0–4 a day
		if i%23 == 0 {
			n = 7
		}
		for k := 0; k < n; k++ {
			recs = append(recs, done("t", "task", day.Year(), day.Month(), day.Day(), 8+k))
		}
	}
	return recs
}

// heatModel is the focus view over a busy archive.
func heatModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, src := writeModel(t, width, height)
	seedArchive(t, src, busyArchive())
	return keys(t, m, "4")
}

func TestFocusViewGolden(t *testing.T) {
	golden(t, "focus_heat", screen(heatModel(t, 130, 24)))
	golden(t, "focus_heat_narrow", screen(heatModel(t, 80, 22)))
}

func TestFocusViewWithNoArchive(t *testing.T) {
	m, _ := writeModel(t, 100, 24)
	m = keys(t, m, "4")
	s := screen(m)
	if !strings.Contains(s, "[4] Focus") || !strings.Contains(s, "No archived tasks yet") {
		t.Errorf("empty archive:\n%s", s)
	}
	// Empty days can be walked through: a week back and a day up.
	m = keys(t, m, "h", "k")
	if want := m.today().AddDays(-8); m.heat.cursor != want {
		t.Errorf("cursor = %v, want %v", m.heat.cursor, want)
	}
}

func TestHeatCountsTasksOnTheirCompletionDay(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{
		done("a", "one", 2026, 9, 20, 9),
		done("b", "two", 2026, 9, 20, 22),
		done("c", "three", 2026, 9, 20, 23), // archived on the 21st, still the 20th
		done("d", "later", 2026, 9, 22, 9),
	})
	m = keys(t, m, "4", "h", "k", "k") // 29th, a week back, two days back: Sun 20 Sep
	if m.heat.cursor != domain.NewCivilDate(2026, 9, 20) {
		t.Fatalf("cursor = %v", m.heat.cursor)
	}
	s := screen(m)
	// One label for the day, then its three tasks.
	if strings.Count(s, "Sun 20 Sep") != 1 || !strings.Contains(s, "09:00  one") || !strings.Contains(s, "22:00  two") || !strings.Contains(s, "23:00  three") {
		t.Errorf("the 20th:\n%s", s)
	}
	m = keys(t, m, "j") // Mon 21: archive day of two of them, no completions
	if s := screen(m); !strings.Contains(s, "No tasks completed on Mon 21 Sep 2026.") || strings.Contains(s, "three") {
		t.Errorf("the 21st:\n%s", s)
	}
}

func TestHeatSelectionKeys(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{
		done("z", "early", 2026, 9, 1, 9),
		done("a", "one", 2026, 9, 14, 9),
		done("b", "two", 2026, 9, 16, 9),
		done("c", "three", 2026, 9, 16, 10),
	})
	today := m.today()
	m = keys(t, m, "4")
	if m.heat.cursor != today || m.heat.anchor != today {
		t.Fatalf("starts on %v / %v", m.heat.cursor, m.heat.anchor)
	}

	// Arrow keys and letters do the same.
	m = keys(t, m, "left", "up")
	m = keys(t, m, "h", "k")
	if want := today.AddDays(-14 - 2); m.heat.cursor != want {
		t.Errorf("cursor = %v, want %v", m.heat.cursor, want)
	}

	// Shift extends from where the selection began, with arrows or capitals.
	m = keys(t, m, "shift+up", "K")
	lo, hi := m.heat.span(m.weekStart)
	if hi != today.AddDays(-16) || lo != today.AddDays(-18) {
		t.Errorf("extended to %v–%v", lo, hi)
	}
	m = keys(t, m, "shift+down", "J", "J")
	if lo, hi = m.heat.span(m.weekStart); lo != today.AddDays(-16) || hi != today.AddDays(-15) {
		t.Errorf("back down: %v–%v", lo, hi)
	}
	// A plain move goes back to one day; esc does too.
	m = keys(t, m, "shift+up", "esc")
	if lo, hi = m.heat.span(m.weekStart); lo != hi {
		t.Errorf("esc kept %v–%v", lo, hi)
	}

	// Week 38 is 14–20 Sep: three tasks on two days.
	m = keys(t, m, "t", "h", "h", "space")
	s := screen(m)
	if !strings.Contains(s, "Week 38 · 14–20 Sep 2026") || !strings.Contains(s, "3 tasks on 2 days") {
		t.Errorf("week:\n%s", s)
	}
	m = keys(t, m, "space")
	if s = screen(m); !strings.Contains(s, "September 2026") || !strings.Contains(s, "4 tasks on 3 days") {
		t.Errorf("month:\n%s", s)
	}
	m = keys(t, m, "space", "t")
	if m.heat.unit != heatDay || m.heat.cursor != today {
		t.Errorf("unit %v cursor %v", m.heat.unit, m.heat.cursor)
	}
}

func TestHeatKeysDoNotMoveTheAgenda(t *testing.T) {
	m := heatModel(t, 100, 24)
	sel, from := m.sel, m.from
	m = keys(t, m, "enter", "g", "G", "[", "]", "pgup")
	if m.sel != sel || m.from != from || m.detail != nil {
		t.Errorf("the agenda moved under the heatmap: sel %q→%q, from %v→%v, detail %v", sel, m.sel, from, m.from, m.detail)
	}
}

func TestFocusViewCarriesTheDayBothWays(t *testing.T) {
	m := heatModel(t, 130, 24)
	m = keys(t, m, "h", "h")
	day := m.heat.cursor
	m = keys(t, m, "2")
	if got, _ := m.viewDay(); got != day {
		t.Errorf("the week view is on %v, want %v", got, day)
	}
	m = keys(t, m, "4")
	if m.heat.cursor != day {
		t.Errorf("back in the focus view on %v, want %v", m.heat.cursor, day)
	}
}

func TestFocusViewNeedsWrite(t *testing.T) {
	m, _ := writeModel(t, 100, 24)
	m.writeOn = false
	m = keys(t, m, "4")
	if m.view == viewHeat || strings.Contains(screen(m), "[4] Focus") {
		t.Errorf("the focus view exists without Write:\n%s", screen(m))
	}
}

func TestHeatReloadsWhenTheArchiveChanges(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{done("a", "one", 2026, 9, 29, 9)})
	m = keys(t, m, "4")
	if s := screen(m); !strings.Contains(s, "09:00  one") || strings.Contains(s, "two") {
		t.Fatalf("one task:\n%s", s)
	}
	seedArchive(t, src, []write.Archived{done("a", "one", 2026, 9, 29, 9), done("b", "two", 2026, 9, 29, 10)})
	m, cmd := send(t, m, tickMsg(m.now.Add(30*time.Second)))
	m, _ = run(t, m, cmd)
	if s := screen(m); !strings.Contains(s, "10:00  two") {
		t.Errorf("after the tick:\n%s", s)
	}
}

func TestHeatmapIsFlatOneHueInThreeStrengths(t *testing.T) {
	m := heatModel(t, 130, 24)
	heat := strings.Join(m.renderHeat(80, 12), "\n")
	plain := ansi.Strip(heat)
	// Every day with tasks is the same square; the rest, and the filler, are
	// dots; a selected day has a dot in its square.
	if strings.ContainsAny(plain, "░▒▓▪█") {
		t.Error("the heatmap has shade glyphs or squares of other sizes")
	}
	for _, r := range plain {
		if !strings.ContainsRune("SMTWF ·■▣\n", r) {
			t.Errorf("unexpected glyph %q in the heatmap", r)
		}
	}
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.Env([]string{"TERM=xterm-256color"})}
	if _, err := w.WriteString(heat); err != nil {
		t.Fatal(err)
	}
	// One hue, the accent (blue, 34), faint and plain; the busiest days are the
	// terminal's own text color in bold, with no color code at all.
	var blue, faint, bold bool
	for _, params := range sgrParams(buf.String()) {
		for _, p := range params {
			switch {
			case p == 34:
				blue = true
			case p == 2:
				faint = true
			case p == 1:
				bold = true
			case (p >= 30 && p <= 37) || (p >= 90 && p <= 97):
				t.Errorf("the heatmap uses color %d: %v", p, params)
			}
		}
	}
	if !blue || !faint || !bold {
		t.Errorf("blue %v, faint %v, bold %v: want all three", blue, faint, bold)
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.renderFocus(130, 24, nil), "\n")), "none  ■ 1–3  ■ 4–6  ■ 7+") {
		t.Error("no legend")
	}
}

func TestMonthBlocksAreSquareAndSeparatedByOneColumn(t *testing.T) {
	m := heatModel(t, 130, 24)
	lines := m.renderHeat(m.mainWidth()-2, heatRows)
	for _, l := range lines {
		body := strings.TrimRight(ansi.Strip(l), " ")
		// A blank column is just between months, and it is narrow: a cell's
		// own trailing space and the gap, never three blanks in a row.
		if strings.Contains(body, "   ") {
			t.Errorf("a wide gap inside the grid: %q", body)
		}
	}
	// Every row has a block's dots where its month has no day, so the blocks are
	// square: the same number of cells in every row of a month's block.
	cols := heatColumns(domain.NewCivilDate(2026, 8, 1), domain.NewCivilDate(2026, 9, 29), 1000)
	var aug []heatCol
	for _, c := range cols {
		if c.start.Month == 8 {
			aug = append(aug, c)
		}
	}
	filled := 0
	for _, c := range aug {
		for row := 0; row < heatRows; row++ {
			if _, ok := c.day(row); !ok {
				filled++
			}
		}
	}
	if want := len(aug)*heatRows - 31; filled != want {
		t.Errorf("August's block has %d filler cells, want %d", filled, want)
	}
	// The filler can't be reached: walking through every day never lands on it.
	d := domain.NewCivilDate(2026, 9, 29)
	seen := map[domain.CivilDate]bool{}
	h := m.heat
	for i := 0; i < 120; i++ {
		h = h.move(heatUp, false, d)
		seen[h.cursor] = true
	}
	if len(seen) != 120 {
		t.Errorf("moving up 120 times visited %d distinct days", len(seen))
	}
}

func TestDayLabelsHaveTheirSquaresColor(t *testing.T) {
	m, src := writeModel(t, 140, 40)
	var recs []write.Archived
	for _, c := range []struct{ day, n int }{{20, 2}, {21, 5}, {22, 9}} {
		for i := 0; i < c.n; i++ {
			recs = append(recs, done("x", "task", 2026, 9, c.day, 8+i%9))
		}
	}
	seedArchive(t, src, recs)
	m = keys(t, m, "4", "h", "k", "k") // Sun 20 Sep: 1–3 tasks
	label := func(m Model, text string) string {
		var buf bytes.Buffer
		w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.Env([]string{"TERM=xterm-256color"})}
		if _, err := w.WriteString(strings.Join(m.renderTasks(m.focusLayout().tasksW, m.focusLayout().tasksH, nil), "\n")); err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(buf.String(), "\n") {
			if strings.Contains(ansi.Strip(l), text) {
				return l
			}
		}
		t.Fatalf("no %q in:\n%s", text, buf.String())
		return ""
	}
	// The name is bold in the text color. Before it comes a square in exactly the
	// style of the day's square in the heatmap: faint blue for 1–3, blue for
	// 4–6, bold in the terminal's own color for 7+.
	for _, c := range []struct {
		day, mark string
		keys      []string
	}{
		{"Sun 20 Sep 2026", "\x1b[2;34m■", nil},
		{"Mon 21 Sep 2026", "\x1b[34m■", []string{"j"}},
		{"Tue 22 Sep 2026", "\x1b[1m■", []string{"j"}},
	} {
		m = keys(t, m, c.keys...)
		line := label(m, c.day)
		if !strings.Contains(line, c.mark+"\x1b[m \x1b[1m"+c.day) {
			t.Errorf("%s: want the square %q, then the bold name: %q", c.day, c.mark, line)
		}
		// The same style as the heatmap's square for that day.
		cell := heatStyles[heatLevel(len(m.heat.days[m.heat.cursor]))].Render(heatDone)
		if !strings.Contains(line, cell) {
			t.Errorf("%s: the square isn't styled like the heatmap's: %q vs %q", c.day, line, cell)
		}
	}
}

func TestTasksPanelListsTheSelectionOldestFirst(t *testing.T) {
	m, src := writeModel(t, 130, 30)
	ev := write.Ref{Calendar: "cal", Key: "weekly", Title: "Weekly sync"}
	seedArchive(t, src, []write.Archived{
		{ID: "c", Text: "write the\nreport", Completed: time.Date(2026, 9, 22, 16, 5, 0, 0, loc), Origin: ev},
		{ID: "a", Text: "first", Completed: time.Date(2026, 9, 21, 9, 30, 0, 0, loc)},
		{ID: "b", Text: "second\x1b[31m", Completed: time.Date(2026, 9, 21, 11, 0, 0, 0, loc)},
		{ID: "z", Text: "outside", Completed: time.Date(2026, 9, 14, 11, 0, 0, 0, loc)},
	})
	m = keys(t, m, "4", "h", "space") // the week of 21 Sep
	s := screen(m)
	for _, want := range []string{"Week 39 · 21–27 Sep 2026", "3 tasks on 2 days", "Mon 21 Sep", "09:30  first", "11:00  second[31m", "Tue 22 Sep", "16:05  write the report  · from Weekly sync"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "outside") || strings.Contains(s, "\x1b") {
		t.Errorf("shows what isn't selected, or an escape code:\n%s", s)
	}
	if strings.Index(s, "first") > strings.Index(s, "second") || strings.Index(s, "second") > strings.Index(s, "write the") {
		t.Errorf("not oldest first:\n%s", s)
	}
	// The heatmap panel sits under the tasks, along the bottom.
	if strings.Index(s, "Heatmap") < strings.Index(s, "write the") {
		t.Errorf("the heatmap should be below the tasks:\n%s", s)
	}
}

func TestTasksPanelScrolls(t *testing.T) {
	m, src := writeModel(t, 130, 24)
	var recs []write.Archived
	for i := 0; i < 30; i++ {
		recs = append(recs, write.Archived{ID: "x", Text: fmt.Sprintf("task number %02d", i), Completed: time.Date(2026, 9, 28, 8, i, 0, 0, loc)})
	}
	seedArchive(t, src, recs)
	m = keys(t, m, "4", "k")                                                         // Mon 28 Sep, the day before today
	if s := screen(m); !strings.Contains(s, "1–") || !strings.Contains(s, "of 31") { // the day's label and 30 tasks
		t.Fatalf("position label:\n%s", s)
	}
	// PgDn from the heatmap scrolls the list; the cursor stays.
	cursor := m.heat.cursor
	m = keys(t, m, "pgdown")
	if m.heat.cursor != cursor || m.heat.scroll == 0 {
		t.Errorf("pgdown: cursor %v→%v, scroll %d", cursor, m.heat.cursor, m.heat.scroll)
	}
	// Tab moves into the list; j/k scroll, G and g jump, esc comes back.
	m = keys(t, m, "tab")
	if m.focus != focusTasks {
		t.Fatalf("tab went to focus %v", m.focus)
	}
	m = keys(t, m, "G")
	if s := screen(m); !strings.Contains(s, "task number 29") {
		t.Errorf("the end of the list isn't shown:\n%s", s)
	}
	m = keys(t, m, "g")
	if m.heat.scroll != 0 {
		t.Errorf("g left scroll at %d", m.heat.scroll)
	}
	m = keys(t, m, "j", "j")
	if m.heat.scroll != 2 {
		t.Errorf("scroll = %d, want 2", m.heat.scroll)
	}
	m = keys(t, m, "esc")
	if m.focus != focusHeat {
		t.Errorf("esc went to focus %v", m.focus)
	}
	// Moving the selection starts the list from the top.
	m = keys(t, m, "pgdown", "j")
	if m.heat.scroll != 0 {
		t.Errorf("a new selection kept scroll %d", m.heat.scroll)
	}
}

func TestFocusViewDropsTheTasksPanelWhenShort(t *testing.T) {
	m := heatModel(t, 130, 14) // the body leaves under 5 rows above the heatmap
	lay := m.focusLayout()
	if lay.tasksH != 0 || lay.heatH != m.bodyHeight() {
		t.Errorf("layout = %+v for a body of %d", lay, m.bodyHeight())
	}
	if s := screen(m); !strings.Contains(s, "[4] Focus") {
		t.Errorf("the tabs have to stay with the top panel:\n%s", s)
	}
}

func TestHeatmapRowsAreSundayToSaturday(t *testing.T) {
	m := heatModel(t, 130, 24)
	var letters []string
	for _, l := range strings.Split(screen(m), "\n") {
		if i := strings.Index(l, "││"); i >= 0 && len(l) > i+5 && strings.ContainsAny(l[i+len("││"):i+len("││")+1], "SMTWF") && l[i+len("││")+1] == ' ' {
			letters = append(letters, l[i+len("││"):i+len("││")+1])
		}
	}
	if got := strings.Join(letters, ""); !strings.HasSuffix(got, "SMTWTFS") {
		t.Errorf("row labels = %q, want S M T W T F S top to bottom", got)
	}
	// A Monday-first setting changes what a week selection is, not the rows.
	m.weekStart = time.Monday
	m = keys(t, m, "t", "space") // the week of Mon 28 Sep: Mon–Sat in one column, Sun 4 Oct is in the future
	lo, hi := m.heat.span(m.weekStart)
	if lo != domain.NewCivilDate(2026, 9, 28) || hi != domain.NewCivilDate(2026, 10, 4) {
		t.Errorf("span = %v–%v", lo, hi)
	}
}

func TestSelectedDaysKeepTheirColor(t *testing.T) {
	m := heatModel(t, 130, 24)
	m = keys(t, m, "h", "k", "K", "K") // a week back, then three days selected
	heat := strings.Join(m.renderHeat(m.mainWidth()-2, heatRows), "\n")
	lo, hi := m.heat.span(m.weekStart)
	if want := 3; hi != lo.AddDays(want-1) || strings.Count(ansi.Strip(heat), heatSel) != want {
		t.Errorf("selected cells = %d, want %d (%v–%v)", strings.Count(ansi.Strip(heat), heatSel), want, lo, hi)
	}
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.Env([]string{"TERM=xterm-256color"})}
	if _, err := w.WriteString(heat); err != nil {
		t.Fatal(err)
	}
	for _, params := range sgrParams(buf.String()) {
		for _, p := range params {
			if p == 7 || (p >= 40 && p <= 47) {
				t.Errorf("a selected day is inverted or on a background: %v", params)
			}
		}
	}
	// No underline either: the dot in the square is the mark, in bold.
	for _, params := range sgrParams(buf.String()) {
		for _, p := range params {
			if p == 4 {
				t.Errorf("a selected day is underlined: %v", params)
			}
		}
	}
}

func TestSingleDayHasNoHeader(t *testing.T) {
	m, src := writeModel(t, 130, 24)
	seedArchive(t, src, []write.Archived{done("a", "one", 2026, 9, 28, 9)})
	m = keys(t, m, "4", "k") // Mon 28 Sep
	s := screen(m)
	if strings.Count(s, "28 Sep") != 1 {
		t.Errorf("the day is named once:\n%s", s)
	}
	m = keys(t, m, "J") // 28–29 Sep
	if s = screen(m); !strings.Contains(s, "28–29 Sep 2026") || !strings.Contains(s, "Mon 28 Sep 2026") {
		t.Errorf("a range has its header and the day's label:\n%s", s)
	}
}

// evSource serves the test events for every window, so the agenda has rows
// after the focus view reloads it.
type evSource struct{ nopSource }

func (evSource) Events(context.Context, time.Time, time.Time) ([]domain.Event, error) {
	return testEvents(), nil
}

// focusModel is the focus view with events behind its agenda and a busy
// archive behind its heatmap.
func focusModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(evSource{}, loc, testNow)
	m.tick = 0
	m, _ = send(t, m,
		tea.WindowSizeMsg{Width: width, Height: height},
		calendarsLoadedMsg{rows: testCalendars},
		eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()},
		syncDoneMsg{at: at(29, 14, 29)},
		flashEndMsg{until: at(29, 14, 29).Add(flashDuration)},
	)
	src := newStoreSource(t)
	m = m.WithWrite(src)
	m.panelFlash = true
	m, _ = run(t, m, m.loadWrite(writeGlobal))
	seedArchive(t, src, busyArchive())
	return keys(t, m, "4")
}

func TestFocusViewLayoutsGolden(t *testing.T) {
	golden(t, "focus_wide", screen(focusModel(t, 140, 32)))
	golden(t, "focus_collapsed_list", screen(focusModel(t, 100, 30)))
	golden(t, "focus_stacked", screen(focusModel(t, 60, 36)))
	golden(t, "focus_agenda_and_heat", screen(focusModel(t, 100, 20)))
}

func TestFocusAgendaShowsWhatsNext(t *testing.T) {
	m := focusModel(t, 140, 32)
	s := screen(m)
	for _, want := range []string{"▸ Tue 29 Sep · today", "15:00   Dentist", "23:00   Night train", "── now 14:30"} {
		if !strings.Contains(s, want) {
			t.Errorf("the agenda lacks %q:\n%s", want, s)
		}
	}
	// The slim panel is as wide as the calendar list.
	lay := m.focusLayout()
	if lay.agendaW != sidebarInner+2 || lay.stacked {
		t.Errorf("layout = %+v", lay)
	}
}

func TestFocusAgendaMovesAndOpensEvents(t *testing.T) {
	m := focusModel(t, 140, 32)
	if m.focus != focusHeat {
		t.Fatalf("4 focuses %v, want the heatmap", m.focus)
	}
	// tab: tasks, then (global strip is closed) the list, then the agenda.
	m = keys(t, m, "tab", "tab", "tab")
	if m.focus != focusMain {
		t.Fatalf("focus = %v, want the agenda", m.focus)
	}
	before := m.sel
	m = keys(t, m, "j")
	if m.sel == before {
		t.Error("j didn't move in the agenda")
	}
	cursor := m.heat.cursor
	m = keys(t, m, "enter")
	if m.detail == nil {
		t.Fatal("enter didn't open the event")
	}
	m = keys(t, m, "esc")
	if m.detail != nil || m.view != viewHeat || m.focus != focusMain || m.heat.cursor != cursor {
		t.Errorf("back from the event: detail %v, view %v, focus %v", m.detail != nil, m.view, m.focus)
	}
	// t is the agenda's today here, and the heatmap's in the heatmap.
	m = keys(t, m, "t")
	if m.from != m.today() {
		t.Errorf("agenda starts %v, want today", m.from)
	}
	m = keys(t, m, "tab", "h", "tab", "tab", "tab", "tab", "t") // heatmap, a week back, round to the heatmap again, today
	if m.focus != focusHeat || m.heat.cursor != m.today() {
		t.Errorf("focus %v cursor %v", m.focus, m.heat.cursor)
	}
}

func TestFocusFilterAppliesToTheAgenda(t *testing.T) {
	m := focusModel(t, 140, 32)
	m = keys(t, m, "/")
	m = typed(t, m, "dentist")
	m = keys(t, m, "enter")
	s := screen(m)
	if !strings.Contains(s, "Dentist") || strings.Contains(s, "Night train") {
		t.Errorf("filter:\n%s", s)
	}
}

func TestFocusAgendaPulses(t *testing.T) {
	row := func(m Model) string {
		for _, l := range strings.Split(renderWithProfile(t, m, []string{"TERM=xterm-256color"}), "\n") {
			if plain := ansi.Strip(l); strings.Contains(plain, "15:00") && strings.Contains(plain, "Dentist") {
				return l
			}
		}
		t.Fatalf("no dentist row:\n%s", screen(m))
		return ""
	}
	m := focusModel(t, 140, 32)
	plain := row(m)
	m.pulse.on = true
	m.pulse.lead, m.pulse.grace = 30*time.Minute, 0
	m, _ = send(t, m, upcomingMsg{events: testEvents()})
	if !dentist(m) {
		t.Fatal("the dentist isn't pulsing")
	}
	if row(m) == plain {
		t.Error("the agenda row doesn't pulse in the focus view")
	}
	if !strings.Contains(screen(m), "Dentist in 30:00") {
		t.Error("the status line doesn't count down")
	}
}

func TestFocusCollapsesTheListThenStacks(t *testing.T) {
	// 140 columns: room for everything.
	m := focusModel(t, 140, 32)
	if m.sidebarCollapsed || m.focusLayout().stacked {
		t.Errorf("wide: collapsed %v, layout %+v", m.sidebarCollapsed, m.focusLayout())
	}
	// 100 columns: the list gives way first, the panels stay side by side.
	m = focusModel(t, 100, 30)
	if !m.sidebarCollapsed || m.focusLayout().stacked {
		t.Errorf("100: collapsed %v, layout %+v", m.sidebarCollapsed, m.focusLayout())
	}
	// And comes back when leaving, or when the window grows.
	back := keys(t, m, "1")
	if back.sidebarCollapsed {
		t.Error("the list stayed collapsed in another view")
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	if m.sidebarCollapsed {
		t.Error("the list stayed collapsed after the window grew")
	}
	// 60 columns: still too narrow, so the panels stack.
	m = focusModel(t, 60, 36)
	lay := m.focusLayout()
	if !m.sidebarCollapsed || !lay.stacked || lay.agendaH == 0 || lay.tasksH == 0 {
		t.Errorf("60: collapsed %v, layout %+v", m.sidebarCollapsed, lay)
	}
	// A list the user collapsed themselves stays that way.
	m = focusModel(t, 140, 32)
	m = keys(t, m, "0", "c", "4", "1")
	if !m.sidebarCollapsed {
		t.Error("the user's collapse was undone")
	}
}

func TestFocusDropsPanelsWhenShort(t *testing.T) {
	m := focusModel(t, 140, 32)
	m = keys(t, m, "tab", "tab", "tab") // the agenda
	if m.focus != focusMain {
		t.Fatalf("focus = %v", m.focus)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 140, Height: 15}) // under the heatmap and 5 rows
	if lay := m.focusLayout(); lay.agendaH != 0 || lay.tasksH != 0 || lay.heatH != m.bodyHeight() {
		t.Errorf("layout = %+v", lay)
	}
	if m.focus != focusHeat {
		t.Errorf("focus stayed on a dropped panel: %v", m.focus)
	}
	if s := screen(m); !strings.Contains(s, "[4] Focus") {
		t.Errorf("the tabs have to stay:\n%s", s)
	}
	// tab skips what isn't there.
	m = keys(t, m, "tab", "tab")
	if m.focus != focusHeat {
		t.Errorf("tab went to %v", m.focus)
	}
}
