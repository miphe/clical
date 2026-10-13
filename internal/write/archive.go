package write

import (
	"encoding/json"
	"strings"
)

// The archive is a JSON Lines file: one archived task or note per line, appended as
// they are filed. Plain text, easy to read by hand and for a heatmap to read
// later. A line that isn't a record is kept as it is.

func encodeArchived(a Archived) string {
	raw, _ := json.Marshal(a)
	return string(raw)
}

type archiveLine struct {
	raw string
	rec *Archived // nil for a line that isn't a record
}

func parseArchive(data []byte) []archiveLine {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	var out []archiveLine
	for _, line := range strings.Split(text, "\n") {
		l := archiveLine{raw: line}
		var a Archived
		if json.Unmarshal([]byte(line), &a) == nil && a.ID != "" {
			l.rec = &a
		}
		out = append(out, l)
	}
	return out
}
