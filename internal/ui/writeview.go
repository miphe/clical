package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// Layout and drawing of the Write panels.

const (
	stripW        = 3   // the collapsed global panel
	splitMinWidth = 100 // the event view puts its panels side by side from here
	itemPrefixW   = 4   // "[ ] " or the indent of a note
	mainMinWidth  = 40  // what the main pane keeps when the global panel opens
	eventMinWidth = 60  // and what the event view keeps
)

// globalWidth is the global panel's width in avail columns, leaving at least
// rest for what's beside it. When it can't open without crowding that out it
// stays a strip.
func (m Model) globalWidth(avail, rest int) int {
	if !m.globalOpen {
		return stripW
	}
	w := min(max(avail/4, 28), 40)
	w = min(w, avail-rest)
	if w < 20 {
		return stripW
	}
	return w
}

// globalRect is the global panel's size.
func (m Model) globalRect() (w, h int) {
	h = m.bodyHeight()
	if m.detail != nil {
		return m.globalWidth(m.width, eventMinWidth), h
	}
	side := sidebarInner + 2
	if m.sidebarCollapsed {
		side = collapsedSidebarInner + 2
	}
	return m.globalWidth(m.width-side, mainMinWidth), h
}

type eventLayout struct {
	detailW, detailH int
	writeW, writeH   int
	stacked          bool
}

// eventLayout splits the event view between the event's details and its
// Write panel: side by side when wide, else the Write panel under the
// details. The details keep the larger share, since the link address needs
// room.
func (m Model) eventLayout() eventLayout {
	gw, _ := m.globalRect()
	rest, h := m.width-gw, m.bodyHeight()
	if rest >= splitMinWidth {
		ww := min(max(rest*2/5, 34), 60)
		return eventLayout{detailW: rest - ww, detailH: h, writeW: ww, writeH: h}
	}
	wh := min(max(h/3, 5), 12)
	wh = min(wh, max(h-8, 3))
	return eventLayout{detailW: rest, detailH: h - wh, writeW: rest, writeH: wh, stacked: true}
}

// writeInner is a panel's text area.
func (m Model) writeInner(which writeWhich) (w, h int) {
	if which == writeEvent {
		l := m.eventLayout()
		return max(l.writeW-2, 1), max(l.writeH-2, 1)
	}
	gw, gh := m.globalRect()
	return max(gw-2, 1), max(gh-2, 1)
}

// wrapped is an item's text broken to width, the first line after the
// prefix and the others indented to match.
func wrapped(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(ansi.Wordwrap(text, max(width, 1), ""), max(width, 1), false), "\n")
}

// itemLines is what an item draws: its text, then where it came from if that
// isn't here. Unstyled, so counting lines and drawing agree.
func (p writePanel) itemLines(it write.Item, inner int) (text []string, origin string) {
	text = wrapped(it.Text, inner-itemPrefixW-1)
	if it.Todo && !it.Origin.Same(p.scope) {
		origin = "from " + refName(it.Origin)
	}
	return text, origin
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

// blockSpan is the lines [top, bottom) that the item at position idx takes.
func (p writePanel) blockSpan(idx, inner int) (top, bottom int) {
	line := 0
	for n, i := range p.rows() {
		text, origin := p.itemLines(p.list.Items[i], inner)
		h := len(text)
		if origin != "" {
			h++
		}
		if n == idx {
			return line, line + h
		}
		line += h
	}
	return line, line
}

func (m Model) writeFocused(which writeWhich) bool {
	return (which == writeEvent && m.focus == focusEventWrite) || (which == writeGlobal && m.focus == focusGlobalWrite)
}

// renderWritePanel draws one Write panel in width×height.
func (m Model) renderWritePanel(which writeWhich, width, height int) []string {
	p := *(&m).panel(which)
	focused := m.writeFocused(which)
	inner, innerH := max(width-2, 1), max(height-2, 1)

	tabs := []tab{{label: "Write", active: true}}
	right := "global"
	if which == writeEvent {
		right = "this event"
	}
	bottom := ""
	switch p.mode {
	case wInsert:
		kind := "TODO"
		if p.addNote || p.editAt >= 0 {
			kind = "note"
		}
		tabs = append(tabs, tab{label: "INSERT " + kind, active: true})
	case wConfirm:
		tabs = append(tabs, tab{label: "Delete? y/n", active: true})
	}
	f := frame{focused: focused, tabs: tabs, right: right}

	if m.writes == nil {
		f.right = ""
		return panel(width, height, f, []string{mutedStyle.Render(" Notes aren't available.")})
	}

	listH := innerH
	if p.mode == wInsert {
		listH--
	}
	if p.note != "" {
		listH-- // room for what the last key couldn't do
	}
	rows := p.rows()
	var lines []string
	for n, i := range rows {
		it := p.list.Items[i]
		selected := focused && n == p.sel
		text, origin := p.itemLines(it, inner)
		for k, l := range text {
			lines = append(lines, writeLine(it, k, l, selected, inner))
		}
		if origin != "" {
			lines = append(lines, "  "+strings.Repeat(" ", itemPrefixW-2)+mutedStyle.Render(fit(origin, inner-itemPrefixW-1)))
		}
	}
	if len(rows) == 0 && p.loaded {
		lines = []string{mutedStyle.Render(" Nothing yet."), mutedStyle.Render(" a adds a TODO, A a note.")}
	}
	if p.offset < len(lines) {
		lines = lines[p.offset:]
	} else {
		lines = nil
	}
	lines = lines[:min(len(lines), max(listH, 0))]
	for len(lines) < listH {
		lines = append(lines, "")
	}
	if p.note != "" {
		lines = append(lines, " "+accentStyle.Render(fit(p.note, inner-2)))
	}
	if p.mode == wInsert {
		in := *p.input
		in.SetWidth(max(inner-4, 4))
		lines = append(lines, " "+accentStyle.Render("› ")+in.View())
	}

	if len(rows) > 1 {
		bottom = fmt.Sprintf("%d of %d", min(p.sel+1, len(rows)), len(rows))
	}
	f.bottom = bottom
	return panel(width, height, f, lines)
}

// writeLine is line k of an item's text.
func writeLine(it write.Item, k int, text string, selected bool, inner int) string {
	prefix := strings.Repeat(" ", itemPrefixW)
	if k == 0 && it.Todo {
		prefix = "[ ] "
		if it.Done {
			prefix = "[x] "
		}
	}
	line := " " + prefix + text
	switch {
	case selected:
		return selectedStyle.Render(fit(line, inner))
	case it.Done:
		return doneStyle.Render(fit(line, inner))
	}
	return fit(line, inner)
}

// renderGlobalPanel is the global panel: expanded, or a strip that says
// "Write" down its side with the number of open TODOs under it.
func (m Model) renderGlobalPanel(width, height int) []string {
	if m.globalOpen && width > stripW {
		return m.renderWritePanel(writeGlobal, width, height)
	}
	open := 0
	for _, it := range m.globalWrite.list.Items {
		if it.Todo && !it.Done {
			open++
		}
	}
	var lines []string
	for _, r := range "Write" {
		lines = append(lines, string(r))
	}
	if open > 0 {
		count := "9"
		if open < 9 {
			count = fmt.Sprint(open)
		}
		lines = append(lines, "", accentStyle.Render(count))
	}
	return panel(width, height, frame{focused: m.focus == focusGlobalWrite, flash: m.globalFlash}, lines)
}

// renderEventView draws the event view: the details and the event's Write
// panel, and the global panel beside them.
func (m Model) renderEventView(width, height int) []string {
	lay := m.eventLayout()
	detail := m.renderDetail(lay.detailW, lay.detailH)
	wp := m.renderWritePanel(writeEvent, lay.writeW, lay.writeH)
	var core []string
	if lay.stacked {
		core = append(detail, wp...)
	} else {
		for i := range detail {
			core = append(core, detail[i]+wp[i])
		}
	}
	gw, _ := m.globalRect()
	glob := m.renderGlobalPanel(gw, height)
	out := make([]string, height)
	for i := range out {
		out[i] = core[i] + glob[i]
	}
	return out
}
