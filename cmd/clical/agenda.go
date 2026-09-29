package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/store"
)

// AgendaCmd prints upcoming events from the local cache as plain text. It
// reads only the store, so no passphrase or network is needed.
type AgendaCmd struct {
	Days int `default:"14" help:"Number of days to show, starting today."`
}

func (c *AgendaCmd) Run() error {
	if c.Days < 1 {
		return fmt.Errorf("--days must be at least 1")
	}

	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	labels, err := calendarLabels(ctx, db)
	if err != nil {
		return err
	}

	loc := time.Local
	today := domain.CivilDateFromTime(time.Now().In(loc))
	from, to := today.In(loc), today.AddDays(c.Days).In(loc)

	events, err := db.EventsInRange(ctx, from, to)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Printf("No events in the next %d days. Run `clical sync` to fetch the latest.\n", c.Days)
		return nil
	}

	type row struct{ when, title, calendar string }
	type section struct {
		header string
		rows   []row
	}
	var sections []section
	titleWidth := 0
	for _, day := range agenda.Build(events, today, c.Days, loc) {
		if len(day.Items) == 0 && day.Date != today {
			continue
		}
		sec := section{header: day.Date.In(loc).Format("Mon 2 Jan")}
		if day.Date == today {
			sec.header += " (today)"
		}
		for _, it := range day.Items {
			r := row{timeLabel(day.Date, it, loc), it.Event.Title, labels[it.Event.CalendarID]}
			titleWidth = max(titleWidth, uniseg.StringWidth(r.title))
			sec.rows = append(sec.rows, r)
		}
		sections = append(sections, sec)
	}

	for i, sec := range sections {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(sec.header)
		if len(sec.rows) == 0 {
			fmt.Println("  nothing scheduled")
		}
		for _, r := range sec.rows {
			fmt.Printf("  %s   %s   %s\n", padRight(r.when, len("00:00–00:00")), padRight(r.title, titleWidth), r.calendar)
		}
	}
	return nil
}

// padRight pads s with spaces to width terminal cells.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-uniseg.StringWidth(s)))
}

// timeLabel renders an item's time as seen on one day: "all day", or
// "HH:MM–HH:MM" with "…" where the event continues from or into another day.
func timeLabel(date domain.CivilDate, it agenda.Item, loc *time.Location) string {
	if it.Event.AllDay {
		return "all day"
	}
	dayStart, dayEnd := date.In(loc), date.AddDays(1).In(loc)

	start := "…"
	if !it.Start.Before(dayStart) {
		start = it.Start.Format("15:04")
	}
	end := "…"
	switch {
	case it.End.Equal(dayEnd):
		end = "24:00"
	case !it.End.After(dayEnd):
		end = it.End.Format("15:04")
	}
	if it.Start.Equal(it.End) {
		return start
	}
	return start + "–" + end
}

// calendarLabels maps calendar ID to "Account · Calendar".
func calendarLabels(ctx context.Context, db *store.Store) (map[string]string, error) {
	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	acctLabel := make(map[string]string, len(accounts))
	for _, a := range accounts {
		acctLabel[a.ID] = a.Label
	}
	cals, err := db.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(cals))
	for _, cal := range cals {
		out[cal.ID] = acctLabel[cal.AccountID] + " · " + cal.Name
	}
	return out, nil
}
