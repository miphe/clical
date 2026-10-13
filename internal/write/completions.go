package write

import (
	"sort"

	"github.com/miphe/clical/internal/domain"
)

// CompletionsByDay groups archived tasks by the day they were completed, not
// the day they were archived. The day is the one in the completion time's own
// UTC offset, so a task checked at 23:30 in Stockholm counts for that day
// wherever the file is read later. Each day's tasks are oldest first, and
// records without a completion time are left out.
func CompletionsByDay(list []Archived) map[domain.CivilDate][]Archived {
	out := make(map[domain.CivilDate][]Archived)
	for _, a := range list {
		if a.Completed.IsZero() {
			continue
		}
		day := domain.CivilDateFromTime(a.Completed)
		out[day] = append(out[day], a)
	}
	for _, tasks := range out {
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Completed.Before(tasks[j].Completed) })
	}
	return out
}
