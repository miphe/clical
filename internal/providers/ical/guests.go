package ical

import (
	"strings"

	"github.com/miphe/clical/internal/domain"
)

// gocal reads an attendee's name and PARTSTAT but drops ROLE, which says
// whether they are optional. So the guests are read from the feed's own
// lines, once, and matched to gocal's events by UID and RECURRENCE-ID.

// guestKey identifies a VEVENT in the feed: an override of one occurrence
// has the same UID as the series, and a RECURRENCE-ID of its own.
func guestKey(uid, recurrenceID string) string { return uid + "\x00" + recurrenceID }

// readGuests collects the ATTENDEEs of every VEVENT in feed. ATTENDEEs of
// nested components (a VALARM's, for one) are not guests.
func readGuests(feed []byte) map[string][]domain.Guest {
	out := map[string][]domain.Guest{}
	var (
		inEvent       bool
		depth         int
		uid, recID    string
		organizerMail string
		guests        []domain.Guest
	)
	for _, l := range logicalLines(feed) {
		key, params, value, ok := splitLine(l.text)
		if !ok {
			continue
		}
		k, v := strings.ToUpper(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch {
		case k == "BEGIN" && !inEvent && strings.EqualFold(v, "VEVENT"):
			inEvent, depth, uid, recID, organizerMail, guests = true, 0, "", "", "", nil
		case !inEvent:
		case k == "BEGIN":
			depth++
		case k == "END" && depth > 0:
			depth--
		case k == "END" && strings.EqualFold(v, "VEVENT"):
			inEvent = false
			if len(guests) == 0 {
				break
			}
			for i := range guests {
				if organizerMail != "" && strings.EqualFold(guests[i].Email, organizerMail) {
					guests[i].Organizer = true
				}
			}
			out[guestKey(uid, recID)] = guests
		case depth > 0:
		case k == "UID":
			uid = v
		case k == "RECURRENCE-ID":
			recID = v
		case k == "ORGANIZER":
			organizerMail = mailAddress(v)
		case k == "ATTENDEE":
			if g, ok := guestOf(params, v); ok {
				guests = append(guests, g)
			}
		}
	}
	return out
}

// guestOf turns an ATTENDEE line into a guest. Rooms and equipment aren't
// people, so they are left out.
func guestOf(params map[string]string, value string) (domain.Guest, bool) {
	g := domain.Guest{Email: mailAddress(value)}
	for name, val := range params {
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.ToUpper(name) {
		case "CN":
			g.Name = val
		case "ROLE":
			g.Optional = strings.EqualFold(val, "OPT-PARTICIPANT")
		case "CUTYPE":
			if strings.EqualFold(val, "RESOURCE") || strings.EqualFold(val, "ROOM") {
				return domain.Guest{}, false
			}
		case "PARTSTAT":
			switch strings.ToUpper(val) {
			case "ACCEPTED":
				g.Response = domain.ResponseYes
			case "TENTATIVE":
				g.Response = domain.ResponseMaybe
			case "DECLINED":
				g.Response = domain.ResponseNo
			}
		}
	}
	return g, g.Name != "" || g.Email != ""
}

// mailAddress is the address in a "mailto:" value, or "" for any other kind
// of calendar user address.
func mailAddress(v string) string {
	if len(v) > 7 && strings.EqualFold(v[:7], "mailto:") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}
