package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

// The guests are part of the event view's scrollable body: a summary of the
// answers, then one row each. Up to shortGuestList of them sit before the
// description; more go after it. A big meeting can have hundreds, so `/`
// finds a person: it narrows the list to the guests whose name or address
// contains each typed word, as you type, and scrolls to it. enter keeps the
// find and esc clears it. The rows show names; the address is shown only for
// a guest without one, but it is matched too.

const shortGuestList = 8

// responseMark is the glyph for an answer. Each is different, so the
// answers can be told apart without color.
func responseMark(r domain.Response) string {
	switch r {
	case domain.ResponseYes:
		return successStyle.Render("✓")
	case domain.ResponseNo:
		return errorStyle.Render("✗")
	case domain.ResponseMaybe:
		return "?"
	}
	return mutedStyle.Render("·")
}

// guestSummary counts the answers: "3 yes · 1 maybe · 1 no · 2 no reply".
func guestSummary(guests []domain.Guest) string {
	var n [4]int
	for _, g := range guests {
		n[g.Response]++
	}
	var parts []string
	for _, p := range []struct {
		r    domain.Response
		name string
	}{{domain.ResponseYes, "yes"}, {domain.ResponseMaybe, "maybe"}, {domain.ResponseNo, "no"}, {domain.ResponseNone, "no reply"}} {
		if n[p.r] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[p.r], p.name))
		}
	}
	return strings.Join(parts, " · ")
}

// matchGuests is the guests that contain every word of find, case-
// insensitively, in their name or address; all of them for an empty find.
func matchGuests(guests []domain.Guest, find string) []domain.Guest {
	terms := strings.Fields(strings.ToLower(find))
	if len(terms) == 0 {
		return guests
	}
	var out []domain.Guest
	for _, g := range guests {
		text := strings.ToLower(g.Name + "\n" + g.Email)
		ok := true
		for _, t := range terms {
			ok = ok && strings.Contains(text, t)
		}
		if ok {
			out = append(out, g)
		}
	}
	return out
}

// guestLines is the Guests section: a blank line, the heading, then a row
// per guest, each already limited to width textW (plus the margin).
func (m Model) guestLines(textW int) []string {
	all := m.detail.Event.Guests
	shown := matchGuests(all, m.detail.find)
	head := " " + boldStyle.Render("Guests") + "  "
	switch {
	case m.detail.find != "":
		head += mutedStyle.Render(fmt.Sprintf("%d of %d match “%s”", len(shown), len(all), m.detail.find))
	case len(all) == 1:
		head += mutedStyle.Render("1")
	default:
		head += mutedStyle.Render(fmt.Sprintf("%d: %s", len(all), guestSummary(all)))
	}
	lines := []string{"", fit(head, textW+1)}
	if len(shown) == 0 {
		return append(lines, " "+mutedStyle.Render("Nobody matches. esc clears the find."))
	}
	for _, g := range shown {
		var tags []string
		if g.Organizer {
			tags = append(tags, "organizer")
		}
		if g.Self {
			tags = append(tags, "you")
		}
		if g.Optional {
			tags = append(tags, "optional")
		}
		tag := ""
		if len(tags) > 0 {
			tag = "  " + mutedStyle.Render(strings.Join(tags, ", "))
		}
		name := ansi.Truncate(g.Display(), max(textW-4-ansi.StringWidth(strings.Join(tags, ", "))-2, 8), "…")
		lines = append(lines, fit("  "+responseMark(g.Response)+" "+name+tag, textW+1))
	}
	return lines
}

// guestsLine is the body line of the Guests heading, 0 when there is none.
func (m Model) guestsLine(textW int) int {
	for i, l := range m.detailBody(textW) {
		if strings.HasPrefix(ansi.Strip(l), " Guests") {
			return i
		}
	}
	return 0
}

func newFindInput(value string) textinput.Model {
	in := textinput.New()
	in.SetStyles(inputStyles())
	in.Prompt = ""
	in.CharLimit = 100
	in.Placeholder = "a name or an address"
	in.SetValue(value)
	in.CursorEnd()
	return in
}

func (m Model) startFind() (tea.Model, tea.Cmd) {
	in := newFindInput(m.detail.find)
	m.findInput = &in
	return m, in.Focus()
}

// setFind narrows the guests and brings them into view.
func (m Model) setFind(value string) Model {
	m.detail.find = strings.TrimSpace(value)
	m.detail.scroll = m.guestsLine(m.detailTextWidth())
	m.clampDetailScroll()
	return m
}

func (m Model) handleFindKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.findInput = nil
		return m.setFind(""), nil
	case "enter":
		m.findInput = nil
		return m, nil
	}
	var cmd tea.Cmd
	*m.findInput, cmd = m.findInput.Update(msg)
	return m.setFind(m.findInput.Value()), cmd
}

func (m Model) findPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	*m.findInput, cmd = m.findInput.Update(msg)
	return m.setFind(m.findInput.Value()), cmd
}

// findLine replaces the footer's hints while typing a find.
func (m Model) findLine() string {
	in := *m.findInput
	in.SetWidth(max(min(40, m.width-34), 10))
	return accentStyle.Render("find guest ") + in.View() + "  " + renderHints([]footHint{
		{key: "enter", desc: "keep"}, {key: "esc", desc: "clear"},
	})
}
