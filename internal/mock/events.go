package mock

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// The events cover a window like the real cache's: 60 days back, 240 ahead.
// Each day's extras come from a generator seeded by the date, so the same day
// looks the same on every run.
const (
	daysBack  = 60
	daysAhead = 240
)

type eventGen struct {
	now    time.Time
	loc    *time.Location
	today  domain.CivilDate
	events []domain.Event
}

func newEvents(now time.Time, loc *time.Location) []domain.Event {
	g := &eventGen{now: now, loc: loc, today: domain.CivilDateFromTime(now.In(loc))}
	for i := -daysBack; i <= daysAhead; i++ {
		g.day(g.today.AddDays(i), i)
	}
	g.special()
	g.nearNow()
	return g.events
}

func (g *eventGen) at(d domain.CivilDate, h, m int) time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, h, m, 0, 0, g.loc)
}

// timed adds an event on day d from h:m for dur, with the series name as the
// ID's first part so each occurrence is its own event.
func (g *eventGen) timed(series, cal, title string, d domain.CivilDate, h, m int, dur time.Duration) domain.Event {
	start := g.at(d, h, m)
	e := domain.NewTimedEvent(series+"/"+d.String(), cal, title, start, start.Add(dur), "", "")
	g.events = append(g.events, e)
	return e
}

func (g *eventGen) add(e domain.Event) { g.events = append(g.events, e) }

func (g *eventGen) allDay(series, cal, title string, from domain.CivilDate, days int) {
	g.events = append(g.events, domain.NewAllDayEvent(series+"/"+from.String(), cal, title, from, from.AddDays(days), "", ""))
}

var meetingTitles = []string{
	"Roadmap review", "Customer call: Acme", "Hiring sync", "Incident review", "Pairing session",
	"Architecture chat", "Budget check-in", "Support triage", "Product demo", "Candidate interview",
	"Security briefing", "Onboarding buddy chat", "Vendor call", "Metrics review", "Docs office hours",
	"Design critique", "Planning poker", "Release readiness", "Team lunch", "Stakeholder update",
}

var friendNames = []string{"Priya", "Marcus", "Lena", "Tom", "Ingrid", "Omar"}

func (g *eventGen) day(d domain.CivilDate, i int) {
	rng := rand.New(rand.NewPCG(uint64(d.Year)*10000+uint64(d.Month)*100+uint64(d.Day), 7))
	wd := d.Weekday()
	weekday := wd >= time.Monday && wd <= time.Friday
	_, week := d.In(g.loc).ISOWeek()

	if weekday {
		g.timed("standup", calTeam, "Daily standup", d, 9, 30, 15*time.Minute)
		if rng.IntN(3) > 0 {
			g.timed("focus", calWork, "Focus time", d, 10, 0, 2*time.Hour)
		}
		switch wd {
		case time.Monday:
			e := g.timed("planning", calTeam, "Sprint planning", d, 10, 0, time.Hour)
			g.events[len(g.events)-1] = withPlace(e, "Room 4B")
		case time.Tuesday:
			g.timed("one-on-one", calWork, "1:1 with Sam", d, 14, 0, 30*time.Minute)
			g.timed("gym", calFitness, "Gym", d, 7, 0, time.Hour)
		case time.Wednesday:
			g.add(meeting("design-review", calWork, "Design review", g.at(d, 11, 0), time.Hour))
			g.timed("bouldering", calClimbing, "Bouldering", d, 19, 0, 2*time.Hour)
			g.add(domain.NewTimedEvent("recycling/"+d.String(), calFamily, "Take out the recycling", g.at(d, 20, 0), g.at(d, 20, 0), "", ""))
		case time.Thursday:
			g.timed("demo", calTeam, "Sprint demo", d, 15, 0, 45*time.Minute)
			g.timed("gym", calFitness, "Gym", d, 7, 0, time.Hour)
		case time.Friday:
			g.add(meeting("weekly-sync", calWork, "Weekly sync", g.at(d, 14, 0), 45*time.Minute))
			if week%2 == 0 {
				g.timed("retro", calTeam, "Retro", d, 16, 0, time.Hour)
			}
			g.timed("pizza", calFamily, "Pizza night", d, 19, 0, 2*time.Hour)
		}
		if wd == time.Monday || wd == time.Wednesday {
			g.timed("pickup", calFamily, "Pick up the kids", d, 16, 30, 30*time.Minute)
		}
		if wd == time.Monday || wd == time.Thursday {
			g.timed("studio", calStudio, "Side project time", d, 20, 0, 2*time.Hour)
		}
		if wd == time.Tuesday {
			g.timed("book-club", calLibrary, "Book club", d, 18, 30, 90*time.Minute)
		}
		extra := []int{0, 0, 1, 1, 2}[rng.IntN(5)]
		if rng.IntN(6) == 0 { // a crunch day
			extra = 4 + rng.IntN(3)
		}
		for k := 0; k < extra; k++ {
			start := 10*60 + rng.IntN(14)*30 // 10:00 to 16:30
			dur := []time.Duration{30, 45, 60, 90}[rng.IntN(4)] * time.Minute
			cal := []string{calWork, calTeam}[rng.IntN(2)]
			title := meetingTitles[rng.IntN(len(meetingTitles))]
			g.timed(fmt.Sprintf("extra%d", k), cal, title, d, start/60, start%60, dur)
		}
		if rng.IntN(8) == 0 {
			g.timed("lunch", calFamily, "Lunch with "+friendNames[rng.IntN(len(friendNames))], d, 12, 0, time.Hour)
		}
	} else {
		if wd == time.Saturday {
			g.timed("long-run", calFitness, "Long run", d, 9, 0, 2*time.Hour)
			if rng.IntN(2) == 0 {
				g.timed("writing", calStudio, "Writing session", d, 13, 0, 2*time.Hour)
			}
			if rng.IntN(2) == 0 {
				g.timed("dinner", calFamily, "Dinner at the Lindqvists", d, 19, 30, 3*time.Hour)
			}
		} else {
			g.timed("brunch", calFamily, "Family brunch", d, 11, 0, 2*time.Hour)
		}
	}
	// On call for a week, every fourth week.
	if wd == time.Monday && week%4 == 1 {
		g.allDay("oncall", calOnCall, "On-call: primary", d, 7)
	}
}

// meeting is a video call with an agenda and a recording link, as in the
// real feeds.
func meeting(series, cal, title string, start time.Time, dur time.Duration) domain.Event {
	d := domain.CivilDateFromTime(start)
	e := domain.NewTimedEvent(series+"/"+d.String(), cal, title, start, start.Add(dur), "",
		"Agenda in the shared doc [2]. Join with [1].").WithLinks([]domain.Link{
		{URL: "https://example.com/meet/" + series, Label: "Join the video call", Kind: domain.LinkVideoCall},
		{URL: "https://example.com/docs/" + series + "-agenda", Label: "Agenda doc", Kind: domain.LinkDocument},
	}).WithGuests(teamGuests())
	return e
}

func withPlace(e domain.Event, place string) domain.Event {
	e.Location = domain.SafeText(place)
	return e
}

var longNotes = strings.Join([]string{
	"Quarterly planning for the platform group. Please read the pre-read [2] before the session, and bring your team's top three asks.",
	"",
	"Agenda",
	"1. Where we are: the last quarter in numbers [3]",
	"2. Customer themes, from support and sales",
	"3. Platform migration: scope, risks and the staffing question",
	"4. Reliability goals and the on-call load",
	"5. Developer experience: what to stop doing",
	"6. Proposals, five minutes each",
	"7. Prioritization exercise",
	"8. Owners, dates and what we tell the rest of the company",
	"",
	"Logistics",
	"Room 4B has the big screen; remote attendees join by video [1]. Lunch is provided, so please say if you have dietary requirements.",
	"The session is recorded, and the notes go in the shared doc afterwards.",
	"",
	"If you can't make it, send a delegate who can commit on your behalf, and add your asks to the doc before Monday.",
	"",
	"Thanks,",
	"The planning committee",
}, "\n")

// special adds the events that exist to be looked at: crowded or odd days that
// the weekly pattern doesn't make.
func (g *eventGen) special() {
	t := g.today
	// A long description with links, to scroll and open.
	q := g.at(t.AddDays(2), 10, 0)
	g.add(domain.NewTimedEvent("quarterly/"+t.AddDays(2).String(), calWork, "Quarterly planning", q, q.Add(150*time.Minute),
		"Room 4B, Stockholm HQ", longNotes).WithLinks([]domain.Link{
		{URL: "https://example.com/meet/quarterly", Label: "Join the video call", Kind: domain.LinkVideoCall},
		{URL: "https://example.com/docs/quarterly-preread", Label: "Pre-read", Kind: domain.LinkDocument},
		{URL: "https://example.com/docs/q3-numbers?view=full&section=summary&utm_source=calendar&utm_medium=invite&utm_campaign=quarterly-planning", Label: "Last quarter in numbers", Kind: domain.LinkDocument},
	}).WithGuests(crowd(60)))
	// Titles that don't fit, and ones that aren't ASCII.
	g.timed("alignment", calWork, "Cross-functional alignment on the platform migration roadmap and Q4 staffing, pre-read required", t.AddDays(3), 15, 0, time.Hour)
	g.timed("jp", calWork, "日本語 meeting 🎉", t.AddDays(1), 13, 0, time.Hour)
	// A crowded day for the time grid (Week+): two events side by side, five
	// at once, back to back, a long one under shorter ones, and a reminder.
	c := t.AddDays(5)
	g.timed("crunch-design", calWork, "Design review", c, 10, 0, 90*time.Minute)
	g.timed("crunch-1on1", calWork, "1:1 with Sam", c, 10, 30, 30*time.Minute)
	for i, title := range []string{"Incident triage", "Customer call", "Hiring panel", "Budget check"} {
		g.timed(fmt.Sprintf("crunch-pile-%d", i), []string{calWork, calTeam, calWork, calFamily}[i], title, c, 14, 0, 45*time.Minute)
	}
	g.timed("crunch-workshop", calTeam, "Planning workshop", c, 13, 30, 3*time.Hour)
	g.timed("crunch-b2b-1", calWork, "Interview: backend", c, 16, 30, 30*time.Minute)
	g.timed("crunch-b2b-2", calWork, "Interview: platform", c, 17, 0, 30*time.Minute)
	g.add(domain.NewTimedEvent("crunch-reminder/"+c.String(), calFamily, "Pick up the parcel", g.at(c, 12, 0), g.at(c, 12, 0), "", ""))
	// Past midnight.
	night := g.at(t.AddDays(12), 23, 0)
	g.add(domain.NewTimedEvent("night-train/"+t.AddDays(12).String(), calFamily, "Night train to Berlin", night, night.Add(7*time.Hour+30*time.Minute), "Stockholm Central", ""))
	// Days spanning days.
	g.allDay("conf", calWork, "Conference: GopherCon", t.AddDays(9), 3)
	g.allDay("offsite", calTeam, "Team offsite", t.AddDays(20), 2)
	g.allDay("vacation", calFamily, "Vacation: the archipelago", t.AddDays(35), 7)
	g.allDay("course", calWork, "Training course", t.AddDays(-20), 2)
	// Birthdays, every so often.
	for i, name := range []string{"Alex", "Mum", "Priya", "Marcus", "Grandpa", "Lena", "Tom", "Ingrid", "Sam", "Omar", "Aunt Karin", "Jonas"} {
		g.allDay("birthday-"+strings.ToLower(strings.ReplaceAll(name, " ", "-")), calBirthdays, "🎂 "+name+"'s birthday", t.AddDays(3+i*17-10), 1)
	}
	// A few public holidays, by date and relative to today.
	for _, h := range []struct {
		m, d  int
		title string
	}{{1, 1, "New Year's Day"}, {5, 1, "Labour Day"}, {10, 31, "Halloween"}, {12, 24, "Christmas Eve"}, {12, 25, "Christmas Day"}, {12, 26, "Boxing Day"}} {
		for _, y := range []int{t.Year, t.Year + 1} {
			if d := domain.NewCivilDate(y, h.m, h.d); !d.Before(t.AddDays(-daysBack)) && d.Before(t.AddDays(daysAhead)) {
				g.allDay("holiday-"+h.title, calHolidays, h.title, d, 1)
			}
		}
	}
	g.allDay("bank-holiday", calHolidays, "Bank holiday", t.AddDays(11), 1)
}

// nearNow are events around the current moment, so the pulse, the "now" line
// and an event in progress can be seen whenever the mock is started.
func (g *eventGen) nearNow() {
	m := func(d time.Duration) time.Time { return g.now.Truncate(time.Minute).Add(d) }
	add := func(id, cal, title string, start time.Time, dur time.Duration) {
		g.add(domain.NewTimedEvent("now/"+id, cal, title, start, start.Add(dur), "", ""))
	}
	add("coffee", calFamily, "Coffee with Priya", m(-3*time.Hour), 30*time.Minute)
	add("deep-work", calWork, "Deep work", m(-25*time.Minute), 90*time.Minute)
	add("quick-sync", calWork, "Quick sync", m(8*time.Minute), 30*time.Minute)
	add("deploy", calOnCall, "Deploy window", m(3*time.Hour), time.Hour)
}
