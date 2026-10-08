package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// Write is the notes-and-TODOs surface: a panel for the open event's own
// list, in the event view, and a global one beside the main view. This file
// is the state, the commands and the keys; writeview.go draws it and lays out
// the event view. Update stays pure: lists are read and changed by
// commands through WriteSource, and the panel shows what comes back.

// WriteSource is what the panels need from the notes files. *app.App
// implements it; with none set the panels say so.
type WriteSource interface {
	WriteList(scope write.Ref) (write.List, error)
	WriteUpdate(scope write.Ref, hdr *write.Header, fn func(*write.List) error) error
	WriteMove(id string, to write.Ref, hdr *write.Header) error
	WriteArchive(id string, now time.Time) error
	WriteArchived() ([]write.Archived, error)
}

// WithWrite sets where notes are kept. Call it before the program starts.
func (m Model) WithWrite(w WriteSource) Model {
	m.writes = w
	return m
}

type writeWhich int

const (
	writeEvent writeWhich = iota
	writeGlobal
)

type writeMode int

const (
	wNormal  writeMode = iota
	wInsert            // typing a TODO or note
	wConfirm           // asking before a delete
)

// writePanel is one Write panel's state.
type writePanel struct {
	scope  write.Ref
	hdr    *write.Header // what an event's file says about it
	list   write.List
	loaded bool

	sel    int // position among the visible items
	offset int // first visible line

	mode    writeMode
	input   *textinput.Model
	editID  string // the TODO being edited; "" when adding or editing a note
	editAt  int    // the note being edited, by position; -1 when not
	addNote bool   // adding a note rather than a TODO
	note    string // a hint after a key that couldn't do anything
}

type (
	writeLoadedMsg struct {
		which write.Ref // scope asked for
		panel writeWhich
		list  write.List
		err   error
	}
	writeDoneMsg     struct{ err error }
	writeFlashEndMsg struct{ seq int }
)

const panelFlashDuration = 700 * time.Millisecond

// rows are the positions in the list of what's shown: TODOs and notes with
// text.
func (p writePanel) rows() []int {
	var out []int
	for i, it := range p.list.Items {
		if it.Todo || strings.TrimSpace(it.Text) != "" {
			out = append(out, i)
		}
	}
	return out
}

// current is the selected item, with its position in the list.
func (p writePanel) current() (write.Item, int, bool) {
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return write.Item{}, 0, false
	}
	return p.list.Items[rows[p.sel]], rows[p.sel], true
}

func (m *Model) panel(which writeWhich) *writePanel {
	if which == writeEvent {
		return &m.eventWrite
	}
	return &m.globalWrite
}

// focusedPanel is the Write panel keys go to, if one has the focus.
func (m *Model) focusedPanel() *writePanel {
	switch {
	case m.focus == focusEventWrite && m.detail != nil:
		return &m.eventWrite
	case m.focus == focusGlobalWrite:
		return &m.globalWrite
	}
	return nil
}

// writeTyping reports whether a Write panel is taking text or an answer, so
// keys like ? and q are input, not commands.
func (m Model) writeTyping() bool {
	p := (&m).focusedPanel()
	return p != nil && p.mode != wNormal
}

// Opening the event view.

// eventPanel is the event's Write panel, fresh.
func (m Model) eventPanel(e domain.Event) writePanel {
	hdr := &write.Header{Event: write.RefFor(e)}
	if i, ok := m.calIndex[e.CalendarID]; ok {
		hdr.Calendar = m.calendars[i].Calendar.DisplayName()
	}
	if start, _, ok := e.TimeRange(); ok {
		hdr.Start = start.In(m.loc).Format(time.RFC3339)
	} else if ds, _, ok := e.DateRange(); ok {
		hdr.Start = ds.String()
	}
	return writePanel{scope: hdr.Event, hdr: hdr, editAt: -1}
}

// Loading and changing.

func (m Model) loadWrite(which writeWhich) tea.Cmd {
	w := m.writes
	if w == nil || !m.writeOn {
		return nil
	}
	p := *(&m).panel(which)
	scope := p.scope
	return func() tea.Msg {
		l, err := w.WriteList(scope)
		return writeLoadedMsg{which: scope, panel: which, list: l, err: err}
	}
}

// editing reports whether the panel is changing an existing item or asking
// about it, which a reload shifting the list would put in the wrong place.
// Adding is fine: the new items just appear above the input.
func (p writePanel) editing() bool {
	return p.mode == wConfirm || (p.mode == wInsert && (p.editID != "" || p.editAt >= 0))
}

// reloadWrite rereads the panels that are showing something.
func (m Model) reloadWrite() tea.Cmd {
	var cmds []tea.Cmd
	if !m.globalWrite.editing() {
		cmds = append(cmds, m.loadWrite(writeGlobal))
	}
	if m.detail != nil && !m.eventWrite.editing() {
		cmds = append(cmds, m.loadWrite(writeEvent))
	}
	return tea.Batch(cmds...)
}

// writeLoaded takes a list that was read. A collapsed global panel whose
// content changed flashes once.
func (m Model) writeLoaded(msg writeLoadedMsg) (Model, tea.Cmd) {
	p := m.panel(msg.panel)
	if msg.panel == writeEvent && (m.detail == nil || !msg.which.Same(p.scope)) {
		return m, nil // the event view moved on
	}
	if msg.err != nil {
		m.actionErr = fmt.Errorf("Can't read notes: %w", msg.err)
		return m, nil
	}
	var cmd tea.Cmd
	if msg.panel == writeGlobal && p.loaded && !m.globalOpen && m.panelFlash &&
		string(write.Format(p.list)) != string(write.Format(msg.list)) {
		m.flashSeq++
		m.globalFlash = true
		seq := m.flashSeq
		cmd = tea.Tick(panelFlashDuration, func(time.Time) tea.Msg { return writeFlashEndMsg{seq: seq} })
	}
	p.list, p.loaded = msg.list, true
	m.clampWrite(msg.panel)
	return m, cmd
}

// writeOp runs a change to the notes, then rereads them.
func (m Model) writeOp(fn func(w WriteSource) error) tea.Cmd {
	w := m.writes
	if w == nil {
		return nil
	}
	return func() tea.Msg { return writeDoneMsg{err: fn(w)} }
}

func (m Model) updateList(p writePanel, fn func(l *write.List) error) tea.Cmd {
	scope, hdr := p.scope, p.hdr
	return m.writeOp(func(w WriteSource) error { return w.WriteUpdate(scope, hdr, fn) })
}

// Keys.

func (m Model) focusEventPanel() (tea.Model, tea.Cmd) {
	if m.detail == nil {
		return m, nil
	}
	m.focus = focusEventWrite
	return m, nil
}

// toggleGlobal opens the global panel and focuses it, or collapses it. force
// opens it whatever state it is in.
func (m Model) toggleGlobal(force bool) (tea.Model, tea.Cmd) {
	switch {
	case !m.globalOpen || force:
		m.globalOpen = true
		m.focus = focusGlobalWrite
		m.clampWrite(writeGlobal)
	case m.focus == focusGlobalWrite:
		m.globalOpen = false
		m.focus = m.mainFocus()
	default:
		m.focus = focusGlobalWrite
	}
	return m, nil
}

func (m Model) handleWriteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var which writeWhich = writeGlobal
	if m.focus == focusEventWrite {
		which = writeEvent
	}
	p := m.panel(which)
	if m.writes == nil {
		if s := msg.String(); s == "esc" || s == "q" || s == "tab" {
			m.focus = m.mainFocus()
		}
		return m, nil
	}
	p.note = ""
	switch p.mode {
	case wInsert:
		return m.writeInsertKey(which, msg)
	case wConfirm:
		return m.writeConfirmKey(which, msg)
	}

	rows := p.rows()
	item, at, have := p.current()
	switch s := msg.String(); s {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.focus = m.mainFocus()
	case "tab":
		return m.cycleFocus(+1)
	case "shift+tab":
		return m.cycleFocus(-1)
	case "w":
		if m.detail != nil {
			m.focus = focusEventWrite
		}
	case "W":
		return m.toggleGlobal(false)
	case "0", "1", "2", "3", "4", "r", "t", "/":
		// The keys that work from every pane work from here too: the panel
		// and view keys, sync, today and the filter. (Inside the event view
		// they don't, as there.)
		if m.detail != nil {
			break
		}
		switch s {
		case "r":
			return m.syncNow()
		case "t":
			m.focus = m.mainFocus()
			return m.goToday()
		case "/":
			return m.startFilter()
		case "0":
			m.focus = focusSidebar
			m.ensureSidebarSelection()
			return m, nil
		case "1":
			return m.switchView(viewAgenda)
		case "2":
			return m.switchView(viewWeek)
		case "4":
			if !m.heatEnabled() {
				return m, nil
			}
			return m.switchView(viewHeat)
		}
		return m.switchView(viewMonth)
	case "j", "down":
		p.sel = min(p.sel+1, max(len(rows)-1, 0))
	case "k", "up":
		p.sel = max(p.sel-1, 0)
	case "g", "home":
		p.sel = 0
	case "G", "end":
		p.sel = max(len(rows)-1, 0)
	case "a":
		return m.startInsert(which, "", "", false, -1)
	case "A":
		return m.startInsert(which, "", "", true, -1)
	case "e", "enter":
		if !have {
			break
		}
		if item.Todo {
			return m.startInsert(which, item.Text, item.ID, false, -1)
		}
		return m.startInsert(which, item.Text, "", true, at)
	case "space", "x":
		if !have || !item.Todo {
			p.note = "Only a TODO can be checked"
			break
		}
		if s == "x" {
			p.note = "Space checks; d deletes"
			break
		}
		id, done, now := item.ID, item.Done, time.Now()
		return m, m.updateList(*p, func(l *write.List) error {
			if done {
				return l.Uncheck(id)
			}
			return l.Check(id, now)
		})
	case "d":
		if have {
			p.mode = wConfirm
		}
	case "m":
		return m.moveSelected(which)
	case "Z":
		// The end of the day: file away everything that's done.
		var ids []string
		for _, i := range rows {
			if it := p.list.Items[i]; it.Todo && it.Done {
				ids = append(ids, it.ID)
			}
		}
		if len(ids) == 0 {
			p.note = "Nothing checked to archive"
			break
		}
		now := time.Now()
		return m, m.writeOp(func(w WriteSource) error {
			for _, id := range ids {
				if err := w.WriteArchive(id, now); err != nil {
					return err
				}
			}
			return nil
		})
	case "z":
		if !have || !item.Todo {
			p.note = "Only a TODO can be archived"
			break
		}
		if !item.Done {
			p.note = "Check it first (space)"
			break
		}
		id, now := item.ID, time.Now()
		return m, m.writeOp(func(w WriteSource) error { return w.WriteArchive(id, now) })
	}
	m.clampWrite(which)
	return m, nil
}

// moveSelected sends a TODO to the other list: an event's to the global one,
// a global one to the open event.
func (m Model) moveSelected(which writeWhich) (tea.Model, tea.Cmd) {
	p := m.panel(which)
	item, _, have := p.current()
	if !have || !item.Todo {
		p.note = "Only a TODO can be moved"
		return m, nil
	}
	var to write.Ref
	var hdr *write.Header
	if which == writeGlobal {
		if m.detail == nil {
			p.note = "Open an event first"
			return m, nil
		}
		to, hdr = m.eventWrite.scope, m.eventWrite.hdr
	}
	id := item.ID
	return m, m.writeOp(func(w WriteSource) error { return w.WriteMove(id, to, hdr) })
}

func (m Model) startInsert(which writeWhich, text, todoID string, note bool, at int) (tea.Model, tea.Cmd) {
	p := m.panel(which)
	in := textinput.New()
	in.SetStyles(inputStyles())
	in.Prompt = ""
	in.CharLimit = 500
	in.SetValue(text)
	in.CursorEnd()
	p.input, p.mode, p.addNote, p.editID, p.editAt = &in, wInsert, note, todoID, at
	return m, in.Focus()
}

func (m Model) writeInsertKey(which writeWhich, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.panel(which)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		p.mode, p.input = wNormal, nil
		return m, nil
	case "enter":
		text := strings.TrimSpace(p.input.Value())
		if text == "" {
			p.mode, p.input = wNormal, nil
			return m, nil
		}
		editing := p.editID != "" || p.editAt >= 0
		id, at, note := p.editID, p.editAt, p.addNote
		cmd := m.updateList(*p, func(l *write.List) error {
			switch {
			case id != "":
				return l.Edit(id, text)
			case at >= 0:
				return l.EditAt(at, text)
			case note:
				return l.AddNote(text)
			}
			_, err := l.AddTodo(text, write.NewID())
			return err
		})
		if editing {
			p.mode, p.input = wNormal, nil
		} else {
			// Adding stays open for the next one: jotting should be quick.
			p.input.SetValue("")
			// Jump to the new item once it arrives.
			p.sel = len(p.rows())
		}
		return m, cmd
	}
	var cmd tea.Cmd
	*p.input, cmd = p.input.Update(msg)
	m.clampWrite(which)
	return m, cmd
}

func (m Model) writeConfirmKey(which writeWhich, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.panel(which)
	p.mode = wNormal
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y":
		item, at, have := p.current()
		if !have {
			return m, nil
		}
		id, todo := item.ID, item.Todo
		return m, m.updateList(*p, func(l *write.List) error {
			if todo {
				return l.Delete(id)
			}
			return l.DeleteAt(at)
		})
	}
	return m, nil
}

func (m Model) writePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	p := m.focusedPanel()
	var cmd tea.Cmd
	*p.input, cmd = p.input.Update(msg)
	m.clampWrite(writeEvent)
	m.clampWrite(writeGlobal)
	return m, cmd
}

// clampWrite keeps the selection in range and in view.
func (m *Model) clampWrite(which writeWhich) {
	p := m.panel(which)
	rows := p.rows()
	p.sel = min(max(p.sel, 0), max(len(rows)-1, 0))
	w, h := m.writeInner(which)
	if p.mode == wInsert {
		h -= len(p.inputLines(w, h-1)) // what's being typed takes the bottom
		h = max(h, 1)
	}
	top, bottom := p.blockSpan(p.sel, w)
	switch {
	case top < p.offset:
		p.offset = top
	case bottom > p.offset+h:
		p.offset = bottom - h
	}
	p.offset = max(p.offset, 0)
}

// focusOrder is the panels tab goes through, in order: the calendar list,
// the main view and the global Write panel when it's open; in the event view,
// the details, the event's Write panel and the global one.
func (m Model) focusOrder() []focus {
	var order []focus
	if m.detail != nil {
		order = []focus{focusMain}
		if m.writeOn {
			order = append(order, focusEventWrite)
		}
	} else if m.view == viewHeat {
		order = []focus{focusSidebar}
		lay := m.focusLayout()
		if lay.agendaH > 0 {
			order = append(order, focusMain)
		}
		order = append(order, focusHeat)
		if lay.tasksH > 0 {
			order = append(order, focusTasks)
		}
	} else {
		order = []focus{focusSidebar, focusMain}
	}
	if gw, _ := m.globalRect(); m.writeOn && m.globalOpen && gw > stripW {
		order = append(order, focusGlobalWrite)
	}
	return order
}

// cycleFocus moves the focus to the next (or previous) panel.
func (m Model) cycleFocus(dir int) (tea.Model, tea.Cmd) {
	order := m.focusOrder()
	at := 0
	for i, f := range order {
		if f == m.focus {
			at = i
		}
	}
	m.focus = order[(at+dir+len(order))%len(order)]
	if m.focus == focusSidebar {
		m.ensureSidebarSelection()
	}
	return m, nil
}
