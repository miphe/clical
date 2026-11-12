package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestScrollbarThumb(t *testing.T) {
	for _, c := range []struct {
		name     string
		bar      scrollbar
		at, size int
		ok       bool
	}{
		{"everything shown: no bar", scrollbar{rows: 10, first: 0, total: 10}, 0, 0, false},
		{"no bar either way: the zero value", scrollbar{}, 0, 0, false},
		{"at the top", scrollbar{rows: 10, first: 0, total: 40}, 0, 3, true},
		{"at the end", scrollbar{rows: 10, first: 30, total: 40}, 7, 3, true},
		{"one line down is off the top", scrollbar{rows: 10, first: 1, total: 400}, 1, 1, true},
		{"one line up is off the bottom", scrollbar{rows: 10, first: 389, total: 400}, 8, 1, true},
		{"half way", scrollbar{rows: 10, first: 15, total: 40}, 4, 3, true},
		{"kept above the end, with room under it", scrollbar{rows: 10, first: 12, total: 20}, 5, 5, true},
		{"a short list never fills the track", scrollbar{rows: 10, first: 1, total: 11}, 1, 9, true},
	} {
		at, size, ok := c.bar.thumb()
		if at != c.at || size != c.size || ok != c.ok {
			t.Errorf("%s: at %d size %d ok %v, want %d %d %v", c.name, at, size, ok, c.at, c.size, c.ok)
		}
	}
}

func TestPanelDrawsTheBarInItsRightBorder(t *testing.T) {
	content := []string{"head", "a", "b", "c", "d"}
	lines := panel(10, 7, frame{scroll: scrollbar{top: 1, rows: 4, first: 4, total: 8}}, content)
	var edge string
	for _, l := range lines[1 : len(lines)-1] {
		r := []rune(ansi.Strip(l))
		edge += string(r[len(r)-1])
	}
	// The header row doesn't scroll; the thumb is the lower half of the rest.
	if edge != "│││┃┃" {
		t.Errorf("right border %q, want │││┃┃", edge)
	}
	plain := panel(10, 7, frame{}, content)
	if s := strings.Join(plain, ""); strings.Contains(s, "┃") {
		t.Errorf("a panel without a bar has one:\n%s", strings.Join(plain, "\n"))
	}
}
