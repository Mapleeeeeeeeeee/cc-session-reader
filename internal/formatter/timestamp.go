package formatter

import (
	"fmt"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/parser"
)

// timestampLayout is the per-message clock, at second precision. The date is
// deliberately absent: it is identical on every message of a day, and a
// session's messages cluster tightly (98% of consecutive events in the
// measured sessions are under a minute apart), so repeating the date on every
// header taxed every header. dayMarkerLayout carries it instead, once per day.
//
// Second precision is what the repeated date paid for: ADR-007 decision 4
// measured this pair at -0.4% tokens against the old "01-02 15:04", while
// adding both seconds and the year that format never had.
const (
	timestampLayout = "15:04:05"
	dayMarkerLayout = "2006-01-02"
	unknownTime     = "??:??:??"
)

// timestampWriter renders one session's message-header times and emits a day
// marker when the date rolls over. Sessions really do span days (the measured
// samples span 2 and 3), so the date has to appear; it just appears 2-3 times
// instead of once per message.
type timestampWriter struct {
	lastDate string
}

// format returns the label for a message header and, when the date has just
// rolled over, the marker line to print above it. An unparseable timestamp
// yields a placeholder rather than a marker, so a malformed event cannot
// inject a bogus date boundary.
func (w *timestampWriter) format(tsStr string) (label string, dayMarker string) {
	t, ok := parser.ParseTimestamp(tsStr)
	if !ok {
		return unknownTime, ""
	}
	if date := t.Format(dayMarkerLayout); date != w.lastDate {
		w.lastDate = date
		dayMarker = fmt.Sprintf("--- %s ---", date)
	}
	return t.Format(timestampLayout), dayMarker
}
