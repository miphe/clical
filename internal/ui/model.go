// Package ui is clical's full-screen terminal interface. It follows the Elm
// architecture: Update is pure and never touches the network or disk — all
// I/O goes through Source, called from tea.Cmds.
package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/engine"
)

// CalendarRow is a calendar together with the account it belongs to.
type CalendarRow struct {
	Account  domain.Account
	Calendar domain.Calendar
}

// Source is everything the UI needs from the outside world.
type Source interface {
	Calendars(ctx context.Context) ([]CalendarRow, error)
	Events(ctx context.Context, from, to time.Time) ([]domain.Event, error)
	Sync(ctx context.Context) ([]engine.Result, error)
}

const (
	initialDays  = 30
	extendDays   = 30
	footerHeight = 2
	narrowWidth  = 70
	syncTimeout  = 3 * time.Minute
)

type focus int

const (
	focusMain focus = iota
	focusSidebar
)

type (
	calendarsLoadedMsg struct {
		rows []CalendarRow
		err  error
	}
	eventsLoadedMsg struct {
		from   domain.CivilDate
		days   int
		events []domain.Event
		err    error
	}
	syncDoneMsg struct {
		at      time.Time
		results []engine.Result
		err     error
	}
	tickMsg time.Time
)

// Model is the whole UI state.
type Model struct {
	src  Source
	keys keyMap
	loc  *time.Location
	now  time.Time
	tick time.Duration // 0 disables the clock tick (tests)

	width, height int

	calendars        []CalendarRow
	colors           map[string]int // calendar ID → palette index
	hidden           map[string]bool
	sidebarCursor    int
	sidebarCollapsed bool
	focus            focus

	from    domain.CivilDate
	days    int
	events  []domain.Event
	loading bool
	loadErr error

	rows   []row
	sel    string // key of the selected row
	offset int    // first visible agenda row

	// pendingDay is a day to select once the load in flight arrives, for a
	// [ or ] that stepped outside the loaded window.
	pendingDay domain.CivilDate
	hasPending bool

	detail *agenda.Item

	syncing      bool
	lastSync     time.Time
	syncFailures []string
	syncErr      error
}

// New builds the UI model. now seeds the clock; after that it advances on a
// 30-second tick.
func New(src Source, loc *time.Location, now time.Time) Model {
	m := Model{
		src:    src,
		keys:   defaultKeyMap(),
		loc:    loc,
		now:    now,
		tick:   30 * time.Second,
		colors: map[string]int{},
		hidden: map[string]bool{},
		days:   initialDays,
		// Init starts both; Init can't record that itself.
		loading: true,
		syncing: true,
	}
	m.from = m.today()
	return m
}

func (m Model) today() domain.CivilDate {
	return domain.CivilDateFromTime(m.now.In(m.loc))
}

// Init loads calendars and events from the cache and starts a sync.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCalendars(), m.loadEvents(), m.syncCmd(), m.tickCmd())
}

// Commands. Each captures what it needs by value, so Update stays pure.

func (m Model) loadCalendars() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		rows, err := src.Calendars(context.Background())
		return calendarsLoadedMsg{rows: rows, err: err}
	}
}

func (m Model) loadEvents() tea.Cmd {
	src, loc, from, days := m.src, m.loc, m.from, m.days
	return func() tea.Msg {
		events, err := src.Events(context.Background(), from.In(loc), from.AddDays(days).In(loc))
		return eventsLoadedMsg{from: from, days: days, events: events, err: err}
	}
}

func (m Model) syncCmd() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		results, err := src.Sync(ctx)
		return syncDoneMsg{at: time.Now(), results: results, err: err}
	}
}

func (m Model) tickCmd() tea.Cmd {
	if m.tick == 0 {
		return nil
	}
	return tea.Tick(m.tick, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.width == 0 && msg.Width < narrowWidth {
			// Start collapsed on narrow terminals; after that it's the user's call.
			m.sidebarCollapsed = true
		}
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		m.rebuild()
		return m, m.tickCmd()

	case calendarsLoadedMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.calendars = msg.rows
		m.colors = make(map[string]int, len(msg.rows))
		for i, r := range msg.rows {
			m.colors[r.Calendar.ID] = i
		}
		m.sidebarCursor = min(m.sidebarCursor, max(0, len(m.calendars)-1))
		m.rebuild()
		return m, nil

	case eventsLoadedMsg:
		if msg.from != m.from || msg.days != m.days {
			return m, nil // a newer load is in flight
		}
		m.loading = false
		m.loadErr = msg.err
		if msg.err == nil {
			m.events = msg.events
		}
		m.rebuild()
		if m.hasPending {
			m.selectDay(m.pendingDay)
			m.hasPending = false
			m.ensureVisible()
		}
		return m, nil

	case syncDoneMsg:
		m.syncing = false
		m.lastSync = msg.at
		m.syncErr = msg.err
		m.syncFailures = nil
		changed := false
		for _, r := range msg.results {
			var unavailable *domain.CalendarUnavailableError
			switch {
			case errors.As(r.Err, &unavailable):
				m.syncFailures = append(m.syncFailures, fmt.Sprintf("%s · %s: feed URL no longer works", r.Account.Label, r.Calendar.Name))
			case r.Err != nil:
				m.syncFailures = append(m.syncFailures, fmt.Sprintf("%s · %s: %v", r.Account.Label, r.Calendar.Name, r.Err))
			case r.Changed:
				changed = true
			}
		}
		if changed {
			return m, m.loadEvents()
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.detail != nil {
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case key.Matches(msg, m.keys.Close):
			m.detail = nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Sync):
		if m.syncing {
			return m, nil
		}
		m.syncing = true
		return m, m.syncCmd()
	case key.Matches(msg, m.keys.Today):
		return m.jumpTo(m.today())
	}

	if m.focus == focusSidebar {
		return m.handleSidebarKey(msg)
	}
	return m.handleMainKey(msg)
}

func (m Model) handleMainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Down):
		return m.moveSelection(1)
	case key.Matches(msg, m.keys.Up):
		return m.moveSelection(-1)
	case key.Matches(msg, m.keys.Top):
		return m.moveSelection(-len(m.rows))
	case key.Matches(msg, m.keys.Bottom):
		return m.moveSelection(len(m.rows))
	case key.Matches(msg, m.keys.NextDay):
		return m.stepDay(1)
	case key.Matches(msg, m.keys.PrevDay):
		return m.stepDay(-1)
	case key.Matches(msg, m.keys.Open):
		if i := m.selIndex(); i >= 0 && m.rows[i].kind == rowItem {
			it := m.rows[i].item
			m.detail = &it
		}
	case key.Matches(msg, m.keys.FocusSidebar):
		m.focus = focusSidebar
	}
	return m, nil
}

func (m Model) handleSidebarKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Down):
		m.sidebarCursor = min(m.sidebarCursor+1, max(0, len(m.calendars)-1))
	case key.Matches(msg, m.keys.Up):
		m.sidebarCursor = max(m.sidebarCursor-1, 0)
	case key.Matches(msg, m.keys.Toggle):
		if m.sidebarCursor < len(m.calendars) {
			id := m.calendars[m.sidebarCursor].Calendar.ID
			m.hidden[id] = !m.hidden[id]
			m.rebuild()
		}
	case key.Matches(msg, m.keys.Collapse):
		m.sidebarCollapsed = !m.sidebarCollapsed
	case key.Matches(msg, m.keys.FocusMain):
		m.focus = focusMain
	}
	return m, nil
}

// jumpTo makes date the first day of the agenda and reloads.
func (m Model) jumpTo(date domain.CivilDate) (tea.Model, tea.Cmd) {
	m.from = date
	m.days = initialDays
	m.sel = ""
	m.offset = 0
	m.hasPending = false
	m.loading = true
	m.focus = focusMain
	return m, m.loadEvents()
}

// currentDay is the day the selection is on — the one [ and ] step from and
// the one whose header is marked.
func (m Model) currentDay() domain.CivilDate {
	if m.hasPending {
		return m.pendingDay
	}
	if i := m.selIndex(); i >= 0 {
		return m.rows[i].date
	}
	return m.from
}

// stepDay moves the selection to the first row of the day before or after
// the current one, growing the loaded window when that day is outside it.
func (m Model) stepDay(delta int) (tea.Model, tea.Cmd) {
	target := m.currentDay().AddDays(delta)
	switch {
	case target.Before(m.from):
		m.from = target
		m.days++ // keep the window's end where it was
	case !target.Before(m.from.AddDays(m.days)):
		m.days += extendDays
	default:
		m.selectDay(target)
		m.ensureVisible()
		return m, nil
	}
	m.pendingDay, m.hasPending = target, true
	m.loading = true
	return m, m.loadEvents()
}

// selectDay selects the first row of date, if it's loaded.
func (m *Model) selectDay(date domain.CivilDate) {
	for _, i := range selectable(m.rows) {
		if m.rows[i].date == date {
			m.sel = m.rows[i].key()
			return
		}
	}
}

// moveSelection moves the cursor by delta item rows. Moving down past the
// last loaded event extends the window.
func (m Model) moveSelection(delta int) (tea.Model, tea.Cmd) {
	items := selectable(m.rows)
	if len(items) == 0 {
		return m, nil
	}
	pos := 0
	if cur := m.selIndex(); cur >= 0 {
		for i, idx := range items {
			if idx == cur {
				pos = i
			}
		}
	}

	if delta > 0 && pos == len(items)-1 && !m.loading {
		m.days += extendDays
		m.loading = true
		return m, m.loadEvents()
	}

	pos = min(max(pos+delta, 0), len(items)-1)
	m.sel = m.rows[items[pos]].key()
	m.ensureVisible()
	return m, nil
}

// rebuild recomputes the agenda rows from events and keeps the selection on
// the same event if it's still there.
func (m *Model) rebuild() {
	var visible []domain.Event
	for _, e := range m.events {
		if !m.hidden[e.CalendarID] {
			visible = append(visible, e)
		}
	}
	m.rows = buildRows(agenda.Build(visible, m.from, m.days, m.loc), m.today(), m.now)
	if m.selIndex() < 0 {
		m.sel = m.defaultSel()
	}
	m.ensureVisible()
}

// selIndex is the row index of the selection, or -1.
func (m Model) selIndex() int {
	if m.sel == "" {
		return -1
	}
	for i, r := range m.rows {
		if (r.kind == rowItem || r.kind == rowNothing) && r.key() == m.sel {
			return i
		}
	}
	return -1
}

// defaultSel keeps the selection on today when today is shown: its first
// event after now, else its last row. Otherwise it's the first row shown.
func (m Model) defaultSel() string {
	today := m.today()
	lastToday := ""
	afterNow := false
	for _, r := range m.rows {
		switch {
		case r.kind == rowNow:
			afterNow = true
		case r.date != today || (r.kind != rowItem && r.kind != rowNothing):
		case afterNow:
			return r.key()
		default:
			lastToday = r.key()
		}
	}
	if lastToday != "" {
		return lastToday
	}
	if items := selectable(m.rows); len(items) > 0 {
		return m.rows[items[0]].key()
	}
	return ""
}

func (m Model) paneHeight() int {
	return m.height - footerHeight
}

// ensureVisible scrolls so the selection is on screen, keeping its day
// header in view when there's room.
func (m *Model) ensureVisible() {
	h := m.paneHeight()
	if h <= 0 {
		return
	}
	if i := m.selIndex(); i >= 0 {
		header := i
		for header > 0 && m.rows[header].kind != rowDay {
			header--
		}
		if i-header < h {
			m.offset = min(m.offset, header)
		}
		m.offset = min(m.offset, i)
		if i >= m.offset+h {
			m.offset = i - h + 1
		}
	}
	m.offset = min(max(m.offset, 0), max(0, len(m.rows)-h))
}
