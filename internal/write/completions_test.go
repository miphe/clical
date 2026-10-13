package write

import (
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func TestArchiveByDay(t *testing.T) {
	cest := time.FixedZone("CEST", 2*3600)
	pdt := time.FixedZone("PDT", -7*3600)
	rec := func(id string, completed, archived time.Time) Archived {
		return Archived{ID: id, Text: id, Completed: completed, Archived: archived}
	}
	list := []Archived{
		// Checked late on the 5th, archived on the 7th: counts for the 5th.
		rec("late", time.Date(2026, 10, 5, 23, 30, 0, 0, cest), time.Date(2026, 10, 7, 9, 0, 0, 0, cest)),
		rec("early", time.Date(2026, 10, 5, 0, 10, 0, 0, cest), time.Date(2026, 10, 7, 9, 0, 0, 0, cest)),
		// 23:30 on the 5th in California is the 6th in UTC; it stays the 5th.
		rec("travel", time.Date(2026, 10, 5, 23, 30, 0, 0, pdt), time.Date(2026, 10, 6, 8, 0, 0, 0, pdt)),
		rec("next", time.Date(2026, 10, 6, 8, 0, 0, 0, cest), time.Date(2026, 10, 6, 9, 0, 0, 0, cest)),
		{ID: "none", Text: "none"},
	}
	got := ArchiveByDay(list)

	fifth, sixth := domain.NewCivilDate(2026, 10, 5), domain.NewCivilDate(2026, 10, 6)
	if len(got) != 2 {
		t.Fatalf("days = %d, want 2 (%v)", len(got), got)
	}
	var ids []string
	for _, a := range got[fifth].Tasks {
		ids = append(ids, a.ID)
	}
	// Oldest first by the instant: "early" 22:10 UTC the 4th, "late" 21:30 UTC
	// the 5th, "travel" 06:30 UTC the 6th.
	if want := []string{"early", "late", "travel"}; len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Errorf("the 5th = %v, want %v", ids, want)
	}
	if len(got[sixth].Tasks) != 1 || got[sixth].Tasks[0].ID != "next" {
		t.Errorf("the 6th = %v", got[sixth].Tasks)
	}
}

func TestArchiveByDayKeepsTheOffsetFromTheFile(t *testing.T) {
	// The offset is written with the record, and survives the round trip.
	cest := time.FixedZone("", 2*3600)
	a := Archived{ID: "x", Text: "x", Completed: time.Date(2026, 10, 5, 23, 30, 0, 0, cest), Archived: time.Date(2026, 10, 6, 1, 0, 0, 0, cest)}
	lines := parseArchive([]byte(encodeArchived(a) + "\n"))
	if len(lines) != 1 || lines[0].rec == nil {
		t.Fatalf("parse = %+v", lines)
	}
	got := ArchiveByDay([]Archived{*lines[0].rec})
	if _, ok := got[domain.NewCivilDate(2026, 10, 5)]; !ok || len(got) != 1 {
		t.Errorf("days = %v, want only the 5th", got)
	}
}

func TestArchiveByDayEmpty(t *testing.T) {
	if got := ArchiveByDay(nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestArchiveByDayFilesNotesByTheDayTheyWereArchived(t *testing.T) {
	cest := time.FixedZone("CEST", 2*3600)
	note := func(id string, at time.Time) Archived { return Archived{ID: id, Text: id, Archived: at, Note: true} }
	list := []Archived{
		note("late", time.Date(2026, 10, 5, 23, 30, 0, 0, cest)),
		note("early", time.Date(2026, 10, 5, 8, 0, 0, 0, cest)),
		{ID: "t", Text: "t", Completed: time.Date(2026, 10, 5, 9, 0, 0, 0, cest), Archived: time.Date(2026, 10, 6, 9, 0, 0, 0, cest)},
		{ID: "bare", Text: "no moment", Note: true},
	}
	got := ArchiveByDay(list)
	fifth := got[domain.NewCivilDate(2026, 10, 5)]
	if len(got) != 1 || len(fifth.Tasks) != 1 || len(fifth.Notes) != 2 {
		t.Fatalf("days = %+v", got)
	}
	if fifth.Notes[0].ID != "early" || fifth.Notes[1].ID != "late" {
		t.Errorf("notes not oldest first: %+v", fifth.Notes)
	}
}
