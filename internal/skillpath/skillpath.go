package skillpath

import (
	"os"
	"path/filepath"
)

const SkillDirName = "cc-session"

func SkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills", SkillDirName), nil
}
