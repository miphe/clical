package ui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// seedArchive writes archive records the way `clical write archive` does.
func seedArchive(t *testing.T, src storeSource, recs []write.Archived) {
	t.Helper()
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(src.st.Dir(), "archive.jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// done is a task completed on a day (the loc zone), archived a day later.
func done(id, text string, y int, mo time.Month, d, hour int) write.Archived {
	at := time.Date(y, mo, d, hour, 0, 0, 0, loc)
	return write.Archived{ID: id, Text: text, Completed: at, Archived: at.AddDate(0, 0, 1)}
}

// busyArchive is five months of varied days before testNow (Tue 29 Sep 2026).
func busyArchive() []write.Archived {
	var recs []write.Archived
	today := time.Date(2026, 9, 29, 0, 0, 0, 0, loc)
	for i := 1; i < 150; i++ {
		day := today.AddDate(0, 0, -i)
		n := int(uint32(i)*2654435761>>28) % 5 // a fixed scatter of 0–4 a day
		if i%23 == 0 {
			n = 7
		}
		for k := 0; k < n; k++ {
			recs = append(recs, done("t", "task", day.Year(), day.Month(), day.Day(), 8+k))
		}
	}
	return recs
}

// heatModel is the focus view over a busy archive.
func heatModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, src := writeModel(t, width, height)
	seedArchive(t, src, busyArchive())
	return keys(t, m, "4")
}

func TestFocusViewGolden(t *testing.T) {
	golden(t, "focus_heat", screen(heatModel(t, 130, 24)))
	golden(t, "focus_heat_narrow", screen(heatModel(t, 80, 22)))
}

func TestFocusViewWithNoArchive(t *testing.T) {
	m, _ := writeModel(t, 100, 24)
	m = keys(t, m, "4")
	s := screen(m)
	if !strings.Contains(s, "[4] Focus") || !strings.Contains(s, "No archived tasks yet") {
		t.Errorf("empty archive:\n%s", s)
	}
	// Still moves, and stays on today (nothing earlier to go to).
	m = keys(t, m, "h", "k")
	if m.heat.cursor != m.today() {
		t.Errorf("cursor = %v, want today", m.heat.cursor)
	}
}

func TestHeatCountsTasksOnTheirCompletionDay(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{
		done("a", "one", 2026, 9, 20, 9),
		done("b", "two", 2026, 9, 20, 22),
		done("c", "three", 2026, 9, 20, 23), // archived on the 21st, still the 20th
		done("d", "later", 2026, 9, 22, 9),
	})
	m = keys(t, m, "4", "h", "k", "k") // 29th, a week back, two days back: Sun 20 Sep
	if m.heat.cursor != domain.NewCivilDate(2026, 9, 20) {
		t.Fatalf("cursor = %v", m.heat.cursor)
	}
	if s := screen(m); !strings.Contains(s, "Sun 20 Sep 2026") || !strings.Contains(s, "3 tasks") {
		t.Errorf("the 20th:\n%s", s)
	}
	m = keys(t, m, "j") // Mon 21: archive day of two of them, no completions
	if s := screen(m); !strings.Contains(s, "Mon 21 Sep 2026") || !strings.Contains(s, "no tasks completed") {
		t.Errorf("the 21st:\n%s", s)
	}
}

func TestHeatSelectionKeys(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{
		done("z", "early", 2026, 9, 1, 9),
		done("a", "one", 2026, 9, 14, 9),
		done("b", "two", 2026, 9, 16, 9),
		done("c", "three", 2026, 9, 16, 10),
	})
	today := m.today()
	m = keys(t, m, "4")
	if m.heat.cursor != today || m.heat.anchor != today {
		t.Fatalf("starts on %v / %v", m.heat.cursor, m.heat.anchor)
	}

	// Arrow keys and letters do the same.
	m = keys(t, m, "left", "up")
	m = keys(t, m, "h", "k")
	if want := today.AddDays(-14 - 2); m.heat.cursor != want {
		t.Errorf("cursor = %v, want %v", m.heat.cursor, want)
	}

	// Shift extends from where the selection began, with arrows or capitals.
	m = keys(t, m, "shift+up", "K")
	lo, hi := m.heat.span(m.weekStart)
	if hi != today.AddDays(-16) || lo != today.AddDays(-18) {
		t.Errorf("extended to %v–%v", lo, hi)
	}
	m = keys(t, m, "shift+down", "J", "J")
	if lo, hi = m.heat.span(m.weekStart); lo != today.AddDays(-16) || hi != today.AddDays(-15) {
		t.Errorf("back down: %v–%v", lo, hi)
	}
	// A plain move goes back to one day; esc does too.
	m = keys(t, m, "shift+up", "esc")
	if lo, hi = m.heat.span(m.weekStart); lo != hi {
		t.Errorf("esc kept %v–%v", lo, hi)
	}

	// Week 38 is 14–20 Sep: three tasks on two days.
	m = keys(t, m, "t", "h", "h", "space")
	s := screen(m)
	if !strings.Contains(s, "Week 38 · 14–20 Sep 2026") || !strings.Contains(s, "3 tasks on 2 days") {
		t.Errorf("week:\n%s", s)
	}
	m = keys(t, m, "space")
	if s = screen(m); !strings.Contains(s, "September 2026") || !strings.Contains(s, "4 tasks on 3 days") {
		t.Errorf("month:\n%s", s)
	}
	m = keys(t, m, "space", "t")
	if m.heat.unit != heatDay || m.heat.cursor != today {
		t.Errorf("unit %v cursor %v", m.heat.unit, m.heat.cursor)
	}
}

func TestHeatKeysDoNotMoveTheAgenda(t *testing.T) {
	m := heatModel(t, 100, 24)
	sel, from := m.sel, m.from
	m = keys(t, m, "enter", "g", "G", "[", "]", "pgup")
	if m.sel != sel || m.from != from || m.detail != nil {
		t.Errorf("the agenda moved under the heatmap: sel %q→%q, from %v→%v, detail %v", sel, m.sel, from, m.from, m.detail)
	}
}

func TestFocusViewCarriesTheDayBothWays(t *testing.T) {
	m := heatModel(t, 130, 24)
	m = keys(t, m, "h", "h")
	day := m.heat.cursor
	m = keys(t, m, "2")
	if got, _ := m.viewDay(); got != day {
		t.Errorf("the week view is on %v, want %v", got, day)
	}
	m = keys(t, m, "4")
	if m.heat.cursor != day {
		t.Errorf("back in the focus view on %v, want %v", m.heat.cursor, day)
	}
}

func TestFocusViewNeedsWrite(t *testing.T) {
	m, _ := writeModel(t, 100, 24)
	m.writeOn = false
	m = keys(t, m, "4")
	if m.view == viewHeat || strings.Contains(screen(m), "[4] Focus") {
		t.Errorf("the focus view exists without Write:\n%s", screen(m))
	}
}

func TestHeatReloadsWhenTheArchiveChanges(t *testing.T) {
	m, src := writeModel(t, 100, 24)
	seedArchive(t, src, []write.Archived{done("a", "one", 2026, 9, 29, 9)})
	m = keys(t, m, "4")
	if s := screen(m); !strings.Contains(s, "1 task") {
		t.Fatalf("one task:\n%s", s)
	}
	seedArchive(t, src, []write.Archived{done("a", "one", 2026, 9, 29, 9), done("b", "two", 2026, 9, 29, 10)})
	m, cmd := send(t, m, tickMsg(m.now.Add(30*time.Second)))
	m, _ = run(t, m, cmd)
	if s := screen(m); !strings.Contains(s, "2 tasks") {
		t.Errorf("after the tick:\n%s", s)
	}
}

func TestHeatmapIsFlatAndOneHue(t *testing.T) {
	m := heatModel(t, 130, 24)
	heat := strings.Join(m.renderHeat(80, 12), "\n")
	if strings.ContainsAny(ansi.Strip(heat), "░▒▓█") {
		t.Error("the heatmap uses shade glyphs; it should be flat squares")
	}
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.Env([]string{"TERM=xterm-256color"})}
	if _, err := w.WriteString(heat); err != nil {
		t.Fatal(err)
	}
	// The only hue is the accent (blue, 34); intensity is faint, plain, bold.
	var blue, faint, bold bool
	for _, params := range sgrParams(buf.String()) {
		for _, p := range params {
			switch {
			case p == 34:
				blue = true
			case p == 2:
				faint = true
			case p == 1:
				bold = true
			case (p >= 30 && p <= 37) || (p >= 90 && p <= 97):
				t.Errorf("the heatmap uses color %d, want only the accent (34): %v", p, params)
			}
		}
	}
	if !blue || !faint || !bold {
		t.Errorf("accent %v, faint %v, bold %v: want all three", blue, faint, bold)
	}
}
