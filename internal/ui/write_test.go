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
func (s storeSource) WriteArchive(id string, now time.Time) error {
	_, err := s.st.Archive(id, now)
	return err
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
	quickTimeout = 30 * time.Millisecond // file access is quick; blink timers aren't waited for
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
	if !strings.Contains(s, "INSERT TODO") || !strings.Contains(s, "enter add") {
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
		t.Errorf("archived an unchecked TODO:\n%s", screen(m))
	}
	m = keys(t, m, "space")
	if items := listOf(t, src, scope); !items[0].Done || items[0].Completed.IsZero() {
		t.Fatalf("not checked: %+v", items)
	}
	if !strings.Contains(screen(m), "[x] file the report") {
		t.Errorf("checked TODO isn't shown:\n%s", screen(m))
	}
	m = keys(t, m, "space")
	if items := listOf(t, src, scope); items[0].Done || !items[0].Completed.IsZero() {
		t.Errorf("not unchecked: %+v", items)
	}
	m = keys(t, m, "space", "z")
	if len(listOf(t, src, scope)) != 0 {
		t.Error("the archived TODO is still listed")
	}
	if arch, _ := src.st.Archived(); len(arch) != 1 || arch[0].Text != "file the report" || !arch[0].Origin.Same(scope) {
		t.Errorf("archive = %+v", arch)
	}
}

func TestNotesCantBeCheckedOrMoved(t *testing.T) {
	m, _ := writeModel(t, 130, 34)
	m, _ = openEvent(t, m)
	m = keys(t, m, "w", "A")
	m = typed(t, m, "just a note")
	m = keys(t, m, "enter", "esc", "space")
	if !strings.Contains(screen(m), "Only a TODO can be checked") {
		t.Errorf("no hint:\n%s", screen(m))
	}
	m = keys(t, m, "m")
	if !strings.Contains(screen(m), "Only a TODO can be moved") {
		t.Errorf("no hint for move:\n%s", screen(m))
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
		t.Error("the strip doesn't count the open TODO")
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
