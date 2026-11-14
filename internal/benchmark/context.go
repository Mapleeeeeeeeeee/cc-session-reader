package benchmark

// OverheadFlagUsage is the help text of the -overhead flag of every command
// that measures a new session's context.
const OverheadFlagUsage = "session overhead tokens (system+tools+CLAUDE.md); measure with a 1-turn session"

// ResolveOverhead returns the -overhead flag value, or DefaultOverhead when the
// flag is unset (zero or negative).
func ResolveOverhead(flagValue int) int {
	if flagValue <= 0 {
		return DefaultOverhead
	}
	return flagValue
}

// NewContextTokens returns the context size of a new session that starts with
// the session overhead and then holds the injected filtered history.
func NewContextTokens(overhead int, filteredTokens int) int {
	return overhead + filteredTokens
}
