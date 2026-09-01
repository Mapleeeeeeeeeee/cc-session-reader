package summarizer

import (
	"path/filepath"
	"strings"
)

// maxVerbLen bounds the command fragment appended to a Bash summary. Measured
// against the alternatives in ADR-007 decision 2: a raw 60-character prefix
// costs +9.8% tokens because the budget is spent on scaffolding, while the
// extracted verb at this length costs +2.0% and is offset by dropping the
// success marker.
const maxVerbLen = 30

// scaffoldingPrograms lead a command segment that positions the shell rather
// than doing the work a reader wants to identify. `echo` is here because the
// commands in real transcripts overwhelmingly use it to label their own
// output ("echo '=== game-engine ===' && sed -n ...").
var scaffoldingPrograms = map[string]bool{
	"cd": true, "export": true, "set": true, "source": true, ".": true,
	"echo": true, "printf": true, "true": true, "clear": true,
}

// commandVerb returns the program and first argument of the first segment of
// cmd that names real work: "pnpm tsc", "git push", "grep -n". It returns ""
// when nothing in cmd names a program.
//
// A plain prefix of the command does not work as a summary. Real commands
// overwhelmingly open with `cd <absolute path> &&`, an env assignment, or an
// echoed heading, so the first 20 characters are usually
// "cd /Users/maple/Desk" and identify nothing. Walking past those segments
// reaches a real program name on 99.8% of the Bash calls in the measured
// sample.
func commandVerb(cmd string) string {
	for _, line := range strings.Split(cmd, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, segment := range splitShellSegments(line) {
			if verb := segmentVerb(segment); verb != "" {
				return verb
			}
		}
	}
	return ""
}

// splitShellSegments breaks a command line on the operators separating one
// invocation from the next, so `cd x && real-command` yields the real command
// as a segment of its own.
func splitShellSegments(line string) []string {
	segments := strings.FieldsFunc(line, func(r rune) bool {
		return r == '&' || r == ';' || r == '|'
	})
	for i, segment := range segments {
		segments[i] = strings.TrimSpace(segment)
	}
	return segments
}

// controlKeywords open a shell construct without naming the work inside it.
// `until gh pr checks` is a poll loop around `gh`, and the reader wants the
// `gh`, so these are stepped over the way an env assignment is.
var controlKeywords = map[string]bool{
	"until": true, "while": true, "for": true, "if": true, "do": true,
	"then": true, "else": true, "elif": true, "time": true, "command": true,
}

// segmentVerb returns "<program> <first-arg>" for a segment naming a real
// program, or "" for scaffolding, a bare env assignment, or an empty segment.
// The program is reduced to its base name so an absolute interpreter path
// ("/opt/homebrew/bin/gh") does not consume the whole budget.
func segmentVerb(segment string) string {
	fields := strings.Fields(segment)
	for len(fields) > 0 && (isEnvAssignment(fields[0]) || controlKeywords[fields[0]]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	program := filepath.Base(fields[0])
	if scaffoldingPrograms[program] {
		return ""
	}
	if len(fields) == 1 {
		return program
	}
	return program + " " + fields[1]
}

// isEnvAssignment reports whether field is a leading VAR=value prefix rather
// than the program name.
func isEnvAssignment(field string) bool {
	return strings.Contains(field, "=") && !strings.HasPrefix(field, "-")
}
