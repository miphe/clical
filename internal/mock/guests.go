package mock

import (
	"fmt"

	"github.com/miphe/clical/internal/domain"
)

var guestNames = []string{
	"Alma Lindqvist", "Bo Eriksson", "Camille Dubois", "Dev Patel", "Elin Nyström", "Farid Haddad",
	"Greta Holm", "Hiro Tanaka", "Ines Costa", "Jonas Berg", "Katarina Svensson", "Lars Öberg",
	"Mei Chen", "Nils Persson", "Olivia Grant", "Pavel Novak", "Quinn Murphy", "Rosa Martín",
	"Sven Andersson", "Tove Larsson", "Ulf Karlsson", "Vera Ivanova", "Wilma Strand", "Xavier Moreau",
	"Yara Nasser", "Zoe Fischer",
}

// teamGuests is a small team: you, the organizer, and a few answers of each
// kind, one of them optional.
func teamGuests() []domain.Guest {
	return []domain.Guest{
		{Name: "Alma Lindqvist", Email: "alma@example.com", Response: domain.ResponseYes, Organizer: true},
		{Name: "You", Email: "you@example.com", Response: domain.ResponseYes, Self: true},
		{Name: "Bo Eriksson", Email: "bo@example.com", Response: domain.ResponseMaybe},
		{Name: "Dev Patel", Email: "dev@example.com", Response: domain.ResponseNo},
		{Email: "guest@example.org", Response: domain.ResponseNone, Optional: true},
	}
}

// crowd is n guests, enough to scroll and to search: answers and optional
// guests are spread out, names repeat with a number, and the first has no
// name, only an address.
func crowd(n int) []domain.Guest {
	out := []domain.Guest{{Name: "Alma Lindqvist", Email: "alma@example.com", Response: domain.ResponseYes, Organizer: true}}
	for i := 1; i < n; i++ {
		name := guestNames[i%len(guestNames)]
		if i >= len(guestNames) {
			name = fmt.Sprintf("%s %d", name, i/len(guestNames)+1)
		}
		g := domain.Guest{Name: name, Email: fmt.Sprintf("guest%d@example.com", i), Response: domain.Response(i % 4), Optional: i%7 == 0}
		if i == 3 {
			g.Name = ""
		}
		out = append(out, g)
	}
	out[2].Self, out[2].Name = true, "You"
	return out
}
