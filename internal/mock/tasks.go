package mock

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// seedWrite fills the notes store: the global list, notes on upcoming events,
// one list whose event is gone, and an archive of completed tasks that reaches
// back eighteen months.
func seedWrite(a *app.App, now time.Time, loc *time.Location, events []domain.Event) error {
	st, err := a.Write()
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(42, 1))
	s := &seeder{st: st, now: now, loc: loc, events: events, rng: rng}
	if err := s.globalList(); err != nil {
		return err
	}
	if err := s.eventLists(); err != nil {
		return err
	}
	return s.archive()
}

type seeder struct {
	st     *write.Store
	now    time.Time
	loc    *time.Location
	events []domain.Event
	rng    *rand.Rand
}

func (s *seeder) id() string { return fmt.Sprintf("%08x", s.rng.Uint32()) }

func (s *seeder) todo(text string, origin write.Ref) write.Item {
	return write.Item{ID: s.id(), Todo: true, Text: text, Origin: origin}
}

func (s *seeder) done(text string, origin write.Ref, ago time.Duration) write.Item {
	it := s.todo(text, origin)
	it.Done, it.Completed = true, s.now.In(s.loc).Add(-ago)
	return it
}

func note(text string) write.Item { return write.Item{Text: text} }

// event finds the next occurrence of a series from now (or the latest before
// now, with before set).
func (s *seeder) event(series string, before bool) (domain.Event, bool) {
	var best domain.Event
	found := false
	for _, e := range s.events {
		if !strings.HasPrefix(e.ID, series+"/") {
			continue
		}
		start, _, ok := e.TimeRange()
		if !ok {
			continue
		}
		if before && start.Before(s.now) && (!found || bestStart(best).Before(start)) {
			best, found = e, true
		}
		if !before && start.After(s.now) && (!found || start.Before(bestStart(best))) {
			best, found = e, true
		}
	}
	return best, found
}

func bestStart(e domain.Event) time.Time { t, _, _ := e.TimeRange(); return t }

func (s *seeder) globalList() error {
	var origin write.Ref
	if e, ok := s.event("weekly-sync", false); ok {
		origin = write.RefFor(e)
	}
	return s.st.Update(write.Ref{}, nil, func(l *write.List) error {
		l.Items = []write.Item{
			s.todo("Renew passport", write.Ref{}),
			s.todo("Reply to the landlord about the lease", write.Ref{}),
			s.todo("Draft the Q4 roadmap one-pager", write.Ref{}),
			s.todo("Review Sam's design doc", write.Ref{}),
			note("Ideas for the offsite: a hike, a hackathon half-day, no slides"),
			s.todo("Send the deck to the board", origin),
			s.todo("Fix the flaky integration test in CI", write.Ref{}),
			s.todo("Look into ergonomic chairs: compare three models, and ask facilities whether the budget covers a standing desk too", write.Ref{}),
			s.todo("Order a birthday present for Alex", write.Ref{}),
			s.todo("Book the dentist", write.Ref{}),
			note("Standing reminder: archive the finished ones on Friday afternoon"),
			s.todo("Write the migration runbook", write.Ref{}),
			s.todo("Cancel unused subscriptions", write.Ref{}),
			s.todo("Buy plant food", write.Ref{}),
			s.done("Water the plants", write.Ref{}, 5*time.Hour),
			s.done("Pay the electricity bill", write.Ref{}, 4*time.Hour),
			s.done("Send the invoice to the client", write.Ref{}, 22*time.Hour),
			note("Conference talk: three points, one demo, finish with the numbers"),
		}
		return nil
	})
}

func (s *seeder) eventLists() error {
	cal := map[string]string{}
	for _, c := range calendars() {
		cal[c.Calendar.ID] = c.Calendar.DisplayName()
	}
	list := func(e domain.Event, items func(ref write.Ref) []write.Item) error {
		ref := write.RefFor(e)
		start, _, _ := e.TimeRange()
		hdr := &write.Header{Event: ref, Calendar: cal[e.CalendarID], Start: start.In(s.loc).Format(time.RFC3339)}
		return s.st.Update(ref, hdr, func(l *write.List) error {
			l.Items = items(ref)
			return nil
		})
	}
	if e, ok := s.event("weekly-sync", false); ok {
		if err := list(e, func(r write.Ref) []write.Item {
			return []write.Item{
				s.todo("Prepare the status update", r), s.todo("Ask about the on-call rota", r),
				note("Agenda: roadmap, hiring, budget"), s.done("Collect the numbers from finance", r, 26*time.Hour),
			}
		}); err != nil {
			return err
		}
	}
	if e, ok := s.event("quarterly", false); ok {
		if err := list(e, func(r write.Ref) []write.Item {
			return []write.Item{
				s.todo("Read the pre-read", r), s.todo("Bring the team's top three asks", r), s.todo("Draft the staffing proposal", r),
				note("Ask for the decision on the migration date, not just the plan"),
				s.todo("Book the room for the follow-up", r), note("Delegate if I'm on call"),
			}
		}); err != nil {
			return err
		}
	}
	for series, items := range map[string][]string{
		"one-on-one": {"Give feedback on the design doc", "Discuss the promotion timeline"},
		"demo":       {"Record the demo", "Check the staging data"},
	} {
		if e, ok := s.event(series, false); ok {
			if err := list(e, func(r write.Ref) []write.Item {
				var out []write.Item
				for _, text := range items {
					out = append(out, s.todo(text, r))
				}
				return out
			}); err != nil {
				return err
			}
		}
	}
	if e, ok := s.event("demo", true); ok { // last week's, finished
		if err := list(e, func(r write.Ref) []write.Item {
			return []write.Item{s.done("Show the new onboarding flow", r, 30*time.Hour), s.done("Share the recording", r, 29*time.Hour)}
		}); err != nil {
			return err
		}
	}
	// A list whose event isn't in any calendar any more: "Unattached".
	gone := write.Ref{Calendar: calTeam, Key: "offsite-planning-cancelled", Title: "Offsite planning (cancelled)"}
	return s.st.Update(gone, &write.Header{Event: gone, Calendar: cal[calTeam], Start: s.now.In(s.loc).AddDate(0, 0, 14).Format(time.RFC3339)}, func(l *write.List) error {
		l.Items = []write.Item{s.todo("Find a venue", gone), s.todo("Poll the team on dates", gone), note("Cancelled: budget freeze")}
		return nil
	})
}

var (
	verbs = []string{"Review", "Draft", "Update", "Fix", "Write", "Send", "Schedule", "Check", "Clean up", "Refactor", "Plan", "Reply to", "Book", "Prepare", "Finish", "Ship", "Test", "Merge", "Archive", "Sort out"}
	nouns = []string{
		"the Q4 roadmap", "Sam's design doc", "the onboarding checklist", "the deploy script", "the flaky CI test", "the budget sheet",
		"the release notes", "customer feedback", "the API docs", "the on-call handbook", "the expense report", "insurance paperwork",
		"the garden shed", "bike tires", "the weekly metrics", "the migration runbook", "hiring panel notes", "the offsite agenda",
		"the launch email", "the staging data", "the dependency upgrade", "the retro actions", "the travel booking", "the tax forms",
		"Priya's question", "the monitoring dashboard", "the style guide", "the vendor contract", "my inbox", "the backlog",
	}
	recurring = []struct{ series, title string }{
		{"weekly-sync", "Weekly sync"}, {"standup", "Daily standup"}, {"planning", "Sprint planning"},
		{"one-on-one", "1:1 with Sam"}, {"design-review", "Design review"}, {"demo", "Sprint demo"},
	}
)

func (s *seeder) text() string {
	switch n := s.rng.IntN(20); {
	case n == 0:
		return fmt.Sprintf("Review pull request #%d", 100+s.rng.IntN(900))
	case n == 1:
		return fmt.Sprintf("Fix issue #%d", 1000+s.rng.IntN(9000))
	}
	return verbs[s.rng.IntN(len(verbs))] + " " + nouns[s.rng.IntN(len(nouns))]
}

// poisson draws a count with the given mean.
func (s *seeder) poisson(mean float64) int {
	limit, p, k := math.Exp(-mean), 1.0, 0
	for {
		k++
		p *= s.rng.Float64()
		if p <= limit {
			return k - 1
		}
	}
}

// weekdayRate is the tasks completed on an ordinary day: the middle of the
// week is busy, Friday is lighter, and weekends are mostly empty.
var weekdayRate = [7]float64{time.Sunday: 0.2, time.Monday: 2.4, time.Tuesday: 3.0, time.Wednesday: 3.2, time.Thursday: 2.8, time.Friday: 1.8, time.Saturday: 0.35}

// pace is how the year went: a normal pace with its own ups and downs, a
// stretch of crunch, a two week holiday, a streak, and a long quiet spell.
func (s *seeder) pace(daysAgo int, week *rand.Rand) float64 {
	f := 0.3 + 1.5*week.Float64()
	switch {
	case daysAgo >= 120 && daysAgo <= 133:
		return 0 // away
	case daysAgo >= 40 && daysAgo <= 46:
		return f * 3.4 // the crunch before a launch
	case daysAgo >= 160 && daysAgo <= 176:
		return 0.25 * f // the holidays
	case daysAgo >= 8 && daysAgo <= 14:
		return 1.5 // a streak
	case daysAgo > 400 && daysAgo < 470:
		return 0.5 * f
	}
	return f
}

func (s *seeder) archive() error {
	var recs []write.Archived
	today := s.now.In(s.loc)
	for ago := 0; ago <= 560; ago++ {
		day := time.Date(today.Year(), today.Month(), today.Day()-ago, 0, 0, 0, 0, s.loc)
		weekSeed := uint64(day.AddDate(0, 0, -int(day.Weekday())).Unix() / 86400)
		week := rand.New(rand.NewPCG(weekSeed, 3))
		n := min(s.poisson(weekdayRate[day.Weekday()]*s.pace(ago, week)), 14)
		if wd := day.Weekday(); wd >= time.Monday && wd <= time.Friday && s.rng.IntN(100) < 10 {
			n = 0 // an off day: meetings all day, or out
		} else if wd := day.Weekday(); wd >= time.Monday && wd <= time.Friday && n > 0 && s.rng.IntN(100) < 6 {
			n += 4 + s.rng.IntN(4) // a big day: a lot got finished at once
		}
		for k := 0; k < n; k++ {
			hour, span := 8, 10
			if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
				hour, span = 10, 6
			}
			completed := day.Add(time.Duration(hour)*time.Hour + time.Duration(s.rng.IntN(span*60))*time.Minute)
			if completed.After(today) {
				if ago > 0 || today.Sub(day) < 9*time.Hour {
					continue // today's tasks only up to now
				}
				completed = day.Add(8*time.Hour + time.Duration(s.rng.Int64N(int64(today.Sub(day)-8*time.Hour))))
			}
			archived := completed.Add(5 * time.Minute)
			if ago > 0 {
				if s.rng.IntN(100) < 55 {
					archived = day.Add(17*time.Hour + 30*time.Minute + time.Duration(s.rng.IntN(90))*time.Minute)
					if archived.Before(completed) {
						archived = completed.Add(10 * time.Minute)
					}
				} else {
					archived = day.AddDate(0, 0, 1).Add(8*time.Hour + time.Duration(s.rng.IntN(30))*time.Minute)
				}
			}
			rec := write.Archived{ID: s.id(), Text: s.text(), Completed: completed, Archived: archived}
			if s.rng.IntN(100) < 22 {
				r := recurring[s.rng.IntN(len(recurring))]
				rec.Origin = write.Ref{Calendar: calWork, Key: r.series + "/" + domain.CivilDateFromTime(completed).String(), Title: r.title}
			}
			recs = append(recs, rec)
		}
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Archived.Before(recs[j].Archived) })
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(s.st.Dir(), "archive.jsonl"), []byte(b.String()), 0o600)
}
