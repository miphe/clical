package ui

import (
	"github.com/charmbracelet/x/ansi"
)

// overlay draws box centered over base, keeping base visible around it.
// Lines are cut by display width, so styled and wide text in base survive.
func overlay(base, box []string, width int) []string {
	if len(box) == 0 || len(base) == 0 {
		return base
	}
	boxW := ansi.StringWidth(box[0])
	x := max(0, (width-boxW)/2)
	y := max(0, (len(base)-len(box))/3) // a little above center, like most dialogs
	out := append([]string(nil), base...)
	for i, b := range box {
		row := y + i
		if row >= len(out) {
			break
		}
		left := ansi.Truncate(out[row], x, "")
		right := ansi.TruncateLeft(out[row], x+boxW, "")
		out[row] = left + "\x1b[0m" + b + "\x1b[0m" + right
	}
	return out
}
