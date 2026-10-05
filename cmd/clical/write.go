package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// WriteCmd is the command-line side of Write: notes and TODOs, either global
// or attached to one event. The files are plain markdown and safe to edit by
// hand; clical write also works while the TUI is open. It needs no passphrase
// or network.
type WriteCmd struct {
	List      WriteListCmd      `cmd:"" default:"1" help:"List TODOs and notes: global ones, or one event's, or all."`
	Add       WriteAddCmd       `cmd:"" help:"Add a TODO (or --note), globally or to an event."`
	Done      WriteDoneCmd      `cmd:"" help:"Check a TODO."`
	Undo      WriteUndoCmd      `cmd:"" help:"Uncheck a TODO."`
	Rm        WriteRmCmd        `cmd:"" help:"Delete a TODO."`
	Move      WriteMoveCmd      `cmd:"" help:"Move a TODO to an event, or to the global list."`
	Archive   WriteArchiveCmd   `cmd:"" help:"File a checked TODO away in the archive."`
	Archived  WriteArchivedCmd  `cmd:"" help:"List the archive."`
	Unarchive WriteUnarchiveCmd `cmd:"" help:"Restore an archived TODO to the global list, checked."`
	Path      WritePathCmd      `cmd:"" help:"Print where the files are."`
}

// eventFlags select an event, for the commands that work on one.
type eventFlags struct {
	Event string `short:"e" help:"An event: its title (or part of it), or its key. Without it, the global list."`
	On    string `help:"The day of the event, YYYY-MM-DD, if its title matches more than one."`
}

const (
	searchBack    = 14 * 24 * time.Hour
	searchForward = 60 * 24 * time.Hour
)

// window is the days to look for the event in.
func (f eventFlags) window(now time.Time) (from, to time.Time, err error) {
	if f.On != "" {
		d, err := domain.ParseCivilDate(f.On)
		if err != nil {
			return from, to, fmt.Errorf("--on must be a date like 2026-10-05")
		}
		return d.In(time.Local), d.AddDays(1).In(time.Local), nil
	}
	return now.Add(-searchBack), now.Add(searchForward), nil
}

// resolve finds the event. With none named it is the global scope.
func (f eventFlags) resolve(ctx context.Context, a *app.App) (write.Ref, *write.Header, error) {
	if f.Event == "" {
		return write.Ref{}, nil, nil
	}
	from, to, err := f.window(time.Now())
	if err != nil {
		return write.Ref{}, nil, err
	}
	e, err := a.FindEvent(ctx, f.Event, from, to)
	var none *app.NoEventError
	var many *app.AmbiguousEventError
	switch {
	case errors.As(err, &none):
		return write.Ref{}, nil, fmt.Errorf("%w in the last two weeks or next two months; try --on YYYY-MM-DD", err)
	case errors.As(err, &many):
		var b strings.Builder
		b.WriteString(many.Error() + ":\n")
		for _, m := range many.Matches {
			fmt.Fprintf(&b, "  %s · %s · key %s\n", m.Title, whenOf(m), m.Identity())
		}
		b.WriteString("Add --on YYYY-MM-DD, or give the key.")
		return write.Ref{}, nil, errors.New(b.String())
	case err != nil:
		return write.Ref{}, nil, err
	}
	ref, hdr := a.EventScope(ctx, e, time.Local)
	return ref, hdr, nil
}

func whenOf(e domain.Event) string {
	if s, _, ok := e.TimeRange(); ok {
		return s.In(time.Local).Format("Mon 2 Jan 15:04")
	}
	if d, _, ok := e.DateRange(); ok {
		return d.In(time.Local).Format("Mon 2 Jan")
	}
	return ""
}

func openWrite() (*app.App, *write.Store, error) {
	a, err := openApp()
	if err != nil {
		return nil, nil, err
	}
	st, err := a.Write()
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	return a, st, nil
}

// idError turns a lookup failure into advice.
func idError(err error, id string) error {
	switch {
	case errors.Is(err, write.ErrNotFound):
		return fmt.Errorf("no TODO %q; see 'clical write list --all'", id)
	case errors.Is(err, write.ErrAmbiguous):
		return fmt.Errorf("%q starts more than one TODO; type more of it", id)
	}
	return err
}

// WriteListCmd lists what's written.
type WriteListCmd struct {
	eventFlags
	All bool `help:"Every list: the global one and each event's."`
}

func (c *WriteListCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()

	var entries []write.Entry
	switch {
	case c.All:
		entries, err = st.All()
	default:
		var ref write.Ref
		if ref, _, err = c.resolve(context.Background(), a); err != nil {
			return err
		}
		var l write.List
		if l, err = st.Get(ref); err == nil {
			entries = []write.Entry{{Scope: ref, List: l}}
		}
	}
	if err != nil {
		return err
	}
	printEntries(os.Stdout, entries, c.All)
	return nil
}

// printEntries prints lists; with showEmpty, those with nothing in them too.
func printEntries(w io.Writer, entries []write.Entry, all bool) {
	printed := false
	for _, e := range entries {
		if all && len(visible(e.List)) == 0 && !e.Scope.IsZero() {
			continue
		}
		if printed {
			fmt.Fprintln(w)
		}
		printed = true
		fmt.Fprintln(w, heading(e))
		items := visible(e.List)
		if len(items) == 0 {
			fmt.Fprintln(w, "  (nothing yet)")
		}
		for _, it := range items {
			fmt.Fprintln(w, "  "+itemLine(it, e.Scope))
		}
	}
	if !printed {
		fmt.Fprintln(w, "Nothing yet. Add something with `clical write add \"...\"`.")
	}
}

func visible(l write.List) []write.Item {
	var out []write.Item
	for _, it := range l.Items {
		if it.Todo || strings.TrimSpace(it.Text) != "" {
			out = append(out, it)
		}
	}
	return out
}

func heading(e write.Entry) string {
	if e.Scope.IsZero() {
		return "Global"
	}
	h := domain.SafeText(e.Scope.Title)
	if e.List.Header != nil {
		var parts []string
		if cal := e.List.Header.Calendar; cal != "" {
			parts = append(parts, domain.SafeText(cal))
		}
		if s := e.List.Header.Start; s != "" {
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			h += "  (" + strings.Join(parts, ", ") + ")"
		}
	}
	return h
}

// itemLine is one item: a TODO with its ID and, if it didn't start here,
// where it came from; a note indented under no checkbox.
func itemLine(it write.Item, scope write.Ref) string {
	text := domain.SafeText(it.Text)
	if !it.Todo {
		return "    " + text
	}
	box := "[ ]"
	if it.Done {
		box = "[x]"
	}
	line := fmt.Sprintf("%s %s  %s", box, it.ID, text)
	var notes []string
	if it.Done && !it.Completed.IsZero() {
		notes = append(notes, "done "+it.Completed.Format("2 Jan 15:04"))
	}
	if !it.Origin.Same(scope) {
		notes = append(notes, "from "+refName(it.Origin))
	}
	if !it.Last.IsZero() && !it.Last.Same(scope) && !it.Last.Same(it.Origin) {
		notes = append(notes, "last in "+refName(it.Last))
	}
	if len(notes) > 0 {
		line += "  (" + strings.Join(notes, "; ") + ")"
	}
	return line
}

func refName(r write.Ref) string {
	if r.IsZero() {
		return "global"
	}
	if r.Title == "" {
		return "an event"
	}
	return domain.SafeText(r.Title)
}

// WriteAddCmd adds a TODO or a note.
type WriteAddCmd struct {
	eventFlags
	Text []string `arg:"" name:"text" help:"What to write."`
	Note bool     `help:"Add a plain note instead of a TODO."`
}

func (c *WriteAddCmd) Run() error {
	text := strings.Join(c.Text, " ")
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	ref, hdr, err := c.resolve(context.Background(), a)
	if err != nil {
		return err
	}
	var id string
	err = st.Update(ref, hdr, func(l *write.List) error {
		if c.Note {
			return l.AddNote(text)
		}
		it, err := l.AddTodo(text, write.NewID())
		id = it.ID
		return err
	})
	if errors.Is(err, write.ErrEmpty) {
		return errors.New("nothing to add: give some text")
	}
	if err != nil {
		return err
	}
	where := "the global list"
	if !ref.IsZero() {
		where = refName(ref)
	}
	if c.Note {
		fmt.Printf("Added a note to %s.\n", where)
	} else {
		fmt.Printf("Added %s to %s.\n", id, where)
	}
	return nil
}

// todoID is the ID, or a start of it, of a TODO in any list.
type todoID struct {
	ID string `arg:"" help:"A TODO's ID, or the start of it (see 'write list')."`
}

// change finds the TODO and applies fn to the list it is in.
func (c todoID) change(fn func(l *write.List, id string) error) (write.Item, error) {
	a, st, err := openWrite()
	if err != nil {
		return write.Item{}, err
	}
	defer a.Close()
	src, item, err := st.Find(c.ID)
	if err != nil {
		return write.Item{}, idError(err, c.ID)
	}
	return item, st.Update(src.Scope, nil, func(l *write.List) error { return fn(l, item.ID) })
}

// WriteDoneCmd checks a TODO.
type WriteDoneCmd struct{ todoID }

func (c *WriteDoneCmd) Run() error {
	it, err := c.change(func(l *write.List, id string) error { return l.Check(id, time.Now()) })
	if err != nil {
		return err
	}
	fmt.Printf("Checked %s: %s\n", it.ID, it.Text)
	return nil
}

// WriteUndoCmd unchecks a TODO.
type WriteUndoCmd struct{ todoID }

func (c *WriteUndoCmd) Run() error {
	it, err := c.change(func(l *write.List, id string) error { return l.Uncheck(id) })
	if err != nil {
		return err
	}
	fmt.Printf("Reopened %s: %s\n", it.ID, it.Text)
	return nil
}

// WriteRmCmd deletes a TODO.
type WriteRmCmd struct{ todoID }

func (c *WriteRmCmd) Run() error {
	it, err := c.change(func(l *write.List, id string) error { return l.Delete(id) })
	if err != nil {
		return err
	}
	fmt.Printf("Deleted %s: %s\n", it.ID, it.Text)
	return nil
}

// WriteMoveCmd moves a TODO between scopes.
type WriteMoveCmd struct {
	todoID
	eventFlags
}

func (c *WriteMoveCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	to, hdr, err := c.resolve(context.Background(), a)
	if err != nil {
		return err
	}
	_, item, err := st.Find(c.ID)
	if err != nil {
		return idError(err, c.ID)
	}
	if err := st.Move(item.ID, to, hdr); err != nil {
		if errors.Is(err, write.ErrSameScope) {
			return fmt.Errorf("%s is already there", item.ID)
		}
		return err
	}
	where := "the global list"
	if !to.IsZero() {
		where = refName(to)
	}
	fmt.Printf("Moved %s to %s.\n", item.ID, where)
	return nil
}

// WriteArchiveCmd archives a checked TODO.
type WriteArchiveCmd struct{ todoID }

func (c *WriteArchiveCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	rec, err := st.Archive(c.ID, time.Now())
	switch {
	case errors.Is(err, write.ErrNotDone):
		return fmt.Errorf("%s isn't checked; run 'clical write done %s' first", c.ID, c.ID)
	case err != nil:
		return idError(err, c.ID)
	}
	fmt.Printf("Archived %s: %s\n", rec.ID, rec.Text)
	return nil
}

// WriteArchivedCmd lists the archive.
type WriteArchivedCmd struct{}

func (c *WriteArchivedCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	items, err := st.Archived()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("The archive is empty.")
		return nil
	}
	for _, it := range items {
		line := fmt.Sprintf("%s  %s  %s", it.ID, it.Completed.Format("2006-01-02 15:04"), domain.SafeText(it.Text))
		if !it.Origin.IsZero() {
			line += "  (from " + refName(it.Origin) + ")"
		}
		fmt.Println(line)
	}
	return nil
}

// WriteUnarchiveCmd restores an archived TODO.
type WriteUnarchiveCmd struct{ todoID }

func (c *WriteUnarchiveCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	rec, err := st.Unarchive(c.ID)
	if err != nil {
		return idError(err, c.ID)
	}
	fmt.Printf("Restored %s to the global list, checked: %s\n", rec.ID, rec.Text)
	return nil
}

// WritePathCmd prints the notes directory.
type WritePathCmd struct{}

func (c *WritePathCmd) Run() error {
	a, st, err := openWrite()
	if err != nil {
		return err
	}
	defer a.Close()
	fmt.Println(st.Dir())
	return nil
}
