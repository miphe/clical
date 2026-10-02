package main

import (
	"testing"

	"github.com/rivo/uniseg"
)

func TestPadRightUsesDisplayWidth(t *testing.T) {
	for _, s := range []string{"abc", "…–01:00", "会議", "🎉 party"} {
		got := padRight(s, 12)
		if width := uniseg.StringWidth(got); width != 12 {
			t.Errorf("padRight(%q) width = %d, want 12", s, width)
		}
	}
}
