package domain

import "fmt"

// ReauthRequiredError belongs to the shelved Google OAuth path: the stored
// credentials for AccountID can no longer be refreshed and the user must
// re-authenticate. Callers detect it with errors.As.
type ReauthRequiredError struct {
	AccountID string
	Err       error
}

func (e *ReauthRequiredError) Error() string {
	return fmt.Sprintf("account %s needs re-authentication: %v", e.AccountID, e.Err)
}

func (e *ReauthRequiredError) Unwrap() error {
	return e.Err
}

// CalendarUnavailableError means a calendar's source refuses access or no
// longer exists — for an iCal feed, the secret URL was reset or the calendar
// deleted. Retrying won't help; the calendar needs a new URL or removal.
type CalendarUnavailableError struct {
	CalendarID string
	Reason     string // e.g. "HTTP 404 Not Found"
}

func (e *CalendarUnavailableError) Error() string {
	if e.CalendarID == "" {
		return "calendar unavailable: " + e.Reason
	}
	return fmt.Sprintf("calendar %s unavailable: %s", e.CalendarID, e.Reason)
}
