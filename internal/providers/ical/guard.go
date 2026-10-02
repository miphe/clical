package ical

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/apognu/gocal/parser"
)

// gocal expands recurring events by stepping from DTSTART one period at a
// time, and only stops at the window's end when the rule has no COUNT. A
// rule with INTERVAL=0, a huge COUNT, or a COUNT whose BYDAY/BYMONTH never
// matches loops forever, and an ancient DTSTART costs one step per period
// since then. Feeds are untrusted, so guardRecurrence rewrites every RRULE
// before gocal sees it, so that it ends just after the sync window and
// always moves forward, and rejects feeds whose expansion would still be too
// costly.
//
// gocal also gets UNTIL wrong, which the rewrite corrects: RFC 5545 makes
// UNTIL inclusive, but gocal drops an occurrence that falls on it, reads a
// date-only UNTIL as the end of the day before, and compares UNTIL with each
// occurrence as wall-clock strings in their own (possibly different) zones.
// So the rewritten UNTIL is the true last moment, as wall-clock time in
// DTSTART's zone, plus a second to make gocal's comparison inclusive.

// untilSlack keeps the added UNTIL clear of the window despite gocal
// comparing times as zone-less strings.
const untilSlack = 48 * time.Hour

// Budgets for a whole feed, well above what a real calendar needs. gocal
// takes ~0.4µs a step, so the step budget is about two seconds of work.
//
// gocal's RECURRENCE-ID check compares every in-window occurrence with every
// event it has kept so far: the in-window one-off events, then each
// occurrence it accepted before. Out-of-window events aren't kept, so
// Google's feeds, which carry the calendar's whole history, don't add to it.
// One comparison takes ~2-3ns, so the ops budget is also about two seconds;
// a busy calendar (TestLargeRealisticFeedIsAccepted) needs ~1% of it.
const (
	maxExpandSteps = 5_000_000   // loop steps, counting EXDATE checks
	maxInstances   = 100_000     // occurrences inside the window
	maxOverrideOps = 600_000_000 // occurrences × events kept, see overrideOps
)

// windowSlack widens the window when counting one-off events gocal keeps,
// so events that start a little before it and overlap it are counted too.
const windowSlack = 31 * 24 * time.Hour

const (
	ctxRoot = iota
	ctxEvent
	ctxOther
)

var errTooComplex = errors.New("feed has too many recurring events to expand")

// guardRecurrence returns feed with its RRULEs made safe for gocal to expand
// within [from, to).
func guardRecurrence(feed []byte, from, to time.Time) ([]byte, error) {
	until := to.Add(untilSlack).UTC()
	lines := logicalLines(feed)

	var (
		out                       bytes.Buffer
		steps, instances, oneOffs int64
		ev                        recurrence
		// The current event's lines, held back until END:VEVENT because
		// its RRULEs can't be written before its DTSTART is known.
		body    [][]byte
		rules   []rule // one per nil entry in body
		inEvent bool
	)
	// ctx mirrors gocal's component stack, so every RRULE gocal will
	// expand is seen here.
	ctx := []int{ctxRoot}
	out.Grow(len(feed))
	for _, l := range lines {
		key, params, value, ok := splitLine(l.text)
		top := ctx[len(ctx)-1]
		k, v := strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case !ok || v == "VCALENDAR":
		case top == ctxRoot && k == "BEGIN" && v == "VEVENT":
			ctx = append(ctx, ctxEvent)
			ev, body, rules, inEvent = recurrence{}, body[:0], rules[:0], true
		case top == ctxEvent && k == "END" && v == "VEVENT":
			ctx = ctx[:len(ctx)-1]
			inEvent = false
			for _, b := range append(body, l.raw) {
				if b == nil {
					if r, ok := rules[0].bounded(until, ev.start); ok {
						out.WriteString(fold("RRULE:" + r))
					}
					rules = rules[1:]
					continue
				}
				out.Write(b)
			}
			if ev.rule == nil {
				if ev.start != nil && !ev.start.Before(from.Add(-windowSlack)) && !ev.start.After(until) {
					oneOffs++
				}
			} else {
				s, n := ev.cost(from, until)
				steps += s
				instances += n
			}
			continue
		case k == "BEGIN":
			ctx = append(ctx, ctxOther)
		case k == "END":
			if len(ctx) == 1 {
				return nil, errors.New("END without matching BEGIN")
			}
			ctx = ctx[:len(ctx)-1]
		case top != ctxEvent:
		case key == "DTSTART":
			ev.start, _ = parser.ParseTime(value, params, parser.TimeStart, false, time.UTC)
		case key == "EXDATE":
			ev.exdates++
		case key == "RRULE":
			ev.rule = parseRule(value)
			if !ev.rule.safe() {
				// A rule that can't be made safe is dropped: the event then
				// occurs once, at DTSTART.
				ev.rule = nil
				continue
			}
			rules = append(rules, ev.rule)
			body = append(body, nil)
			continue
		}
		if inEvent {
			body = append(body, l.raw)
			continue
		}
		out.Write(l.raw)
	}
	if steps > maxExpandSteps || instances > maxInstances || overrideOps(instances, oneOffs) > maxOverrideOps {
		return nil, errTooComplex
	}
	return out.Bytes(), nil
}

// overrideOps is how many events gocal compares occurrences with, in total.
func overrideOps(instances, oneOffs int64) int64 {
	return instances*oneOffs + instances*instances/2
}

type rule map[string]string

// parseRule reads an RRULE value the way gocal does.
func parseRule(value string) rule {
	_, params := parser.ParseRecurrenceParams(value)
	return params
}

// The parts of a rule gocal acts on; anything else it ignores.
var ruleKeys = []string{"FREQ", "INTERVAL", "COUNT", "BYMONTH", "BYDAY"}

// periodDays is the shortest length of one step, in days. ok is false for a
// frequency gocal doesn't expand.
func (r rule) periodDays() (days int64, ok bool) {
	interval := int64(1)
	if v, set := r["INTERVAL"]; set {
		if n, err := strconv.Atoi(v); err == nil {
			// gocal falls back to 1 only when INTERVAL doesn't parse.
			interval = int64(n)
		}
	}
	per := map[string]int64{"DAILY": 1, "WEEKLY": 7, "MONTHLY": 28, "YEARLY": 365}[r["FREQ"]]
	return per * interval, per != 0
}

// safe reports whether r always advances; one that doesn't is dropped.
func (r rule) safe() bool {
	if days, ok := r.periodDays(); ok && days <= 0 {
		return false
	}
	return !strings.ContainsAny(r["FREQ"], `\;:=`)
}

// bounded returns r rewritten to end by limit, or by its own UNTIL if that
// is earlier, for an event starting at start.
func (r rule) bounded(limit time.Time, start *time.Time) (string, bool) {
	if !r.safe() {
		return "", false
	}
	loc := time.UTC
	if start != nil {
		loc = start.Location()
	}
	end := limit.In(loc)
	if t, ok := lastMoment(r["UNTIL"], loc); ok && t.Before(end) {
		end = t
	}
	parts := []string{}
	for _, k := range ruleKeys {
		v, set := r[k]
		if !set {
			continue
		}
		if strings.ContainsAny(v, `\;:=`) {
			if k == "FREQ" {
				return "", false
			}
			continue
		}
		parts = append(parts, k+"="+v)
	}
	parts = append(parts, "UNTIL="+end.Add(time.Second).Format("20060102T150405"))
	return strings.Join(parts, ";"), true
}

// lastMoment is the last moment an UNTIL value allows, in loc: a UTC time
// as is, a floating time read in loc, a date to the end of that day.
func lastMoment(v string, loc *time.Location) (time.Time, bool) {
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t.In(loc), true
	}
	if t, err := time.ParseInLocation("20060102T150405", v, loc); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("20060102", v, loc); err == nil {
		return t.Add(24*time.Hour - time.Second), true
	}
	return time.Time{}, false
}

func parseUntil(v string) (time.Time, bool) {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type recurrence struct {
	rule    rule
	start   *time.Time
	exdates int64
}

// cost estimates the loop steps (including EXDATE checks) and in-window
// occurrences gocal will produce for this event once its rule is bounded.
func (e recurrence) cost(from, until time.Time) (steps, instances int64) {
	if e.rule == nil || e.start == nil {
		return 0, 0
	}
	days, ok := e.rule.periodDays()
	if !ok || days <= 0 {
		return 0, 0
	}
	end := until
	if t, ok := parseUntil(e.rule["UNTIL"]); ok && t.Before(until) {
		end = t
	}
	// With BYDAY, gocal walks 7 days per step and yields one occurrence for
	// each weekday the rule lists.
	walk, perStep := int64(1), int64(1)
	if byDay, ok := e.rule["BYDAY"]; ok {
		walk, perStep = 7, weekdaysIn(byDay)
	}
	n := spanDays(*e.start, end)/days + 1
	steps = n * walk * (1 + e.exdates)
	first := *e.start
	if first.Before(from) {
		first = from
	}
	if !end.Before(first) {
		instances = (spanDays(first, end)/days + 1) * perStep
	}
	return steps, instances
}

// weekdaysIn counts the weekdays a BYDAY value names, matching gocal, which
// checks each day with strings.Contains (so "1MO" and "-1MO" mean Monday).
func weekdaysIn(byDay string) int64 {
	var n int64
	for _, d := range []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"} {
		if strings.Contains(byDay, d) {
			n++
		}
	}
	return n
}

func spanDays(a, b time.Time) int64 {
	if b.Before(a) {
		return 0
	}
	// Not b.Sub(a): a Duration can't span more than ~292 years.
	return (b.Unix()-a.Unix())/86400 + 1
}

type logicalLine struct {
	raw  []byte // the physical lines, as they were
	text string // unfolded, as gocal reads it
}

// logicalLines splits feed into content lines, joining folded ones the way
// gocal does (only a leading space marks a continuation).
func logicalLines(feed []byte) []logicalLine {
	var out []logicalLine
	for len(feed) > 0 {
		i := bytes.IndexByte(feed, '\n')
		if i < 0 {
			i = len(feed) - 1
		}
		phys := feed[:i+1]
		feed = feed[i+1:]
		text := strings.TrimSuffix(strings.TrimSuffix(string(phys), "\n"), "\r")
		if len(out) > 0 && strings.HasPrefix(text, " ") {
			last := &out[len(out)-1]
			last.raw = last.raw[:len(last.raw):len(last.raw)]
			last.raw = append(last.raw, phys...)
			last.text += text[1:]
			continue
		}
		out = append(out, logicalLine{raw: phys, text: text})
	}
	return out
}

// splitLine parses a content line into its name, parameters and unescaped
// value, the way gocal does.
func splitLine(l string) (key string, params map[string]string, value string, ok bool) {
	name, val, found := cutUnquoted(l)
	if !found {
		return "", nil, "", false
	}
	key, params = parser.ParseParameters(name)
	return key, params, parser.UnescapeString(strings.TrimPrefix(val, " ")), true
}

// cutUnquoted splits l at its first colon outside double quotes.
func cutUnquoted(l string) (before, after string, found bool) {
	quoted := false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			quoted = !quoted
		case ':':
			if !quoted {
				return l[:i], l[i+1:], true
			}
		}
	}
	return "", "", false
}

// fold writes a content line folded at 75 octets, as RFC 5545 asks.
func fold(l string) string {
	var b strings.Builder
	for len(l) > 75 {
		b.WriteString(l[:75])
		b.WriteString("\r\n ")
		l = l[75:]
	}
	b.WriteString(l)
	b.WriteString("\r\n")
	return b.String()
}
