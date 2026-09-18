package domain

import "fmt"

// ReauthRequiredError is returned by a CalendarProvider's ListCalendars or
// SyncEvents when the stored credentials for AccountID can no longer be
// refreshed and the user must re-authenticate. Callers detect it with
// errors.As without needing to know anything about the provider's transport.
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
