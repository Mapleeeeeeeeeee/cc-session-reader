package parser

import (
	"os"
	"path/filepath"

	"github.com/Mapleeeeeeeeeee/cc-session-reader/internal/session"
)

// Store points at Claude Code's on-disk session data.
type Store struct {
	ProjectsDir    string
	SessionMetaDir string
	HeaderScanner  session.HeaderScanner
}

// DefaultStore returns a Store derived from the current user's ~/.claude.
// Call DefaultStoreWith to inject a HeaderScanner.
func DefaultStore() Store {
	claudeDir := filepath.Join(homeDir(), ".claude")
	return Store{
		ProjectsDir:    filepath.Join(claudeDir, "projects"),
		SessionMetaDir: filepath.Join(claudeDir, "usage-data", "session-meta"),
	}
}

// DefaultStoreWith returns a Store with the given HeaderScanner injected.
func DefaultStoreWith(scanner session.HeaderScanner) Store {
	s := DefaultStore()
	s.HeaderScanner = scanner
	return s
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
