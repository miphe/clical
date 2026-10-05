// Package write is clical's notes and TODOs, kept in two scopes: one event
// occurrence, or global. This file is the pure model: items, lists and the
// rules for checking, moving and archiving them. format.go reads and writes
// the markdown form, archive.go the archive's, and store.go the files. It
// knows nothing about terminals.
package write

import (
	"errors"
	"strings"
	"time"

	"github.com/miphe/clical/internal/domain"
)

var (
	ErrEmpty     = errors.New("write: nothing to write")
	ErrNotFound  = errors.New("write: no such item")
	ErrAmbiguous = errors.New("write: more than one item starts with that")
	ErrNotTodo   = errors.New("write: not a TODO")
	ErrNotDone   = errors.New("write: only a checked TODO can be archived")
	ErrChanged   = errors.New("write: the file changed while it was being updated")
	ErrSameScope = errors.New("write: already there")
)

// Ref points at a scope: one event occurrence, identified by its calendar and
// stable key (domain.Event.Identity), with the event's title as it was when
// last seen, so a reference still reads sensibly after the event is gone.
// The zero Ref is the global scope, or "none".
type Ref struct {
	Calendar string `json:"cal,omitempty"`
	Key      string `json:"key,omitempty"`
	Title    string `json:"title,omitempty"`
}

// IsZero reports whether r is the global scope (or no scope).
func (r Ref) IsZero() bool { return r.Calendar == "" && r.Key == "" }

// Same reports whether r and o are the same scope, whatever their titles.
func (r Ref) Same(o Ref) bool { return r.Calendar == o.Calendar && r.Key == o.Key }

// RefFor is the scope of an event's notes.
func RefFor(e domain.Event) Ref {
	return Ref{Calendar: e.CalendarID, Key: e.Identity(), Title: e.Title}
}

// Item is one line of a Write list: a note, or a TODO.
type Item struct {
	// ID names a TODO; notes have none.
	ID   string
	Todo bool
	Text string

	// Done and Completed are for TODOs. Completed is the latest moment the
	// TODO was checked, with the offset of the zone it was checked in, and is
	// zero when it isn't done.
	Done      bool
	Completed time.Time

	// Origin is where the TODO was created, and never changes: the zero Ref
	// for global. Last is the most recent event it was in, or zero if it has
	// never been in one.
	Origin, Last Ref

	derived bool // ID was worked out from the text, not stored
}

// Header is what an event's file says about the event, as of the last time
// clical wrote it.
type Header struct {
	Event    Ref
	Calendar string // the calendar's name
	Start    string // RFC 3339 for a timed event, a date for an all-day one
}

// List is one scope's items, in order. Header is set for an event's list.
type List struct {
	Header *Header
	Items  []Item
}

// Scope is where this list lives.
func (l List) Scope() Ref {
	if l.Header == nil {
		return Ref{}
	}
	return l.Header.Event
}

// cleanText makes text fit on one markdown line, safe to print and unable to
// end a metadata comment early.
func cleanText(s string) (string, error) {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(domain.SafeText(s))
	s = strings.NewReplacer("<!--", "<!-", "-->", "->").Replace(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrEmpty
	}
	return s, nil
}

// AddNote appends a note.
func (l *List) AddNote(text string) error {
	t, err := cleanText(text)
	if err != nil {
		return err
	}
	l.Items = append(l.Items, Item{Text: t})
	return nil
}

// AddTodo appends an unchecked TODO created here.
func (l *List) AddTodo(text, id string) (Item, error) {
	t, err := cleanText(text)
	if err != nil {
		return Item{}, err
	}
	it := Item{ID: id, Todo: true, Text: t, Origin: l.Scope()}
	l.Items = append(l.Items, it)
	return it, nil
}

// find locates a TODO by ID or by a prefix of it that no other TODO shares.
func (l List) find(id string) (int, error) {
	if id == "" {
		return 0, ErrNotFound
	}
	found := -1
	for i, it := range l.Items {
		if !it.Todo {
			continue
		}
		if it.ID == id {
			return i, nil
		}
		if strings.HasPrefix(it.ID, id) {
			if found >= 0 {
				return 0, ErrAmbiguous
			}
			found = i
		}
	}
	if found < 0 {
		return 0, ErrNotFound
	}
	return found, nil
}

// Index is where the TODO with that ID (or unique prefix) is in Items.
func (l List) Index(id string) (int, error) { return l.find(id) }

// Check marks a TODO done at now. Checking one that is already done moves its
// completion time to now.
func (l *List) Check(id string, now time.Time) error {
	i, err := l.find(id)
	if err != nil {
		return err
	}
	l.Items[i].Done, l.Items[i].Completed = true, now
	return nil
}

// Uncheck reopens a TODO and forgets when it was completed.
func (l *List) Uncheck(id string) error {
	i, err := l.find(id)
	if err != nil {
		return err
	}
	l.Items[i].Done, l.Items[i].Completed = false, time.Time{}
	return nil
}

// Edit changes a TODO's text.
func (l *List) Edit(id, text string) error {
	i, err := l.find(id)
	if err != nil {
		return err
	}
	t, err := cleanText(text)
	if err != nil {
		return err
	}
	l.Items[i].Text = t
	return nil
}

// Delete removes a TODO for good.
func (l *List) Delete(id string) error {
	_, err := l.take(id)
	return err
}

// EditAt and DeleteAt change any item by its position; notes have no ID.
func (l *List) EditAt(i int, text string) error {
	if i < 0 || i >= len(l.Items) {
		return ErrNotFound
	}
	t, err := cleanText(text)
	if err != nil {
		return err
	}
	l.Items[i].Text = t
	return nil
}

// DeleteAt removes the item at position i.
func (l *List) DeleteAt(i int) error {
	if i < 0 || i >= len(l.Items) {
		return ErrNotFound
	}
	l.Items = append(l.Items[:i], l.Items[i+1:]...)
	return nil
}

// take removes and returns a TODO.
func (l *List) take(id string) (Item, error) {
	i, err := l.find(id)
	if err != nil {
		return Item{}, err
	}
	it := l.Items[i]
	l.Items = append(l.Items[:i], l.Items[i+1:]...)
	return it, nil
}

// Receive appends a TODO that came from elsewhere. Arriving in an event's
// list makes that event its last event; its origin stays as it was.
func (l *List) Receive(it Item) {
	it.derived = false
	if !l.Scope().IsZero() {
		it.Last = l.Scope()
	}
	l.Items = append(l.Items, it)
}

// Move takes the TODO out of from and appends it to to.
func Move(from, to *List, id string) error {
	if from.Scope().Same(to.Scope()) {
		return ErrSameScope
	}
	it, err := from.take(id)
	if err != nil {
		return err
	}
	to.Receive(it)
	return nil
}

// Archived is a completed TODO filed away. It keeps what a timeline needs:
// when the work was last completed, and where it came from.
type Archived struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Completed time.Time `json:"completed"`
	Archived  time.Time `json:"archived"`
	Origin    Ref       `json:"origin,omitempty"`
	Last      Ref       `json:"last,omitempty"`
}

// Archive takes a checked TODO out of l and returns its archived form.
func (l *List) Archive(id string, now time.Time) (Archived, error) {
	i, err := l.find(id)
	if err != nil {
		return Archived{}, err
	}
	it := l.Items[i]
	if !it.Done {
		return Archived{}, ErrNotDone
	}
	completed := it.Completed
	if completed.IsZero() {
		completed = now // checked by hand in an editor: no time was recorded
	}
	l.Items = append(l.Items[:i], l.Items[i+1:]...)
	return Archived{ID: it.ID, Text: it.Text, Completed: completed, Archived: now, Origin: it.Origin, Last: it.Last}, nil
}

// Restore is the TODO an archived one becomes when unarchived: checked, as it
// was completed, with its provenance intact. It goes to the global list,
// always there, whatever became of its event.
func Restore(a Archived) Item {
	return Item{ID: a.ID, Todo: true, Text: a.Text, Done: true, Completed: a.Completed, Origin: a.Origin, Last: a.Last}
}
