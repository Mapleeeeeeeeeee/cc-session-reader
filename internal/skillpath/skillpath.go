package skillpath

import (
	"os"
	"path/filepath"
)

const SkillDirName = "cc-session"

func SkillDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills", SkillDirName)
}
