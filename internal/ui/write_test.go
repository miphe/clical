package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/write"
)

// storeSource is the notes files, for tests that want the real thing.
type storeSource struct{ st *write.Store }

func (s storeSource) WriteList(scope write.Ref) (write.List, error) { return s.st.Get(scope) }
func (s storeSource) WriteUpdate(scope write.Ref, hdr *write.Header, fn func(*write.List) error) error {
	return s.st.Update(scope, hdr, fn)
}
func (s storeSource) WriteMove(id string, to write.Ref, hdr *write.Header) error {
	return s.st.Move(id, to, hdr)
}
func (s storeSource) WriteArchived() ([]write.Archived, error) { return s.st.Archived() }
func (s storeSource) WriteArchive(id string, now time.Time) error {
	_, err := s.st.Archive(id, now)
	return err
}

func (s storeSource) WriteArchiveNote(scope write.Ref, at int, text string, now time.Time) error {
	_, err := s.st.ArchiveNote(scope, at, text, now)
	return err
}

func (s storeSource) WriteMoveNote(from write.Ref, at int, text string, to write.Ref, hdr *write.Header) error {
	return s.st.MoveNote(from, at, text, to, hdr)
}

func newStoreSource(t *testing.T) storeSource {
	t.Helper()
	st, err := write.Open(filepath.Join(t.TempDir(), "write"))
	if err != nil {
		t.Fatal(err)
	}
	return storeSource{st: st}
}

// writeModel is a loaded model with notes behind it, wide enough for the
// event view to put its panels side by side.
func writeModel(t *testing.T, width, height int) (Model, storeSource) {
	t.Helper()
	src := newStoreSource(t)
	m := loaded(t, width, height).WithWrite(src)
	m.panelFlash = true
	m, _ = run(t, m, m.loadWrite(writeGlobal))
	return m, src
}

// keys presses each key and runs what it asks for.
func keys(t *testing.T, m Model, ks ...string) Model {
	t.Helper()
	defer func(d time.Duration) { quickTimeout = d }(quickTimeout)
	quickTimeout = 100 * time.Millisecond // file access is quick, even with -race; blink timers aren't waited for
	for _, k := range ks {
		var cmd tea.Cmd
		m, cmd = send(t, m, press(k))
		m, _ = run(t, m, cmd)
	}
	return m
}

// typed types text a character at a time.
func typed(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		k := string(r)
		if r == ' ' {
			k = "space"
		}
		m = keys(t, m, k)
	}
	return m
}

// openEvent opens the selected event (the dentist) and returns its Write
// scope.
func openEvent(t *testing.T, m Model) (Model, write.Ref) {
	t.Helper()
	m = keys(t, m, "enter")
	if m.detail == nil {
		t.Fatal("the event view didn't open")
	}
	return m, m.eventWrite.scope
}

func listOf(t *testing.T, src storeSource, scope write.Ref) []write.Item {
	t.Helper()
	l, err := src.st.Get(scope)
	if err != nil {
		t.Fatal(err)
	}
	var out []write.Item
	for _, it := range l.Items {
		if it.Todo || it.Text != "" {
			out = append(out, it)
		}
	}
	return out
}

func TestEventViewHasDetailsAndWritePanels(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	s := screen(m)
	var top string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "╭─ Event") {
			top = l
		}
	}
	if !strings.Contains(top, "╭─ Write") {
		t.Errorf("at 130 columns the panels aren't side by side:\n%s", s)
	}
	if !strings.Contains(s, "Description") || !strings.Contains(s, "Bring the insurance card") {
		t.Errorf("the notes aren't labelled Description:\n%s", s)
	}
	// Narrower, the Write panel goes under the details.
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	s = screen(m)
	if strings.Index(s, "╭─ Write") < strings.Index(s, "╭─ Event") || strings.Count(s, "╭─ Write") != 1 {
		t.Errorf("stacked layout:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if ansi.StringWidth(l) > 80 {
			t.Errorf("a line is wider than the window: %q", l)
		}
	}
}

func TestLowerWOnlyFocusesTheEventsWritePanel(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	before := m
	m = keys(t, m, "w")
	if m.focus != before.focus || m.globalOpen != before.globalOpen {
		t.Fatalf("w outside the event view changed focus or the global panel: focus %v, open %v", m.focus, m.globalOpen)
	}
	m = keys(t, m, "W")
	if m.focus != focusGlobalWrite || !m.globalOpen {
		t.Fatal("W didn't open and focus the global panel")
	}
}

func TestAddTodosAndNotesWhileViewingAnEvent(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w")
	if m.focus != focusEventWrite {
		t.Fatal("w didn't focus the event's Write panel")
	}
	m = typed(t, m, "a")
	if m.eventWrite.mode != wInsert {
		t.Fatal("a didn't start typing")
	}
	// ?, q and S are text while typing.
	m = typed(t, m, "send the deck? q S")
	m = keys(t, m, "enter")
	m = typed(t, m, "book a room")
	m = keys(t, m, "enter", "esc")
	if m.eventWrite.mode != wNormal || m.focus != focusEventWrite {
		t.Errorf("esc: mode %v, focus %v", m.eventWrite.mode, m.focus)
	}
	m = typed(t, m, "A")
	m = typed(t, m, "agenda: roadmap")
	m = keys(t, m, "enter", "esc")

	items := listOf(t, src, scope)
	if len(items) != 3 || items[0].Text != "send the deck? q S" || !items[0].Todo || items[2].Todo || items[2].Text != "agenda: roadmap" {
		t.Fatalf("saved = %+v", items)
	}
	if !items[0].Origin.Same(scope) || items[0].Origin.Title != "Dentist" {
		t.Errorf("origin = %+v", items[0].Origin)
	}
	s := screen(m)
	for _, want := range []string{"[ ] send the deck", "[ ] book a room", "agenda: roadmap"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q isn't in the panel:\n%s", want, s)
		}
	}
}

func TestInsertModeShowsInTheTitleAndFooter(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "a")
	s := screen(m)
	if !strings.Contains(s, "INSERT task") || !strings.Contains(s, "enter add") {
		t.Errorf("no mode label:\n%s", s)
	}
}

func TestCheckArchiveNeedsCheckedFirst(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "file the report")
	m = keys(t, m, "enter", "esc")

	m = keys(t, m, "z")
	if !strings.Contains(screen(m), "Check it first") || len(listOf(t, src, scope)) != 1 {
		t.Errorf("archived an unchecked task:\n%s", screen(m))
	}
	m = keys(t, m, "space")
	if items := listOf(t, src, scope); !items[0].Done || items[0].Completed.IsZero() {
		t.Fatalf("not checked: %+v", items)
	}
	if !strings.Contains(screen(m), "[x] file the report") {
		t.Errorf("checked task isn't shown:\n%s", screen(m))
	}
	m = keys(t, m, "space")
	if items := listOf(t, src, scope); items[0].Done || !items[0].Completed.IsZero() {
		t.Errorf("not unchecked: %+v", items)
	}
	m = keys(t, m, "space", "z")
	if len(listOf(t, src, scope)) != 0 {
		t.Error("the archived task is still listed")
	}
	if arch, _ := src.st.Archived(); len(arch) != 1 || arch[0].Text != "file the report" || !arch[0].Origin.Same(scope) {
		t.Errorf("archive = %+v", arch)
	}
}

func TestNotesCantBeChecked(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "A")
	m = typed(t, m, "just a note")
	m = keys(t, m, "enter", "esc", "space")
	if !strings.Contains(screen(m), "Only a task can be checked") {
		t.Errorf("no hint:\n%s", screen(m))
	}
}

func TestANoteIsMarkedSoItIsNotTakenForAWrappedTask(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "a task long enough that it has to wrap onto a second line in the panel, for sure, yes")
	m = keys(t, m, "enter", "esc", "A")
	m = typed(t, m, "just a note")
	m = keys(t, m, "enter", "esc")
	var task, note, rest []string
	for _, l := range strings.Split(screen(m), "\n") {
		switch {
		case strings.Contains(l, "[ ] a task long"):
			task = append(task, l)
		case strings.Contains(l, "just a note"):
			note = append(note, l)
		case strings.Contains(l, "second line in the panel"), strings.Contains(l, "for sure, yes"):
			rest = append(rest, l)
		}
	}
	if len(note) == 0 || !strings.Contains(note[0], "¶ ") || strings.Contains(note[0], "[ ]") {
		t.Errorf("the note isn't marked: %q", note)
	}
	if len(task) == 0 || strings.Contains(task[0], "¶") {
		t.Errorf("the task is marked as a note: %q", task)
	}
	for _, l := range rest {
		if strings.Contains(l, "¶") {
			t.Errorf("a wrapped task line is marked as a note: %q", l)
		}
	}
	// The text starts in the same column, marker or box.
	if len(task) > 0 && len(note) > 0 {
		col := func(l, sub string) int { return len([]rune(l[:strings.Index(l, sub)])) }
		if a, b := col(task[0], "a task"), col(note[0], "just"); a != b {
			t.Errorf("text starts at column %d for the task and %d for the note", a, b)
		}
	}
}

func TestNotesMoveBetweenTheEventAndTheGlobalList(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "A")
	m = typed(t, m, "a note for later")
	m = keys(t, m, "enter", "esc")
	m = keys(t, m, "a")
	m = typed(t, m, "a task")
	m = keys(t, m, "enter", "esc")
	// The note is first; move it to the global list.
	m = keys(t, m, "g", "m")
	if ev := listOf(t, src, scope); len(ev) != 1 || ev[0].Text != "a task" {
		t.Fatalf("event list = %+v", ev)
	}
	gl := listOf(t, src, write.Ref{})
	if len(gl) != 1 || gl[0].Todo || gl[0].Text != "a note for later" {
		t.Fatalf("global list = %+v", gl)
	}
	// And from the global panel back into the open event.
	m = keys(t, m, "W", "g", "m")
	if ev := listOf(t, src, scope); len(ev) != 2 || ev[1].Todo || ev[1].Text != "a note for later" {
		t.Fatalf("event list after = %+v", ev)
	}
	if len(listOf(t, src, write.Ref{})) != 0 {
		t.Error("still in the global list")
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "one")
	m = keys(t, m, "enter", "esc", "d")
	if !strings.Contains(screen(m), "Delete? y/n") {
		t.Errorf("no question:\n%s", screen(m))
	}
	m = keys(t, m, "n")
	if len(listOf(t, src, scope)) != 1 {
		t.Fatal("deleted after n")
	}
	m = keys(t, m, "d", "y")
	if len(listOf(t, src, scope)) != 0 {
		t.Error("not deleted after y")
	}
}

func TestEditChangesTheText(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "draft")
	m = keys(t, m, "enter", "esc", "e")
	m = typed(t, m, "!")
	m = keys(t, m, "enter")
	if items := listOf(t, src, scope); len(items) != 1 || items[0].Text != "draft!" || m.eventWrite.mode != wNormal {
		t.Errorf("after edit: %+v, mode %v", items, m.eventWrite.mode)
	}
}

func TestEscReturnsToTheEventView(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "esc")
	if m.detail == nil || m.focus != focusMain {
		t.Errorf("detail open %v, focus %v", m.detail != nil, m.focus)
	}
	m = keys(t, m, "esc")
	if m.detail != nil {
		t.Error("esc didn't close the event view")
	}
}

func TestMoveToGlobalFlashesTheCollapsedPanel(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "follow up")
	m = keys(t, m, "enter", "esc")
	if m.globalOpen {
		t.Fatal("the global panel should start collapsed")
	}
	m = keys(t, m, "m")

	if len(listOf(t, src, scope)) != 0 {
		t.Error("still in the event's list")
	}
	g := listOf(t, src, write.Ref{})
	if len(g) != 1 || !g[0].Origin.Same(scope) || g[0].Last.IsZero() == false {
		t.Fatalf("global = %+v", g)
	}
	if !m.globalFlash || m.globalOpen {
		t.Errorf("flash %v, open %v; want a flash and still collapsed", m.globalFlash, m.globalOpen)
	}
	if !strings.Contains(screen(m), "1") {
		t.Error("the strip doesn't count the open task")
	}
	m, _ = send(t, m, writeFlashEndMsg{seq: m.flashSeq})
	if m.globalFlash {
		t.Error("the flash didn't end")
	}
}

func TestFlashCanBeSwitchedOff(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m.panelFlash = false
	src.st.Update(write.Ref{}, nil, func(l *write.List) error { _, err := l.AddTodo("x", "11111111"); return err })
	m, _ = send(t, m, tickMsg(testNow))
	m, _ = run(t, m, m.reloadWrite())
	if m.globalFlash {
		t.Error("flashed with the setting off")
	}
}

func TestExpandedPanelDoesNotFlash(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m = keys(t, m, "W")
	src.st.Update(write.Ref{}, nil, func(l *write.List) error { _, err := l.AddTodo("x", "11111111"); return err })
	m, _ = run(t, m, m.reloadWrite())
	if m.globalFlash || !strings.Contains(screen(m), "[ ] x") {
		t.Errorf("flash %v:\n%s", m.globalFlash, screen(m))
	}
}

func TestChangesOutsideTheAppAppearAndFlash(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	src.st.Update(write.Ref{}, nil, func(l *write.List) error { _, err := l.AddTodo("added by the CLI", "11111111"); return err })
	m, cmd := send(t, m, tickMsg(testNow))
	m, _ = run(t, m, cmd)
	if !m.globalFlash || len(m.globalWrite.list.Items) != 1 {
		t.Errorf("flash %v, items %+v", m.globalFlash, m.globalWrite.list.Items)
	}
}

func TestMoveFromGlobalIntoTheOpenEvent(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	src.st.Update(write.Ref{}, nil, func(l *write.List) error { _, err := l.AddTodo("water plants", "11111111"); return err })
	m, _ = run(t, m, m.reloadWrite())

	// With no event open there's nowhere to move it.
	m = keys(t, m, "W", "m")
	if !strings.Contains(screen(m), "Open an event first") {
		t.Errorf("no hint:\n%s", screen(m))
	}
	m = keys(t, m, "esc")

	m, scope := openEvent(t, m)
	m = keys(t, m, "W") // focuses it, it's already open
	if m.focus != focusGlobalWrite {
		t.Fatalf("focus = %v", m.focus)
	}
	m = keys(t, m, "m")
	ev := listOf(t, src, scope)
	if len(ev) != 1 || !ev[0].Origin.IsZero() || !ev[0].Last.Same(scope) {
		t.Fatalf("event list = %+v", ev)
	}
	if len(listOf(t, src, write.Ref{})) != 0 {
		t.Error("still in the global list")
	}
}

func TestWToggleCollapsesAndExpands(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m = keys(t, m, "W")
	if !m.globalOpen || m.focus != focusGlobalWrite || !strings.Contains(screen(m), "╭─ Write") {
		t.Fatalf("W didn't open and focus it:\n%s", screen(m))
	}
	m = keys(t, m, "W")
	if m.globalOpen || m.focus != focusMain {
		t.Error("W didn't collapse it")
	}
}

func TestEventViewKeepsRoomWithTheGlobalPanelOpen(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "W")
	s := screen(m)
	if strings.Count(s, "╭─ Write") != 2 {
		t.Errorf("want the event's and the global panel:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if ansi.StringWidth(l) > 130 {
			t.Errorf("line wider than the window: %q", l)
		}
	}
}

func TestEveryWriteLayoutFits(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {120, 40}, {200, 50}} {
		m, _ := writeModel(t, size[0], size[1])
		m, _ = openEvent(t, m)
		for _, k := range []string{"w", "esc", "W", "W"} {
			m = keys(t, m, k)
			lines := strings.Split(screen(m), "\n")
			if len(lines) != size[1] {
				t.Errorf("%v after %s: %d lines", size, k, len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Errorf("%v after %s: %q is wider than the window", size, k, l)
				}
			}
		}
	}
}

func TestKeyInTheEventWriteEndsTheEventsPulse(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m.pulse.on, m.pulse.lead, m.pulse.grace = true, 30*time.Minute, 0
	m, _ = send(t, m, upcomingMsg{events: testEvents()})
	m, _ = openEvent(t, m)
	if !dentist(m) {
		t.Fatal("setup: the event pulses while its view is open")
	}
	m = keys(t, m, "w")
	if dentist(m) {
		t.Error("interacting with the event's Write panel didn't stop its pulse")
	}
}

func TestWithoutNotesTheKeysAreHarmless(t *testing.T) {
	m := loaded(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "a", "m", "esc")
	if !strings.Contains(screen(m), "Event") {
		t.Errorf("screen:\n%s", screen(m))
	}
}

func TestGoldenEventWrite(t *testing.T) {
	m, _ := writeModel(t, 120, 30)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "send the deck")
	m = keys(t, m, "enter")
	m = typed(t, m, "book a room for the offsite so everyone fits comfortably around the table")
	m = keys(t, m, "enter", "esc", "A")
	m = typed(t, m, "Agenda: roadmap")
	m = keys(t, m, "enter", "esc", "k", "k", "space", "W")
	golden(t, "event_write", screen(m))
}

func TestWritePanelGrowsWithRoomUpToTwiceItsUsualWidth(t *testing.T) {
	widthAt := func(w int) (write, detail int) {
		m, _ := writeModel(t, w, 34)
		m, _ = openEvent(t, m)
		l := m.eventLayout()
		return l.writeW, l.detailW
	}
	w130, d130 := widthAt(130)
	w180, d180 := widthAt(180)
	w300, d300 := widthAt(300)
	if w130 > 60 || d130 < 70 {
		t.Errorf("at 130: write %d, details %d; the usual share is up to 60", w130, d130)
	}
	if w180 <= w130 || d180 < detailRoom {
		t.Errorf("at 180: write %d (was %d at 130), details %d; want it to grow once the details have their room", w180, w130, d180)
	}
	if w300 != 120 || d300 != 300-stripW-120 {
		t.Errorf("at 300: write %d, details %d; want write capped at twice 60", w300, d300)
	}
}

func TestSelectionIsAChevronAndDoneIsAChecked(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "one")
	m = keys(t, m, "enter")
	m = typed(t, m, "two")
	m = keys(t, m, "enter", "esc", "space")
	raw := m.View().Content
	var checked, open string
	for _, l := range strings.Split(raw, "\n") {
		switch {
		case strings.Contains(ansi.Strip(l), "[x] two"):
			checked = l
		case strings.Contains(ansi.Strip(l), "[ ] one"):
			open = l
		}
	}
	if checked == "" || open == "" {
		t.Fatalf("rows missing:\n%s", screen(m))
	}
	if strings.Contains(checked, "\x1b[9m") || strings.Contains(open, "\x1b[9m") {
		t.Error("a task is struck through")
	}
	if strings.Contains(checked, "\x1b[7m") || strings.Contains(open, "\x1b[7m") {
		t.Error("the selected row is highlighted as a whole")
	}
	if !strings.Contains(ansi.Strip(checked), "› [x] two") || !strings.Contains(ansi.Strip(open), "  [ ] one") {
		t.Errorf("no chevron on the selected row:\n%s\n%s", ansi.Strip(checked), ansi.Strip(open))
	}
	if !strings.Contains(checked, "\x1b[1;32m[x]") {
		t.Errorf("the checked box isn't bold green: %q", checked)
	}
}

func TestWFromTheEventViewThenWBackToTheEventPanel(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "W")
	if m.focus != focusGlobalWrite {
		t.Fatalf("W: focus = %v", m.focus)
	}
	m = keys(t, m, "w")
	if m.focus != focusEventWrite {
		t.Errorf("w from the global panel: focus = %v; want the event's panel", m.focus)
	}
	// And w with no event open leaves the global panel focused.
	m = keys(t, m, "esc", "esc", "W", "w")
	if m.focus != focusGlobalWrite {
		t.Errorf("w without an event: focus = %v", m.focus)
	}
}

func TestZArchivesEverythingChecked(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "Z")
	if !strings.Contains(screen(m), "Nothing checked to archive") {
		t.Errorf("no hint:\n%s", screen(m))
	}
	for _, text := range []string{"one", "two", "three"} {
		m = keys(t, m, "a")
		m = typed(t, m, text)
		m = keys(t, m, "enter", "esc")
	}
	// Check one and three.
	m = keys(t, m, "g", "space", "j", "j", "space")
	m = keys(t, m, "Z")
	left := listOf(t, src, scope)
	if len(left) != 1 || left[0].Text != "two" {
		t.Fatalf("left in the panel: %+v", left)
	}
	arch, _ := src.st.Archived()
	if len(arch) != 2 {
		t.Errorf("archive = %+v", arch)
	}
}

func TestTabCyclesThroughEveryPanel(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	// The main view: the list, the view, and the global panel once it's open.
	m = keys(t, m, "tab")
	if m.focus != focusSidebar {
		t.Fatalf("tab from the view: %v", m.focus)
	}
	m = keys(t, m, "tab")
	if m.focus != focusMain {
		t.Fatalf("tab from the list: %v", m.focus)
	}
	m = keys(t, m, "W", "esc") // opened, focus back on the view
	var seen []focus
	for i := 0; i < 3; i++ {
		m = keys(t, m, "tab")
		seen = append(seen, m.focus)
	}
	if seen[0] != focusGlobalWrite || seen[1] != focusSidebar || seen[2] != focusMain {
		t.Errorf("order with the global panel open: %v", seen)
	}
	m = keys(t, m, "shift+tab")
	if m.focus != focusSidebar {
		t.Errorf("shift+tab: %v", m.focus)
	}

	// The event view: details, the event's panel, the global panel.
	m = keys(t, m, "esc")
	m.focus = focusMain
	m, _ = openEvent(t, m)
	seen = nil
	for i := 0; i < 4; i++ {
		m = keys(t, m, "tab")
		seen = append(seen, m.focus)
	}
	want := []focus{focusEventWrite, focusGlobalWrite, focusMain, focusEventWrite}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("event view tab order = %v; want %v", seen, want)
			break
		}
	}
}

func TestTabSkipsAStrip(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	for i := 0; i < 3; i++ {
		m = keys(t, m, "tab")
		if m.focus == focusGlobalWrite {
			t.Error("tab went to the collapsed global panel")
		}
	}
}

func TestWriteCanBeSwitchedOff(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	src.st.Update(write.Ref{}, nil, func(l *write.List) error { _, err := l.AddTodo("x", "11111111"); return err })
	m, _ = run(t, m, m.reloadWrite())
	if !strings.Contains(screen(m), "W\n") && !strings.Contains(screen(m), "│W│") {
		t.Fatalf("setup: the strip is shown:\n%s", screen(m))
	}

	m.writeOn = false
	m, _ = openEvent(t, m)
	s := screen(m)
	if strings.Contains(s, "Write") || strings.Contains(s, "write") {
		t.Errorf("Write still shows:\n%s", s)
	}
	for _, k := range []string{"w", "W", "a"} {
		m = keys(t, m, k)
		if m.focusedPanel() != nil || m.globalOpen || m.eventWrite.mode != wNormal {
			t.Fatalf("%s did something with Write off", k)
		}
	}
	if m.eventLayout().detailW != 130 {
		t.Errorf("the details don't take the whole width: %+v", m.eventLayout())
	}
	m = keys(t, m, "esc")
	if strings.Contains(screen(m), "│W│") {
		t.Errorf("the strip shows with Write off:\n%s", screen(m))
	}
	if m.loadWrite(writeGlobal) != nil {
		t.Error("the notes are still read")
	}
}

func TestWriteSettingRowTurnsItOffAndOn(t *testing.T) {
	var saved []Prefs
	src := newStoreSource(t)
	m := loaded(t, 130, 34).WithWrite(src).WithSettings(Settings{
		Write: true, WeekStart: time.Monday, PulseLead: 5 * time.Minute, PulseGrace: time.Minute,
		Save: func(p Prefs) error { saved = append(saved, p); return nil },
	})
	m = keys(t, m, "W") // focus the global panel
	m = keys(t, m, "S")
	m = goTo(t, m, rowWrite)
	if !strings.Contains(screen(m), "[on]") {
		t.Fatalf("the Write row:\n%s", screen(m))
	}
	m = keys(t, m, "enter")
	if m.writeOn || m.focus != focusMain {
		t.Errorf("writeOn %v, focus %v after turning it off", m.writeOn, m.focus)
	}
	m.saveCmdRan(t, &saved)
	if len(saved) == 0 || saved[len(saved)-1].Write {
		t.Errorf("saved = %+v", saved)
	}
	m = keys(t, m, "enter")
	if !m.writeOn {
		t.Error("not back on")
	}
}

func TestPaneAndViewHotkeysWorkFromTheGlobalPanel(t *testing.T) {
	for _, c := range []struct {
		key   string
		view  viewKind
		focus focus
	}{
		{"0", viewAgenda, focusSidebar},
		{"1", viewAgenda, focusMain},
		{"2", viewWeek, focusMain},
		{"3", viewMonth, focusMain},
	} {
		m, _ := writeModel(t, 130, 34)
		m = keys(t, m, "W")
		if m.focus != focusGlobalWrite {
			t.Fatal("setup: the global panel has the focus")
		}
		m = keys(t, m, c.key)
		if m.view != c.view || m.focus != c.focus {
			t.Errorf("%s from the global panel: view %v, focus %v; want %v, %v", c.key, m.view, m.focus, c.view, c.focus)
		}
	}
	// While typing, they're text.
	m, _ := writeModel(t, 130, 34)
	m = keys(t, m, "W", "a", "2")
	if m.view != viewAgenda || m.globalWrite.input.Value() != "2" {
		t.Errorf("2 while typing: view %v, input %q", m.view, m.globalWrite.input.Value())
	}
}

func TestSyncTodayAndFilterWorkFromTheGlobalPanel(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m = keys(t, m, "W")

	m.syncing = false
	m, cmd := send(t, m, press("r"))
	if !m.syncing || cmd == nil || m.focus != focusGlobalWrite {
		t.Errorf("r: syncing %v, focus %v", m.syncing, m.focus)
	}

	m = keys(t, m, "2", "W", "t")
	if m.view != viewWeek || m.focus != focusMain {
		t.Errorf("t: view %v, focus %v", m.view, m.focus)
	}

	m = keys(t, m, "W", "/")
	if m.filterInput == nil || m.focus != focusMain {
		t.Errorf("/: filter input open %v, focus %v", m.filterInput != nil, m.focus)
	}
	m = keys(t, m, "esc")

	// Typing in a Write panel keeps them as text.
	m = keys(t, m, "W", "a")
	m = typed(t, m, "r t /")
	if m.globalWrite.input.Value() != "r t /" || m.filterInput != nil {
		t.Errorf("input = %q", m.globalWrite.input.Value())
	}
}

func TestTwoWritePanelsInTheEventViewAreEqual(t *testing.T) {
	for _, w := range []int{120, 130, 180, 300} {
		m, _ := writeModel(t, w, 34)
		m, _ = openEvent(t, m)
		m = keys(t, m, "W")
		l := m.eventLayout()
		gw, _ := m.globalRect()
		if l.writeW != gw || l.detailW+l.writeW+gw != w {
			t.Errorf("at %d: event write %d, global %d, details %d", w, l.writeW, gw, l.detailW)
		}
	}
}

func TestGlobalPanelAimsForUpTo80Columns(t *testing.T) {
	for w, want := range map[int]int{120: 54, 180: 77, 300: 80} {
		m, _ := writeModel(t, w, 34)
		m = keys(t, m, "W")
		if gw, _ := m.globalRect(); gw != want {
			t.Errorf("at %d: global panel %d, want %d", w, gw, want)
		}
	}
}

func TestAltJAndKReorderItems(t *testing.T) {
	for _, down := range []string{"alt+j", "alt+down"} {
		m, src := writeModel(t, 130, 34)
		m, scope := openEvent(t, m)
		m = keys(t, m, "w")
		for _, text := range []string{"one", "two", "three"} {
			m = keys(t, m, "a")
			m = typed(t, m, text)
			m = keys(t, m, "enter", "esc")
		}
		order := func() string {
			var s []string
			for _, it := range listOf(t, src, scope) {
				s = append(s, it.Text)
			}
			return strings.Join(s, ",")
		}
		m = keys(t, m, "g", down)
		if got := order(); got != "two,one,three" {
			t.Fatalf("%s: order %s", down, got)
		}
		m = keys(t, m, down, down) // the third press is at the end: nothing
		if got := order(); got != "two,three,one" || m.eventWrite.sel != 2 {
			t.Fatalf("%s: order %s, selected %d", down, got, m.eventWrite.sel)
		}
		m = keys(t, m, "alt+k", "alt+up")
		if got := order(); got != "one,two,three" || m.eventWrite.sel != 0 {
			t.Fatalf("up: order %s, selected %d", got, m.eventWrite.sel)
		}
	}
}

func TestWrapInputKeepsEveryRuneOnSomeLine(t *testing.T) {
	for _, in := range []string{"", "short", "a fairly long line of words that must wrap", "unbreakablewordthatislongerthanthewidth", "trailing space  "} {
		rs := []rune(in)
		segs := wrapInput(rs, 10)
		at := 0
		for _, sg := range segs {
			if sg[0] != at || sg[1]-sg[0] > 10 {
				t.Errorf("%q: bad line %v in %v", in, sg, segs)
			}
			at = sg[1]
		}
		if at != len(rs) {
			t.Errorf("%q: lines end at %d of %d", in, at, len(rs))
		}
	}
}

func TestTypingALongItemWrapsInsteadOfClipping(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m = keys(t, m, "W", "a")
	text := "this is a rather long todo that cannot possibly fit on one line of the panel"
	m = typed(t, m, text)
	s := screen(m)
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ").Replace(s)), " ")
	// Every word is on screen, from the first to the last.
	for _, w := range strings.Fields(text) {
		if !strings.Contains(flat, w) {
			t.Fatalf("%q is not on screen:\n%s", w, s)
		}
	}
	if strings.Count(s, "›") < 1 {
		t.Errorf("no prompt:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if ansi.StringWidth(l) > 130 {
			t.Errorf("line wider than the window: %q", l)
		}
	}
	// And it still adds the whole text.
	m = keys(t, m, "enter")
	if !strings.Contains(strings.Join(strings.Fields(screen(m)), " "), "possibly") {
		t.Errorf("item missing after enter")
	}
}

func TestEnterIsLabeledAddWhenCreatingAndSaveWhenEditing(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m = keys(t, m, "W", "a")
	m = typed(t, m, "first")
	m = keys(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "enter add") {
		t.Errorf("adding: want 'enter add':\n%s", s)
	}
	m = keys(t, m, "esc", "e")
	if s := screen(m); !strings.Contains(s, "enter save") || strings.Contains(s, "enter add") {
		t.Errorf("editing: want 'enter save':\n%s", s)
	}
}

func TestZArchivesANoteAndBigZLeavesNotes(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w")
	m = keys(t, m, "A")
	m = typed(t, m, "the team went bowling")
	m = keys(t, m, "enter", "esc")
	m = keys(t, m, "a")
	m = typed(t, m, "a task")
	m = keys(t, m, "enter", "esc")
	// Z sweeps checked tasks only: here that's nothing, and the note stays.
	m = keys(t, m, "g", "Z")
	if got := listOf(t, src, scope); len(got) != 2 {
		t.Fatalf("after Z: %+v", got)
	}
	arch, _ := src.st.Archived()
	if len(arch) != 0 {
		t.Fatalf("Z archived %+v", arch)
	}
	// z on the note archives it, without checking anything first.
	m = keys(t, m, "z")
	left := listOf(t, src, scope)
	if len(left) != 1 || left[0].Text != "a task" {
		t.Fatalf("left in the panel: %+v", left)
	}
	arch, _ = src.st.Archived()
	if len(arch) != 1 || !arch[0].Note || arch[0].Text != "the team went bowling" || !arch[0].Origin.Same(scope) {
		t.Errorf("archive = %+v", arch)
	}
	// z on an unchecked task still says to check it first.
	m = keys(t, m, "z")
	if !strings.Contains(screen(m), "Check it first") {
		t.Errorf("no hint:\n%s", screen(m))
	}
}

func TestBangTogglesPriorityAndCheckingClearsIt(t *testing.T) {
	m, src := writeModel(t, 130, 34)
	m, scope := openEvent(t, m)
	m = keys(t, m, "w", "a")
	m = typed(t, m, "ship it")
	m = keys(t, m, "enter", "esc", "!")
	if items := listOf(t, src, scope); !items[0].Priority {
		t.Fatalf("not a priority: %+v", items)
	}
	var row string
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(l), "[!] ship it") {
			row = l
		}
	}
	if row == "" {
		t.Fatalf("no [!] row:\n%s", screen(m))
	}
	if !strings.Contains(row, "\x1b[1;35m[!]") || strings.Contains(row, "\x1b[7m") || strings.Contains(row, "\x1b[45m") {
		t.Errorf("priority row is not bold magenta without a background: %q", row)
	}
	m = keys(t, m, "!")
	if items := listOf(t, src, scope); items[0].Priority || !strings.Contains(screen(m), "[ ] ship it") {
		t.Errorf("not toggled off:\n%s", screen(m))
	}
	m = keys(t, m, "!", "space")
	if items := listOf(t, src, scope); !items[0].Done || items[0].Priority || !strings.Contains(screen(m), "[x] ship it") {
		t.Errorf("checking kept the priority: %+v\n%s", items, screen(m))
	}
	m = keys(t, m, "!")
	if !strings.Contains(screen(m), "Only an open task can have a priority") {
		t.Errorf("no hint for a done task:\n%s", screen(m))
	}
}

func TestBangOnANoteSaysSo(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "A")
	m = typed(t, m, "just a note")
	m = keys(t, m, "enter", "esc", "!")
	if !strings.Contains(screen(m), "Only an open task can have a priority") {
		t.Errorf("no hint:\n%s", screen(m))
	}
}
