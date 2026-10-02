package ui

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// The event view shows an event's details and its links. Links never open
// blind: j/k (or 1–9) select one, and the selected link's full address is
// always shown in a strip at the bottom — wrapped, never truncated, with its
// host in bold — so it can be checked before enter opens it. If the window
// is too small to show the whole address, the link can't be opened until
// it's enlarged. Event content comes from whoever sent the invite, so this
// matters: a link labelled "Agenda" may point anywhere.

type detailView struct {
	agenda.Item
	link   int // selected link
	scroll int // first visible body line
}

type openedMsg struct {
	url string
	err error
}

func (m *Model) openDetail(it agenda.Item) {
	m.detail = &detailView{Item: it}
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.detail
	links := d.Event.Links
	page := max(m.detailBodyHeight()-1, 1)
	switch s := msg.String(); {
	case s == "ctrl+c":
		return m, tea.Quit
	case s == "esc" || s == "q":
		m.detail = nil
		return m, nil
	case s == "enter" || s == "o":
		return m.openSelectedLink()
	case (s == "j" || s == "down") && len(links) > 0:
		d.link = min(d.link+1, len(links)-1)
		m.scrollToSelectedLink()
	case (s == "k" || s == "up") && len(links) > 0:
		d.link = max(d.link-1, 0)
		m.scrollToSelectedLink()
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9' && int(s[0]-'0') <= len(links):
		d.link = int(s[0] - '1')
		m.scrollToSelectedLink()
	case s == "j" || s == "down":
		d.scroll++
	case s == "k" || s == "up":
		d.scroll--
	case s == "pgdown" || s == "ctrl+d" || s == "space":
		d.scroll += page
	case s == "pgup" || s == "ctrl+u":
		d.scroll -= page
	}
	m.clampDetailScroll()
	return m, nil
}

// openSelectedLink opens the selected link in the default browser, if its
// full address is on screen and it's a web link.
func (m Model) openSelectedLink() (tea.Model, tea.Cmd) {
	links := m.detail.Event.Links
	if len(links) == 0 {
		return m, nil
	}
	if !m.linkFits() {
		m.actionErr = errors.New("enlarge the window to see the full address before opening this link")
		return m, nil
	}
	target := links[m.detail.link].URL
	if err := checkWebLink(target); err != nil {
		m.actionErr = err
		return m, nil
	}
	m.actionErr = nil
	open := m.openURL
	return m, func() tea.Msg { return openedMsg{url: target, err: open(target)} }
}

// checkWebLink accepts only http(s) URLs.
func checkWebLink(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("only web (http/https) links can be opened")
	}
	return nil
}

// openInBrowser hands a URL to the system's opener (macOS open, xdg-open
// elsewhere). The URL is a single argument, never passed through a shell.
func openInBrowser(target string) error {
	if err := checkWebLink(target); err != nil {
		return err
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, target)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go cmd.Wait() // reap it; the browser outlives this
	return nil
}

func (m Model) opened(msg openedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.actionErr = msg.err
		return m, nil
	}
	return m, m.startFlash(flashOpened, m.now)
}

// Layout.

func (m Model) detailInner() (width, height int) {
	return m.width - 2, m.bodyHeight() - 2
}

func (m Model) detailTextWidth() int {
	w, _ := m.detailInner()
	return min(w-2, 76) // one column of padding each side, readable line length
}

// detailBodyHeight is how many body lines fit above the link strip.
func (m Model) detailBodyHeight() int {
	w, h := m.detailInner()
	return max(h-len(m.linkStrip(w)), 0)
}

// linkFits reports whether the selected link's full address is on screen.
func (m Model) linkFits() bool {
	w, h := m.detailInner()
	return len(m.addressLines(w)) > 0 && 1+len(m.addressLines(w)) <= h-1
}

func (m *Model) clampDetailScroll() {
	body := m.detailBody(m.detailTextWidth())
	m.detail.scroll = min(max(m.detail.scroll, 0), max(0, len(body)-m.detailBodyHeight()))
}

func (m *Model) scrollToSelectedLink() {
	line := m.selectedLinkLine(m.detailTextWidth())
	h := m.detailBodyHeight()
	if line < m.detail.scroll {
		m.detail.scroll = line
	} else if line >= m.detail.scroll+h {
		m.detail.scroll = line - h + 1
	}
}

// renderDetail draws the event view as a full-width panel: a scrollable body
// and, when the event has links, the fixed strip with the selected address.
func (m Model) renderDetail(width, height int) []string {
	inner := width - 2
	body := m.detailBody(m.detailTextWidth())
	strip := m.linkStrip(inner)
	bodyH := max(height-2-len(strip), 0)

	scroll := min(m.detail.scroll, max(0, len(body)-bodyH))
	end := min(scroll+bodyH, len(body))
	content := padLines(append([]string(nil), body[scroll:end]...), bodyH, inner)
	content = append(content, strip...)

	position := ""
	if len(body) > bodyH && bodyH > 0 {
		position = fmt.Sprintf("lines %d–%d of %d", scroll+1, end, len(body))
	}
	return panel(width, height, frame{focused: true, tabs: []tab{{label: "Event", active: true}}, bottom: position}, content)
}

// detailBody is every line of the scrollable part, already width-limited.
func (m Model) detailBody(textW int) []string {
	e := m.detail.Event
	var lines []string
	add := func(s string) {
		for _, l := range strings.Split(s, "\n") {
			lines = append(lines, " "+l)
		}
	}
	wrap := func(s string, w int) string {
		return ansi.Hardwrap(ansi.Wordwrap(s, w, ""), w, false)
	}

	add(boldStyle.Render(wrap(e.Title, textW)))
	add(m.detailWhen())
	if label := m.calendarLabel(e.CalendarID); label != "" {
		add(calendarBullet(m.calendarSlot(e.CalendarID)) + " " + label)
	}
	if e.Location != "" {
		lines = append(lines, "")
		add(mutedStyle.Render("Location") + "  " + wrap(e.Location, max(textW-10, 10)))
	}
	if len(e.Links) > 0 {
		lines = append(lines, "", " "+boldStyle.Render("Links"))
		for i := range e.Links {
			lines = append(lines, " "+m.linkRow(i, textW))
		}
	}
	if notes := strings.TrimSpace(e.Notes); notes != "" {
		lines = append(lines, "")
		add(wrap(notes, textW))
	}
	return lines
}

// selectedLinkLine is the body line index of the selected link's row.
func (m Model) selectedLinkLine(textW int) int {
	body := m.detailBody(textW)
	for i, l := range body {
		if ansi.Strip(l) == " Links" {
			return i + 1 + m.detail.link
		}
	}
	return 0
}

// linkRow is "▸ 1 ▶ Join Google Meet      meet.google.com": selection
// marker, number (the key that selects it), a ▶ for video calls, the label
// and the host.
func (m Model) linkRow(i, width int) string {
	l := m.detail.Event.Links[i]
	selected := i == m.detail.link

	marker, icon := " ", " "
	if selected {
		marker = "▸"
	}
	if l.Kind == domain.LinkVideoCall {
		icon = "▶"
	}
	num := " "
	if i < 9 {
		num = fmt.Sprintf("%d", i+1)
	}
	host := hostOf(l.URL)
	hostW := min(ansi.StringWidth(host), max(width/3, 12))
	left := fmt.Sprintf("%s %s %s %s", marker, num, icon, l.Label)
	leftW := max(width-hostW-2, 1)

	if selected {
		return selectedStyle.Render(fit(left, leftW) + "  " + fit(host, hostW))
	}
	return fit(left, leftW) + "  " + mutedStyle.Render(fit(host, hostW))
}

// linkStrip is the fixed bottom part: a rule, then the selected link's full
// address, or a notice when the window can't fit it.
func (m Model) linkStrip(inner int) []string {
	if len(m.detail.Event.Links) == 0 {
		return nil
	}
	_, innerH := m.detailInner()
	label := " Opens "
	rule := mutedStyle.Render("─" + label + strings.Repeat("─", max(inner-1-ansi.StringWidth(label), 0)))
	addr := m.addressLines(inner)
	if 1+len(addr) > innerH-1 {
		return []string{rule, " " + errorStyle.Render(fit("Window too small to show this address in full.", inner-1))}
	}
	return append([]string{rule}, addr...)
}

// addressLines is the selected link's whole URL hard-wrapped to fit, its host
// in bold, each line a clickable hyperlink in terminals that support it.
func (m Model) addressLines(inner int) []string {
	links := m.detail.Event.Links
	if len(links) == 0 || inner < 4 {
		return nil
	}
	full := links[m.detail.link].URL
	hostStart, hostEnd := hostSpan(full)

	var out []string
	offset := 0
	for _, chunk := range strings.Split(ansi.Hardwrap(full, inner-2, false), "\n") {
		cStart, cEnd := offset, offset+len(chunk)
		offset = cEnd
		// Bold the part of this chunk that's inside the host.
		a, b := max(hostStart, cStart)-cStart, min(hostEnd, cEnd)-cStart
		styled := chunk
		if a < b {
			styled = chunk[:a] + boldStyle.Render(chunk[a:b]) + chunk[b:]
		}
		out = append(out, " "+ansi.SetHyperlink(full)+styled+ansi.ResetHyperlink())
	}
	return out
}

// hostSpan is the byte range of the host in a URL ("" scheme://HOST/...).
func hostSpan(raw string) (int, int) {
	start := strings.Index(raw, "://")
	if start < 0 {
		return 0, 0
	}
	start += 3
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	return start, end
}

func hostOf(raw string) string {
	a, b := hostSpan(raw)
	return raw[a:b]
}

func (m Model) detailWhen() string {
	e := m.detail.Event
	if ds, de, ok := e.DateRange(); ok {
		last := de.AddDays(-1)
		if last == ds || de == ds {
			return ds.In(m.loc).Format("Mon 2 Jan 2006") + " · all day"
		}
		return ds.In(m.loc).Format("Mon 2 Jan") + " – " + last.In(m.loc).Format("Mon 2 Jan 2006") + " · all day"
	}
	start, end := m.detail.Start, m.detail.End
	dur := formatDuration(end.Sub(start))
	if domain.CivilDateFromTime(start) == domain.CivilDateFromTime(end) || end.Equal(start) {
		s := start.Format("Mon 2 Jan 2006") + " · " + start.Format("15:04")
		if !end.Equal(start) {
			s += "–" + end.Format("15:04") + " (" + dur + ")"
		}
		return s
	}
	return start.Format("Mon 2 Jan 15:04") + " – " + end.Format("Mon 2 Jan 2006 15:04") + " (" + dur + ")"
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days, hours, mins := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	return strings.Join(parts, " ")
}
