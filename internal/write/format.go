package write

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// The markdown form. A task is a task-list line, `- [ ] text` or
// `- [x] text`; every other line is a note, kept exactly as it is, so a file
// edited by hand loses nothing. A task's bookkeeping rides in a trailing
// comment that markdown hides:
//
//	- [x] Send the deck <!--{"id":"a1b2c3d4","done":"2026-10-05T12:00:00+03:00"}-->
//
// An event's file starts with a header naming the event:
//
//	# Weekly sync
//	<!-- clical:event {"cal":"...","key":"...","title":"Weekly sync",...} -->
//
// The comments are JSON, which escapes < and >, so nothing in them can end
// the comment early.

var todoLine = regexp.MustCompile(`^- \[([ xX])\] (.*\S.*)$`)

const (
	headerPrefix = "<!-- clical:event "
	headerSuffix = " -->"
	metaOpen     = "<!--{"
	metaClose    = "}-->"
)

type meta struct {
	ID     string `json:"id,omitempty"`
	Done   string `json:"done,omitempty"`
	Origin *Ref   `json:"from,omitempty"`
	Last   *Ref   `json:"last,omitempty"`
}

type headerJSON struct {
	Ref
	Calendar string `json:"calendar,omitempty"`
	Start    string `json:"start,omitempty"`
}

// Parse reads a list. It never fails: anything it doesn't understand is a
// note.
func Parse(data []byte) List {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return List{}
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	var l List
	if h, used := parseHeader(lines); h != nil {
		l.Header, lines = h, lines[used:]
	}
	seen := map[string]int{}
	for _, line := range lines {
		m := todoLine.FindStringSubmatch(line)
		if m == nil {
			l.Items = append(l.Items, Item{Text: line})
			continue
		}
		it := Item{Todo: true, Done: m[1] != " "}
		var originSet bool
		it.Text, it.ID, it.Completed, it.Origin, originSet, it.Last = splitMeta(m[2])
		if !originSet {
			it.Origin = l.Scope() // written here, which is all the file says
		}
		if !it.Done {
			it.Completed = time.Time{}
		}
		if it.ID == "" {
			// Worked out from the text so it's the same on every read, until
			// the file is written with it.
			n := seen[it.Text]
			seen[it.Text]++
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", it.Text, n)))
			it.ID, it.derived = hex.EncodeToString(sum[:])[:8], true
		}
		l.Items = append(l.Items, it)
	}
	return l
}

func parseHeader(lines []string) (*Header, int) {
	at := -1
	for i := 0; i < len(lines) && i < 2; i++ {
		if strings.HasPrefix(lines[i], headerPrefix) && strings.HasSuffix(lines[i], headerSuffix) {
			at = i
			break
		}
	}
	if at < 0 || (at == 1 && !strings.HasPrefix(lines[0], "# ")) {
		return nil, 0
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(lines[at], headerPrefix), headerSuffix)
	var hj headerJSON
	if json.Unmarshal([]byte(raw), &hj) != nil {
		return nil, 0
	}
	used := at + 1
	if used < len(lines) && lines[used] == "" {
		used++ // the blank line Format puts after it
	}
	return &Header{Event: hj.Ref, Calendar: hj.Calendar, Start: hj.Start}, used
}

func splitMeta(s string) (text, id string, done time.Time, origin Ref, originSet bool, last Ref) {
	i := strings.LastIndex(s, metaOpen)
	if i < 0 || !strings.HasSuffix(s, metaClose) {
		return strings.TrimSpace(s), "", time.Time{}, Ref{}, false, Ref{}
	}
	var m meta
	if json.Unmarshal([]byte(s[i+len("<!--"):len(s)-len("-->")]), &m) != nil {
		return strings.TrimSpace(s), "", time.Time{}, Ref{}, false, Ref{}
	}
	text = strings.TrimSpace(s[:i])
	if m.Done != "" {
		done, _ = time.Parse(time.RFC3339, m.Done)
	}
	if m.Origin != nil {
		origin, originSet = *m.Origin, true
	}
	if m.Last != nil {
		last = *m.Last
	}
	return text, m.ID, done, origin, originSet, last
}

// Format writes a list in its markdown form.
func Format(l List) []byte {
	var b strings.Builder
	if h := l.Header; h != nil {
		title := domain.SafeText(h.Event.Title)
		if title == "" {
			title = "(untitled)"
		}
		raw, _ := json.Marshal(headerJSON{Ref: h.Event, Calendar: h.Calendar, Start: h.Start})
		fmt.Fprintf(&b, "# %s\n%s%s%s\n", strings.ReplaceAll(title, "\n", " "), headerPrefix, raw, headerSuffix)
		if len(l.Items) > 0 {
			b.WriteString("\n")
		}
	}
	for _, it := range l.Items {
		if !it.Todo {
			b.WriteString(it.Text + "\n")
			continue
		}
		box := " "
		if it.Done {
			box = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s", box, it.Text)
		if m := metaFor(it, l.Scope()); m != (meta{}) {
			raw, _ := json.Marshal(m)
			b.WriteString(" <!--" + string(raw) + "-->")
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// metaFor is what a task's comment says. The origin is left out when it is
// the list the task is in, which is the usual case and keeps lines short.
func metaFor(it Item, scope Ref) meta {
	var m meta
	if it.Done && !it.Completed.IsZero() {
		m.Done = it.Completed.Format(time.RFC3339)
	}
	if !it.Origin.Same(scope) {
		o := it.Origin // for a task that began in the global list, this is {}
		m.Origin = &o
	}
	if !it.Last.IsZero() {
		la := it.Last
		m.Last = &la
	}
	if !it.derived || m != (meta{}) {
		m.ID = it.ID
	}
	return m
}
