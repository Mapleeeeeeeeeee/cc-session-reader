package parser

import "time"

// ParseTimestamp parses a transcript ISO timestamp. It reports false when the
// string is empty or in no recognized form, so callers doing their own
// formatting can render a placeholder rather than a wrong time.
func ParseTimestamp(tsStr string) (time.Time, bool) {
	if tsStr == "" {
		return time.Time{}, false
	}
	t, err := parseISO(tsStr)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// FormatTimestamp converts an ISO timestamp string to "MM-DD HH:MM" format.
func FormatTimestamp(tsStr string) string {
	if tsStr == "" {
		return "??-?? ??:??"
	}
	t, err := parseISO(tsStr)
	if err != nil {
		return "??-?? ??:??"
	}
	return t.Format("01-02 15:04")
}
