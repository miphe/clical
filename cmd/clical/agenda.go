package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// AgendaCmd prints upcoming events from the local cache as plain text. It
// needs no passphrase or network.
type AgendaCmd struct {
	Days int `default:"14" help:"Number of days to show, starting today."`
}

func (c *AgendaCmd) Run() error {
	if c.Days < 1 {
		return fmt.Errorf("--days must be at least 1")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	clock := clockFor(cfg)
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()

	ctx := context.Background()
	cals, err := a.Calendars(ctx)
	if err != nil {
		return err
	}
	labels := make(map[string]string, len(cals))
	for _, cal := range cals {
		labels[cal.Calendar.ID] = cal.Label()
	}

	loc := time.Local
	today := domain.CivilDateFromTime(time.Now().In(loc))
	from, to := today.In(loc), today.AddDays(c.Days).In(loc)

	events, err := a.Events(ctx, from, to)
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
			r := row{it.TimeLabel(day.Date, loc, clock), it.Event.Title, labels[it.Event.CalendarID]}
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
			fmt.Printf("  %s   %s   %s\n", padRight(r.when, clock.Width()), padRight(r.title, titleWidth), r.calendar)
		}
	}
	return nil
}

// padRight pads s with spaces to width terminal cells.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-uniseg.StringWidth(s)))
}
