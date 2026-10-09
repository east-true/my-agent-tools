package cli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/east-true/my-agent-tools/internal/github"
)

func defaultRerunState(api github.API, repo string, runID int64) string {
	provider, ok := api.(interface{ StateDirectory() string })
	if !ok || provider.StateDirectory() == "" {
		return ""
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(repo))))
	return filepath.Join(provider.StateDirectory(), "ci-rerun", digest[:16], fmt.Sprintf("%d.json", runID))
}

func readRerunCheckpoint(path, repo string, runID int64) (*github.CIRerunCheckpoint, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state github.CIRerunCheckpoint
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid rerun state file: %w", err)
	}
	if err := state.Validate(repo, runID); err != nil {
		return nil, err
	}
	return &state, nil
}
