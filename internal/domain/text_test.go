package domain

import (
	"testing"
	"time"
)

func TestSafeText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Plain title", "Plain title"},
		{"two\nlines", "two\nlines"},
		{"tab\there", "tab here"},
		{"osc\x1b]52;c;aGk=\x07end", "osc]52;c;aGk=end"},
		{"csi\x1b[2Jend", "csi[2Jend"},
		{"c1\u009b2Jend", "c12Jend"},
		{"cr\r\nlf", "cr\nlf"},
		{"del\x7f", "del"},
		{"bad\xffutf8", "bad�utf8"},
		{"Café ☕", "Café ☕"},
	}
	for _, tt := range tests {
		if got := SafeText(tt.in); got != tt.want {
			t.Errorf("SafeText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEventTextIsMadeSafe(t *testing.T) {
	now := time.Now()
	e := NewTimedEvent("id", "cal", "a\x1b[31m", now, now, "b\x1b]0;x\x07", "c\x1b[H").
		WithLinks([]Link{{URL: "https://example.com", Label: "d\x1b[8m"}})
	if e.Title != "a[31m" || e.Location != "b]0;x" || e.Notes != "c[H" || e.Links[0].Label != "d[8m" {
		t.Errorf("escape bytes survived: %q %q %q %q", e.Title, e.Location, e.Notes, e.Links[0].Label)
	}
	d := NewCivilDate(2026, 1, 1)
	if a := NewAllDayEvent("id", "cal", "\x1b[1m", d, d.AddDays(1), "", ""); a.Title != "[1m" {
		t.Errorf("all-day title = %q", a.Title)
	}
}
