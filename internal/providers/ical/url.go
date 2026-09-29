package ical

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// NormalizeURL cleans up a pasted feed URL: trims whitespace, rewrites
// webcal:// to https://, and rejects anything that isn't https — the URL is a
// credential and must never be fetched in cleartext.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if rest, ok := strings.CutPrefix(raw, "webcal://"); ok {
		raw = "https://" + rest
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("ical: not a valid URL")
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", errors.New("ical: feed URL must start with https:// or webcal://")
	}
	return u.String(), nil
}

// CalendarID derives a stable calendar ID from a normalized feed URL, so the
// same feed added twice is recognized, without the ID revealing the URL.
func CalendarID(feedURL string) string {
	sum := sha256.Sum256([]byte(feedURL))
	return hex.EncodeToString(sum[:])[:16]
}

// SecretKey is the vault key under which a calendar's feed URL is stored.
func SecretKey(calendarID string) string {
	return "ical-url-" + calendarID
}
