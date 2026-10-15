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
	itemPrefixW   = 5   // "[ ] " or the indent of a note
	mainMinWidth  = 40  // what the main pane keeps when the global panel opens
	eventMinWidth = 60  // and what the event view keeps
	detailRoom    = 80  // the event details are done growing at this width
)

// globalWidth is the global panel's width in avail columns, leaving at least
// rest for what's beside it. When it can't open without crowding that out it
// stays a strip.
func (m Model) globalWidth(avail, rest int) int {
	if !m.globalOpen {
		return stripW
	}
	w := min(max(avail/2, 56), 80)
	w = min(w, avail-rest)
	if w < 20 {
		return stripW
	}
	return w
}

// equalWrites is the width each of the two Write panels gets in the event
// view when the global one is open: they share what the details leave, which
// is half the screen, within the room the details need. ok is false when
// there's no room for that, and the usual layout applies.
func (m Model) equalWrites() (each int, ok bool) {
	if !m.writeOn || !m.globalOpen || m.detail == nil {
		return 0, false
	}
	details := min(max(m.width/2, eventMinWidth), detailRoom)
	each = min((m.width-details)/2, 80)
	return each, each >= 28
}

// globalRect is the global panel's size.
func (m Model) globalRect() (w, h int) {
	h = m.bodyHeight()
	if !m.writeOn {
		return 0, h
	}
	if m.detail != nil {
		if each, ok := m.equalWrites(); ok {
			return each, h
		}
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
	if !m.writeOn {
		return eventLayout{detailW: m.width, detailH: m.bodyHeight(), stacked: true}
	}
	gw, _ := m.globalRect()
	rest, h := m.width-gw, m.bodyHeight()
	if each, ok := m.equalWrites(); ok {
		return eventLayout{detailW: m.width - 2*each, detailH: h, writeW: each, writeH: h}
	}
	if rest >= splitMinWidth {
		// The Write panel's usual share, and once the details have the room
		// they can use (their text is at most 76 columns), up to twice that.
		base := min(max(rest*2/5, 34), 60)
		ww := min(max(base, rest-detailRoom), 2*base)
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
		kind := "task"
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
	var input []string
	if p.mode == wInsert {
		input = p.inputLines(inner, innerH-1)
		listH -= len(input)
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
			lines = append(lines, strings.Repeat(" ", itemPrefixW+1)+mutedStyle.Render(fit(origin, inner-itemPrefixW-1)))
		}
	}
	if len(rows) == 0 && p.loaded {
		lines = []string{mutedStyle.Render(" Nothing yet."), mutedStyle.Render(" a adds a task, A a note.")}
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
	lines = append(lines, input...)

	if len(rows) > 1 {
		bottom = fmt.Sprintf("%d of %d", min(p.sel+1, len(rows)), len(rows))
	}
	f.bottom = bottom
	return panel(width, height, f, lines)
}

// inputWidth is how many columns of an item being typed fit on a line: the
// panel less the margin and the prompt, and one for the cursor at the end.
func inputWidth(inner int) int { return max(inner-4, 3) }

// wrapInput breaks what is being typed into lines of at most width columns,
// at spaces where it can and in the middle of a word where it must. It
// returns each line's [start, end) in rs. The lines are contiguous, so every
// position, the end included, is on one of them.
func wrapInput(rs []rune, width int) [][2]int {
	segs := [][2]int{}
	for start := 0; start < len(rs) || len(segs) == 0; {
		w, end, brk := 0, start, -1
		for end < len(rs) {
			rw := ansi.StringWidth(string(rs[end]))
			if w+rw > width {
				break
			}
			w += rw
			end++
			if rs[end-1] == ' ' {
				brk = end
			}
		}
		if end < len(rs) && brk > start {
			end = brk
		}
		if end == start && start < len(rs) {
			end++
		}
		segs = append(segs, [2]int{start, end})
		start = end
	}
	return segs
}

// inputLines is the text being typed, all of it, broken into rows like the
// items are and with the cursor drawn where it is. When there are more rows
// than max, the ones around the cursor are shown.
func (p writePanel) inputLines(inner, maxRows int) []string {
	rs := []rune(p.input.Value())
	pos := min(max(p.input.Position(), 0), len(rs))
	segs := wrapInput(rs, inputWidth(inner))
	cur := len(segs) - 1
	for i, sg := range segs {
		if sg[0] <= pos && pos < sg[1] {
			cur = i
			break
		}
	}
	end := len(segs)
	if n := max(maxRows, 1); end > n {
		end = max(cur+1, n)
		segs = segs[end-n : end]
		cur -= end - n
	}
	out := make([]string, len(segs))
	for i, sg := range segs {
		prefix := "   "
		if sg[0] == 0 {
			prefix = " " + accentStyle.Render("› ")
		}
		text := string(rs[sg[0]:sg[1]])
		if i == cur {
			rest, ch := "", " "
			if pos < len(rs) && pos < sg[1] {
				ch, rest = string(rs[pos]), string(rs[pos+1:sg[1]])
			}
			text = string(rs[sg[0]:pos]) + selectedStyle.Render(ch) + rest
		}
		out[i] = prefix + text
	}
	return out
}

// writeLine is line k of an item's text. The selected item is marked with a
// chevron in the margin; a finished task has its box checked, in the success
// color. A note's first line has a pilcrow where a task has its box, so it
// can't be mistaken for the wrapped line of the task above it.
func writeLine(it write.Item, k int, text string, selected bool, inner int) string {
	marker, box := " ", "    "
	if selected && k == 0 {
		marker = accentStyle.Render("›")
	}
	switch {
	case k == 0 && it.Todo:
		box = "[ ] "
		if it.Done {
			box = successStyle.Render("[x]") + " "
		}
	case k == 0:
		box = " " + mutedStyle.Render(noteMark) + "  "
	}
	return fit(marker+" "+box+text, inner)
}

// renderGlobalPanel is the global panel: expanded, or a strip that says
// "Write" down its side with the number of open tasks under it.
func (m Model) renderGlobalPanel(width, height int) []string {
	if width <= 0 {
		return make([]string, height)
	}
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
	if !m.writeOn {
		return m.renderDetail(width, height)
	}
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
