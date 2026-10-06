package mock

import (
	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// Calendar IDs, which events refer to.
const (
	calFamily    = "mock-family"
	calBirthdays = "mock-birthdays"
	calHolidays  = "mock-holidays"
	calFitness   = "mock-fitness"
	calWork      = "mock-work"
	calTeam      = "mock-team"
	calOnCall    = "mock-oncall"
	calStudio    = "mock-studio"
	calLibrary   = "mock-library"
	calClimbing  = "mock-climbing"
)

var (
	acctPersonal = domain.Account{ID: domain.NewAccountID("ical", "Personal"), Provider: "ical", Label: "Personal"}
	acctWork     = domain.Account{ID: domain.NewAccountID("google", "Work"), Provider: "google", Label: "Work"}
	acctSide     = domain.Account{ID: domain.NewAccountID("ical", "Side project"), Provider: "ical", Label: "Side project"}
	acctClub     = domain.Account{ID: domain.NewAccountID("google", "Climbing club"), Provider: "google", Label: "Climbing club"}
)

// calendars are ten, so the bullet shapes change after the sixth hue. One has
// a dead feed and one account needs to sign in again, to see those states.
func calendars() []app.CalendarInfo {
	c := func(a domain.Account, id, name string, slot int) app.CalendarInfo {
		return app.CalendarInfo{Account: a, Calendar: domain.Calendar{ID: id, AccountID: a.ID, Name: name, ColorSlot: slot}}
	}
	return []app.CalendarInfo{
		c(acctPersonal, calFamily, "Family", 1),
		c(acctPersonal, calBirthdays, "Birthdays", 2),
		c(acctPersonal, calHolidays, "Public holidays", 3),
		c(acctPersonal, calFitness, "Fitness", 4),
		c(acctWork, calWork, "Calendar", 5),
		c(acctWork, calTeam, "Team", 6),
		c(acctWork, calOnCall, "On-call", 7),
		c(acctSide, calStudio, "Studio", 8),
		c(acctSide, calLibrary, "Library (old feed)", 9),
		c(acctClub, calClimbing, "Bouldering", 10),
	}
}
