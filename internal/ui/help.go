package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The help screen (?) lists every key and interaction, grouped by where it
// works. The footer shows only the keys that fit; this is the full list.
// It opens from the main screen and the event view (popups take ? as text)
// and closes back to where it was opened.

type helpView struct{ scroll int }

type helpEntry struct{ keys, desc string }

type helpSection struct {
	title   string
	entries []helpEntry
}

func helpSections() []helpSection {
	return []helpSection{
		{"Everywhere", []helpEntry{
			{"0", "Calendar list"},
			{"1 2 3 4", "Agenda, week, month, focus"},
			{"tab", "Next panel: the list, the view, the global Write panel"},
			{"t", "Today"},
			{"r", "Sync now (also in the background)"},
			{"a", "Add a calendar: iCal link or Google"},
			{"/", "Filter events by title or location"},
			{"w  W", "Write: the event's panel; the global panel (show, hide)"},
			{"S  ,", "Settings: clock, week, sync, pulse"},
			{"?", "This help"},
			{"q  ctrl+c", "Quit"},
		}},
		{"Filter", []helpEntry{
			{"type", "Narrows as you type; all words match"},
			{"enter", "Keep the filter, move around again"},
			{"esc", "Clear the filter"},
			{"/", "Change the filter"},
			{"", "The agenda searches a year ahead and"},
			{"", "[ ] jump between days with matches"},
		}},
		{"Agenda", []helpEntry{
			{"j k  ↓ ↑", "Next, previous event"},
			{"g G", "First, last event"},
			{"[ ]  pgup pgdn", "Previous, next day"},
			{"enter", "Event details"},
			{"", "Moving past the end loads more days"},
		}},
		{"Week", []helpEntry{
			{"h l  ← →", "Previous, next day"},
			{"j k  ↓ ↑", "Next, previous event of the day"},
			{"g G", "First, last event of the day"},
			{"[ ]  pgup pgdn", "Previous, next week"},
			{"enter", "Event details"},
		}},
		{"Month", []helpEntry{
			{"h l  ← →", "Previous, next day"},
			{"j k  ↓ ↑", "Next, previous week"},
			{"g G", "First, last day of the month"},
			{"[ ]  pgup pgdn", "Previous, next month"},
			{"enter", "Open the day in the agenda"},
		}},
		{"Calendar list", []helpEntry{
			{"j k  g G", "Move"},
			{"space", "Show or hide a calendar, or an account"},
			{"enter", "Fold or unfold an account"},
			{"n", "Rename a calendar or account"},
			{"d", "Remove a calendar (asks first)"},
			{"s", "Sign a Google account in again (⚠)"},
			{"u", "New address for an iCal calendar (⚠)"},
			{"c", "Collapse or expand the list"},
			{"l  →", "Back to the view"},
		}},
		{"Focus", []helpEntry{
			{"tab", "Next panel: list, agenda, heatmap, completed tasks"},
			{"j k  ↓ ↑", "Next, previous day in the heatmap"},
			{"h l  ← →", "Previous, next week"},
			{"shift + move", "Extend the selection (or H J K L)"},
			{"space", "Select by day, week or month"},
			{"esc", "Back to one day"},
			{"pgup pgdn", "Scroll the completed tasks (j k in that panel)"},
			{"t", "Today (the agenda's, when it has focus)"},
			{"", "The agenda works as in view 1: j k, enter, / filter"},
			{"", "A day's strength is how many tasks were completed:"},
			{"", "faint blue 1–3, blue 4–6, bold text color 7+. Archived only."},
		}},
		{"Event details", []helpEntry{
			{"j k  ↓ ↑", "Select a link, or scroll without links"},
			{"1–9", "Select a link by number"},
			{"enter  o", "Open the link in the browser"},
			{"", "The full address shows before opening"},
			{"pgup pgdn  space", "Scroll"},
			{"esc  q", "Close"},
		}},
		{"Write", []helpEntry{
			{"w", "The event's Write panel (event view only)"},
			{"W", "Show the global Write panel, or collapse it"},
			{"j k  g G", "Move"},
			{"a  A", "Add a TODO, a note; enter adds the next"},
			{"e  enter", "Edit"},
			{"space", "Check or uncheck a TODO"},
			{"d", "Delete (asks first)"},
			{"m", "Move a TODO: event ↔ global (global → the open event)"},
			{"z", "Archive a checked TODO"},
			{"Z", "Archive every checked TODO in the panel (end of the day)"},
			{"tab", "Next panel (list, view, global Write)"},
			{"0–3  r  t  /", "Work here too, as in every pane (not in the event view)"},
			{"esc", "Back"},
			{"", "A collapsed global panel flashes when it changes"},
		}},
		{"Settings", []helpEntry{
			{"j k  ↓ ↑", "Move"},
			{"enter  l  →", "Next choice; [ ] marks the active one"},
			{"h  ←", "Previous choice"},
			{"", "custom asks for a number; empty goes back"},
			{"esc  S", "Close"},
		}},
		{"Event pulse", []helpEntry{
			{"", "Off by default; switch on in settings"},
			{"", "Pulses from the lead time until a grace"},
			{"", "period after the start, in every view and"},
			{"", "the event view; the status line counts."},
			{"", "Selecting the event, or a key in its"},
			{"", "event view, stops it."},
		}},
		{"Popups", []helpEntry{
			{"tab  ↓ ↑", "Next, previous field or choice"},
			{"enter", "Next field, or confirm"},
			{"esc", "Cancel"},
			{"space", "Tick a calendar (Google picker)"},
			{"o", "Open the Google sign-in page again"},
			{"r  c", "Retry sign-in, change the OAuth client"},
			{"y n", "Remove, keep (remove popup)"},
			{"", "Pasting works in every text field"},
		}},
		{"Status line", []helpEntry{
			{"left", "Activity: working, synced, failed"},
			{"right", "Last sync, or a failure and its retry"},
			{"⚠", "Sign in again (s) or new address (u)"},
		}},
	}
}

// helpLines lays the sections out in one column, or two when they fit side
// by side in width.
func helpLines(width int) []string {
	sections := helpSections()
	keyW, descW := 0, 0
	for _, s := range sections {
		for _, e := range s.entries {
			keyW = max(keyW, ansi.StringWidth(e.keys))
			descW = max(descW, ansi.StringWidth(e.desc))
		}
	}
	colW := 1 + keyW + 2 + descW
	render := func(s helpSection) []string {
		lines := []string{" " + accentStyle.Render(s.title)}
		for _, e := range s.entries {
			k := e.keys + strings.Repeat(" ", keyW-ansi.StringWidth(e.keys))
			desc := e.desc
			if e.keys == "" {
				desc = mutedStyle.Render(desc)
			}
			lines = append(lines, " "+boldStyle.Render(k)+"  "+desc)
		}
		return append(lines, "")
	}

	const gap = 4
	var all [][]string
	for _, s := range sections {
		all = append(all, render(s))
	}
	join := func(parts [][]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out[:max(len(out)-1, 0)] // no blank line after the last
	}
	if width < 2*colW+gap {
		return join(all)
	}
	// Two columns, sections kept whole, split where the columns come out
	// closest in height.
	split, best := 0, -1
	for k := 1; k < len(all); k++ {
		l, r := len(join(all[:k])), len(join(all[k:]))
		if h := max(l, r); best < 0 || h < best {
			split, best = k, h
		}
	}
	left, right := join(all[:split]), join(all[split:])
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out[i] = l + strings.Repeat(" ", colW+gap-ansi.StringWidth(l)) + r
	}
	return out
}

func (m Model) renderHelp(width, height int) []string {
	lines := helpLines(width - 2)
	bodyH := max(height-2, 0)
	scroll := min(m.help.scroll, max(0, len(lines)-bodyH))
	end := min(scroll+bodyH, len(lines))
	position := ""
	if len(lines) > bodyH && bodyH > 0 {
		position = fmt.Sprintf("lines %d–%d of %d", scroll+1, end, len(lines))
	}
	return panel(width, height, frame{focused: true, tabs: []tab{{label: "Keys", active: true}}, bottom: position}, lines[scroll:end])
}

func (m Model) handleHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	page := max(m.height-footerHeight-3, 1)
	h := m.help
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "?":
		m.help = nil
		return m, nil
	case "j", "down":
		h.scroll++
	case "k", "up":
		h.scroll--
	case "pgdown", "ctrl+d", "space":
		h.scroll += page
	case "pgup", "ctrl+u":
		h.scroll -= page
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		h.scroll = len(helpLines(m.width - 2))
	}
	// Keep scroll within what can be shown, so scrolling back up responds
	// at once.
	h.scroll = min(max(h.scroll, 0), max(0, len(helpLines(m.width-2))-(m.height-footerHeight-2)))
	return m, nil
}
