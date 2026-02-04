package setup

import (
	"log"
	"os"
	"path/filepath"
)

// CleanUserDataDir removes app-managed components from the user data directory
// while preserving user data (profiles, preferences, game library).
func CleanUserDataDir(userDataPath string, warn func(error)) {
	appDirs := []string{
		"broth",
		"logs",
		"crash_logs",
		"prereqs",
	}

	for _, dir := range appDirs {
		fullPath := filepath.Join(userDataPath, dir)
		info, err := os.Lstat(fullPath)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			log.Printf("skipping symlink: %s", fullPath)
			continue
		}
		log.Printf("delete (%s)/", fullPath)
		if err := os.RemoveAll(fullPath); err != nil {
			warn(err)
		}
	}
}
