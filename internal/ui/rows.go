package ui

import (
	"time"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

type rowKind int

const (
	rowBlank rowKind = iota
	rowDay
	rowItem
	rowNow
	rowNothing
)

// row is one line of the agenda pane.
type row struct {
	kind rowKind
	date domain.CivilDate
	item agenda.Item // rowItem only
}

// key identifies a selectable row across reloads, so the selection survives
// a sync even if rows shift around it. An empty day's row has no event ID.
func (r row) key() string {
	return rowKey(r.date, r.item.Event.ID)
}

func rowKey(date domain.CivilDate, eventID string) string {
	return date.String() + "|" + eventID
}

// buildRows lays out every day as agenda lines: a header, then its events or
// "nothing scheduled". Today also gets a "now" marker before the first event
// starting after now.
func buildRows(days []agenda.Day, today domain.CivilDate, now time.Time) []row {
	var rows []row
	for _, day := range days {
		isToday := day.Date == today
		if len(rows) > 0 {
			rows = append(rows, row{kind: rowBlank})
		}
		rows = append(rows, row{kind: rowDay, date: day.Date})

		if len(day.Items) == 0 {
			rows = append(rows, row{kind: rowNothing, date: day.Date})
			if isToday {
				rows = append(rows, row{kind: rowNow, date: day.Date})
			}
			continue
		}

		marked := !isToday
		for _, it := range day.Items {
			if !marked && !it.Event.AllDay && it.Start.After(now) {
				rows = append(rows, row{kind: rowNow, date: day.Date})
				marked = true
			}
			rows = append(rows, row{kind: rowItem, date: day.Date, item: it})
		}
		if !marked {
			rows = append(rows, row{kind: rowNow, date: day.Date})
		}
	}
	return rows
}

// selectable returns the indices of rows the cursor can land on: events, and
// the "nothing scheduled" line of an empty day.
func selectable(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowItem || r.kind == rowNothing {
			out = append(out, i)
		}
	}
	return out
}
