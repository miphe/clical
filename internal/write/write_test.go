package write

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	t0      = time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("EEST", 3*3600))
	meeting = Ref{Calendar: "cal", Key: "weekly/2026-10-05T09:00:00Z", Title: "Weekly sync"}
	other   = Ref{Calendar: "cal", Key: "dentist", Title: "Dentist"}
)

func eventList(r Ref) List {
	return List{Header: &Header{Event: r, Calendar: "Work", Start: "2026-10-05T09:00:00Z"}}
}

func TestAddSetsOriginOnce(t *testing.T) {
	ev := eventList(meeting)
	it, err := ev.AddTodo("  send the deck \n", "aaaa1111")
	if err != nil || it.Text != "send the deck" || !it.Origin.Same(meeting) || !it.Last.IsZero() {
		t.Fatalf("AddTodo = %+v, %v", it, err)
	}
	g := List{}
	g.AddTodo("water plants", "bbbb2222")
	if !g.Items[0].Origin.IsZero() {
		t.Error("a global task has an origin")
	}
	if _, err := g.AddTodo("   ", "x"); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty task: %v", err)
	}
}

func TestMoveKeepsOriginAndTracksLastEvent(t *testing.T) {
	a, g, b := eventList(meeting), List{}, eventList(other)
	a.AddTodo("follow up", "aaaa1111")

	if err := Move(&a, &g, "aaaa"); err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 0 || len(g.Items) != 1 {
		t.Fatalf("after move: %d and %d items", len(a.Items), len(g.Items))
	}
	it := g.Items[0]
	if !it.Origin.Same(meeting) || !it.Last.IsZero() {
		t.Errorf("moved to global: origin %v, last %v", it.Origin, it.Last)
	}
	if err := Move(&g, &b, "aaaa1111"); err != nil {
		t.Fatal(err)
	}
	it = b.Items[0]
	if !it.Origin.Same(meeting) || !it.Last.Same(other) {
		t.Errorf("moved into another event: origin %v, last %v", it.Origin, it.Last)
	}
	if err := Move(&b, &b, "aaaa1111"); !errors.Is(err, ErrSameScope) {
		t.Errorf("move to the same scope: %v", err)
	}
}

func TestCheckTimestampIsTheLatest(t *testing.T) {
	l := List{}
	l.AddTodo("a", "11111111")
	l.Check("11", t0)
	later := t0.Add(48 * time.Hour)
	l.Check("11", later)
	if !l.Items[0].Completed.Equal(later) {
		t.Errorf("completed = %v; want the latest check", l.Items[0].Completed)
	}
	l.Uncheck("11")
	if l.Items[0].Done || !l.Items[0].Completed.IsZero() {
		t.Errorf("unchecked item = %+v", l.Items[0])
	}
}

func TestArchiveNeedsCheckedAndKeepsProvenance(t *testing.T) {
	l := eventList(meeting)
	l.AddTodo("a", "11111111")
	if _, err := l.Archive("11111111", t0); !errors.Is(err, ErrNotDone) {
		t.Fatalf("archiving an unchecked task: %v", err)
	}
	l.Check("11111111", t0)
	l.Items[0].Last = other
	arch := t0.Add(time.Hour)
	a, err := l.Archive("11111111", arch)
	if err != nil || len(l.Items) != 0 {
		t.Fatalf("Archive = %+v, %v", a, err)
	}
	if !a.Completed.Equal(t0) || !a.Archived.Equal(arch) || !a.Origin.Same(meeting) || !a.Last.Same(other) {
		t.Errorf("archived = %+v", a)
	}
	back := Restore(a)
	if !back.Done || !back.Completed.Equal(t0) || !back.Origin.Same(meeting) {
		t.Errorf("restored = %+v", back)
	}
}

func TestPrefixLookup(t *testing.T) {
	l := List{}
	l.AddTodo("a", "abcd0001")
	l.AddTodo("b", "abcd0002")
	l.AddTodo("c", "ffff0003")
	if _, err := l.Index("abcd"); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("shared prefix: %v", err)
	}
	if i, err := l.Index("abcd0002"); err != nil || i != 1 {
		t.Errorf("exact: %d, %v", i, err)
	}
	if i, err := l.Index("ff"); err != nil || i != 2 {
		t.Errorf("prefix: %d, %v", i, err)
	}
	if _, err := l.Index("zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestRoundTripKeepsEverything(t *testing.T) {
	l := eventList(meeting)
	l.AddNote("Agenda: <!-- not a comment --> done")
	it, _ := l.AddTodo("send the deck", "a1b2c3d4")
	_ = it
	l.AddTodo("book a room", "e5f6a7b8")
	l.Check("a1b2c3d4", t0)
	l.Items[2].Last = other
	got := Parse(Format(l))
	if got.Header == nil || !got.Header.Event.Same(meeting) || got.Header.Event.Title != "Weekly sync" || got.Header.Calendar != "Work" {
		t.Fatalf("header = %+v", got.Header)
	}
	if len(got.Items) != 3 {
		t.Fatalf("%d items: %+v", len(got.Items), got.Items)
	}
	todo := got.Items[1]
	if !todo.Done || !todo.Completed.Equal(t0) || !todo.Origin.Same(meeting) || todo.ID != "a1b2c3d4" {
		t.Errorf("checked task = %+v", todo)
	}
	if _, off := todo.Completed.Zone(); off != 3*3600 {
		t.Errorf("completion lost its offset: %d", off)
	}
	if got.Items[2].Last.Key != other.Key {
		t.Errorf("last event = %+v", got.Items[2].Last)
	}
	if string(Format(got)) != string(Format(l)) {
		t.Errorf("not stable:\n%s\n--\n%s", Format(got), Format(l))
	}
}

func TestHandWrittenFilesSurvive(t *testing.T) {
	src := "Notes from the call\n\n## Plan\n- a bullet, not a task\n  - [ ] indented, so not a task\n- [ ] first\n- [x] second\n- [ ] \n\nthe end\n"
	l := Parse([]byte(src))
	if got := string(Format(l)); got != src {
		t.Errorf("round trip changed the file:\n%q\n%q", got, src)
	}
	var todos []Item
	for _, it := range l.Items {
		if it.Todo {
			todos = append(todos, it)
		}
	}
	if len(todos) != 2 || todos[0].Text != "first" || todos[0].Done || !todos[1].Done {
		t.Fatalf("todos = %+v", todos)
	}
	if todos[0].ID == "" || todos[0].ID != Parse([]byte(src)).Items[5].ID {
		t.Error("a hand-written task's ID isn't the same on every read")
	}
	// Identical lines get different IDs.
	two := Parse([]byte("- [ ] same\n- [ ] same\n"))
	if two.Items[0].ID == two.Items[1].ID {
		t.Error("two identical tasks share an ID")
	}
}

func TestHandCheckedTodoArchivesWithTheArchiveTime(t *testing.T) {
	l := Parse([]byte("- [x] did it\n"))
	a, err := l.Archive(l.Items[0].ID, t0)
	if err != nil || !a.Completed.Equal(t0) {
		t.Errorf("Archive = %+v, %v", a, err)
	}
}

func TestMetadataCantBeForgedByText(t *testing.T) {
	l := List{}
	l.AddTodo(`ends the comment --> and starts <!--{"id":"evil"}-->`, "11111111")
	got := Parse(Format(l))
	if len(got.Items) != 1 || got.Items[0].ID != "11111111" {
		t.Errorf("items = %+v", got.Items)
	}
}

func TestControlCharactersAreRemoved(t *testing.T) {
	l := List{}
	l.AddNote("a\x1b[31mred\x07")
	if got := l.Items[0].Text; strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("text = %q", got)
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "write"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func addTodo(t *testing.T, s *Store, scope Ref, text, id string) {
	t.Helper()
	hdr := &Header{Event: scope, Calendar: "Work", Start: "2026-10-05T09:00:00Z"}
	if scope.IsZero() {
		hdr = nil
	}
	if err := s.Update(scope, hdr, func(l *List) error { _, err := l.AddTodo(text, id); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestStoreFilesAreNamedByHashAndPrivate(t *testing.T) {
	s := openStore(t)
	evil := Ref{Calendar: "cal", Key: "../../etc/passwd", Title: "x"}
	addTodo(t, s, evil, "a", "11111111")
	files, _ := filepath.Glob(filepath.Join(s.Dir(), "events", "*"))
	if len(files) != 1 || strings.Contains(files[0], "passwd") || strings.Contains(files[0], "..") {
		t.Fatalf("files = %v", files)
	}
	fi, _ := os.Stat(files[0])
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(s.Dir())
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", di.Mode().Perm())
	}
}

func TestStoreMoveArchiveUnarchive(t *testing.T) {
	s := openStore(t)
	addTodo(t, s, meeting, "follow up", "aaaa1111")
	addTodo(t, s, Ref{}, "water plants", "bbbb2222")

	if err := s.Move("aaaa", Ref{}, nil); err != nil {
		t.Fatal(err)
	}
	g, _ := s.Get(Ref{})
	ev, _ := s.Get(meeting)
	if len(g.Items) != 2 || len(ev.Items) != 0 || !g.Items[1].Origin.Same(meeting) {
		t.Fatalf("after move: global %+v, event %+v", g.Items, ev.Items)
	}

	if _, err := s.Archive("aaaa", t0); !errors.Is(err, ErrNotDone) {
		t.Fatalf("archive unchecked: %v", err)
	}
	if err := s.Update(Ref{}, nil, func(l *List) error { return l.Check("aaaa", t0) }); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Archive("aaaa", t0.Add(time.Hour))
	if err != nil || rec.Text != "follow up" {
		t.Fatalf("Archive = %+v, %v", rec, err)
	}
	g, _ = s.Get(Ref{})
	if len(g.Items) != 1 {
		t.Errorf("archived task still in the global list: %+v", g.Items)
	}
	arch, _ := s.Archived()
	if len(arch) != 1 || !arch[0].Origin.Same(meeting) || !arch[0].Completed.Equal(t0) {
		t.Fatalf("archive = %+v", arch)
	}

	if _, err := s.Unarchive("aaaa"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.Get(Ref{})
	arch, _ = s.Archived()
	last := g.Items[len(g.Items)-1]
	if len(arch) != 0 || !last.Done || !last.Origin.Same(meeting) || !last.Completed.Equal(t0) {
		t.Errorf("after unarchive: archive %+v, restored %+v", arch, last)
	}
}

func TestStoreRetriesWhenTheFileChangesUnderIt(t *testing.T) {
	s := openStore(t)
	addTodo(t, s, Ref{}, "one", "11111111")
	calls := 0
	err := s.Update(Ref{}, nil, func(l *List) error {
		calls++
		if calls == 1 { // someone edits the file mid-update
			f := filepath.Join(s.Dir(), "global.md")
			os.WriteFile(f, append(Format(*l), []byte("added by hand\n")...), 0o600)
		}
		_, err := l.AddTodo("two", "22222222")
		return err
	})
	if err != nil || calls != 2 {
		t.Fatalf("Update: %v after %d calls", err, calls)
	}
	g, _ := s.Get(Ref{})
	var texts []string
	for _, it := range g.Items {
		texts = append(texts, it.Text)
	}
	if strings.Join(texts, "|") != "one|added by hand|two" {
		t.Errorf("items = %v; the hand edit was lost", texts)
	}
}

func TestUpdateWritesNothingWhenNothingChanged(t *testing.T) {
	s := openStore(t)
	if err := s.Update(Ref{}, nil, func(*List) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "global.md")); !os.IsNotExist(err) {
		t.Error("an empty update created a file")
	}
}

func TestAllListsGlobalThenEvents(t *testing.T) {
	s := openStore(t)
	addTodo(t, s, other, "b", "22222222")
	addTodo(t, s, meeting, "a", "11111111")
	addTodo(t, s, Ref{}, "g", "33333333")
	os.WriteFile(filepath.Join(s.Dir(), "events", "stray.md"), []byte("not ours\n"), 0o600)
	all, err := s.All()
	if err != nil || len(all) != 3 || !all[0].Scope.IsZero() {
		t.Fatalf("All = %+v, %v", all, err)
	}
	if e, it, err := s.Find("2222"); err != nil || !e.Scope.Same(other) || it.Text != "b" {
		t.Errorf("Find = %+v %+v %v", e, it, err)
	}
}

func TestArchiveFileKeepsLinesItDoesntUnderstand(t *testing.T) {
	s := openStore(t)
	addTodo(t, s, Ref{}, "x", "11111111")
	s.Update(Ref{}, nil, func(l *List) error { return l.Check("11", t0) })
	s.Archive("11", t0)
	f := filepath.Join(s.Dir(), "archive.jsonl")
	data, _ := os.ReadFile(f)
	os.WriteFile(f, append([]byte("# my comment\n"), data...), 0o600)
	if _, err := s.Unarchive("11"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(f)
	if string(after) != "# my comment\n" {
		t.Errorf("archive = %q", after)
	}
}

func TestOriginSurvivesTheFileForEveryDirection(t *testing.T) {
	s := openStore(t)
	addTodo(t, s, Ref{}, "began global", "11111111")
	addTodo(t, s, meeting, "began in the meeting", "22222222")
	hdr := &Header{Event: other, Calendar: "Work"}
	if err := s.Move("1111", other, hdr); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("2222", other, hdr); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Get(other)
	byID := map[string]Item{}
	for _, it := range l.Items {
		byID[it.ID] = it
	}
	if g := byID["11111111"]; !g.Origin.IsZero() || !g.Last.Same(other) {
		t.Errorf("began global: origin %+v, last %+v", g.Origin, g.Last)
	}
	if m := byID["22222222"]; !m.Origin.Same(meeting) || !m.Last.Same(other) {
		t.Errorf("began in the meeting: origin %+v, last %+v", m.Origin, m.Last)
	}
	// And an item that never left its list says nothing about its origin.
	addTodo(t, s, other, "home", "33333333")
	data, _ := os.ReadFile(s.path(other))
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "home") && strings.Contains(line, `"from"`) {
			t.Errorf("a task in its own list repeats its origin: %s", line)
		}
	}
}

func TestMoveItem(t *testing.T) {
	mk := func() List {
		return List{Items: []Item{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	}
	order := func(l List) string {
		s := ""
		for _, it := range l.Items {
			s += it.Text
		}
		return s
	}
	for _, c := range []struct {
		from, to int
		want     string
	}{{0, 1, "bac"}, {0, 2, "bca"}, {2, 0, "cab"}, {1, 1, "abc"}} {
		l := mk()
		if err := l.MoveItem(c.from, c.to); err != nil || order(l) != c.want {
			t.Errorf("%d→%d: %s, %v; want %s", c.from, c.to, order(l), err, c.want)
		}
	}
	l := mk()
	if err := l.MoveItem(0, 3); err != ErrNotFound {
		t.Errorf("out of range: %v", err)
	}
}

func TestArchiveNoteAt(t *testing.T) {
	meeting := Ref{Calendar: "c", Key: "k", Title: "Weekly sync"}
	l := List{Header: &Header{Event: meeting}}
	l.AddNote("team building today")
	l.AddTodo("task", "11111111")
	now := time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)
	a, err := l.ArchiveNoteAt(0, now)
	if err != nil || len(l.Items) != 1 || !l.Items[0].Todo {
		t.Fatalf("ArchiveNoteAt = %+v, %v; items %+v", a, err, l.Items)
	}
	if !a.Note || a.ID == "" || a.Text != "team building today" || !a.Archived.Equal(now) || !a.Completed.IsZero() || !a.Origin.Same(meeting) {
		t.Errorf("archived = %+v", a)
	}
	if _, err := l.ArchiveNoteAt(0, now); !errors.Is(err, ErrNotANote) {
		t.Errorf("a task: %v", err)
	}
	if _, err := l.ArchiveNoteAt(5, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("out of range: %v", err)
	}
	if back := Restore(a); back.Todo || back.Text != a.Text || back.ID != "" {
		t.Errorf("restored = %+v", back)
	}
}

func TestStoreArchiveNoteAndUnarchive(t *testing.T) {
	s := openStore(t)
	hdr := &Header{Event: meeting, Calendar: "Work", Start: "2026-10-05T09:00:00Z"}
	if err := s.Update(meeting, hdr, func(l *List) error {
		l.AddNote("standing note")
		l.AddNote("team building today")
		_, err := l.AddTodo("task", "aaaa1111")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)
	// The list moved on since the panel drew it: nothing is archived.
	if _, err := s.ArchiveNote(meeting, 1, "something else", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale text: %v", err)
	}
	if _, err := s.ArchiveNote(meeting, 2, "task", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a task: %v", err)
	}
	if arch, _ := s.Archived(); len(arch) != 0 {
		t.Fatalf("archive = %+v", arch)
	}
	rec, err := s.ArchiveNote(meeting, 1, "team building today", now)
	if err != nil || !rec.Note || !rec.Origin.Same(meeting) || rec.Origin.Title != meeting.Title {
		t.Fatalf("ArchiveNote = %+v, %v", rec, err)
	}
	l, _ := s.Get(meeting)
	if len(l.Items) != 2 || l.Items[0].Text != "standing note" || !l.Items[1].Todo {
		t.Errorf("list = %+v", l.Items)
	}
	if arch, _ := s.Archived(); len(arch) != 1 || !arch[0].Note || arch[0].ID != rec.ID {
		t.Errorf("archive = %+v", arch)
	}
	// Unarchiving puts the note back in the global list, as a note.
	if _, err := s.Unarchive(rec.ID); err != nil {
		t.Fatal(err)
	}
	g, _ := s.Get(Ref{})
	if len(g.Items) != 1 || g.Items[0].Todo || g.Items[0].Text != "team building today" {
		t.Errorf("global = %+v", g.Items)
	}
	if arch, _ := s.Archived(); len(arch) != 0 {
		t.Errorf("archive after unarchive = %+v", arch)
	}
}

func TestStoreMoveNote(t *testing.T) {
	s := openStore(t)
	hdr := &Header{Event: meeting, Calendar: "Work", Start: "2026-10-05T09:00:00Z"}
	if err := s.Update(meeting, hdr, func(l *List) error {
		l.AddNote("first")
		l.AddNote("second")
		_, err := l.AddTodo("task", "aaaa1111")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveNote(meeting, 1, "stale", Ref{}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("stale text: %v", err)
	}
	if err := s.MoveNote(meeting, 2, "task", Ref{}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a task: %v", err)
	}
	if err := s.MoveNote(meeting, 1, "second", meeting, hdr); !errors.Is(err, ErrSameScope) {
		t.Errorf("same list: %v", err)
	}
	if err := s.MoveNote(meeting, 1, "second", Ref{}, nil); err != nil {
		t.Fatal(err)
	}
	ev, _ := s.Get(meeting)
	if len(ev.Items) != 2 || ev.Items[0].Text != "first" || !ev.Items[1].Todo {
		t.Errorf("event = %+v", ev.Items)
	}
	g, _ := s.Get(Ref{})
	if len(g.Items) != 1 || g.Items[0].Todo || g.Items[0].Text != "second" {
		t.Errorf("global = %+v", g.Items)
	}
	// Into an event's list too, which gets its header.
	if err := s.MoveNote(Ref{}, 0, "second", meeting, hdr); err != nil {
		t.Fatal(err)
	}
	if ev, _ = s.Get(meeting); len(ev.Items) != 3 || ev.Items[2].Text != "second" {
		t.Errorf("event after = %+v", ev.Items)
	}
}
