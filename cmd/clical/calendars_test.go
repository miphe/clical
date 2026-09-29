package main

import (
	"strings"
	"testing"

	"github.com/miphe/clical/internal/domain"
)

func TestMatchCalendar(t *testing.T) {
	accounts := []domain.Account{{ID: "p", Label: "Personal"}, {ID: "w", Label: "Work"}}
	cals := []domain.Calendar{
		{ID: "id-main", AccountID: "p", Name: "Main"},
		{ID: "id-team", AccountID: "w", Name: "Team"},
		{ID: "id-holp", AccountID: "p", Name: "Holidays"},
		{ID: "id-holw", AccountID: "w", Name: "Holidays"},
	}

	cases := []struct {
		ref, account, wantID, wantErr string
	}{
		{"main", "", "id-main", ""},
		{"id-team", "", "id-team", ""},
		{"Holidays", "work", "id-holw", ""},
		{"Holidays", "", "", "several"},
		{"nope", "", "", "no calendar"},
		{"Team", "Personal", "", "no calendar"},
	}
	for _, c := range cases {
		_, cal, err := matchCalendar(accounts, cals, c.ref, c.account)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("matchCalendar(%q, %q) error = %v, want %q", c.ref, c.account, err, c.wantErr)
			}
			continue
		}
		if err != nil || cal.ID != c.wantID {
			t.Errorf("matchCalendar(%q, %q) = %q, %v; want %q", c.ref, c.account, cal.ID, err, c.wantID)
		}
	}
}
