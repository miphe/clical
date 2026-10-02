// Package eventtext turns an event's raw content — description (often
// HTML), location and link fields — into readable notes and a clean list of
// links. It's provider-neutral: the iCal and Google providers map their own
// fields into Content.
package eventtext

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/miphe/clical/internal/domain"
)

// Event content is untrusted input from whoever sent the invite. Links come
// from several places, inconsistently:
//
//   - X-GOOGLE-CONFERENCE / X-MICROSOFT-SKYPETEAMSMEETINGURL: the video call
//   - URL and LOCATION
//   - DESCRIPTION, which Google fills with HTML (<a href>, <br>, <li>) even
//     though iCalendar says it's plain text; Outlook adds an HTML copy in
//     X-ALT-DESC
//   - bare URLs in plain-text descriptions (Zoom, Webex, pasted documents)
//
// Describe gathers them all into one deduplicated list (video calls first),
// and turns the description into readable plain-text notes that refer to the
// links as "[1]", "[2]", ... Only http(s) links are kept.

// Content is an event's raw, untrusted content, as a provider has it.
type Content struct {
	Description string   // plain text or HTML
	AltHTML     string   // an HTML copy of the description, if any (Outlook's X-ALT-DESC)
	Location    string   // may contain a URL
	URL         string   // the event's own link field, if any
	Conference  []string // video-call URLs from structured fields
	// Extra are links the provider already knows the meaning of (e.g. Google
	// Drive attachments); their labels and kinds are kept. Listed after links
	// found in the content of the same kind.
	Extra []domain.Link
}

// Describe returns readable notes and the event's links.
func Describe(c Content) (string, []domain.Link) {
	desc := c.Description
	useHTML := looksLikeHTML(desc)
	if !useHTML && looksLikeHTML(c.AltHTML) {
		desc, useHTML = c.AltHTML, true
	}
	desc, meetLinks := stripGoogleMeetBlock(desc)

	// Pass 1: collect every link, then fix their order (and so their numbers).
	links := newLinkSet()
	for _, u := range c.Conference {
		links.add(u, "")
	}
	for _, u := range meetLinks {
		links.add(u, "")
	}
	links.add(c.URL, "")
	for _, u := range findURLs(c.Location) {
		links.add(u, "")
	}
	if useHTML {
		htmlToText(desc, links.add)
	} else {
		plainToText(desc, links.add)
	}
	for _, l := range c.Extra {
		links.addKnown(l)
	}
	links.sort()

	// Pass 2: write the notes with the final numbers.
	var notes string
	if useHTML {
		notes = htmlToText(desc, links.marker)
	} else {
		notes = plainToText(desc, links.marker)
	}
	return tidy(notes), links.list
}

// linkSet collects links once each, by destination.
type linkSet struct {
	list  []domain.Link
	index map[string]int
}

func newLinkSet() *linkSet { return &linkSet{index: map[string]int{}} }

// add records raw (with an optional label from its anchor text) and returns
// the text to show in its place. It's also used, discarding the result,
// while collecting.
func (s *linkSet) add(raw, label string) string {
	u, ok := cleanURL(raw)
	if !ok {
		return label
	}
	key := linkKey(u)
	if i, seen := s.index[key]; seen {
		if label != "" && s.list[i].Label == defaultLabel(u) && !isURLText(label) {
			s.list[i].Label = label
		}
		return label
	}
	kind := classify(u)
	l := domain.Link{URL: u.String(), Kind: kind, Label: defaultLabel(u)}
	if label != "" && !isURLText(label) && kind != domain.LinkVideoCall {
		l.Label = label
	}
	s.index[key] = len(s.list)
	s.list = append(s.list, l)
	return label
}

// addKnown records a link whose label and kind the provider already knows.
func (s *linkSet) addKnown(l domain.Link) {
	u, ok := cleanURL(l.URL)
	if !ok {
		return
	}
	key := linkKey(u)
	if _, seen := s.index[key]; seen {
		return
	}
	if l.Label == "" {
		l.Label = defaultLabel(u)
	}
	l.URL = u.String()
	s.index[key] = len(s.list)
	s.list = append(s.list, l)
}

// marker returns the notes text for a link: its label (or host) and number.
func (s *linkSet) marker(raw, label string) string {
	u, ok := cleanURL(raw)
	if !ok {
		return label
	}
	i, seen := s.index[linkKey(u)]
	if !seen {
		return label
	}
	text := label
	if text == "" || isURLText(text) {
		text = u.Host
	}
	return text + " [" + strconv.Itoa(i+1) + "]"
}

// sort puts video calls first, then documents, then the rest, keeping the
// order they were found in otherwise.
func (s *linkSet) sort() {
	rank := map[domain.LinkKind]int{domain.LinkVideoCall: 0, domain.LinkDocument: 1, domain.LinkOther: 2}
	sort.SliceStable(s.list, func(a, b int) bool { return rank[s.list[a].Kind] < rank[s.list[b].Kind] })
	for i, l := range s.list {
		u, _ := url.Parse(l.URL)
		s.index[linkKey(u)] = i
	}
}

// cleanURL parses raw, unwraps redirect wrappers, and accepts only http(s).
func cleanURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, false
	}
	u.Host = strings.ToLower(u.Host)
	if inner, ok := unwrapRedirect(u); ok {
		return inner, true
	}
	return u, true
}

// unwrapRedirect replaces Google's link tracker (google.com/url?q=…) and
// Outlook Safe Links (…safelinks.protection.outlook.com/?url=…) with their
// destination, so the real address is what gets shown and judged.
func unwrapRedirect(u *url.URL) (*url.URL, bool) {
	var target string
	switch {
	case (u.Host == "www.google.com" || u.Host == "google.com") && u.Path == "/url":
		target = u.Query().Get("q")
	case strings.HasSuffix(u.Host, ".safelinks.protection.outlook.com"):
		target = u.Query().Get("url")
	default:
		return nil, false
	}
	inner, err := url.Parse(target)
	if err != nil || (inner.Scheme != "https" && inner.Scheme != "http") || inner.Host == "" {
		return nil, false
	}
	inner.Host = strings.ToLower(inner.Host)
	return inner, true
}

// linkKey identifies a destination for deduplication.
func linkKey(u *url.URL) string {
	return u.Host + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
}

type service struct {
	host  string // matched as the host or a parent domain
	path  string // optional path prefix
	kind  domain.LinkKind
	label string
}

var services = []service{
	{host: "meet.google.com", kind: domain.LinkVideoCall, label: "Join Google Meet"},
	{host: "zoom.us", kind: domain.LinkVideoCall, label: "Join Zoom meeting"},
	{host: "teams.microsoft.com", kind: domain.LinkVideoCall, label: "Join Teams meeting"},
	{host: "teams.live.com", kind: domain.LinkVideoCall, label: "Join Teams meeting"},
	{host: "webex.com", kind: domain.LinkVideoCall, label: "Join Webex meeting"},
	{host: "whereby.com", kind: domain.LinkVideoCall, label: "Join Whereby meeting"},
	{host: "meet.jit.si", kind: domain.LinkVideoCall, label: "Join Jitsi meeting"},
	{host: "gotomeeting.com", kind: domain.LinkVideoCall, label: "Join GoTo meeting"},
	{host: "meet.goto.com", kind: domain.LinkVideoCall, label: "Join GoTo meeting"},
	{host: "chime.aws", kind: domain.LinkVideoCall, label: "Join Chime meeting"},
	{host: "facetime.apple.com", kind: domain.LinkVideoCall, label: "Join FaceTime"},

	{host: "docs.google.com", path: "/document", kind: domain.LinkDocument, label: "Google Doc"},
	{host: "docs.google.com", path: "/spreadsheets", kind: domain.LinkDocument, label: "Google Sheet"},
	{host: "docs.google.com", path: "/presentation", kind: domain.LinkDocument, label: "Google Slides"},
	{host: "docs.google.com", kind: domain.LinkDocument, label: "Google Docs"},
	{host: "drive.google.com", kind: domain.LinkDocument, label: "Google Drive"},
	{host: "notion.so", kind: domain.LinkDocument},
	{host: "notion.site", kind: domain.LinkDocument},
	{host: "atlassian.net", kind: domain.LinkDocument},
	{host: "sharepoint.com", kind: domain.LinkDocument},
	{host: "onedrive.live.com", kind: domain.LinkDocument},
	{host: "1drv.ms", kind: domain.LinkDocument},
	{host: "dropbox.com", kind: domain.LinkDocument},
	{host: "github.com", kind: domain.LinkDocument},
	{host: "gitlab.com", kind: domain.LinkDocument},
	{host: "figma.com", kind: domain.LinkDocument},
	{host: "miro.com", kind: domain.LinkDocument},
}

func lookup(u *url.URL) (service, bool) {
	for _, s := range services {
		if (u.Host == s.host || strings.HasSuffix(u.Host, "."+s.host)) && strings.HasPrefix(u.Path, s.path) {
			return s, true
		}
	}
	return service{}, false
}

func classify(u *url.URL) domain.LinkKind {
	if s, ok := lookup(u); ok {
		return s.kind
	}
	return domain.LinkOther
}

func defaultLabel(u *url.URL) string {
	if s, ok := lookup(u); ok && s.label != "" {
		return s.label
	}
	return u.Host
}

func isURLText(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "www.")
}

var (
	htmlTag = regexp.MustCompile(`(?i)<(a|br|p|div|span|b|i|u|ul|ol|li|strong|em|html|body)\b[^>]*>`)
	bareURL = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)
)

func looksLikeHTML(s string) bool { return htmlTag.MatchString(s) }

// findURLs returns the http(s) URLs in plain text, without trailing
// punctuation that's usually sentence, not address.
func findURLs(s string) []string {
	var out []string
	for _, m := range bareURL.FindAllString(s, -1) {
		out = append(out, trimURL(m))
	}
	return out
}

func trimURL(u string) string {
	for {
		trimmed := strings.TrimRight(u, ".,;:!?")
		if strings.HasSuffix(trimmed, ")") && strings.Count(trimmed, "(") < strings.Count(trimmed, ")") {
			trimmed = strings.TrimSuffix(trimmed, ")")
		}
		if trimmed == u {
			return u
		}
		u = trimmed
	}
}

// plainToText replaces each URL in s with link(url, "").
func plainToText(s string, link func(raw, label string) string) string {
	return bareURL.ReplaceAllStringFunc(s, func(m string) string {
		u := trimURL(m)
		return link(u, "") + m[len(u):]
	})
}

// htmlToText renders an HTML description as plain text: paragraphs and line
// breaks become newlines, list items "• ", anchors link(href, text), and
// everything else just its text. Scripts and styles are dropped.
func htmlToText(s string, link func(raw, label string) string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	var href string
	var anchor strings.Builder
	inAnchor, skip := false, 0
	write := func(t string) {
		if inAnchor {
			anchor.WriteString(t)
		} else {
			b.WriteString(t)
		}
	}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			if skip == 0 {
				text := string(z.Text())
				if inAnchor {
					write(text)
				} else {
					b.WriteString(plainToText(text, link))
				}
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			end := tt == html.EndTagToken
			switch tag {
			case "script", "style":
				if end {
					skip = max(skip-1, 0)
				} else if tt == html.StartTagToken {
					skip++
				}
			case "br":
				write("\n")
			case "p", "div", "ul", "ol", "tr", "h1", "h2", "h3", "h4":
				write("\n")
			case "li":
				if !end {
					write("\n• ")
				}
			case "a":
				if !end {
					href, inAnchor = "", true
					anchor.Reset()
					for hasAttr {
						var k, v []byte
						k, v, hasAttr = z.TagAttr()
						if string(k) == "href" {
							href = string(v)
						}
					}
				} else if inAnchor {
					inAnchor = false
					text := strings.TrimSpace(anchor.String())
					if href == "" {
						b.WriteString(text)
					} else {
						b.WriteString(link(href, text))
					}
				}
			}
		}
	}
}

// stripGoogleMeetBlock removes the boilerplate Google adds to events with a
// Meet call ("-::~:~::~…", "Join with Google Meet: …", dial-in numbers,
// "Please do not edit this section."), returning any meet.google.com links
// from it so the call isn't lost if X-GOOGLE-CONFERENCE is missing.
func stripGoogleMeetBlock(s string) (string, []string) {
	const mark = "-::~:~::~"
	first, last := strings.Index(s, mark), strings.LastIndex(s, mark)
	if first < 0 {
		return s, nil
	}
	end := len(s)
	if last > first {
		if k := strings.Index(s[last:], "::-"); k >= 0 {
			end = last + k + len("::-")
		}
	}
	var meet []string
	for _, u := range findURLs(stripTags(s[first:end])) {
		if strings.Contains(u, "meet.google.com/") {
			meet = append(meet, u)
		}
	}
	return s[:first] + s[end:], meet
}

var anyTag = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return anyTag.ReplaceAllString(s, "") }

// tidy trims trailing spaces, collapses runs of blank lines and trims the
// whole text.
func tidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
			l = ""
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
