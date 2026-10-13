package write

import (
	"sort"

	"github.com/miphe/clical/internal/domain"
)

// DayArchive is what was filed under one day: the tasks completed on it and
// the notes archived on it, each oldest first.
type DayArchive struct {
	Tasks, Notes []Archived
}

// ArchiveByDay groups the archive by day. A task belongs to the day it was
// completed, not the day it was archived; a note, which is never completed,
// to the day it was archived. The day is the one in that moment's own UTC
// offset, so a task checked at 23:30 in Stockholm counts for that day wherever
// the file is read later. Records without a moment are left out.
func ArchiveByDay(list []Archived) map[domain.CivilDate]DayArchive {
	out := make(map[domain.CivilDate]DayArchive)
	for _, a := range list {
		at := a.Moment()
		if at.IsZero() {
			continue
		}
		day := domain.CivilDateFromTime(at)
		d := out[day]
		if a.Note {
			d.Notes = append(d.Notes, a)
		} else {
			d.Tasks = append(d.Tasks, a)
		}
		out[day] = d
	}
	for _, d := range out {
		for _, group := range [][]Archived{d.Tasks, d.Notes} {
			sort.SliceStable(group, func(i, j int) bool { return group[i].Moment().Before(group[j].Moment()) })
		}
	}
	return out
}
