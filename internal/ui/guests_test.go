package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

func guestEvent(guests []domain.Guest) domain.Event {
	return domain.NewTimedEvent("g", "main", "Planning", at(29, 15, 0), at(29, 16, 0), "",
		"The description.").WithGuests(guests)
}

// crowdOf is n guests with distinct names and addresses.
func crowdOf(n int) []domain.Guest {
	out := make([]domain.Guest, n)
	for i := range out {
		out[i] = domain.Guest{Name: fmt.Sprintf("Person %03d", i), Email: fmt.Sprintf("p%03d@example.com", i), Response: domain.Response(i % 4)}
	}
	out[40].Name = "" // known by address only
	return out
}

func smallGuests() []domain.Guest {
	return []domain.Guest{
		{Name: "Jane Doe", Email: "jane@example.com", Response: domain.ResponseYes, Organizer: true},
		{Name: "Me", Response: domain.ResponseMaybe, Self: true},
		{Name: "Bob", Response: domain.ResponseNo, Optional: true},
		{Name: "Cy", Response: domain.ResponseNone},
	}
}

func TestGuestsShowAnswersAndRoles(t *testing.T) {
	m := detailOn(t, 90, 24, guestEvent(smallGuests()))
	out := ansi.Strip(screen(m))
	for _, want := range []string{
		"Guests  4: 1 yes · 1 maybe · 1 no · 1 no reply",
		"✓ Jane Doe  organizer",
		"? Me  you",
		"✗ Bob  optional",
		"· Cy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "jane@example.com") {
		t.Error("an address is shown beside a name")
	}
	// A short list is read before the description.
	if strings.Index(out, "Guests") > strings.Index(out, "Description") {
		t.Error("a short guest list should come before the description")
	}
}

func TestALongGuestListGoesAfterTheDescription(t *testing.T) {
	m := detailOn(t, 90, 40, guestEvent(crowdOf(60)))
	out := ansi.Strip(screen(m))
	if strings.Index(out, "Description") > strings.Index(out, "Guests") {
		t.Errorf("a long list should come after the description:\n%s", out)
	}
}

func TestFindNarrowsTheGuestsAndEscClearsItBeforeClosing(t *testing.T) {
	m := detailOn(t, 90, 24, guestEvent(crowdOf(60)))
	m = keys(t, m, "/")
	if m.findInput == nil {
		t.Fatal("/ did not open the find")
	}
	m = typed(t, m, "person 017")
	out := ansi.Strip(screen(m))
	if !strings.Contains(out, "1 of 60 match “person 017”") || !strings.Contains(out, "Person 017") || strings.Contains(out, "Person 018") {
		t.Errorf("find shows the wrong guests:\n%s", out)
	}
	if !strings.Contains(out, "find guest") {
		t.Errorf("the footer doesn't show the find:\n%s", out)
	}
	m = keys(t, m, "enter")
	if m.findInput != nil || m.detail.find != "person 017" {
		t.Fatalf("enter should keep the find: input %v, find %q", m.findInput, m.detail.find)
	}
	m = keys(t, m, "esc")
	if m.detail == nil || m.detail.find != "" {
		t.Fatalf("the first esc should clear the find and stay: detail %v", m.detail != nil)
	}
	m = keys(t, m, "esc")
	if m.detail != nil {
		t.Error("the second esc should close the event")
	}
}

func TestFindMatchesAnAddressAndCanComeUpEmpty(t *testing.T) {
	m := detailOn(t, 90, 24, guestEvent(crowdOf(60)))
	m = typed(t, keys(t, m, "/"), "p040@")
	out := ansi.Strip(screen(m))
	if !strings.Contains(out, "1 of 60 match") || !strings.Contains(out, "p040@example.com") {
		t.Errorf("a guest known by address wasn't found:\n%s", out)
	}
	m = typed(t, m, "zzz")
	if out := ansi.Strip(screen(m)); !strings.Contains(out, "Nobody matches") {
		t.Errorf("no empty-find notice:\n%s", out)
	}
	// esc inside the find clears it and leaves the input.
	m = keys(t, m, "esc")
	if m.findInput != nil || m.detail.find != "" {
		t.Error("esc should end the find and clear it")
	}
}

func TestEverySpotOfALongListIsReachable(t *testing.T) {
	m := detailOn(t, 90, 16, guestEvent(crowdOf(60)))
	for i := 0; i < 80 && !strings.Contains(ansi.Strip(screen(m)), "Person 059"); i++ {
		m = keys(t, m, "pgdown")
	}
	if !strings.Contains(ansi.Strip(screen(m)), "Person 059") {
		t.Error("the last guest can't be scrolled to")
	}
}

func TestSlashDoesNothingWithoutGuests(t *testing.T) {
	m := detailOn(t, 90, 24, linkEvent())
	m = keys(t, m, "/")
	if m.findInput != nil {
		t.Error("/ opened a find in an event with no guests")
	}
}

func TestGoldenDetailWithGuests(t *testing.T) {
	m := detailOn(t, 90, 24, guestEvent(smallGuests()))
	golden(t, "detail_guests", screen(m))
}
