package formatter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/summarizer"
)

// FormatOptions controls verbosity for formatting functions.
type FormatOptions struct {
	VerboseAgents   bool
	VerboseBash     bool
	VerboseThinking bool
	VerboseCommands bool
}

// ContentSink receives each unit of kept content as it is emitted by the read
// render pipeline, tagged with the category it belongs to. analyzer.ComputeStats
// uses this so its KEPT breakdown is derived from the exact same pass that
// produces the injected text (see RenderReadEventsWithSink), instead of a
// second, drift-prone reimplementation of what read/context actually keep.
type ContentSink func(category string, text string)

// renderContext bundles the render-invariant values threaded unchanged through
// the read/context render pipelines (renderReadEvents -> handleToolResultRead
// -> flushPendingTools, and renderContextEvents -> flushPendingTools) so
// callers pass them once instead of repeating agentIDs/opts/out/sink as
// positional parameters at every layer.
type renderContext struct {
	agentIDs map[string]bool
	opts     FormatOptions
	out      io.Writer
	sink     ContentSink
}

// Content categories reported to ContentSink. The values match the keys
// analyzer.StatsResult.Categories uses for its KEPT buckets.
const (
	CategoryUserText      = "user_text"
	CategoryUserAnswer    = "user_answers"
	CategoryAssistantText = "assistant_text"
	CategoryToolSummary   = "tool_summaries"
)

// userRender is the rendered form of a user-message event: the body to print
// and whether anything should be printed at all.
type userRender struct {
	body string
	show bool
}

// renderUserMessage resolves how a user-message event should appear given the
// verbosity options. It is the single rendering policy shared by read and
// context so both stay consistent:
//   - command invocation -> always show the marker (e.g. "[/context]")
//   - command noise -> drop by default; show ANSI-stripped body under
//     -verbose-commands, except caveats which are always dropped
//   - plain typed message -> show verbatim
func renderUserMessage(user *session.UserMessage, opts FormatOptions, seenSkills map[string]bool) userRender {
	if user == nil {
		return userRender{}
	}
	if user.CommandMarker != "" {
		return userRender{body: user.CommandMarker, show: true}
	}
	if user.IsCommandNoise {
		if !opts.VerboseCommands || user.IsCaveat {
			return userRender{}
		}
		body := strings.TrimSpace(session.StripANSI(user.Text))
		if body == "" {
			return userRender{}
		}
		return userRender{body: body, show: true}
	}

	// Harness-injected subtypes: strip or compact.
	if user.IsSystemReminder || user.IsContextUsage {
		return userRender{}
	}
	if user.IsSkillInjection {
		return userRender{body: session.CompactSkillInjection(user, seenSkills), show: true}
	}
	if user.IsTeammateMessage {
		if body, ok := session.CompactTeammateMessage(user.Text); ok {
			return userRender{body: body, show: true}
		}
		return userRender{body: user.Text, show: true}
	}
	if user.IsCommandInjection {
		if body, ok := session.CompactCommandInjection(user.Text); ok {
			return userRender{body: body, show: true}
		}
		return userRender{body: user.Text, show: true}
	}

	if strings.TrimSpace(user.Text) == "" {
		return userRender{}
	}
	if body, ok := session.CompactTaskNotification(user.Text); ok {
		return userRender{body: body, show: true}
	}
	return userRender{body: user.Text, show: true}
}

type pendingTool struct {
	toolUseID        string
	summary          string
	name             string // e.g. "Bash", "Read", "Edit"
	injectSessionID  string // non-empty when this is a cc-session inherit/inject/read/context call
	injectTotalLines int    // total lines from the last page marker
	ccSubcommand     string // "inherit", "inject" (legacy), "read", or "context"

	// callLabel is the tagged call summary as summarizeToolUse first produced
	// it (e.g. "[Bash#id] description"), kept alongside summary — which
	// appendToolResult mutates by appending the result — so collapseRetryLoops
	// can rebuild a single collapsed line from the last attempt's own label.
	callLabel string

	// retrySignature is the normalized identity collapseRetryLoops keys a
	// retry loop on: the first line of the Bash command, or of the raw input
	// for any other tool, with whitespace collapsed. Empty when there is
	// nothing meaningful to key on, which makes the call ineligible for
	// retry-loop collapsing rather than risking a false match against an
	// equally-empty, unrelated call.
	retrySignature string

	// readFilePath is Read's raw file_path input (grouping key) and
	// readDisplayPath its cleaned display form, both used by
	// collapseSameFileReads to detect and render consecutive reads of the
	// same file. Empty for non-Read tools.
	readFilePath    string
	readDisplayPath string

	// resultReceived and resultSuccess record whether a tool_result has been
	// merged into this pendingTool yet and its outcome. Both collapse passes
	// need to tell "this call finished and failed/succeeded" apart from
	// "this call is still awaiting its result".
	resultReceived bool
	resultSuccess  bool
}

func loadEvents(transcriptPath string, isVerboseAgents bool, reader session.TranscriptReader) ([]session.Event, map[string]bool, error) {
	events, err := reader.ReadAll(transcriptPath)
	if err != nil {
		return nil, nil, err
	}
	agentIDs := map[string]bool{}
	if isVerboseAgents {
		agentIDs = session.CollectAgentToolIDs(events)
	}
	return events, agentIDs, nil
}

func applyInjectResult(pt *pendingTool, result *session.ToolResult) {
	if !result.Success {
		pt.injectSessionID = ""
	}
	if pt.injectSessionID != "" {
		pt.injectTotalLines = parseTotalLines(result.Text)
	}
}

func appendToolResult(result *session.ToolResult, pendingTools *[]pendingTool, opts FormatOptions) {
	if result.ToolUseID != "" {
		for i := range *pendingTools {
			pt := &(*pendingTools)[i]
			if pt.toolUseID == result.ToolUseID {
				applyInjectResult(pt, result)
				pt.resultReceived = true
				pt.resultSuccess = result.Success
				if opts.VerboseBash && pt.name == session.ToolBash {
					pt.summary += formatVerboseBashResult(result)
					return
				}
				pt.summary += result.Summary()
				return
			}
		}
	}
	if len(*pendingTools) > 0 {
		last := &(*pendingTools)[len(*pendingTools)-1]
		applyInjectResult(last, result)
		last.resultReceived = true
		last.resultSuccess = result.Success
		if opts.VerboseBash && last.name == session.ToolBash {
			last.summary += formatVerboseBashResult(result)
			return
		}
		last.summary += result.Summary()
		return
	}
	name := result.RawName
	if name == "" {
		name = "ToolResult"
	}
	summary := fmt.Sprintf("[%s]%s", name, result.Summary())
	if opts.VerboseBash && name == session.ToolBash {
		summary = fmt.Sprintf("[%s]%s", name, formatVerboseBashResult(result))
	}
	*pendingTools = append(*pendingTools, pendingTool{
		summary:        summary,
		name:           name,
		resultReceived: true,
		resultSuccess:  result.Success,
	})
}

func formatVerboseBashResult(result *session.ToolResult) string {
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return fmt.Sprintf(" -> %s", result.Status())
	}
	indented := indentBlock(text, "    ")
	return fmt.Sprintf(" -> %s:\n%s", result.Status(), indented)
}

func indentBlock(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func summarizeToolUse(tool session.ToolUse) pendingTool {
	name := tool.Name
	if name == "" {
		name = "?"
	}
	shortID := session.ToolShortID(tool.ID)
	summary := summarizer.SummarizeToolUse(name, tool.Input, tool.Cwd)
	// Inject "#shortID" before the closing ']' of the first bracket group
	// so "[Bash] cmd" becomes "[Bash#ol-1] cmd" and
	// "[Agent(general)] desc" becomes "[Agent(general)#ol-1] desc".
	tagged := injectShortID(summary, shortID)
	pt := pendingTool{
		toolUseID:      tool.ID,
		summary:        tagged,
		callLabel:      tagged,
		name:           name,
		retrySignature: retrySignature(name, tool.Input),
	}
	if name == session.ToolBash {
		cmd := tool.Input.String("command")
		if sub, sessionPrefix := parseCCSessionCommand(cmd); sessionPrefix != "" {
			pt.injectSessionID = sessionPrefix
			pt.ccSubcommand = sub
		}
	}
	if name == session.ToolRead {
		path := tool.Input.String("file_path")
		pt.readFilePath = path
		if path != "" {
			pt.readDisplayPath = summarizer.CleanPath(path, tool.Cwd)
		}
	}
	return pt
}

// retrySignature returns the normalized identity collapseRetryLoops keys a
// retry loop on: the first line of the Bash command (multi-line scripts
// don't defeat the match), or the first line of the raw input JSON for any
// other tool. Returns "" when there is nothing to key on.
func retrySignature(name string, input session.ToolInput) string {
	raw := input.String("command")
	if name != session.ToolBash {
		raw = input.MarshalNoEscape()
	}
	firstLine := strings.SplitN(strings.TrimSpace(raw), "\n", 2)[0]
	return normalizeWhitespace(firstLine)
}

// normalizeWhitespace collapses runs of whitespace to single spaces so
// cosmetic formatting differences (extra spaces, tabs) don't defeat the
// retry-loop prefix comparison.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// injectShortID inserts "#id" before the first ']' in summary.
// "[Bash] Run tests" -> "[Bash#uCVa] Run tests"
// "[Agent(general)] Inspect" -> "[Agent(general)#uCVa] Inspect"
func injectShortID(summary string, shortID string) string {
	if shortID == "" {
		return summary
	}
	idx := strings.Index(summary, "]")
	if idx < 0 {
		return summary
	}
	return summary[:idx] + "#" + shortID + summary[idx:]
}

// parseCCSessionCommand checks if cmd is a cc-session inherit/inject/read/context
// command and returns the subcommand and session ID prefix (first 8 chars).
// "inject" is the legacy name for "inherit" (kept for old transcripts) and is
// treated as an equivalent verb for collapsing purposes.
// Returns ("", "") if not a cc-session command.
func parseCCSessionCommand(cmd string) (subcommand string, sessionID string) {
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) < 3 || fields[0] != "cc-session" {
		return "", ""
	}
	switch fields[1] {
	case "inherit", "inject", "read", "context":
	default:
		return "", ""
	}
	id := fields[2]
	if strings.HasPrefix(id, "-") {
		return "", ""
	}
	if len(id) >= 8 {
		return fields[1], id[:8]
	}
	return fields[1], id
}

// parseTotalLines extracts the total line count from a cc-session page marker
// like "ok: [page 1/4 | lines 1-377 of 1320]". Returns 0 if not found.
func parseTotalLines(text string) int {
	firstLine := text
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		firstLine = text[:nl]
	}
	const marker = " of "
	idx := strings.LastIndex(firstLine, marker)
	if idx < 0 {
		return 0
	}
	rest := firstLine[idx+len(marker):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

// collapseCCSessionTools collapses consecutive pendingTools that share the same
// non-empty injectSessionID into a single summary line.
func collapseCCSessionTools(tools []pendingTool) []pendingTool {
	if len(tools) == 0 {
		return tools
	}
	hasInject := false
	for _, t := range tools {
		if t.injectSessionID != "" {
			hasInject = true
			break
		}
	}
	if !hasInject {
		return tools
	}
	result := make([]pendingTool, 0, len(tools))
	for i := 0; i < len(tools); i++ {
		pt := tools[i]
		if pt.injectSessionID == "" {
			result = append(result, pt)
			continue
		}
		j := i + 1
		for j < len(tools) && tools[j].injectSessionID == pt.injectSessionID {
			j++
		}
		last := tools[j-1]
		verb := "loaded"
		switch pt.ccSubcommand {
		case "inherit":
			verb = "inherited"
		case "inject":
			// Legacy verb: old transcripts recorded "cc-session inject" before
			// the CLI rename to "inherit". Keep the historical wording so old
			// sessions still read naturally.
			verb = "injected"
		}
		shortID := session.ToolShortID(pt.toolUseID)
		if last.injectTotalLines > 0 {
			last.summary = fmt.Sprintf("(cc-session#%s: %s session %s here, %d lines omitted)", shortID, verb, last.injectSessionID, last.injectTotalLines)
		} else {
			last.summary = fmt.Sprintf("(cc-session#%s: %s session %s here)", shortID, verb, last.injectSessionID)
		}
		result = append(result, last)
		i = j - 1
	}
	return result
}

// collapseRetryLoops collapses a consecutive run (>=2) of pendingTools that
// share the same tool name, the same (whitespace-normalized, prefix-matched)
// retrySignature, and all FAILED, into a single "FAILED xN" line — a retry
// loop that repeats the same failing call N times otherwise contributes N
// near-identical lines. A run of exactly 1 is left untouched (no "x1" label);
// a success or an unrelated call in between breaks the run so its individual
// failure information is never silently dropped.
func collapseRetryLoops(tools []pendingTool) []pendingTool {
	result := make([]pendingTool, 0, len(tools))
	for i := 0; i < len(tools); i++ {
		pt := tools[i]
		if !isRetryEligible(pt) {
			result = append(result, pt)
			continue
		}
		j := i + 1
		for j < len(tools) && isRetryEligible(tools[j]) &&
			tools[j].name == pt.name &&
			sameRetryCommand(pt.retrySignature, tools[j].retrySignature) {
			j++
		}
		count := j - i
		if count < 2 {
			result = append(result, pt)
			continue
		}
		last := tools[j-1]
		last.summary = formatRetryCollapse(last, count)
		result = append(result, last)
		i = j - 1
	}
	return result
}

// isRetryEligible reports whether pt finished with a failure and carries a
// non-empty retry signature to key on — the precondition for taking part in
// a retry-loop group at all.
func isRetryEligible(pt pendingTool) bool {
	return pt.resultReceived && !pt.resultSuccess && pt.retrySignature != ""
}

// sameRetryCommand reports whether a and b identify the same retried call:
// exact match, or one is a non-empty prefix of the other (covers "only the
// trailing argument differs" retries, e.g. a script rerun with a new seed).
func sameRetryCommand(a, b string) bool {
	if a == b {
		return true
	}
	shorter, longer := a, b
	if len(longer) < len(shorter) {
		shorter, longer = longer, shorter
	}
	return shorter != "" && strings.HasPrefix(longer, shorter)
}

// formatRetryCollapse rebuilds the collapsed retry-loop line from the last
// attempt's own call label (tool id + description) plus its failure
// excerpt, annotated with how many attempts failed. last.summary is always
// last.callLabel followed by exactly the tail appendToolResult appended
// (ToolResult.Summary(), which contains the literal "FAILED" once), so
// inserting the count there mirrors the string-surgery injectShortID already
// uses to tag summaries.
func formatRetryCollapse(last pendingTool, count int) string {
	tail := strings.TrimPrefix(last.summary, last.callLabel)
	tail = strings.Replace(tail, "FAILED", fmt.Sprintf("FAILED ×%d", count), 1)
	return last.callLabel + tail
}

// collapseSameFileReads collapses a consecutive run (>=2) of successful Read
// calls against the same file into a single "[Read#id xN] path -> ok" line.
// offset/limit differing across the run is expected (progressive paging
// through a long file) and does not break the collapse; any failure, a
// different file, or another tool in between does.
func collapseSameFileReads(tools []pendingTool) []pendingTool {
	result := make([]pendingTool, 0, len(tools))
	for i := 0; i < len(tools); i++ {
		pt := tools[i]
		if !isCollapsibleRead(pt) {
			result = append(result, pt)
			continue
		}
		j := i + 1
		for j < len(tools) && isCollapsibleRead(tools[j]) && tools[j].readFilePath == pt.readFilePath {
			j++
		}
		count := j - i
		if count < 2 {
			result = append(result, pt)
			continue
		}
		last := tools[j-1]
		shortID := session.ToolShortID(last.toolUseID)
		last.summary = fmt.Sprintf("[Read#%s ×%d] %s -> ok", shortID, count, pt.readDisplayPath)
		result = append(result, last)
		i = j - 1
	}
	return result
}

// isCollapsibleRead reports whether pt is a successful Read call carrying a
// known file path — the precondition for taking part in a same-file-read
// group at all.
func isCollapsibleRead(pt pendingTool) bool {
	return pt.name == session.ToolRead && pt.resultReceived && pt.resultSuccess && pt.readFilePath != ""
}

func flushPendingTools(pendingTools *[]pendingTool, rc renderContext) {
	tools := *pendingTools
	if !rc.opts.VerboseBash {
		tools = collapseCCSessionTools(tools)
		tools = collapseRetryLoops(tools)
	}
	tools = collapseSameFileReads(tools)
	for _, pt := range tools {
		fmt.Fprintf(rc.out, "  %s\n", pt.summary)
		if rc.sink != nil {
			rc.sink(CategoryToolSummary, pt.summary)
		}
	}
	if len(*pendingTools) > 0 {
		fmt.Fprintln(rc.out)
	}
	*pendingTools = (*pendingTools)[:0]
}

// applyPagination slices the formatted output by offset and maxLines, writing
// the result to out. It appends a truncation message when lines were cut.
func applyPagination(formatted string, maxLines int, offset int, out io.Writer) error {
	allLines := strings.Split(formatted, "\n")
	// strings.Split on a trailing newline produces an empty last element; exclude it
	// from the count so line math matches what the user sees.
	totalLines := len(allLines)
	if totalLines > 0 && allLines[totalLines-1] == "" {
		totalLines--
	}

	if offset >= totalLines {
		if totalLines > 0 {
			fmt.Fprintf(out, "--- offset %d exceeds total ~%d lines ---\n", offset, totalLines)
		}
		return nil
	}

	visibleLines := allLines[offset:]
	isTruncated := false
	if maxLines > 0 && len(visibleLines) > maxLines {
		visibleLines = visibleLines[:maxLines]
		isTruncated = true
	}

	fmt.Fprint(out, strings.Join(visibleLines, "\n"))
	// Restore the trailing newline that strings.Split consumed, unless the last
	// visible line is already empty (which would produce a double newline).
	lastVisible := visibleLines[len(visibleLines)-1]
	if lastVisible != "" {
		fmt.Fprintln(out)
	}

	if isTruncated {
		resumeAt := offset + maxLines
		fmt.Fprintf(out, "\n--- truncated at line %d (total ~%d lines) — use --offset %d to continue ---\n", resumeAt, totalLines, resumeAt)
	}
	return nil
}
