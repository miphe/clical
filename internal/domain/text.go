package domain

import "strings"

// SafeText makes untrusted text safe to print to a terminal: it drops
// control characters (C0 except newline, DEL, C1), which is how escape
// sequences start, turns tabs into spaces and repairs invalid UTF-8.
func SafeText(s string) string {
	clean := true
	for _, r := range s {
		if isUnsafe(r) || r == '\t' || r == '�' {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case !isUnsafe(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isUnsafe(r rune) bool {
	return (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f)
}
