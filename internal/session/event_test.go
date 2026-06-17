package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestStripANSI_GivenEscapeSequences_ThenRemovesThemButKeepsContent verifies
// SGR colour codes and other CSI escapes are removed while printable content,
// including the "⛁ ⛶" box glyphs used in the /context usage chart, survives.
func TestStripANSI_GivenEscapeSequences_ThenRemovesThemButKeepsContent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold then reset", "\x1b[1mContext Usage\x1b[22m", "Context Usage"},
		{"24-bit colour", "\x1b[38;2;136;136;136m⛁ ⛁ \x1b[39m", "⛁ ⛁ "},
		{"no escapes is identity", "plain text ⛶", "plain text ⛶"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripANSI(tc.in); got != tc.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestToolShortID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"toolu_01MgFTqrK7rZxtcLxfnuuCVa", "uCVa"},
		{"abc", "abc"},
		{"", ""},
		{"abcd", "abcd"},
		{"abcde", "bcde"},
	}
	for _, tc := range cases {
		if got := ToolShortID(tc.id); got != tc.want {
			t.Errorf("ToolShortID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxRunes int
		want     string
	}{
		{
			// Byte fast path: ASCII string already within the byte budget is
			// returned verbatim without allocating a rune slice.
			name:     "given short ascii within byte budget then returns verbatim",
			s:        "hello",
			maxRunes: 10,
			want:     "hello",
		},
		{
			// Multi-byte string whose byte length exceeds maxRunes but whose
			// rune count does not: "你好" is 6 bytes / 2 runes. With maxRunes=3
			// the byte fast path is skipped (6 > 3) but the rune check passes
			// (2 <= 3), so it must be returned untouched.
			name:     "given multibyte within rune budget but over byte budget then returns verbatim",
			s:        "你好",
			maxRunes: 3,
			want:     "你好",
		},
		{
			// Real truncation across a multi-byte boundary: 4 CJK runes cut to
			// 2 must yield exactly the first 2 runes as valid UTF-8, never a
			// half-byte of the third rune.
			name:     "given multibyte over rune budget then cuts on rune boundary",
			s:        "甲乙丙丁",
			maxRunes: 2,
			want:     "甲乙",
		},
		{
			// Boundary: rune count exactly equal to maxRunes is not truncated.
			name:     "given rune count equal to budget then returns verbatim",
			s:        "甲乙丙",
			maxRunes: 3,
			want:     "甲乙丙",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.maxRunes)
			if got != tt.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tt.s, tt.maxRunes, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", tt.s, tt.maxRunes, got)
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxRunes int
		want     string
	}{
		{
			// Multi-line input: only the first line survives, the rest is dropped.
			name:     "given multiline then keeps only first line",
			s:        "first line\nsecond line\nthird",
			maxRunes: 80,
			want:     "first line",
		},
		{
			// First line itself exceeds the budget: it is truncated to maxRunes.
			name:     "given long first line then truncates first line to budget",
			s:        "abcdefghij\nsecond",
			maxRunes: 4,
			want:     "abcd",
		},
		{
			// Leading/trailing whitespace is trimmed before the first line is taken.
			name:     "given surrounding whitespace then trims before splitting",
			s:        "  \n  hello\nworld  ",
			maxRunes: 80,
			want:     "hello",
		},
		{
			name:     "given empty string then returns empty",
			s:        "",
			maxRunes: 80,
			want:     "",
		},
		{
			name:     "given all whitespace then returns empty",
			s:        "   \n\t  \n  ",
			maxRunes: 80,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstLine(tt.s, tt.maxRunes); got != tt.want {
				t.Fatalf("FirstLine(%q, %d) = %q, want %q", tt.s, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		maxLen int
		want   string
	}{
		{
			name:   "given id longer than max then keeps prefix",
			id:     "12345678-1234-1234",
			maxLen: 8,
			want:   "12345678",
		},
		{
			name:   "given id equal to max then returns verbatim",
			id:     "12345678",
			maxLen: 8,
			want:   "12345678",
		},
		{
			name:   "given id shorter than max then returns verbatim",
			id:     "abc",
			maxLen: 8,
			want:   "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortID(tt.id, tt.maxLen); got != tt.want {
				t.Fatalf("ShortID(%q, %d) = %q, want %q", tt.id, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestToolInputString(t *testing.T) {
	input := ToolInput{Raw: map[string]any{
		"command":     "echo hi",
		"line_number": 42, // non-string value: type assertion must fail
	}}

	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "given key present with string value then returns value",
			key:  "command",
			want: "echo hi",
		},
		{
			name: "given key absent then returns empty",
			key:  "missing",
			want: "",
		},
		{
			// Type assertion failure branch: key exists but holds a non-string,
			// must yield "" rather than panic or a coerced value.
			name: "given key present with non-string value then returns empty",
			key:  "line_number",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := input.String(tt.key); got != tt.want {
				t.Fatalf("ToolInput.String(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestToolInputString_NilRaw(t *testing.T) {
	// Nil map lookup must not panic and returns the empty-string zero value.
	if got := (ToolInput{}).String("anything"); got != "" {
		t.Fatalf("ToolInput.String on nil Raw = %q, want empty", got)
	}
}

func TestToolInputMarshalNoEscape(t *testing.T) {
	input := ToolInput{Raw: map[string]any{
		"html": "<tag>",
	}}
	got := input.MarshalNoEscape()
	want := `{"html":"<tag>"}`
	if got != want {
		t.Fatalf("MarshalNoEscape() = %q, want %q", got, want)
	}
}

func TestToolInputMarshalNoEscape_NilRaw(t *testing.T) {
	got := (ToolInput{}).MarshalNoEscape()
	if got != "{}" {
		t.Fatalf("MarshalNoEscape() = %q, want {}", got)
	}
}

func TestToolResultStatus(t *testing.T) {
	tests := []struct {
		name   string
		result ToolResult
		want   string
	}{
		{name: "success", result: ToolResult{Success: true}, want: "ok"},
		{name: "failure", result: ToolResult{Success: false}, want: "FAILED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Status(); got != tt.want {
				t.Fatalf("Status() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolResultSummary(t *testing.T) {
	tests := []struct {
		name   string
		result ToolResult
		want   string
	}{
		{name: "success with text", result: ToolResult{Success: true, Text: "first\nsecond"}, want: " -> ok: first"},
		{name: "failure with text", result: ToolResult{Success: false, Text: "bad"}, want: " -> FAILED: bad"},
		{name: "success without text", result: ToolResult{Success: true}, want: " -> ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Summary(); got != tt.want {
				t.Fatalf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompactTaskNotification_GivenFullNotification_ThenKeepsSummaryAndResult(t *testing.T) {
	input := `<task-notification>
<task-id>ad4760fe24f754e27</task-id>
<tool-use-id>toolu_01XYfH8em2hJFSRtguEyqKXN</tool-use-id>
<output-file>/private/tmp/claude-501/tasks/ad4760fe24f754e27.output</output-file>
<status>completed</status>
<summary>Agent "Review test quality" completed</summary>
<result>Found 3 blockers in the test suite.</result>
</task-notification>`

	got, ok := CompactTaskNotification(input)
	if !ok {
		t.Fatal("CompactTaskNotification returned false, want true")
	}
	if !strings.Contains(got, `[Agent "Review test quality" completed]`) {
		t.Fatalf("missing summary line in: %q", got)
	}
	if !strings.Contains(got, "Found 3 blockers") {
		t.Fatalf("missing result content in: %q", got)
	}
	if strings.Contains(got, "task-id") || strings.Contains(got, "output-file") || strings.Contains(got, "tool-use-id") {
		t.Fatalf("XML metadata not stripped: %q", got)
	}
}

func TestCompactTaskNotification_GivenNonNotification_ThenReturnsFalse(t *testing.T) {
	_, ok := CompactTaskNotification("just a normal user message")
	if ok {
		t.Fatal("CompactTaskNotification returned true for non-notification")
	}
}

func TestCompactTaskNotification_GivenNotificationWithoutResult_ThenReturnsSummaryOnly(t *testing.T) {
	input := `<task-notification>
<task-id>abc123</task-id>
<status>completed</status>
<summary>Agent finished</summary>
</task-notification>`

	got, ok := CompactTaskNotification(input)
	if !ok {
		t.Fatal("CompactTaskNotification returned false")
	}
	if got != "[Agent finished]" {
		t.Fatalf("got %q, want %q", got, "[Agent finished]")
	}
}
