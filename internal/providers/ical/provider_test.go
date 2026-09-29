package ical

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

type feedServer struct {
	*httptest.Server
	requests    int
	lastIfMatch string
}

func newFeedServer(t *testing.T, etag string) *feedServer {
	t.Helper()
	fs := &feedServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.requests++
		fs.lastIfMatch = r.Header.Get("If-None-Match")
		if etag != "" && fs.lastIfMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Type", "text/calendar")
		// Like Google, stamp every event with the download time.
		stamp := fmt.Sprintf("DTSTAMP:20260929T1200%02dZ", fs.requests)
		body := strings.ReplaceAll(strings.Join(fixtureLines, "\r\n")+"\r\n", "DTSTAMP:20260901T000000Z", stamp)
		w.Write([]byte(body))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func testProvider(feedURL string, now *time.Time) *Provider {
	p := New(func(string) (string, error) { return feedURL, nil })
	p.now = func() time.Time { return *now }
	return p
}

func TestSyncEventsFullThenNotModified(t *testing.T) {
	srv := newFeedServer(t, `"v1"`)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p := testProvider(srv.URL, &now)
	cal := domain.Calendar{ID: "cal1"}

	first, err := p.SyncEvents(context.Background(), cal, "")
	if err != nil {
		t.Fatalf("first SyncEvents() error = %v", err)
	}
	if !first.FullResync || len(first.Upserted) == 0 {
		t.Fatalf("first sync = FullResync %v with %d events, want full resync with events", first.FullResync, len(first.Upserted))
	}

	second, err := p.SyncEvents(context.Background(), cal, first.NextToken)
	if err != nil {
		t.Fatalf("second SyncEvents() error = %v", err)
	}
	if srv.lastIfMatch != `"v1"` {
		t.Errorf("second request If-None-Match = %q, want %q", srv.lastIfMatch, `"v1"`)
	}
	if second.FullResync || len(second.Upserted) != 0 || second.NextToken != first.NextToken {
		t.Errorf("second sync = %+v, want empty non-full result keeping the token", second)
	}
}

func TestSyncEventsUnchangedEventsWithoutValidatorsIsNoop(t *testing.T) {
	srv := newFeedServer(t, "") // no ETag, like Google's feeds
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p := testProvider(srv.URL, &now)
	cal := domain.Calendar{ID: "cal1"}

	first, err := p.SyncEvents(context.Background(), cal, "")
	if err != nil {
		t.Fatalf("first SyncEvents() error = %v", err)
	}
	second, err := p.SyncEvents(context.Background(), cal, first.NextToken)
	if err != nil {
		t.Fatalf("second SyncEvents() error = %v", err)
	}
	if srv.requests != 2 {
		t.Errorf("requests = %d, want 2 (body is still downloaded)", srv.requests)
	}
	if second.FullResync || len(second.Upserted) != 0 {
		t.Errorf("second sync = FullResync %v with %d events, want no-op", second.FullResync, len(second.Upserted))
	}
}

func TestSyncEventsNewDayForcesFullFetch(t *testing.T) {
	srv := newFeedServer(t, `"v1"`)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p := testProvider(srv.URL, &now)
	cal := domain.Calendar{ID: "cal1"}

	first, err := p.SyncEvents(context.Background(), cal, "")
	if err != nil {
		t.Fatalf("SyncEvents() error = %v", err)
	}

	now = now.Add(24 * time.Hour)
	next, err := p.SyncEvents(context.Background(), cal, first.NextToken)
	if err != nil {
		t.Fatalf("next-day SyncEvents() error = %v", err)
	}
	if srv.lastIfMatch != "" {
		t.Errorf("next-day request sent If-None-Match %q, want none", srv.lastIfMatch)
	}
	if !next.FullResync {
		t.Error("next-day sync FullResync = false, want true")
	}
}

func TestSyncEventsErrorsDoNotLeakURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	secret := srv.URL + "/private-s3cr3t/basic.ics"

	now := time.Now()
	for name, feedURL := range map[string]string{
		"http status":   secret,
		"network error": "https://127.0.0.1:1/private-s3cr3t/basic.ics",
	} {
		_, err := testProvider(feedURL, &now).SyncEvents(context.Background(), domain.Calendar{ID: "cal1"}, "")
		if err == nil {
			t.Fatalf("%s: SyncEvents() error = nil, want error", name)
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: error leaks the feed URL: %v", name, err)
		}
	}
}

func TestSyncEventsLookupFailure(t *testing.T) {
	p := New(func(string) (string, error) { return "", errors.New("no such secret") })
	if _, err := p.SyncEvents(context.Background(), domain.Calendar{ID: "cal1"}, ""); err == nil {
		t.Error("SyncEvents() with failing lookup error = nil, want error")
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"  https://calendar.google.com/x/basic.ics \n", "https://calendar.google.com/x/basic.ics", false},
		{"webcal://calendar.google.com/x/basic.ics", "https://calendar.google.com/x/basic.ics", false},
		{"http://calendar.google.com/x/basic.ics", "", true},
		{"not a url", "", true},
		{"https://", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}
