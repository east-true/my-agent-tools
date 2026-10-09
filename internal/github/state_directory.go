package github

import (
	"os"
	"path/filepath"
	"strings"
)

// Share state across linked worktrees without writing inside Git metadata.
func defaultStateDirectory() string {
	working, err := os.Getwd()
	if err != nil {
		return ".tools/state"
	}
	for directory := working; ; directory = filepath.Dir(directory) {
		gitPath := filepath.Join(directory, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			if info.IsDir() {
				return filepath.Join(directory, ".tools", "state")
			}
			if info.Mode().IsRegular() && info.Size() < 4096 {
				data, _ := os.ReadFile(gitPath)
				if strings.HasPrefix(string(data), "gitdir: ") {
					gitDir := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
					if !filepath.IsAbs(gitDir) {
						gitDir = filepath.Join(directory, gitDir)
					}
					if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
						commonDir := strings.TrimSpace(string(common))
						if !filepath.IsAbs(commonDir) {
							commonDir = filepath.Join(gitDir, commonDir)
						}
						commonDir = filepath.Clean(commonDir)
						if filepath.Base(commonDir) == ".git" {
							return filepath.Join(filepath.Dir(commonDir), ".tools", "state")
						}
					}
				}
			}
			return filepath.Join(directory, ".tools", "state")
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}
	return filepath.Join(working, ".tools", "state")
}
