package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/oauth2"

	"github.com/miphe/clical/internal/domain"
)

type memTokens map[string]*oauth2.Token

func (m memTokens) LoadToken(id string) (*oauth2.Token, error) {
	if t, ok := m[id]; ok {
		return t, nil
	}
	return nil, errors.New("no token")
}
func (m memTokens) SaveToken(id string, t *oauth2.Token) error { m[id] = t; return nil }

// fakeAPI serves handler as the Calendar API and returns a provider using it
// without OAuth.
func fakeAPI(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := New(func() (OAuthConfig, error) { return OAuthConfig{ClientID: "id", ClientSecret: "s"}, nil }, memTokens{})
	p.base = srv.URL
	p.client = func(context.Context, string) (*http.Client, error) { return srv.Client(), nil }
	p.now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	p.newBackOff = func() backoff.BackOff { return backoff.NewConstantBackOff(time.Millisecond) }
	return p
}

var cal = domain.Calendar{ID: CalendarID("acct", "team@group.calendar.google.com"), AccountID: "acct", Name: "Team"}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestSyncEventsMapsGoogleEvents(t *testing.T) {
	var queries []url.Values
	p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/team@group.calendar.google.com/events" {
			t.Errorf("path = %s", r.URL.Path)
		}
		queries = append(queries, r.URL.Query())
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(w, map[string]any{"nextPageToken": "p2", "items": []map[string]any{
				{
					"id": "sync", "status": "confirmed", "summary": "Weekly sync",
					"description": `Agenda: <a href="https://www.google.com/url?q=https://docs.google.com/document/d/1&amp;sa=D">doc</a>`,
					"start":       map[string]string{"dateTime": "2026-10-06T10:00:00+02:00"},
					"end":         map[string]string{"dateTime": "2026-10-06T10:30:00+02:00"},
					"hangoutLink": "https://meet.google.com/abc-defg-hij",
					"htmlLink":    "https://www.google.com/calendar/event?eid=xyz",
					"conferenceData": map[string]any{"entryPoints": []map[string]string{
						{"entryPointType": "video", "uri": "https://meet.google.com/abc-defg-hij"},
						{"entryPointType": "phone", "uri": "tel:+1-555-0100"},
					}},
					"attachments": []map[string]string{{"fileUrl": "https://drive.google.com/file/d/2/view", "title": "Slides.pdf"}},
				},
				{"id": "cancelled", "status": "cancelled", "summary": "Gone",
					"start": map[string]string{"dateTime": "2026-10-07T10:00:00Z"}, "end": map[string]string{"dateTime": "2026-10-07T11:00:00Z"}},
			}})
			return
		}
		writeJSON(w, map[string]any{"items": []map[string]any{
			{"id": "holiday", "status": "confirmed", "summary": "Conference",
				"start": map[string]string{"date": "2026-10-08"}, "end": map[string]string{"date": "2026-10-10"}},
			{"id": "declined", "status": "confirmed", "summary": "Said no",
				"start": map[string]string{"dateTime": "2026-10-09T10:00:00Z"}, "end": map[string]string{"dateTime": "2026-10-09T11:00:00Z"},
				"attendees": []map[string]any{{"self": true, "responseStatus": "declined"}}},
			{"id": "untitled", "status": "confirmed",
				"start": map[string]string{"dateTime": "2026-10-09T12:00:00Z"}, "end": map[string]string{"dateTime": "2026-10-09T13:00:00Z"}},
		}})
	})

	r, err := p.SyncEvents(context.Background(), cal, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0].Get("singleEvents") != "true" || queries[0].Get("timeMin") != "2026-09-02T12:00:00Z" {
		t.Errorf("queries = %v", queries)
	}
	byID := map[string]domain.Event{}
	for _, e := range r.Upserted {
		byID[e.ID] = e
	}
	if len(byID) != 3 || byID["cancelled"].ID != "" || byID["declined"].ID != "" {
		t.Fatalf("events = %v, want sync, holiday, untitled", r.Upserted)
	}
	if !r.FullResync {
		t.Error("FullResync = false")
	}

	sync := byID["sync"]
	start, end, _ := sync.TimeRange()
	if !start.Equal(time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)) || end.Sub(start) != 30*time.Minute {
		t.Errorf("sync time = %v–%v", start, end)
	}
	wantLinks := []domain.Link{
		{URL: "https://meet.google.com/abc-defg-hij", Label: "Join Google Meet", Kind: domain.LinkVideoCall},
		{URL: "https://docs.google.com/document/d/1", Label: "doc", Kind: domain.LinkDocument},
		{URL: "https://drive.google.com/file/d/2/view", Label: "Slides.pdf", Kind: domain.LinkDocument},
		{URL: "https://www.google.com/calendar/event?eid=xyz", Label: "Open in Google Calendar", Kind: domain.LinkOther},
	}
	if fmt.Sprint(sync.Links) != fmt.Sprint(wantLinks) {
		t.Errorf("links =\n%v\nwant\n%v", sync.Links, wantLinks)
	}
	if sync.Notes != "Agenda: doc [2]" {
		t.Errorf("notes = %q", sync.Notes)
	}

	ds, de, ok := byID["holiday"].DateRange()
	if !ok || ds != domain.NewCivilDate(2026, 10, 8) || de != domain.NewCivilDate(2026, 10, 10) {
		t.Errorf("all-day = %s–%s ok=%v (end is exclusive, as Google's is)", ds, de, ok)
	}
	if byID["untitled"].Title != "(No title)" {
		t.Errorf("untitled title = %q", byID["untitled"].Title)
	}

	// Same events, same day: nothing to do.
	again, err := p.SyncEvents(context.Background(), cal, r.NextToken)
	if err != nil || again.FullResync || len(again.Upserted) != 0 {
		t.Errorf("second sync = %+v, %v; want a no-op", again, err)
	}
}

func TestSyncErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		check     func(error) bool
		wantCalls int
	}{
		{"expired sign-in", 401, `{"error":{"message":"Invalid Credentials"}}`, func(err error) bool {
			var r *domain.ReauthRequiredError
			return errors.As(err, &r) && r.AccountID == "acct"
		}, 1},
		{"calendar gone", 404, `{"error":{"message":"Not Found"}}`, func(err error) bool {
			var u *domain.CalendarUnavailableError
			return errors.As(err, &u) && u.CalendarID == cal.ID
		}, 1},
		{"admin blocked", 403, `{"error":{"message":"Access blocked by your administrator","errors":[{"reason":"forbidden"}]}}`, func(err error) bool {
			return strings.Contains(err.Error(), "Access blocked by your administrator")
		}, 1},
		{"rate limited", 403, `{"error":{"message":"Rate Limit Exceeded","errors":[{"reason":"rateLimitExceeded"}]}}`, func(err error) bool {
			return strings.Contains(err.Error(), "Rate Limit")
		}, 4},
		{"server error", 503, ``, func(err error) bool { return err != nil }, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.body)
			})
			_, err := p.SyncEvents(context.Background(), cal, "")
			if err == nil || !c.check(err) {
				t.Errorf("error = %v", err)
			}
			if calls != c.wantCalls {
				t.Errorf("calls = %d, want %d", calls, c.wantCalls)
			}
		})
	}
}

func TestRateLimitRetrySucceeds(t *testing.T) {
	calls := 0
	p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(429)
			return
		}
		writeJSON(w, map[string]any{"items": []any{}})
	})
	if _, err := p.SyncEvents(context.Background(), cal, ""); err != nil || calls != 2 {
		t.Errorf("err %v calls %d, want success on the second try", err, calls)
	}
}

func TestListCalendars(t *testing.T) {
	p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/calendarList" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(w, map[string]any{"nextPageToken": "2", "items": []map[string]any{
				{"id": "team@group", "summary": "Team", "accessRole": "reader"},
			}})
			return
		}
		writeJSON(w, map[string]any{"items": []map[string]any{
			{"id": "me@company.com", "summary": "me@company.com", "summaryOverride": "Me", "primary": true, "accessRole": "owner"},
		}})
	})
	got, err := p.ListCalendars(context.Background(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Primary || got[0].Name != "Me" || got[1].ID != "team@group" {
		t.Errorf("calendars = %+v, want primary (named by its override) first", got)
	}
}

type fakeSource struct {
	tok *oauth2.Token
	err error
}

func (f fakeSource) Token() (*oauth2.Token, error) { return f.tok, f.err }

func TestSavingSource(t *testing.T) {
	tokens := memTokens{}
	s := &savingSource{accountID: "acct", tokens: tokens, base: fakeSource{tok: &oauth2.Token{AccessToken: "new"}}}
	if _, err := s.Token(); err != nil || tokens["acct"] == nil || tokens["acct"].AccessToken != "new" {
		t.Errorf("refreshed token not saved: %v %v", tokens, err)
	}

	s = &savingSource{accountID: "acct", tokens: tokens, base: fakeSource{err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}}}
	var reauth *domain.ReauthRequiredError
	if _, err := s.Token(); !errors.As(err, &reauth) || reauth.AccountID != "acct" {
		t.Errorf("invalid_grant = %v, want ReauthRequiredError", err)
	}
}

func TestSignInFlow(t *testing.T) {
	if _, err := BeginSignIn(OAuthConfig{}, ""); err == nil {
		t.Error("BeginSignIn without a client = nil error")
	}

	var gotVerifier string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotVerifier = r.Form.Get("code_verifier")
		writeJSON(w, map[string]any{"access_token": "at", "refresh_token": "rt", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	infoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]string{"email": "me@company.com"})
	}))
	defer infoSrv.Close()
	userInfoURL = infoSrv.URL
	defer func() { userInfoURL = "https://openidconnect.googleapis.com/v1/userinfo" }()

	s, err := BeginSignIn(OAuthConfig{ClientID: "cid", ClientSecret: "sec"}, "me@company.com")
	if err != nil {
		t.Fatal(err)
	}
	s.conf.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: tokenSrv.URL}

	u, _ := url.Parse(s.URL)
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" ||
		q.Get("login_hint") != "me@company.com" || q.Get("access_type") != "offline" ||
		!strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.Contains(q.Get("scope"), "calendar.readonly") {
		t.Fatalf("sign-in URL = %s", s.URL)
	}

	// A callback with someone else's state is rejected...
	if resp, err := http.Get(q.Get("redirect_uri") + "?state=forged&code=x"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("forged callback status = %d", resp.StatusCode)
		}
	}
	// ...which fails this sign-in; start again for the real one.
	if _, _, err := s.Wait(context.Background()); err == nil {
		t.Fatal("Wait after a forged callback = nil error")
	}

	s, err = BeginSignIn(OAuthConfig{ClientID: "cid", ClientSecret: "sec"}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.conf.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: tokenSrv.URL}
	u, _ = url.Parse(s.URL)
	q = u.Query()
	go func() {
		resp, err := http.Get(q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state")) + "&code=the-code")
		if err == nil {
			resp.Body.Close()
		}
	}()
	acct, tok, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acct.Label != "me@company.com" || acct.Provider != "google" || acct.ID != domain.NewAccountID("google", "me@company.com") {
		t.Errorf("account = %+v", acct)
	}
	if tok.RefreshToken != "rt" || gotVerifier == "" {
		t.Errorf("token %+v, verifier sent %q", tok, gotVerifier)
	}
}

func TestSignInRejectedClient(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client","error_description":"Unauthorized"}`)
	}))
	defer tokenSrv.Close()

	s, err := BeginSignIn(OAuthConfig{ClientID: "cid", ClientSecret: "wrong"}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.conf.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: tokenSrv.URL}
	u, _ := url.Parse(s.URL)
	q := u.Query()
	go func() {
		if resp, err := http.Get(q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state")) + "&code=c"); err == nil {
			resp.Body.Close()
		}
	}()
	if _, _, err := s.Wait(context.Background()); !errors.Is(err, ErrClientRejected) {
		t.Errorf("Wait() = %v, want ErrClientRejected", err)
	}
}

func TestTransportErrorsDoNotNameTheCalendar(t *testing.T) {
	p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {})
	p.base = "http://127.0.0.1:1" // nothing listens here
	p.client = func(context.Context, string) (*http.Client, error) { return http.DefaultClient, nil }
	primary := domain.Calendar{ID: CalendarID("acct", "someone@example.com"), AccountID: "acct"}
	_, err := p.SyncEvents(context.Background(), primary, "")
	if err == nil {
		t.Fatal("SyncEvents() against a closed port succeeded")
	}
	if strings.Contains(err.Error(), "someone") {
		t.Errorf("error names the calendar: %v", err)
	}
}

func TestCalendarNamesAreMadeSafe(t *testing.T) {
	p := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []map[string]any{{"id": "x", "summary": "Team\x1b]0;pwned\x07"}}})
	})
	cals, err := p.ListCalendars(context.Background(), "acct")
	if err != nil || len(cals) != 1 || cals[0].Name != "Team]0;pwned" {
		t.Errorf("ListCalendars() = %+v, %v", cals, err)
	}
}

func TestGuestsComeFromTheAttendeesAndRoomsAreLeftOut(t *testing.T) {
	var it apiEvent
	err := json.Unmarshal([]byte(`{"id":"g","summary":"Sync","start":{"dateTime":"2026-10-06T08:00:00Z"},"end":{"dateTime":"2026-10-06T09:00:00Z"},
		"attendees":[
			{"email":"jane@example.com","displayName":"Jane","responseStatus":"accepted","organizer":true},
			{"email":"me@example.com","self":true,"responseStatus":"tentative"},
			{"email":"bob@example.com","responseStatus":"needsAction","optional":true},
			{"email":"cy@example.com","responseStatus":"declined"},
			{"email":"room@resource.calendar.google.com","displayName":"Room 4B","resource":true,"responseStatus":"accepted"}]}`), &it)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := toDomainEvent("cal", it)
	if !ok {
		t.Fatal("event dropped")
	}
	want := []domain.Guest{
		{Name: "Jane", Email: "jane@example.com", Response: domain.ResponseYes, Organizer: true},
		{Email: "me@example.com", Response: domain.ResponseMaybe, Self: true},
		{Email: "bob@example.com", Optional: true},
		{Email: "cy@example.com", Response: domain.ResponseNo},
	}
	if len(e.Guests) != len(want) {
		t.Fatalf("guests = %+v, want %+v", e.Guests, want)
	}
	for i, w := range want {
		if e.Guests[i] != w {
			t.Errorf("guest %d = %+v, want %+v", i, e.Guests[i], w)
		}
	}
}
