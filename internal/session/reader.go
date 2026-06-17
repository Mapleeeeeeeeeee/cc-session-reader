package session

// TranscriptReader abstracts transcript parsing so consumers
// don't couple to a specific JSONL format.
type TranscriptReader interface {
	ReadAll(path string) ([]Event, error)
}

// HeaderInfo contains minimal session metadata extracted from transcript headers.
type HeaderInfo struct {
	Timestamp       string
	FirstUserPrompt string
}

// HeaderScanner extracts metadata from the first few lines of a transcript.
type HeaderScanner interface {
	ScanHeader(path string) (*HeaderInfo, error)
}
