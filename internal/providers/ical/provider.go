// Package ical implements domain.CalendarProvider for iCalendar feeds
// fetched from a secret URL (e.g. Google Calendar's "Secret address in iCal
// format"). There is no login and no incremental sync: each sync downloads
// the whole feed, skipped via conditional GET when it hasn't changed.
package ical

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

	"github.com/miphe/clical/internal/domain"
)

const maxFeedBytes = 64 << 20

// Provider syncs iCal feeds. Feed URLs are secrets, so the provider never
// stores them itself — it asks lookupURL for one at sync time.
type Provider struct {
	lookupURL func(calendarID string) (string, error)
	client    *http.Client
	now       func() time.Time

	// Occurrences outside [now-past, now+future) are not materialized.
	past, future time.Duration
}

var _ domain.CalendarProvider = (*Provider)(nil)

// New builds a Provider that resolves a calendar's feed URL via lookupURL.
func New(lookupURL func(calendarID string) (string, error)) *Provider {
	return &Provider{
		lookupURL: lookupURL,
		client:    &http.Client{Timeout: 30 * time.Second},
		now:       time.Now,
		past:      30 * 24 * time.Hour,
		future:    365 * 24 * time.Hour,
	}
}

// syncState is the opaque sync token. Day records when the feed was last
// expanded: the expansion window slides forward daily, so a new day forces a
// full fetch even if the feed itself is unchanged. Hash covers servers that
// send no validators (Google's feeds send neither ETag nor Last-Modified):
// the feed is still downloaded, but if the parsed events are identical the
// sync is a no-op. It hashes events rather than the raw body because Google
// rewrites every event's DTSTAMP to the download time.
type syncState struct {
	Day          string `json:"day"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Hash         string `json:"hash,omitempty"`
}

// SyncEvents fetches cal's feed. An unchanged feed (HTTP 304, or the same
// events as the last fetch today) yields an empty, non-full result;
// otherwise every occurrence in the window is returned with FullResync set,
// replacing whatever the store held for cal.
func (p *Provider) SyncEvents(ctx context.Context, cal domain.Calendar, token string) (domain.SyncResult, error) {
	feedURL, err := p.lookupURL(cal.ID)
	if err != nil {
		return domain.SyncResult{}, fmt.Errorf("ical: look up feed URL for %s: %w", cal.ID, err)
	}

	now := p.now()
	today := domain.CivilDateFromTime(now).String()

	var prev syncState
	if token != "" {
		// An unreadable token just means a full fetch.
		_ = json.Unmarshal([]byte(token), &prev)
	}
	if prev.Day != today {
		prev = syncState{}
	}

	resp, err := p.get(ctx, feedURL, prev)
	if err != nil {
		return domain.SyncResult{}, fmt.Errorf("ical: fetch %s: %w", cal.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return domain.SyncResult{NextToken: token}, nil
	}

	events, err := parse(io.LimitReader(resp.Body, maxFeedBytes), cal.ID, now.Add(-p.past), now.Add(p.future))
	if err != nil {
		return domain.SyncResult{}, fmt.Errorf("ical: %s: %w", cal.ID, err)
	}
	hash := eventsHash(events)
	if prev.Hash == hash {
		return domain.SyncResult{NextToken: token}, nil
	}

	next, err := json.Marshal(syncState{
		Day:          today,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		Hash:         hash,
	})
	if err != nil {
		return domain.SyncResult{}, fmt.Errorf("ical: encode sync token: %w", err)
	}
	return domain.SyncResult{Upserted: events, NextToken: string(next), FullResync: true}, nil
}

// Probe fetches and parses feedURL once, returning how many occurrences fall
// in the sync window. It's used to validate a URL before it's saved.
func eventsHash(events []domain.Event) string {
	keys := make([]string, 0, len(events))
	for _, e := range events {
		var span string
		if ds, de, ok := e.DateRange(); ok {
			span = ds.String() + "/" + de.String()
		} else {
			ts, te, _ := e.TimeRange()
			span = ts.Format(time.RFC3339) + "/" + te.Format(time.RFC3339)
		}
		keys = append(keys, strings.Join([]string{e.ID, e.Title, span, e.Location, e.Notes}, "\x00"))
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0xff})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p *Provider) Probe(ctx context.Context, feedURL string) (int, error) {
	resp, err := p.get(ctx, feedURL, syncState{})
	if err != nil {
		return 0, fmt.Errorf("ical: fetch feed: %w", err)
	}
	defer resp.Body.Close()

	now := p.now()
	events, err := parse(io.LimitReader(resp.Body, maxFeedBytes), "probe", now.Add(-p.past), now.Add(p.future))
	if err != nil {
		return 0, err
	}
	return len(events), nil
}

// get issues a (possibly conditional) GET. The returned response is either
// 200 or 304; anything else is an error. Errors never contain the URL.
func (p *Provider) get(ctx context.Context, feedURL string, cond syncState) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, errors.New("invalid feed URL")
	}
	req.Header.Set("Accept", "text/calendar")
	if cond.ETag != "" {
		req.Header.Set("If-None-Match", cond.ETag)
	}
	if cond.LastModified != "" {
		req.Header.Set("If-Modified-Since", cond.LastModified)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	return resp, nil
}

// redact strips the request URL out of an HTTP client error, since the URL is
// a credential and errors end up in logs and on screen.
func redact(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return &url.Error{Op: uerr.Op, URL: "<feed URL>", Err: uerr.Err}
	}
	return err
}
