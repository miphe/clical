package ui

import (
	"image/color"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// clical has no color palette of its own. Every color is one of the
// terminal's 16 ANSI colors, referenced by index, so the app follows whatever
// theme the terminal has (Omarchy, for one, themes apps by retinting exactly
// these 16). Text without a role renders in the terminal's default foreground
// on its default background; nothing sets a background.
//
// Emphasis uses attributes rather than colors where it can: bold, dim,
// underline, reverse. Dim is used for muted text instead of ANSI 8 (bright
// black), which some themes make almost the same as the background.
//
// This file is the only place colors are named. internal/archtest fails the
// build if lipgloss.Color appears anywhere else in the UI, or with anything
// but an index 0–15.

// ANSI palette slots by name. Only the six normal hues are used: many themes
// make the bright variants (9–14) identical to them, and Solarized makes
// several of them grays.
var (
	ansiRed     = lipgloss.Color("1")
	ansiGreen   = lipgloss.Color("2")
	ansiYellow  = lipgloss.Color("3")
	ansiBlue    = lipgloss.Color("4")
	ansiMagenta = lipgloss.Color("5")
	ansiCyan    = lipgloss.Color("6")
)

// Semantic roles. Everything in the UI goes through these. Measured against
// real light and dark themes (Tokyo Night, Catppuccin, Gruvbox, Solarized,
// macOS Terminal, xterm), blue is the hue readable as text everywhere, so it
// carries everything routine. Green and red only color symbols (✓, ✗) or the
// thin "now" line, with the words beside them in the default color, except
// errors, which are bold red to stand out.
var (
	colorAccent  = ansiBlue  // focus, the active tab, today, work in progress
	colorSuccess = ansiGreen // a finished sync
	colorError   = ansiRed   // failures
	colorNow     = ansiRed   // the "now" line: red by calendar convention, not an error
	// colorWarning is ANSI yellow. Nothing uses it yet: yellow text reads
	// poorly on light backgrounds, so it's reserved for genuine warnings.
	colorWarning = ansiYellow
)

// Styles built from the roles and attributes.
var (
	boldStyle  = lipgloss.NewStyle().Bold(true)
	mutedStyle = lipgloss.NewStyle().Faint(true)

	accentStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorError)
	nowStyle     = lipgloss.NewStyle().Foreground(colorNow)

	// The heatmap is flat: every day with tasks is the same square, in one hue.
	// The busier the day the stronger it is: the accent faint, the accent, then
	// the terminal's own text color in bold, which is near-white on a dark
	// theme and near-black on a light one, so it always stands out where a
	// literal white would vanish. (Bold doesn't brighten the accent on every
	// terminal, so it isn't used to tell the first two apart.) Days without
	// tasks are a quiet dot, and so is the filler that squares a month's block.
	heatStyles = [4]lipgloss.Style{
		mutedStyle,
		lipgloss.NewStyle().Foreground(colorAccent).Faint(true),
		lipgloss.NewStyle().Foreground(colorAccent),
		lipgloss.NewStyle().Bold(true),
	}
	heatFillStyle = mutedStyle

	todayStyle         = accentStyle
	busyStyle          = accentStyle
	activeTabStyle     = accentStyle
	focusedBorderStyle = accentStyle
	borderStyle        = mutedStyle

	// A panel border lit for a moment, to say something changed inside it.
	flashBorderStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Reverse(true)
	selectedStyle    = lipgloss.NewStyle().Reverse(true)
	// The selection stays visible, but quieter, while another panel has focus.
	inactiveSelStyle = lipgloss.NewStyle().Underline(true)
)

// inputStyles replaces the text input's built-in defaults, which use a
// 256-color gray for placeholders. Typed text uses the terminal's default
// color, and the cursor is the terminal's own (a nil color).
func inputStyles() textinput.Styles {
	state := textinput.StyleState{
		Text:        lipgloss.NewStyle(),
		Placeholder: mutedStyle,
		Suggestion:  mutedStyle,
		Prompt:      lipgloss.NewStyle(),
	}
	return textinput.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textinput.CursorStyle{Blink: true},
	}
}

// Calendars are told apart by color and, every six, by bullet shape. Only
// six ANSI hues are reliably distinct in every theme (black and white vanish
// against one background or the other; bright variants often duplicate the
// normal ones), so shape carries the rest: 6 hues × 4 shapes = 24 distinct
// calendars.
//
// Calendar colors only ever color a bullet glyph, never text: some hues
// (yellow, and green on light themes) are too faint to read as text.
// Yellow, the weakest, comes last.
var calendarHues = []color.Color{
	ansiBlue, ansiMagenta, ansiGreen, ansiCyan, ansiRed, ansiYellow,
}

// calendarShapes are the bullet shapes, each filled (shown) and hollow
// (hidden in the sidebar).
var calendarShapes = []struct{ shown, hidden string }{
	{"●", "○"}, {"◆", "◇"}, {"■", "□"}, {"▲", "△"},
}

// calendarColor maps a calendar's permanent color slot (1-based) to its hue.
func calendarColor(slot int) color.Color {
	return calendarHues[slotIndex(slot)%len(calendarHues)]
}

// calendarGlyph is the calendar's bullet: its slot's shape, filled or hollow.
func calendarGlyph(slot int, hidden bool) string {
	shape := calendarShapes[(slotIndex(slot)/len(calendarHues))%len(calendarShapes)]
	if hidden {
		return shape.hidden
	}
	return shape.shown
}

// calendarBullet is the calendar's bullet in its color.
func calendarBullet(slot int) string {
	return lipgloss.NewStyle().Foreground(calendarColor(slot)).Render(calendarGlyph(slot, false))
}

func slotIndex(slot int) int {
	return max(slot, 1) - 1
}
