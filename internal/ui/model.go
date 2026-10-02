// Package ui is clical's full-screen terminal interface — presentation only.
// It follows the Elm architecture: Update is pure and never touches the
// network or disk. Everything it knows about calendars comes through Source
// (implemented by *app.App), called only from tea.Cmds. Calendar logic
// belongs in internal/app, not here; see docs/ARCHITECTURE.md.
package ui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// Source is everything the UI needs from the rest of clical. *app.App
// implements it; tests use a stub.
type Source interface {
	Calendars(ctx context.Context) ([]app.CalendarInfo, error)
	Events(ctx context.Context, from, to time.Time) ([]domain.Event, error)
	Sync(ctx context.Context) ([]app.SyncOutcome, error)
	SyncCalendar(ctx context.Context, cal app.CalendarInfo) app.SyncOutcome
	CheckFeed(ctx context.Context, rawURL string) (app.Feed, error)
	AddCalendar(ctx context.Context, account, name string, feed app.Feed) (app.CalendarInfo, error)
	RemoveCalendar(ctx context.Context, cal app.CalendarInfo) error
	RenameCalendar(ctx context.Context, cal app.CalendarInfo, name string) (app.CalendarInfo, error)
	RenameAccount(ctx context.Context, acct domain.Account, name string) (domain.Account, error)

	SavedGoogleClientID() (string, error)
	BeginGoogleSignIn(expect string, client *app.GoogleClient) (app.SignIn, error)
	GoogleCalendars(ctx context.Context, acct domain.Account) ([]app.RemoteCalendar, error)
	AddGoogleCalendars(ctx context.Context, acct domain.Account, picks []app.RemoteCalendar) ([]app.CalendarInfo, error)
}

const (
	initialDays  = 30
	extendDays   = 30
	footerHeight = 2
	narrowWidth  = 70
	syncTimeout  = 3 * time.Minute
	// pollInterval is how long after one sync of every calendar finishes
	// the next one starts. An iCal sync downloads the whole feed, so this
	// stays well clear of a feed provider's patience.
	pollInterval = 5 * time.Minute
)

type focus int

const (
	focusMain focus = iota
	focusSidebar
)

type viewKind int

const (
	viewAgenda viewKind = iota
	viewWeek
	viewMonth
	viewCount
)

type (
	calendarsLoadedMsg struct {
		rows []app.CalendarInfo
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
		results []app.SyncOutcome
		err     error
		// partial is true when only some calendars were synced (one just
		// added, or an account signed in again).
		partial bool
	}
	// pollMsg starts a scheduled sync, unless a later schedule replaced it.
	pollMsg        struct{ seq int }
	monthLoadedMsg struct {
		start  domain.CivilDate
		weeks  int
		events []domain.Event
		err    error
	}
	weekLoadedMsg struct {
		start  domain.CivilDate
		events []domain.Event
		err    error
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
	poll time.Duration // 0 disables background syncing (tests)

	width, height int

	calendars        []app.CalendarInfo
	calIndex         map[string]int // calendar ID → index in calendars
	hidden           map[string]bool
	side             sidebarState
	sidebarCollapsed bool
	focus            focus
	view             viewKind

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

	week  weekState
	month monthState

	detail  *detailView
	openURL func(string) error // opens a link in the browser; swapped in tests

	// Popups (form.go, googleflow.go).
	chooser       *int // add a calendar: which source is highlighted
	gflow         *googleFlow
	needsSignIn   map[string]bool // account ID → must sign in again
	form          *addForm
	confirmRemove *app.CalendarInfo
	rename        *renameForm // rename.go
	help          *helpView   // help.go
	removing      bool
	actionErr     error // last add/remove failure outside the form

	syncing  bool
	lastSync time.Time
	// syncFailures is each calendar's last sync failure, kept until that
	// calendar syncs again without one.
	syncFailures map[string]string
	syncErr      error
	pollSeq      int       // the live schedule; older pollMsgs are ignored
	nextPoll     time.Time // when the scheduled sync starts

	// Activity indicator (activity.go).
	spin       spinner.Model
	spinning   bool // the spinner's tick loop is running
	flash      flashKind
	flashUntil time.Time
}

// New builds the UI model. now seeds the clock; after that it advances on a
// 30-second tick.
func New(src Source, loc *time.Location, now time.Time) Model {
	m := Model{
		src:          src,
		keys:         defaultKeyMap(),
		loc:          loc,
		now:          now,
		tick:         30 * time.Second,
		poll:         pollInterval,
		calIndex:     map[string]int{},
		needsSignIn:  map[string]bool{},
		syncFailures: map[string]string{},
		openURL:      openInBrowser,
		hidden:       map[string]bool{},
		days:         initialDays,
		// Init starts both, and the spinner for them; Init can't record that
		// itself.
		loading:  true,
		syncing:  true,
		spin:     newSpinner(),
		spinning: true,
	}
	m.from = m.today()
	return m
}

func (m Model) today() domain.CivilDate {
	return domain.CivilDateFromTime(m.now.In(m.loc))
}

// Init loads calendars and events from the cache and starts a sync.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCalendars(), m.loadEvents(), m.syncCmd(), m.tickCmd(), m.spin.Tick)
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

// schedulePoll replaces any scheduled sync with one m.poll after at.
func (m *Model) schedulePoll(at time.Time) tea.Cmd {
	if m.poll == 0 {
		return nil
	}
	m.pollSeq++
	m.nextPoll = at.Add(m.poll)
	seq := m.pollSeq
	return tea.Tick(m.poll, func(time.Time) tea.Msg { return pollMsg{seq: seq} })
}

func (m Model) tickCmd() tea.Cmd {
	if m.tick == 0 {
		return nil
	}
	return tea.Tick(m.tick, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles one message, then makes sure the activity spinner is
// running if anything is in progress.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(spinner.TickMsg); ok {
		if m.busyLabel() == "" {
			m.spinning = false // let the tick loop end
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(tick)
		return m, cmd
	}
	next, cmd := m.update(msg)
	nm := next.(Model)
	spin := nm.keepSpinning() // mutates nm; must run before nm is returned
	return nm, tea.Batch(cmd, spin)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.rebuildWeek()
		m.rebuildMonth()
		if m.poll != 0 && !m.syncing && !m.nextPoll.IsZero() && !m.now.Before(m.nextPoll) {
			// Overdue: timers stop while the computer sleeps, but the clock
			// doesn't, so a sync due during sleep starts on waking.
			m.syncing = true
			m.pollSeq++
			return m, tea.Batch(m.tickCmd(), m.syncCmd())
		}
		return m, m.tickCmd()

	case calendarsLoadedMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.calendars = msg.rows
		m.calIndex = make(map[string]int, len(msg.rows))
		for i, r := range msg.rows {
			m.calIndex[r.Calendar.ID] = i
		}
		for id := range m.syncFailures {
			if _, ok := m.calIndex[id]; !ok {
				delete(m.syncFailures, id) // the calendar was removed
			}
		}
		m.ensureSidebarSelection()
		m.rebuild()
		m.rebuildWeek()
		m.rebuildMonth()
		return m, nil

	case weekLoadedMsg:
		return m.weekLoaded(msg), nil

	case monthLoadedMsg:
		return m.monthLoaded(msg), nil

	case addDoneMsg:
		return m.addDone(msg)

	case removeDoneMsg:
		return m.removeDone(msg)

	case renameDoneMsg:
		return m.renameDone(msg)

	case openedMsg:
		return m.opened(msg)

	case googleConfiguredMsg, signInStartedMsg, signInDoneMsg, googleCalendarsMsg, googleAddedMsg:
		return m.googleMsg(msg)

	case tea.PasteMsg:
		switch {
		case m.form != nil:
			return m.formPaste(msg)
		case m.rename != nil:
			return m.renamePaste(msg)
		case m.gflow != nil:
			return m.googlePaste(msg)
		}
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

	case flashEndMsg:
		if msg.until.Equal(m.flashUntil) {
			m.flash = flashNone
		}
		return m, nil

	case pollMsg:
		if msg.seq != m.pollSeq {
			return m, nil // r or a newer schedule replaced this one
		}
		if m.syncing {
			// Something else is syncing; try again a full interval later.
			return m, m.schedulePoll(m.now)
		}
		m.syncing = true
		return m, m.syncCmd()

	case syncDoneMsg:
		m.syncing = false
		m.lastSync = msg.at
		var cmds []tea.Cmd
		if !msg.partial {
			// Whatever happened, the next sync is due an interval from now:
			// a failure is retried then, and the calendar works meanwhile
			// from the cache.
			m.syncErr = msg.err
			cmds = append(cmds, m.schedulePoll(msg.at))
			if msg.err == nil {
				m.syncFailures = map[string]string{}
			}
		}
		changed, failed := false, false
		for _, r := range msg.results {
			if r.Err == nil {
				delete(m.needsSignIn, r.Account.ID)
				delete(m.syncFailures, r.Calendar.ID)
				changed = changed || r.Changed
				continue
			}
			failed = true
			m.syncFailures[r.Calendar.ID] = describeSyncFailure(r)
			if r.NeedsSignIn() {
				m.needsSignIn[r.Account.ID] = true
			}
		}
		result := flashOK
		if msg.err != nil || failed {
			result = flashFailed
		}
		cmds = append(cmds, m.startFlash(result, msg.at))
		if !changed {
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, m.loadEvents())
		if m.week.loaded {
			m.week.pendingID = m.selectedWeekEventID()
			m.week.pendingSelect = true
			cmds = append(cmds, m.loadWeek())
		}
		if m.month.loaded {
			cmds = append(cmds, m.loadMonth())
		}
		return m, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.form != nil:
		return m.handleFormKey(msg)
	case m.gflow != nil:
		return m.handleGoogleKey(msg)
	case m.chooser != nil:
		return m.handleChooserKey(msg)
	case m.confirmRemove != nil:
		return m.handleConfirmKey(msg)
	case m.rename != nil:
		return m.handleRenameKey(msg)
	case m.help != nil:
		return m.handleHelpKey(msg)
	case msg.String() == "?":
		m.help = &helpView{}
		return m, nil
	}
	if m.detail != nil {
		return m.handleDetailKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Sync):
		if m.syncing {
			return m, nil
		}
		m.syncing = true
		m.pollSeq++ // the next scheduled sync counts from this one
		return m, m.syncCmd()
	case key.Matches(msg, m.keys.Today):
		switch m.view {
		case viewWeek:
			return m.showWeek(m.today(), "")
		case viewMonth:
			return m.showMonth(m.today())
		}
		return m.jumpTo(m.today())
	case key.Matches(msg, m.keys.ViewAgenda):
		return m.switchView(viewAgenda)
	case key.Matches(msg, m.keys.ViewWeek):
		return m.switchView(viewWeek)
	case key.Matches(msg, m.keys.ViewMonth):
		return m.switchView(viewMonth)
	case key.Matches(msg, m.keys.FocusList):
		m.focus = focusSidebar
		m.ensureSidebarSelection()
		return m, nil
	case key.Matches(msg, m.keys.SwitchPane):
		if m.focus == focusSidebar {
			m.focus = focusMain
		} else {
			m.focus = focusSidebar
			m.ensureSidebarSelection()
		}
		return m, nil
	case key.Matches(msg, m.keys.Add):
		choice := sourceICal
		m.chooser = &choice
		return m, nil
	}

	switch {
	case m.focus == focusSidebar:
		return m.handleSidebarKey(msg)
	case m.view == viewWeek:
		return m.handleWeekKey(msg)
	case m.view == viewMonth:
		return m.handleMonthKey(msg)
	}
	return m.handleMainKey(msg)
}

// switchView changes view, carrying the current day (and selected event, if
// the target view selects events) across, so every view stays on the same
// place in time.
func (m Model) switchView(to viewKind) (tea.Model, tea.Cmd) {
	m.focus = focusMain
	if to == m.view {
		return m, nil
	}
	day, id := m.viewDay()
	switch to {
	case viewWeek:
		return m.showWeek(day, id)
	case viewMonth:
		return m.showMonth(day)
	}
	return m.showAgenda(day, id)
}

// viewDay is the current view's selected day, and its selected event's ID
// ("" in the month view, which selects days).
func (m Model) viewDay() (domain.CivilDate, string) {
	switch m.view {
	case viewWeek:
		return m.selectedWeekDate(), m.selectedWeekEventID()
	case viewMonth:
		return m.month.sel, ""
	}
	return m.currentDay(), m.selectedAgendaEventID()
}

// showAgenda switches to the agenda with day (and eventID, if found) selected,
// reloading from day if it's outside the loaded window.
func (m Model) showAgenda(day domain.CivilDate, eventID string) (tea.Model, tea.Cmd) {
	m.view = viewAgenda
	if !day.Before(m.from) && day.Before(m.from.AddDays(m.days)) {
		m.sel = rowKey(day, eventID)
		if eventID == "" || m.selIndex() < 0 {
			m.selectDay(day)
		}
		m.ensureVisible()
		return m, nil
	}
	m.from, m.days, m.offset, m.sel = day, initialDays, 0, ""
	m.pendingDay, m.hasPending = day, true
	m.loading = true
	return m, m.loadEvents()
}

func (m Model) selectedAgendaEventID() string {
	if i := m.selIndex(); i >= 0 && m.rows[i].kind == rowItem {
		return m.rows[i].item.Event.ID
	}
	return ""
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
			m.openDetail(m.rows[i].item)
		}
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

// bodyHeight is the screen minus the footer: the height of the panels.
func (m Model) bodyHeight() int {
	return m.height - footerHeight
}

// agendaHeight is how many agenda rows fit inside the main panel's border.
func (m Model) agendaHeight() int {
	return m.bodyHeight() - 2
}

// ensureVisible scrolls so the selection is on screen, keeping its day
// header in view when there's room.
func (m *Model) ensureVisible() {
	h := m.agendaHeight()
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

// describeSyncFailure words a calendar's failed sync for the status line.
func describeSyncFailure(r app.SyncOutcome) string {
	if r.NeedsSignIn() {
		return r.Account.DisplayName() + ": sign in again (s on the account)"
	}
	if _, ok := r.Unavailable(); ok {
		if r.Account.Provider == "ical" {
			return r.Label() + ": feed URL no longer works"
		}
		return r.Label() + ": calendar no longer available"
	}
	return fmt.Sprintf("%s: %v", r.Label(), r.Err)
}

// failureList is the calendars' sync failures in sidebar order, each worded
// once (an account that must sign in again fails for all its calendars).
func (m Model) failureList() []string {
	var out []string
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, c := range m.calendars {
		if f, ok := m.syncFailures[c.Calendar.ID]; ok {
			add(f)
		}
	}
	var rest []string
	for id, f := range m.syncFailures {
		if _, ok := m.calIndex[id]; !ok {
			rest = append(rest, f)
		}
	}
	sort.Strings(rest)
	for _, f := range rest {
		add(f)
	}
	return out
}
