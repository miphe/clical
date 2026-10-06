package ui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"
)

// The settings dialog (S, or ,) is a screen like the help one. Every setting
// is a row of choices with the active one marked: enter (or l) moves to the
// next, h to the previous. Where a number of your own makes sense, the last
// choice is "custom", which asks for it. Changes apply at once and are saved
// by Settings.Save; a failure is shown in the status line.

// Prefs are the settings the dialog changes. config.Prefs has the same
// fields, in the same order.
type Prefs struct {
	Clock12      bool
	WeekStart    time.Weekday
	SyncInterval time.Duration
	Pulse        bool
	PulseLead    time.Duration
	PulseGrace   time.Duration // or graceEvent
	PanelFlash   bool
	Write        bool
}

type settingsView struct {
	row    int
	input  *textinput.Model // typing a custom number
	picked int              // index of the "custom" choice being filled in
	note   string           // what was wrong with it
}

type prefsSavedMsg struct{ err error }

func (m Model) prefs() Prefs {
	return Prefs{
		Clock12:      m.clock.Twelve,
		WeekStart:    m.weekStart,
		SyncInterval: m.poll,
		Pulse:        m.pulse.on,
		PulseLead:    m.pulse.lead,
		PulseGrace:   m.pulse.grace,
		PanelFlash:   m.panelFlash,
		Write:        m.writeOn,
	}
}

// saveCmd saves the settings, if the program was given a way to.
func (m Model) saveCmd() tea.Cmd {
	save, p := m.save, m.prefs()
	if save == nil {
		return nil
	}
	return func() tea.Msg { return prefsSavedMsg{err: save(p)} }
}

// A settingRow is one setting: its choices, and how to apply one.
type settingRow struct {
	label, hint string
	choices     []string // the fixed choices, in order
	current     int      // the active choice; len(choices) means custom
	// A custom number: its unit, bounds, current value if it is the active
	// choice, and how to show and apply it. unit 0 means no custom choice.
	unit     time.Duration
	min, max int
	custom   int
	apply    func(m *Model, choice int) tea.Cmd // a fixed choice
	applyN   func(m *Model, n int) tea.Cmd      // a custom number
}

func (r settingRow) options() int {
	if r.unit != 0 {
		return len(r.choices) + 1
	}
	return len(r.choices)
}

func minutesLabel(d time.Duration) string {
	if d%time.Hour == 0 {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

// pick finds d among presets, or the custom index.
func pick(d time.Duration, presets ...time.Duration) int {
	for i, p := range presets {
		if d == p {
			return i
		}
	}
	return len(presets)
}

func (m Model) settingRows() []settingRow {
	twelve := 0
	if m.clock.Twelve {
		twelve = 1
	}
	week := 0
	if m.weekStart == time.Sunday {
		week = 1
	}
	pulseOn := 0
	if m.pulse.on {
		pulseOn = 1
	}
	syncs := []time.Duration{5 * time.Minute, 10 * time.Minute, 30 * time.Minute, 0}
	leads := []time.Duration{10 * time.Minute, 5 * time.Minute, time.Minute}
	graces := []time.Duration{10 * time.Minute, 5 * time.Minute, time.Minute, graceEvent}

	syncCur := pick(m.poll, syncs...)
	customSync := 0
	if syncCur == len(syncs) {
		customSync = int(m.poll / time.Minute)
	}
	leadCur := pick(m.pulse.lead, leads...)
	customLead := 0
	if leadCur == len(leads) {
		customLead = int(m.pulse.lead / time.Second)
	}
	graceCur := pick(m.pulse.grace, graces...)
	customGrace := 0
	if graceCur == len(graces) {
		customGrace = int(m.pulse.grace / time.Minute)
	}

	writeOn := 0
	if m.writeOn {
		writeOn = 1
	}
	flash := 0
	if m.panelFlash {
		flash = 1
	}
	return []settingRow{
		{label: "Time format", hint: "12-hour is 3:04pm, 24-hour is 15:04",
			choices: []string{"24h", "12h"}, current: twelve,
			apply: func(m *Model, c int) tea.Cmd { m.clock = agenda.Clock{Twelve: c == 1}; return nil }},
		{label: "Week starts on", hint: "Week view and month grid",
			choices: []string{"Monday", "Sunday"}, current: week,
			apply: func(m *Model, c int) tea.Cmd { return m.setWeekStart([]time.Weekday{time.Monday, time.Sunday}[c]) }},
		{label: "Sync interval", hint: "Between background syncs; r always syncs",
			choices: []string{"5m", "10m", "30m", "off"}, current: syncCur,
			unit: time.Minute, min: 1, max: 1440, custom: customSync,
			apply:  func(m *Model, c int) tea.Cmd { m.setSyncInterval(syncs[c]); return nil },
			applyN: func(m *Model, n int) tea.Cmd { m.setSyncInterval(time.Duration(n) * time.Minute); return nil }},
		{label: "Event pulse", hint: "An event pulses as it starts; the status line counts down",
			choices: []string{"off", "on"}, current: pulseOn,
			apply: func(m *Model, c int) tea.Cmd { m.pulse.on = c == 1; return nil }},
		{label: "Pulse starts", hint: "Before the event starts; custom is in seconds",
			choices: []string{"10m", "5m", "1m"}, current: leadCur,
			unit: time.Second, min: 1, max: 3600, custom: customLead,
			apply:  func(m *Model, c int) tea.Cmd { m.pulse.lead = leads[c]; return nil },
			applyN: func(m *Model, n int) tea.Cmd { m.pulse.lead = time.Duration(n) * time.Second; return nil }},
		{label: "Pulse continues", hint: "After it has started, for late arrivals; custom is in minutes",
			choices: []string{"10m", "5m", "1m", "whole event"}, current: graceCur,
			unit: time.Minute, min: 0, max: 1440, custom: customGrace,
			apply:  func(m *Model, c int) tea.Cmd { m.pulse.grace = graces[c]; return nil },
			applyN: func(m *Model, n int) tea.Cmd { m.pulse.grace = time.Duration(n) * time.Minute; return nil }},
		{label: "Write", hint: "Notes and TODOs, on events and globally; off hides it all (the files stay)",
			choices: []string{"off", "on"}, current: writeOn,
			apply: func(m *Model, c int) tea.Cmd { return m.setWrite(c == 1) }},
		{label: "Panel flash", hint: "A collapsed panel's border flashes once when its content changes",
			choices: []string{"off", "on"}, current: flash,
			apply: func(m *Model, c int) tea.Cmd { m.panelFlash = c == 1; return nil }},
	}
}

// setWeekStart changes the first day of the week and regrids the week and
// month views that are loaded, keeping their selected day.
func (m *Model) setWeekStart(d time.Weekday) tea.Cmd {
	if m.weekStart == d {
		return nil
	}
	var cmds []tea.Cmd
	if m.week.loaded {
		day := m.selectedWeekDate()
		m.weekStart = d
		m.week.start = agenda.WeekStart(day, d)
		m.week.day = daysBetween(m.week.start, day)
		m.week.pendingSelect, m.week.pendingID = true, ""
		m.week.loaded, m.week.loading = false, true
		cmds = append(cmds, m.loadWeek())
	}
	if m.month.loaded {
		m.weekStart = d
		m.month.start, m.month.weeks = agenda.MonthGrid(m.month.month, d)
		m.month.loaded, m.month.loading = false, true
		cmds = append(cmds, m.loadMonth())
	}
	m.weekStart = d
	return tea.Batch(cmds...)
}

// setSyncInterval changes the time between background syncs. The schedule
// restarts from now, or stops when off.
func (m *Model) setSyncInterval(d time.Duration) {
	m.poll = d
	m.pollSeq++ // the old schedule is dead
	m.nextPoll = time.Time{}
}

func (m Model) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	if s.input != nil {
		return m.handleCustomInput(msg)
	}
	rows := m.settingRows()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "S", ",":
		m.settings = nil
		return m, nil
	case "j", "down", "tab":
		s.row = (s.row + 1) % len(rows)
	case "k", "up", "shift+tab":
		s.row = (s.row + len(rows) - 1) % len(rows)
	case "enter", "space", "l", "right":
		return m.step(rows[s.row], +1)
	case "h", "left":
		return m.step(rows[s.row], -1)
	}
	return m, nil
}

// step moves the row to its next (or previous) choice.
func (m Model) step(r settingRow, dir int) (tea.Model, tea.Cmd) {
	n := r.options()
	next := (r.current + dir + n) % n
	if r.unit != 0 && next == len(r.choices) {
		return m.askCustom(r, next)
	}
	load := r.apply(&m, next)
	return m, tea.Batch(load, m.settingsChanged())
}

// settingsChanged saves and refreshes whatever depends on the settings.
func (m *Model) settingsChanged() tea.Cmd {
	cmds := []tea.Cmd{m.saveCmd(), m.pulseRefresh()}
	if m.poll != 0 && !m.syncing {
		cmds = append(cmds, m.schedulePoll(m.now))
	}
	return tea.Batch(cmds...)
}

func (m Model) askCustom(r settingRow, choice int) (tea.Model, tea.Cmd) {
	in := textinput.New()
	in.SetStyles(inputStyles())
	in.Prompt = ""
	in.CharLimit = 4
	if r.custom != 0 {
		in.SetValue(strconv.Itoa(r.custom))
		in.CursorEnd()
	}
	m.settings.input, m.settings.picked, m.settings.note = &in, choice, ""
	return m, in.Focus()
}

func (m Model) handleCustomInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	r := m.settingRows()[s.row]
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		s.input, s.note = nil, ""
		return m, nil
	case "enter":
		v := strings.TrimSpace(s.input.Value())
		if v == "" { // nothing typed: back to the first choice
			s.input, s.note = nil, ""
			load := r.apply(&m, 0)
			return m, tea.Batch(load, m.settingsChanged())
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < r.min || n > r.max {
			s.note = "Whole number, " + strconv.Itoa(r.min) + "–" + strconv.Itoa(r.max)
			return m, nil
		}
		s.input, s.note = nil, ""
		load := r.applyN(&m, n)
		return m, tea.Batch(load, m.settingsChanged())
	}
	if t := msg.Text; t != "" && strings.Trim(t, "0123456789") != "" {
		return m, nil // digits only
	}
	var cmd tea.Cmd
	*s.input, cmd = s.input.Update(msg)
	return m, cmd
}

func unitName(u time.Duration) string {
	if u == time.Second {
		return "seconds"
	}
	return "minutes"
}

func (m Model) renderSettings(width, height int) []string {
	s := m.settings
	rows := m.settingRows()
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, ansi.StringWidth(r.label))
	}

	lines := []string{""}
	for i, r := range rows {
		cursor := "  "
		label := r.label
		if i == s.row {
			cursor, label = accentStyle.Render("› "), boldStyle.Render(r.label)
		}
		label += strings.Repeat(" ", labelW-ansi.StringWidth(r.label))

		var opts []string
		for c, text := range r.choices {
			opts = append(opts, choiceText(text, c == r.current))
		}
		if r.unit != 0 {
			idx := len(r.choices)
			switch {
			case s.input != nil && i == s.row:
				in := *s.input
				in.SetWidth(5)
				text := accentStyle.Render("[") + in.View() + accentStyle.Render("]") + " " + unitName(r.unit)
				if s.note != "" {
					text += "  " + errorStyle.Render(s.note)
				}
				opts = append(opts, text)
			case r.current == idx:
				opts = append(opts, choiceText(customLabel(r), true))
			default:
				opts = append(opts, choiceText("custom", false))
			}
		}
		lines = append(lines, " "+cursor+label+"   "+strings.Join(opts, "  "))
		hint := ""
		if i == s.row {
			hint = mutedStyle.Render(r.hint)
		}
		lines = append(lines, " "+strings.Repeat(" ", 2+labelW+3)+hint, "")
	}
	lines = append(lines, " "+mutedStyle.Render("Changes apply at once and are saved."))
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width-2, "…")
	}
	return panel(width, height, frame{focused: true, tabs: []tab{{label: "Settings", active: true}}}, lines)
}

// choiceText marks the active choice: bracketed, bold and in the accent
// color. The others are plain and muted.
func choiceText(text string, active bool) string {
	if active {
		return accentStyle.Render("[" + text + "]")
	}
	return mutedStyle.Render(" " + text + " ")
}

// customLabel is the active custom value, e.g. 7m or 90s.
func customLabel(r settingRow) string {
	return minutesLabel(time.Duration(r.custom) * r.unit)
}

// setWrite turns the whole Write feature on or off. Off hides the panels and
// their keys and stops reading the files, which are left as they are.
func (m *Model) setWrite(on bool) tea.Cmd {
	m.writeOn = on
	if !on {
		m.globalFlash = false
		if m.focus == focusEventWrite || m.focus == focusGlobalWrite {
			m.focus = m.mainFocus()
		}
		m.eventWrite.mode, m.globalWrite.mode = wNormal, wNormal
		return nil
	}
	cmds := []tea.Cmd{m.loadWrite(writeGlobal)}
	if m.detail != nil {
		cmds = append(cmds, m.loadWrite(writeEvent))
	}
	return tea.Batch(cmds...)
}
