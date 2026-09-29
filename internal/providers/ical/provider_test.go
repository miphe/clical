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

	"github.com/cenkalti/backoff/v5"

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
	p.newBackOff = func() backoff.BackOff { return backoff.NewConstantBackOff(time.Millisecond) }
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

// statusServer answers with codes in order, repeating the last one, and
// serves the fixture feed on 200.
func statusServer(t *testing.T, codes ...int) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := codes[min(requests, len(codes)-1)]
		requests++
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Write([]byte(strings.Join(fixtureLines, "\r\n") + "\r\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestSyncEventsRetries(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		codes           []int
		wantErr         bool
		wantUnavailable bool
		wantRequests    int
	}{
		{"transient 503 then ok", []int{503, 503, 200}, false, false, 3},
		{"rate limited then ok", []int{429, 200}, false, false, 2},
		{"persistent 500 gives up", []int{500}, true, false, 4},
		{"404 is unavailable, no retry", []int{404}, true, true, 1},
		{"410 is unavailable, no retry", []int{410}, true, true, 1},
		{"401 is unavailable, no retry", []int{401}, true, true, 1},
		{"400 fails, no retry", []int{400}, true, false, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, requests := statusServer(t, c.codes...)
			_, err := testProvider(srv.URL, &now).SyncEvents(context.Background(), domain.Calendar{ID: "cal1"}, "")

			if (err != nil) != c.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, c.wantErr)
			}
			var unavailable *domain.CalendarUnavailableError
			if got := errors.As(err, &unavailable); got != c.wantUnavailable {
				t.Errorf("CalendarUnavailableError = %v, want %v (err: %v)", got, c.wantUnavailable, err)
			}
			if c.wantUnavailable && unavailable.CalendarID != "cal1" {
				t.Errorf("CalendarID = %q, want cal1", unavailable.CalendarID)
			}
			if *requests != c.wantRequests {
				t.Errorf("requests = %d, want %d", *requests, c.wantRequests)
			}
		})
	}
}

func TestSyncEventsStopsRetryingWhenContextCancelled(t *testing.T) {
	srv, requests := statusServer(t, 503)
	now := time.Now()
	p := testProvider(srv.URL, &now)
	p.newBackOff = func() backoff.BackOff { return backoff.NewConstantBackOff(time.Hour) }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := p.SyncEvents(ctx, domain.Calendar{ID: "cal1"}, ""); err == nil {
		t.Fatal("error = nil, want context error")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v, want prompt return on cancellation", time.Since(start))
	}
	if *requests != 1 {
		t.Errorf("requests = %d, want 1", *requests)
	}
}
