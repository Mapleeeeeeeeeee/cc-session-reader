package tokens

import (
	"strings"
	"testing"
)

// Expected values follow the documented rates: 1.4 tokens per CJK rune and
// 2.2 other runes per token.
func TestEstimate_GivenText_WhenEstimated_ThenCJKAndOtherRunesUseTheirOwnRates(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"empty text", "", 0},
		{"ten Han characters", "一二三四五六七八九十", 14},
		{"twenty-two ASCII characters", strings.Repeat("a", 22), 10},
		{"Han characters mixed with ASCII", "你好" + strings.Repeat("a", 11), 8}, // 2.8 + 5.0
		{"fullwidth punctuation counts as CJK", "，。，。，", 7},
		{"Japanese kana and Korean hangul count as CJK", "あア한", 4}, // 4.2
		{"non-CJK non-ASCII characters count at the ASCII rate", strings.Repeat("é", 22), 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Estimate(tc.text); got != tc.want {
				t.Fatalf("Estimate(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}
