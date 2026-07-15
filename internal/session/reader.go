package session

// TranscriptReader abstracts transcript parsing so consumers
// don't couple to a specific JSONL format.
type TranscriptReader interface {
	ReadAll(path string) ([]Event, error)
}

// HeaderInfo contains minimal session metadata extracted from transcript headers.
type HeaderInfo struct {
	// Timestamp is the transcript's first timestamp (session start).
	Timestamp string

	// EndTimestamp is the transcript's last timestamp, read from a bounded
	// tail scan rather than a full read of the file. Combined with
	// Timestamp, callers derive session duration. Empty when the tail scan
	// found no timestamped entry.
	EndTimestamp string

	FirstUserPrompt string
}

// HeaderScanner extracts metadata from the first few lines of a transcript.
type HeaderScanner interface {
	ScanHeader(path string) (*HeaderInfo, error)
}
