package formatter_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/formatter"
)

// harnessTextLeakCases are transcript lines Claude Code writes on its own
// behalf, in the shapes seen in real transcripts (text shortened). None of
// them is something a person or the model said.
var harnessTextLeakCases = map[string]struct {
	line     string
	wantBody string
}{
	"an invocation notice for a published Artifact type": {
		line: `{"type":"user","timestamp":"2026-11-02T02:44:46Z","isMeta":true,"turnCompanion":true,` +
			`"message":{"role":"user","content":[{"type":"text","text":"` + "`/design` was invoked: a request for a design made as a NEW Artifact. Call the `Artifact` tool." +
			`\n\nNo brief was given."}]}}`,
		wantBody: "[invoked: /design]",
	},
	"a usage-limit wrap-up note": {
		line: `{"type":"user","timestamp":"2026-11-11T07:30:23Z","isMeta":true,"turnCompanion":true,"usageLimitNote":"wrap_up",` +
			`"message":{"role":"user","content":"[Usage limit reached; a short grace allowance remains, then this turn is cut off without warning.]"}}`,
		wantBody: "[usage limit note: wrap_up]",
	},
	"a usage-limit release note": {
		line: `{"type":"user","timestamp":"2026-11-11T08:21:28Z","isMeta":true,"turnCompanion":true,"usageLimitNote":"release",` +
			`"message":{"role":"user","content":"[Earlier usage-limit notes no longer apply. Continue working normally.]"}}`,
		wantBody: "[usage limit note: release]",
	},
	"a permission retry prompt": {
		line: `{"type":"user","timestamp":"2026-11-01T14:34:53Z","isMeta":true,"origin":{"kind":"auto-continuation"},` +
			`"message":{"role":"user","content":"Permission granted for this exact command only, as one whole string: \"git fetch -q --prune\". You may now retry this exact command if you would like."}}`,
		wantBody: "[auto-continue]",
	},
	"a continuation after the usage limit reset": {
		line: `{"type":"user","timestamp":"2026-09-25T20:28:20Z","isMeta":true,"origin":{"kind":"auto-continuation"},"promptSource":"system",` +
			`"message":{"role":"user","content":"Your claude.ai usage limit has reset. Continue the task you were working on when the limit was reached."}}`,
		wantBody: "[auto-continue]",
	},
	"a synthetic API connection error": {
		line: `{"type":"assistant","timestamp":"2026-11-11T11:58:29Z","apiError":"connection_failed","isApiErrorMessage":true,` +
			`"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"}]}}`,
		wantBody: "[api error] API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)",
	},
	"a synthetic usage-limit message": {
		line: `{"type":"assistant","timestamp":"2026-11-11T07:36:31Z","apiError":"usage_limit_reached","isApiErrorMessage":true,` +
			`"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"You've hit your 5-hour limit · resets 4:20pm (Asia/Taipei)"}]}}`,
		wantBody: "[api error] You've hit your 5-hour limit · resets 4:20pm (Asia/Taipei)",
	},
}

// Regression: text Claude Code injects itself (invocation notice, usage-limit notes, permission retry prompt, synthetic API errors) was printed as "user:" or "assistant:" (v0.1.84)
func TestIntegration_Read_GivenHarnessWrittenText_WhenFormatted_ThenLabelsItHarnessInCompactForm(t *testing.T) {
	for name, tc := range harnessTextLeakCases {
		t.Run(name, func(t *testing.T) {
			got := readOutputFor(t, tc.line+"\n")

			wantBlock := "] harness:\n" + tc.wantBody + "\n"
			if !strings.Contains(got, wantBlock) {
				t.Errorf("output missing %q\ngot:\n%s", wantBlock, got)
			}
			if strings.Contains(got, "user:") || strings.Contains(got, "assistant:") {
				t.Errorf("output still labels the text as spoken by a person or the model\ngot:\n%s", got)
			}
		})
	}
}

// Messages that look close to the harness-written ones but are not: ADR-009
// keeps isMeta from being a harness marker on its own, because image
// placeholders carry it and belong to the user, and a message from the model
// stays the model's even when its model field is "<synthetic>".
func TestIntegration_Read_GivenLookalikesOfHarnessWrittenText_WhenFormatted_ThenKeepsTheirOriginalLabel(t *testing.T) {
	tests := map[string]struct {
		line      string
		wantBlock string
	}{
		"an image placeholder that carries isMeta": {
			line: `{"type":"user","timestamp":"2026-11-02T02:44:46Z","isMeta":true,"turnCompanion":true,` +
				`"message":{"role":"user","content":[{"type":"text","text":"[Image: source: /tmp/shot.png]"}]}}`,
			wantBlock: "] user:\n[Image: source: /tmp/shot.png]\n",
		},
		"a typed message that quotes the invocation notice wording": {
			line: `{"type":"user","timestamp":"2026-11-02T02:44:46Z","promptSource":"typed",` +
				`"message":{"role":"user","content":"` + "`/design` was invoked: why does it print this?" + `"}}`,
			wantBlock: "] user:\n`/design` was invoked: why does it print this?\n",
		},
		"a model answer whose model field is synthetic but is not an API error": {
			line: `{"type":"assistant","timestamp":"2026-11-02T02:44:46Z",` +
				`"message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Side answer from the model."}]}}`,
			wantBlock: "] assistant:\nSide answer from the model.\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := readOutputFor(t, tc.line+"\n")

			if !strings.Contains(got, tc.wantBlock) {
				t.Errorf("output missing %q\ngot:\n%s", tc.wantBlock, got)
			}
			if strings.Contains(got, "harness:") {
				t.Errorf("output labels a non-harness message as harness\ngot:\n%s", got)
			}
		})
	}
}

func TestIntegration_Context_GivenSyntheticAPIError_WhenFormatted_ThenPrefixesItHarness(t *testing.T) {
	events := readTranscript(t, harnessTextLeakCases["a synthetic usage-limit message"].line+"\n")

	var out bytes.Buffer
	if err := formatter.FormatContextEvents(events, map[string]bool{}, 0, 0, formatter.FormatOptions{}, &out); err != nil {
		t.Fatalf("FormatContextEvents returned error: %v", err)
	}

	want := "H: [api error] You've hit your 5-hour limit · resets 4:20pm (Asia/Taipei)\n"
	if got := out.String(); !strings.Contains(got, want) {
		t.Errorf("output missing %q\ngot:\n%s", want, got)
	}
}
