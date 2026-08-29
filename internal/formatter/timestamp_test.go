package formatter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/claudecodec"
)

// ADR-007 decision 4: the per-message header repeated a date that is identical
// all day and omitted the year entirely, so a session read months later could
// not be placed in time. The date moved to a day marker and the space it freed
// bought second precision.

func TestTimestampWriter_GivenEventsOnOneDay_WhenFormatted_ThenMarksTheDateOnceAndClocksEveryMessage(t *testing.T) {
	writer := &timestampWriter{}

	label, marker := writer.format("2026-08-06T03:06:15Z")
	if want := "--- 2026-08-06 ---"; marker != want {
		t.Errorf("first event marker = %q, want %q", marker, want)
	}
	if want := "03:06:15"; label != want {
		t.Errorf("first event label = %q, want %q", label, want)
	}

	label, marker = writer.format("2026-08-06T11:23:04Z")
	if marker != "" {
		t.Errorf("same-day event must not repeat the date, got marker %q", marker)
	}
	if want := "11:23:04"; label != want {
		t.Errorf("second event label = %q, want %q", label, want)
	}
}

func TestTimestampWriter_GivenSessionCrossingMidnight_WhenFormatted_ThenMarksTheNewDate(t *testing.T) {
	writer := &timestampWriter{}
	writer.format("2026-08-06T23:59:00Z")

	_, marker := writer.format("2026-08-07T02:08:31Z")

	if want := "--- 2026-08-07 ---"; marker != want {
		t.Errorf("day-change marker = %q, want %q", marker, want)
	}
}

// A malformed timestamp must not reset the day state or invent a boundary:
// doing so would print a marker for a date the session never reached.
func TestTimestampWriter_GivenUnparseableTimestamp_WhenFormatted_ThenPlaceholderWithoutMarker(t *testing.T) {
	writer := &timestampWriter{}
	writer.format("2026-08-06T03:06:15Z")

	label, marker := writer.format("not-a-timestamp")

	if want := unknownTime; label != want {
		t.Errorf("label = %q, want %q", label, want)
	}
	if marker != "" {
		t.Errorf("unparseable timestamp must not emit a day marker, got %q", marker)
	}

	if _, marker := writer.format("2026-08-06T04:00:00Z"); marker != "" {
		t.Errorf("day state must survive an unparseable timestamp, got marker %q", marker)
	}
}

func TestFormatRead_GivenTranscript_WhenRendered_ThenOpensWithTheDateAndCarriesTheYear(t *testing.T) {
	transcriptPath, _ := writeFormatterFixture(t)

	var out bytes.Buffer
	if err := FormatRead(transcriptPath, 0, 0, FormatOptions{}, &out, claudecodec.Codec{}); err != nil {
		t.Fatalf("FormatRead returned error: %v", err)
	}
	got := out.String()

	if !strings.HasPrefix(got, "--- 2026-05-28 ---\n") {
		t.Errorf("read output must open with the full date\ngot:\n%q", got)
	}
	if strings.Contains(got, "[05-28") {
		t.Errorf("per-message header must no longer repeat the date\ngot:\n%q", got)
	}
}
