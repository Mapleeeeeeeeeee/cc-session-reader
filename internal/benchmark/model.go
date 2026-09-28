package benchmark

// RatioPct returns the ratio of NewContextTokens to ContextTokens as a percentage.
func (r Result) RatioPct() float64 {
	if r.ContextTokens == 0 {
		return 0
	}
	return float64(r.NewContextTokens) / float64(r.ContextTokens) * 100
}

// Result holds per-session benchmark output.
type Result struct {
	ShortID          string
	ContextTokens    int
	FilteredTokens   int
	NewContextTokens int
	SavedPct         float64
	CallsPerTurn     float64
	ToolIOPerCall    int // derived from actual PerTool data
	AvgResponse      int // derived from TotalOutputTokens / APICallCount
	Prompt           int // derived from context growth, or fallback perTurnPrompt
	InjectPages      int // pages needed by cc-session inject; <=1 keeps one-shot setup
	BreakEven        int
	Saving10Pct      float64
	Saving100Pct     float64
	WarmBreakEven    int
	WarmSaving10Pct  float64
	WarmSaving100Pct float64
}
