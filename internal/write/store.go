package write

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Store keeps Write lists as files in one directory:
//
//	global.md          the global list
//	events/<hash>.md   one file per event occurrence
//	archive.jsonl      archived TODOs, appended as they are filed
//
// The hash is of the event's calendar and key. Keys come from calendar feeds,
// so they are never used as file names. The files are meant to be read and
// edited by hand, so every write checks that the file is the one that was
// read and re-applies the change if it isn't.
type Store struct {
	dir string
	mu  sync.Mutex // serializes this process's writes
}

// Open uses dir, creating it (readable by the user only) if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "events"), 0o700); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir is the directory the files are in.
func (s *Store) Dir() string { return s.dir }

// NewID makes the ID of a new TODO.
func NewID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system's random source is gone; nothing sensible is left
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) path(scope Ref) string {
	if scope.IsZero() {
		return filepath.Join(s.dir, "global.md")
	}
	sum := sha256.Sum256([]byte(scope.Calendar + "\x00" + scope.Key))
	return filepath.Join(s.dir, "events", hex.EncodeToString(sum[:])[:16]+".md")
}

// rev identifies a version of a file.
type rev struct {
	exists bool
	mod    time.Time
	size   int64
}

func statRev(path string) rev {
	fi, err := os.Stat(path)
	if err != nil {
		return rev{}
	}
	return rev{exists: true, mod: fi.ModTime(), size: fi.Size()}
}

func (s *Store) read(path string) ([]byte, rev, error) {
	before := statRev(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, rev{}, nil
	}
	if err != nil {
		return nil, rev{}, fmt.Errorf("write: %w", err)
	}
	return data, before, nil
}

// Get reads a scope's list; one that has no file yet is empty.
func (s *Store) Get(scope Ref) (List, error) {
	data, _, err := s.read(s.path(scope))
	if err != nil {
		return List{}, err
	}
	return s.parsed(scope, data), nil
}

func (s *Store) parsed(scope Ref, data []byte) List {
	l := Parse(data)
	if l.Header == nil && !scope.IsZero() {
		l.Header = &Header{Event: scope}
	}
	return l
}

// Update changes a scope's list: fn gets the current one and edits it. hdr, if
// given, refreshes what an event's file says about the event. Nothing is
// written when fn changes nothing.
func (s *Store) Update(scope Ref, hdr *Header, fn func(*List) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(scope)
	for attempt := 0; attempt < 3; attempt++ {
		data, r, err := s.read(path)
		if err != nil {
			return err
		}
		l := s.parsed(scope, data)
		if hdr != nil && !scope.IsZero() {
			h := *hdr
			h.Event.Calendar, h.Event.Key = scope.Calendar, scope.Key
			l.Header = &h
		}
		if err := fn(&l); err != nil {
			return err
		}
		out := Format(l)
		if string(out) == string(data) {
			return nil
		}
		if len(out) == 0 && !r.exists {
			return nil
		}
		if statRev(path) != r {
			continue // changed since it was read: apply the change again
		}
		return atomicWrite(path, out)
	}
	return ErrChanged
}

// Entry is one list found in the directory.
type Entry struct {
	Scope Ref
	List  List
}

// All lists every list: the global one first, then each event's, soonest
// first.
func (s *Store) All() ([]Entry, error) {
	global, err := s.Get(Ref{})
	if err != nil {
		return nil, err
	}
	out := []Entry{{List: global}}
	files, err := filepath.Glob(filepath.Join(s.dir, "events", "*.md"))
	if err != nil {
		return nil, err
	}
	var events []Entry
	for _, f := range files {
		data, _, err := s.read(f)
		if err != nil {
			return nil, err
		}
		l := Parse(data)
		if l.Header == nil {
			continue // not one of ours
		}
		events = append(events, Entry{Scope: l.Header.Event, List: l})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].List.Header.Start < events[j].List.Header.Start
	})
	return append(out, events...), nil
}

// Find locates a TODO by ID or unique prefix across every list.
func (s *Store) Find(id string) (Entry, Item, error) {
	all, err := s.All()
	if err != nil {
		return Entry{}, Item{}, err
	}
	var (
		hit   Entry
		item  Item
		found bool
	)
	for _, e := range all {
		i, err := e.List.find(id)
		if errors.Is(err, ErrAmbiguous) {
			return Entry{}, Item{}, err
		}
		if err != nil {
			continue
		}
		if found && item.ID != e.List.Items[i].ID {
			return Entry{}, Item{}, ErrAmbiguous
		}
		hit, item, found = e, e.List.Items[i], true
	}
	if !found {
		return Entry{}, Item{}, ErrNotFound
	}
	return hit, item, nil
}

// Move takes a TODO to another scope. It's added there before it's removed
// here, so a failure in between leaves a copy rather than losing it.
func (s *Store) Move(id string, to Ref, toHdr *Header) error {
	src, item, err := s.Find(id)
	if err != nil {
		return err
	}
	if src.Scope.Same(to) {
		return ErrSameScope
	}
	if err := s.Update(to, toHdr, func(l *List) error {
		l.Receive(item)
		return nil
	}); err != nil {
		return err
	}
	return s.Update(src.Scope, nil, func(l *List) error { return l.Delete(item.ID) })
}

// Archive files a checked TODO away. It's appended to the archive before it's
// removed from its list, for the same reason as Move.
func (s *Store) Archive(id string, now time.Time) (Archived, error) {
	src, item, err := s.Find(id)
	if err != nil {
		return Archived{}, err
	}
	var rec Archived
	probe := src.List
	if rec, err = probe.Archive(item.ID, now); err != nil {
		return Archived{}, err
	}
	if err := s.appendArchive(rec); err != nil {
		return Archived{}, err
	}
	return rec, s.Update(src.Scope, nil, func(l *List) error {
		_, err := l.Archive(item.ID, now)
		return err
	})
}

func (s *Store) archivePath() string { return filepath.Join(s.dir, "archive.jsonl") }

func (s *Store) appendArchive(a Archived) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.archivePath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if _, err := f.WriteString(encodeArchived(a) + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("write: %w", err)
	}
	return f.Close()
}

// Archived lists the archive, oldest first.
func (s *Store) Archived() ([]Archived, error) {
	data, _, err := s.read(s.archivePath())
	if err != nil {
		return nil, err
	}
	var out []Archived
	for _, l := range parseArchive(data) {
		if l.rec != nil {
			out = append(out, *l.rec)
		}
	}
	return out, nil
}

// Unarchive restores an archived TODO, checked, to the global list, then
// takes it out of the archive.
func (s *Store) Unarchive(id string) (Archived, error) {
	all, err := s.Archived()
	if err != nil {
		return Archived{}, err
	}
	var hit []Archived
	for _, a := range all {
		if a.ID == id {
			hit = []Archived{a}
			break
		}
		if id != "" && strings.HasPrefix(a.ID, id) {
			hit = append(hit, a)
		}
	}
	switch len(hit) {
	case 0:
		return Archived{}, ErrNotFound
	case 1:
	default:
		return Archived{}, ErrAmbiguous
	}
	rec := hit[0]
	if err := s.Update(Ref{}, nil, func(l *List) error {
		l.Receive(Restore(rec))
		return nil
	}); err != nil {
		return Archived{}, err
	}
	return rec, s.removeArchived(rec.ID)
}

func (s *Store) removeArchived(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.archivePath()
	data, r, err := s.read(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, l := range parseArchive(data) {
		if l.rec != nil && l.rec.ID == id {
			continue
		}
		kept = append(kept, l.raw)
	}
	out := ""
	if len(kept) > 0 {
		out = strings.Join(kept, "\n") + "\n"
	}
	if statRev(path) != r {
		return ErrChanged
	}
	return atomicWrite(path, []byte(out))
}

// atomicWrite replaces path with data, so a reader sees the old file or the
// new one and never a part of it.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
