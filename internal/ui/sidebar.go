package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/app"
)

// The sidebar lists calendars grouped by account. Account headers can be
// folded; the list scrolls to keep the cursor visible. When the sidebar is
// collapsed to a strip, only calendar swatches show (folds are ignored).

type sidebarState struct {
	sel    string          // key of the selected row
	offset int             // first visible row
	folded map[string]bool // account ID → folded
}

type sideKind int

const (
	sideAccount sideKind = iota
	sideCalendar
)

type sideRow struct {
	kind    sideKind
	account string // account ID
	label   string // account label, for headers
	count   int    // calendars in the account, for headers
	cal     app.CalendarInfo
}

func (r sideRow) key() string {
	if r.kind == sideAccount {
		return "account|" + r.account
	}
	return "calendar|" + r.cal.Calendar.ID
}

// sidebarRows lists what the sidebar shows, top to bottom.
func (m Model) sidebarRows() []sideRow {
	var rows []sideRow
	for i, c := range m.calendars {
		if m.sidebarCollapsed {
			rows = append(rows, sideRow{kind: sideCalendar, account: c.Account.ID, cal: c})
			continue
		}
		if i == 0 || m.calendars[i-1].Account.ID != c.Account.ID {
			count := 0
			for _, o := range m.calendars {
				if o.Account.ID == c.Account.ID {
					count++
				}
			}
			rows = append(rows, sideRow{kind: sideAccount, account: c.Account.ID, label: c.Account.Label, count: count})
		}
		if !m.side.folded[c.Account.ID] {
			rows = append(rows, sideRow{kind: sideCalendar, account: c.Account.ID, cal: c})
		}
	}
	return rows
}

func (m Model) sidebarIndex(rows []sideRow) int {
	for i, r := range rows {
		if r.key() == m.side.sel {
			return i
		}
	}
	return -1
}

// ensureSidebarSelection keeps the cursor on a row that exists: the same row
// if possible, else its account's header (after folding or collapsing), else
// the first row.
func (m *Model) ensureSidebarSelection() {
	rows := m.sidebarRows()
	if m.sidebarIndex(rows) >= 0 || len(rows) == 0 {
		return
	}
	if calID, ok := strings.CutPrefix(m.side.sel, "calendar|"); ok {
		for _, c := range m.calendars {
			if c.Calendar.ID == calID {
				for _, r := range rows {
					if r.account == c.Account.ID {
						m.side.sel = r.key()
						return
					}
				}
			}
		}
	}
	m.side.sel = rows[0].key()
}

// selectedSidebarRow is the row under the cursor, if any.
func (m Model) selectedSidebarRow() (sideRow, bool) {
	rows := m.sidebarRows()
	if i := m.sidebarIndex(rows); i >= 0 {
		return rows[i], true
	}
	return sideRow{}, false
}

func (m Model) handleSidebarKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := m.sidebarRows()
	cur := max(m.sidebarIndex(rows), 0)
	move := func(i int) {
		if len(rows) > 0 {
			m.side.sel = rows[min(max(i, 0), len(rows)-1)].key()
		}
	}

	switch {
	case key.Matches(msg, m.keys.Down):
		move(cur + 1)
	case key.Matches(msg, m.keys.Up):
		move(cur - 1)
	case key.Matches(msg, m.keys.Top):
		move(0)
	case key.Matches(msg, m.keys.Bottom):
		move(len(rows) - 1)
	case key.Matches(msg, m.keys.Collapse):
		m.sidebarCollapsed = !m.sidebarCollapsed
		m.ensureSidebarSelection()
	case key.Matches(msg, m.keys.FocusMain):
		m.focus = focusMain
	case key.Matches(msg, m.keys.Remove):
		if r, ok := m.selectedSidebarRow(); ok && r.kind == sideCalendar {
			cal := r.cal
			m.confirmRemove = &cal
		}
	case len(rows) == 0:
	case rows[cur].kind == sideAccount && msg.String() == "enter":
		if m.side.folded == nil {
			m.side.folded = map[string]bool{}
		}
		m.side.folded[rows[cur].account] = !m.side.folded[rows[cur].account]
	case key.Matches(msg, m.keys.Toggle):
		m.toggleVisibility(rows[cur])
	}
	m.ensureSidebarVisible()
	return m, nil
}

// toggleVisibility shows or hides a calendar, or every calendar of an
// account: if any of them is shown, all are hidden, otherwise all are shown.
func (m *Model) toggleVisibility(r sideRow) {
	if r.kind == sideCalendar {
		id := r.cal.Calendar.ID
		m.hidden[id] = !m.hidden[id]
	} else {
		anyShown := false
		for _, c := range m.calendars {
			if c.Account.ID == r.account && !m.hidden[c.Calendar.ID] {
				anyShown = true
			}
		}
		for _, c := range m.calendars {
			if c.Account.ID == r.account {
				m.hidden[c.Calendar.ID] = anyShown
			}
		}
	}
	m.rebuild()
	m.rebuildWeek()
	m.rebuildMonth()
}

func (m Model) sidebarHeight() int {
	return m.bodyHeight() - 2
}

// ensureSidebarVisible scrolls the sidebar so the cursor is on screen.
func (m *Model) ensureSidebarVisible() {
	h := m.sidebarHeight()
	if h <= 0 {
		return
	}
	rows := m.sidebarRows()
	if i := m.sidebarIndex(rows); i >= 0 {
		m.side.offset = min(m.side.offset, i)
		if i >= m.side.offset+h {
			m.side.offset = i - h + 1
		}
	}
	m.side.offset = min(max(m.side.offset, 0), max(0, len(rows)-h))
}

// renderSidebar draws the calendar list panel, height lines tall.
func (m Model) renderSidebar(height int) []string {
	width := sidebarInner
	var tabs []tab
	if m.sidebarCollapsed {
		width = collapsedSidebarInner
	} else {
		tabs = []tab{{label: "Calendars", active: true}}
	}
	focused := m.focus == focusSidebar
	innerH := height - 2

	rows := m.sidebarRows()
	if len(rows) == 0 && !m.sidebarCollapsed {
		return panel(width+2, height, frame{focused: focused, tabs: tabs}, []string{
			mutedStyle.Render(" No calendars yet."),
			"",
			" " + boldStyle.Render("a") + mutedStyle.Render(" add one"),
		})
	}

	sel := m.sidebarIndex(rows)
	end := min(m.side.offset+innerH, len(rows))
	var lines []string
	for i := m.side.offset; i < end; i++ {
		lines = append(lines, m.renderSidebarRow(rows[i], width, focused && i == sel))
	}

	position := ""
	if !m.sidebarCollapsed && len(rows) > innerH && innerH > 0 {
		position = fmt.Sprintf("%d of %d", sel+1, len(rows))
	}
	return panel(width+2, height, frame{focused: focused, tabs: tabs, bottom: position}, lines)
}

func (m Model) renderSidebarRow(r sideRow, width int, selected bool) string {
	if r.kind == sideAccount {
		marker := "▾"
		if m.side.folded[r.account] {
			marker = "▸"
		}
		text := fmt.Sprintf(" %s %s (%d)", marker, r.label, r.count)
		if selected {
			return selectedStyle.Render(fit(text, width))
		}
		return fit(" "+marker+" "+boldStyle.Render(r.label)+mutedStyle.Render(fmt.Sprintf(" (%d)", r.count)), width)
	}

	hidden := m.hidden[r.cal.Calendar.ID]
	slot := r.cal.Calendar.ColorSlot
	swatch := calendarGlyph(slot, hidden)
	text := "   " + swatch + " " + r.cal.Calendar.Name
	if m.sidebarCollapsed {
		text = " " + swatch + " "
	}
	switch {
	case selected:
		return selectedStyle.Render(fit(text, width))
	case hidden:
		return mutedStyle.Render(fit(text, width))
	}
	colored := calendarBullet(slot)
	return fit(strings.Replace(text, swatch, colored, 1), width)
}
