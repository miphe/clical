package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// icsText escapes s as an iCalendar TEXT value.
func icsText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`)
	return r.Replace(s)
}

// parseOne parses a single event whose extra property lines are props.
func parseOne(t *testing.T, props ...string) domain.Event {
	t.Helper()
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN",
		"BEGIN:VEVENT", "UID:e1", "DTSTAMP:20260901T000000Z",
		"DTSTART:20261006T080000Z", "DTEND:20261006T083000Z", "SUMMARY:Weekly sync",
	}
	lines = append(lines, props...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events, err := parse(strings.NewReader(strings.Join(lines, "\r\n")+"\r\n"), "cal", from, from.AddDate(0, 3, 0))
	if err != nil || len(events) != 1 {
		t.Fatalf("parse() = %d events, %v", len(events), err)
	}
	return events[0]
}

type wantLink struct {
	url, label string
	kind       domain.LinkKind
}

func checkLinks(t *testing.T, got []domain.Link, want []wantLink) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d links %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].URL != w.url || got[i].Label != w.label || got[i].Kind != w.kind {
			t.Errorf("link %d = %+v, want %+v", i+1, got[i], w)
		}
	}
}

const meetBlock = "-::~:~::~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~::~:~::-\n" +
	"Join with Google Meet: https://meet.google.com/abc-defg-hij\n" +
	"Or dial: (US) +1 555-0100 PIN: 123#\n" +
	"Learn more about Meet at: https://support.google.com/a/users/answer/9282720\n\n" +
	"Please do not edit this section.\n" +
	"-::~:~::~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~:~::~:~::-"

func TestGoogleInvite(t *testing.T) {
	desc := `Agenda in <a href="https://www.google.com/url?q=https://docs.google.com/document/d/abc123/edit&amp;sa=D&amp;source=calendar">the agenda doc</a>.<br><br>` +
		`<ul><li>Item one</li><li>Item &amp; two</li></ul><script>alert(1)</script>` + "\n\n" + meetBlock
	e := parseOne(t,
		"DESCRIPTION:"+icsText(desc),
		"X-GOOGLE-CONFERENCE:https://meet.google.com/abc-defg-hij",
	)
	checkLinks(t, e.Links, []wantLink{
		{"https://meet.google.com/abc-defg-hij", "Join Google Meet", domain.LinkVideoCall},
		{"https://docs.google.com/document/d/abc123/edit", "the agenda doc", domain.LinkDocument},
	})
	want := "Agenda in the agenda doc [2].\n\n• Item one\n• Item & two"
	if e.Notes != want {
		t.Errorf("notes = %q, want %q", e.Notes, want)
	}
}

func TestGoogleMeetBlockWithoutConferenceProperty(t *testing.T) {
	e := parseOne(t, "DESCRIPTION:"+icsText("Quick chat.\n\n"+meetBlock))
	checkLinks(t, e.Links, []wantLink{
		{"https://meet.google.com/abc-defg-hij", "Join Google Meet", domain.LinkVideoCall},
	})
	if e.Notes != "Quick chat." {
		t.Errorf("notes = %q, want the Meet block removed", e.Notes)
	}
}

func TestPlainTextZoomInvite(t *testing.T) {
	desc := "Join Zoom Meeting\nhttps://us02web.zoom.us/j/81234567890?pwd=abcDEF123.\n\nDocs: https://example.com/notes (see section 2)."
	e := parseOne(t, "DESCRIPTION:"+icsText(desc))
	checkLinks(t, e.Links, []wantLink{
		{"https://us02web.zoom.us/j/81234567890?pwd=abcDEF123", "Join Zoom meeting", domain.LinkVideoCall},
		{"https://example.com/notes", "example.com", domain.LinkOther},
	})
	want := "Join Zoom Meeting\nus02web.zoom.us [1].\n\nDocs: example.com [2] (see section 2)."
	if e.Notes != want {
		t.Errorf("notes = %q, want %q", e.Notes, want)
	}
}

func TestOutlookInvite(t *testing.T) {
	alt := `<html><body><p>Agenda</p><p><a href="https://eur01.safelinks.protection.outlook.com/?url=https%3A%2F%2Fcontoso.sharepoint.com%2Fsites%2Fteam%2Fplan.docx&amp;data=xyz">Q3 plan</a></p></body></html>`
	e := parseOne(t,
		"DESCRIPTION:Agenda",
		"X-ALT-DESC;FMTTYPE=text/html:"+icsText(alt),
		"X-MICROSOFT-SKYPETEAMSMEETINGURL:https://teams.microsoft.com/l/meetup-join/19%3ameeting_abc%40thread.v2/0",
	)
	checkLinks(t, e.Links, []wantLink{
		{"https://teams.microsoft.com/l/meetup-join/19%3ameeting_abc%40thread.v2/0", "Join Teams meeting", domain.LinkVideoCall},
		{"https://contoso.sharepoint.com/sites/team/plan.docx", "Q3 plan", domain.LinkDocument},
	})
	if want := "Agenda\n\nQ3 plan [2]"; e.Notes != want {
		t.Errorf("notes = %q, want %q", e.Notes, want)
	}
}

func TestOnlyWebLinksAreKept(t *testing.T) {
	desc := `<a href="javascript:alert(1)">click</a> <a href="file:///etc/passwd">file</a> <a href="mailto:a@b.example">mail</a> data:text/html,x`
	e := parseOne(t, "DESCRIPTION:"+icsText(desc), "URL:ftp://files.example/x")
	if len(e.Links) != 0 {
		t.Errorf("links = %+v, want none (only http/https are kept)", e.Links)
	}
	if !strings.Contains(e.Notes, "click") || strings.Contains(e.Notes, "javascript") {
		t.Errorf("notes = %q, want link text kept and targets dropped", e.Notes)
	}
}

func TestSameLinkFromManyPlacesIsListedOnce(t *testing.T) {
	e := parseOne(t,
		"LOCATION:https://meet.google.com/abc-defg-hij",
		"URL:https://meet.google.com/abc-defg-hij/",
		"DESCRIPTION:"+icsText("Call https://meet.google.com/abc-defg-hij"),
	)
	checkLinks(t, e.Links, []wantLink{
		{"https://meet.google.com/abc-defg-hij/", "Join Google Meet", domain.LinkVideoCall},
	})
	if e.Notes != "Call meet.google.com [1]" {
		t.Errorf("notes = %q", e.Notes)
	}
}

func TestVideoCallsComeFirst(t *testing.T) {
	desc := "Doc https://docs.google.com/spreadsheets/d/1 then call https://zoom.us/j/1"
	e := parseOne(t, "DESCRIPTION:"+icsText(desc))
	checkLinks(t, e.Links, []wantLink{
		{"https://zoom.us/j/1", "Join Zoom meeting", domain.LinkVideoCall},
		{"https://docs.google.com/spreadsheets/d/1", "Google Sheet", domain.LinkDocument},
	})
	if want := "Doc docs.google.com [2] then call zoom.us [1]"; e.Notes != want {
		t.Errorf("notes = %q, want %q", e.Notes, want)
	}
}

func TestPlainDescriptionWithoutLinksIsUnchanged(t *testing.T) {
	e := parseOne(t, "DESCRIPTION:"+icsText("Bring snacks, 3 < 4 is fine.\n\n\n\nSee you"))
	if len(e.Links) != 0 || e.Notes != "Bring snacks, 3 < 4 is fine.\n\nSee you" {
		t.Errorf("notes = %q links = %+v", e.Notes, e.Links)
	}
}
