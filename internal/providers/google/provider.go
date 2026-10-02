// Package google syncs calendars from Google accounts through the Calendar
// REST API (read-only), signing in with the user's own OAuth client. It
// talks to two endpoints directly rather than through Google's generated
// client library: users/me/calendarList and calendars/{id}/events.
package google

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/oauth2"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/eventtext"
)

const apiBase = "https://www.googleapis.com/calendar/v3"

// Tokens stores each account's OAuth token; the app backs it with the vault.
type Tokens interface {
	LoadToken(accountID string) (*oauth2.Token, error)
	SaveToken(accountID string, tok *oauth2.Token) error
}

// Provider syncs Google calendars. A calendar's domain ID is
// "<accountID>/<Google calendar ID>", so the same shared calendar in two
// accounts stays two calendars.
type Provider struct {
	config func() (OAuthConfig, error)
	tokens Tokens
	base   string
	now    func() time.Time
	// client returns an authorized HTTP client for an account; tests swap it.
	client func(ctx context.Context, accountID string) (*http.Client, error)

	past, future time.Duration
	newBackOff   func() backoff.BackOff
	maxTries     uint
}

var _ domain.CalendarProvider = (*Provider)(nil)

// New builds a Provider. config is read at sync time, so an OAuth client set
// up after the vault was unlocked is picked up.
func New(config func() (OAuthConfig, error), tokens Tokens) *Provider {
	p := &Provider{
		config:     config,
		tokens:     tokens,
		base:       apiBase,
		now:        time.Now,
		past:       30 * 24 * time.Hour,
		future:     365 * 24 * time.Hour,
		newBackOff: defaultBackOff,
		maxTries:   4,
	}
	p.client = p.oauthClient
	return p
}

func defaultBackOff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = time.Second
	b.MaxInterval = 10 * time.Second
	return b
}

// CalendarID is the domain ID of an account's Google calendar.
func CalendarID(accountID, googleID string) string {
	return accountID + "/" + googleID
}

func googleID(cal domain.Calendar) string {
	return strings.TrimPrefix(cal.ID, cal.AccountID+"/")
}

// oauthClient builds an HTTP client that refreshes the account's token as
// needed and saves refreshed tokens back.
func (p *Provider) oauthClient(ctx context.Context, accountID string) (*http.Client, error) {
	cfg, err := p.config()
	if err != nil {
		return nil, err
	}
	tok, err := p.tokens.LoadToken(accountID)
	if err != nil {
		return nil, &domain.ReauthRequiredError{AccountID: accountID, Err: err}
	}
	src := &savingSource{accountID: accountID, tokens: p.tokens, last: tok,
		base: cfg.oauth2("").TokenSource(ctx, tok)}
	return oauth2.NewClient(ctx, oauth2.ReuseTokenSource(tok, src)), nil
}

// savingSource persists refreshed tokens and turns a refused refresh (the
// grant was revoked, or expired after 7 days while the OAuth app is in
// "Testing") into ReauthRequiredError.
type savingSource struct {
	accountID string
	tokens    Tokens
	base      oauth2.TokenSource
	last      *oauth2.Token
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		var rerr *oauth2.RetrieveError
		if errors.As(err, &rerr) && (rerr.ErrorCode == "invalid_grant" || rerr.Response != nil && rerr.Response.StatusCode == http.StatusUnauthorized) {
			return nil, &domain.ReauthRequiredError{AccountID: s.accountID, Err: err}
		}
		return nil, err
	}
	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		_ = s.tokens.SaveToken(s.accountID, tok) // a failed save only costs a refresh next time
		s.last = tok
	}
	return tok, nil
}

// RemoteCalendar is a calendar in a Google account's calendar list.
type RemoteCalendar struct {
	ID         string // Google's calendar ID
	Name       string
	Primary    bool
	AccessRole string // "owner", "writer", "reader" or "freeBusyReader"
}

// ListCalendars lists the account's calendars (including ones shared with
// it), primary first.
func (p *Provider) ListCalendars(ctx context.Context, accountID string) ([]RemoteCalendar, error) {
	var out []RemoteCalendar
	page := ""
	for {
		q := url.Values{"fields": {"items(id,summary,summaryOverride,primary,accessRole),nextPageToken"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var resp struct {
			Items []struct {
				ID              string `json:"id"`
				Summary         string `json:"summary"`
				SummaryOverride string `json:"summaryOverride"`
				Primary         bool   `json:"primary"`
				AccessRole      string `json:"accessRole"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := p.get(ctx, accountID, "/users/me/calendarList", q, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			name := it.SummaryOverride
			if name == "" {
				name = it.Summary
			}
			out = append(out, RemoteCalendar{ID: it.ID, Name: domain.SafeText(name), Primary: it.Primary, AccessRole: it.AccessRole})
		}
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Primary && !out[b].Primary })
	return out, nil
}

// syncState is the opaque sync token: like the iCal provider, every sync
// fetches the whole window, and identical events on the same day are a no-op.
type syncState struct {
	Day  string `json:"day"`
	Hash string `json:"hash"`
}

// SyncEvents fetches cal's events in the window (now −30 days to +365 days).
func (p *Provider) SyncEvents(ctx context.Context, cal domain.Calendar, token string) (domain.SyncResult, error) {
	now := p.now()
	from, to := now.Add(-p.past), now.Add(p.future)

	var items []apiEvent
	page := ""
	for {
		q := url.Values{
			"singleEvents": {"true"},
			"timeMin":      {from.UTC().Format(time.RFC3339)},
			"timeMax":      {to.UTC().Format(time.RFC3339)},
			"maxResults":   {"2500"},
			"fields": {"items(id,status,summary,description,location,start,end,hangoutLink,htmlLink," +
				"conferenceData(entryPoints(entryPointType,uri)),attachments(fileUrl,title),attendees(self,responseStatus)),nextPageToken"},
		}
		if page != "" {
			q.Set("pageToken", page)
		}
		var resp struct {
			Items         []apiEvent `json:"items"`
			NextPageToken string     `json:"nextPageToken"`
		}
		path := "/calendars/" + url.PathEscape(googleID(cal)) + "/events"
		if err := p.get(ctx, cal.AccountID, path, q, &resp); err != nil {
			var unavailable *domain.CalendarUnavailableError
			if errors.As(err, &unavailable) {
				unavailable.CalendarID = cal.ID
			}
			return domain.SyncResult{}, err
		}
		items = append(items, resp.Items...)
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}

	var events []domain.Event
	for _, it := range items {
		if e, ok := toDomainEvent(cal.ID, it); ok {
			events = append(events, e)
		}
	}

	today := domain.CivilDateFromTime(now).String()
	hash := eventsHash(events)
	var prev syncState
	_ = json.Unmarshal([]byte(token), &prev)
	if prev.Day == today && prev.Hash == hash {
		return domain.SyncResult{NextToken: token}, nil
	}
	next, err := json.Marshal(syncState{Day: today, Hash: hash})
	if err != nil {
		return domain.SyncResult{}, err
	}
	return domain.SyncResult{Upserted: events, NextToken: string(next), FullResync: true}, nil
}

type apiTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

type apiEvent struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Summary        string  `json:"summary"`
	Description    string  `json:"description"`
	Location       string  `json:"location"`
	Start          apiTime `json:"start"`
	End            apiTime `json:"end"`
	HangoutLink    string  `json:"hangoutLink"`
	HTMLLink       string  `json:"htmlLink"`
	ConferenceData *struct {
		EntryPoints []struct {
			Type string `json:"entryPointType"`
			URI  string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData"`
	Attachments []struct {
		FileURL string `json:"fileUrl"`
		Title   string `json:"title"`
	} `json:"attachments"`
	Attendees []struct {
		Self           bool   `json:"self"`
		ResponseStatus string `json:"responseStatus"`
	} `json:"attendees"`
}

// toDomainEvent maps an API event. Cancelled events, and ones the user has
// declined, are left out.
func toDomainEvent(calendarID string, it apiEvent) (domain.Event, bool) {
	if it.Status == "cancelled" {
		return domain.Event{}, false
	}
	for _, a := range it.Attendees {
		if a.Self && a.ResponseStatus == "declined" {
			return domain.Event{}, false
		}
	}

	content := eventtext.Content{Description: it.Description, Location: it.Location}
	content.Conference = append(content.Conference, it.HangoutLink)
	if it.ConferenceData != nil {
		for _, ep := range it.ConferenceData.EntryPoints {
			if ep.Type == "video" {
				content.Conference = append(content.Conference, ep.URI)
			}
		}
	}
	for _, a := range it.Attachments {
		content.Extra = append(content.Extra, domain.Link{URL: a.FileURL, Label: a.Title, Kind: domain.LinkDocument})
	}
	if it.HTMLLink != "" {
		content.Extra = append(content.Extra, domain.Link{URL: it.HTMLLink, Label: "Open in Google Calendar", Kind: domain.LinkOther})
	}
	notes, links := eventtext.Describe(content)

	title := it.Summary
	if title == "" {
		title = "(No title)"
	}

	if it.Start.Date != "" {
		start, err1 := domain.ParseCivilDate(it.Start.Date)
		end, err2 := domain.ParseCivilDate(it.End.Date) // exclusive, as in our model
		if err1 != nil || err2 != nil {
			return domain.Event{}, false
		}
		if !start.Before(end) {
			end = start.AddDays(1)
		}
		return domain.NewAllDayEvent(it.ID, calendarID, title, start, end, it.Location, notes).WithLinks(links), true
	}
	start, err1 := time.Parse(time.RFC3339, it.Start.DateTime)
	end, err2 := time.Parse(time.RFC3339, it.End.DateTime)
	if err1 != nil || err2 != nil {
		return domain.Event{}, false
	}
	return domain.NewTimedEvent(it.ID, calendarID, title, start, end, it.Location, notes).WithLinks(links), true
}

func eventsHash(events []domain.Event) string {
	keys := make([]string, 0, len(events))
	for _, e := range events {
		b, _ := json.Marshal(struct {
			ID, Title, Location, Notes string
			Start, End                 string
			Links                      []domain.Link
		}{e.ID, e.Title, e.Location, e.Notes, spanStart(e), spanEnd(e), e.Links})
		keys = append(keys, string(b))
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func spanStart(e domain.Event) string {
	if ds, _, ok := e.DateRange(); ok {
		return ds.String()
	}
	ts, _, _ := e.TimeRange()
	return ts.Format(time.RFC3339)
}

func spanEnd(e domain.Event) string {
	if _, de, ok := e.DateRange(); ok {
		return de.String()
	}
	_, te, _ := e.TimeRange()
	return te.Format(time.RFC3339)
}

// get calls the API and decodes its JSON reply into out, retrying rate
// limits and server errors. 401 means the account must sign in again; 404
// means the calendar is gone or no longer shared.
func (p *Provider) get(ctx context.Context, accountID, path string, q url.Values, out any) error {
	client, err := p.client(ctx, accountID)
	if err != nil {
		return err
	}
	endpoint := p.base + path + "?" + q.Encode()

	attempt := func() (struct{}, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return struct{}{}, backoff.Permanent(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			var reauth *domain.ReauthRequiredError
			if errors.As(err, &reauth) {
				return struct{}{}, backoff.Permanent(reauth)
			}
			if ctx.Err() != nil {
				return struct{}{}, backoff.Permanent(ctx.Err())
			}
			return struct{}{}, redact(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return struct{}{}, err
		}

		switch code := resp.StatusCode; {
		case code == http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return struct{}{}, backoff.Permanent(fmt.Errorf("google: decode reply: %w", err))
			}
			return struct{}{}, nil
		case code == http.StatusUnauthorized:
			return struct{}{}, backoff.Permanent(&domain.ReauthRequiredError{AccountID: accountID, Err: apiError(resp, body)})
		case code == http.StatusNotFound || code == http.StatusGone:
			return struct{}{}, backoff.Permanent(&domain.CalendarUnavailableError{Reason: "HTTP " + resp.Status})
		case code == http.StatusTooManyRequests || code >= 500 || isRateLimit(code, body):
			return struct{}{}, apiError(resp, body)
		default:
			return struct{}{}, backoff.Permanent(apiError(resp, body))
		}
	}
	_, err = backoff.Retry(ctx, attempt, backoff.WithBackOff(p.newBackOff()), backoff.WithMaxTries(p.maxTries))
	return err
}

// redact drops the request URL from a transport error: it names the
// calendar, and a primary calendar's ID is the account's email address.
func redact(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return &url.Error{Op: uerr.Op, URL: "<Google Calendar API>", Err: uerr.Err}
	}
	return err
}

type googleError struct {
	Error struct {
		Message string `json:"message"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

func isRateLimit(code int, body []byte) bool {
	if code != http.StatusForbidden {
		return false
	}
	var ge googleError
	_ = json.Unmarshal(body, &ge)
	for _, e := range ge.Error.Errors {
		if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
			return true
		}
	}
	return false
}

// apiError keeps Google's own message ("Calendar API has not been used in
// project…", "access blocked by admin"), which tells the user what to fix.
func apiError(resp *http.Response, body []byte) error {
	var ge googleError
	if json.Unmarshal(body, &ge) == nil && ge.Error.Message != "" {
		return fmt.Errorf("google: %s: %s", resp.Status, ge.Error.Message)
	}
	return fmt.Errorf("google: %s", resp.Status)
}
