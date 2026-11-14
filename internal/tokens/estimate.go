package tokens

import (
	"math"
	"unicode"
	"unicode/utf8"
)

// Offline token estimate for callers that cannot reach count_tokens.
//
// A single characters-per-token constant cannot fit Chinese-heavy sessions: one
// CJK character costs more than one token, while a character of Claude Code
// transcript text (code, paths, JSON, timestamps) costs about 0.45. Characters
// of the two kinds are therefore estimated separately.
//
// The two rates are fitted to count_tokens (claude-opus-5-5 and claude-fable-5-2
// returned identical counts) on the filtered text of 40 recent sessions, whose
// CJK share ranged from 1% to 44%. Against those sessions the estimate is off
// by 2.6% on average and 8.7% at worst; characters / 2 was off by 15.7% on
// average and 40% at worst, always low for the Chinese-heavy ones. The rates
// describe transcript text, so plain English prose is overestimated by about
// 50% (it runs near 3.2 characters per token).
const (
	cjkTokensPerChar   = 1.4
	otherCharsPerToken = 2.2
)

var cjkPunctuationAndFullwidthForms = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x3000, Hi: 0x303F, Stride: 1}, // CJK Symbols and Punctuation
		{Lo: 0xFF00, Hi: 0xFFEF, Stride: 1}, // Halfwidth and Fullwidth Forms
	},
}

// CountCJKRunes returns how many runes of text are Han, Hiragana, Katakana,
// Hangul, CJK punctuation or fullwidth forms.
func CountCJKRunes(text string) int {
	count := 0
	for _, r := range text {
		if isCJK(r) {
			count++
		}
	}
	return count
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, cjkPunctuationAndFullwidthForms)
}

// EstimateFromRuneCounts estimates the token count of text made of cjkRunes
// CJK runes (see CountCJKRunes) and otherRunes other runes.
func EstimateFromRuneCounts(cjkRunes int, otherRunes int) int {
	return int(math.Round(float64(cjkRunes)*cjkTokensPerChar + float64(otherRunes)/otherCharsPerToken))
}

// Estimate estimates the token count of text without calling the API.
func Estimate(text string) int {
	cjkRunes := CountCJKRunes(text)
	return EstimateFromRuneCounts(cjkRunes, utf8.RuneCountInString(text)-cjkRunes)
}
