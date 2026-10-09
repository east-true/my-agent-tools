package github

import (
	"errors"
	"strings"
	"time"
)

type CIRerunCheckpoint struct {
	Version          int       `json:"version"`
	Repo             string    `json:"repo"`
	RunID            int64     `json:"run_id"`
	PreviousAttempt  int       `json:"previous_attempt"`
	ExpectedAttempt  int       `json:"expected_attempt"`
	HeadSHA          string    `json:"head_sha"`
	Mode             string    `json:"mode"`
	RequestedAt      time.Time `json:"requested_at"`
	RequestAttempted bool      `json:"request_attempted"`
	Accepted         bool      `json:"accepted"`
	Terminal         bool      `json:"terminal"`
}

func (state CIRerunCheckpoint) Validate(repo string, runID int64) error {
	if state.Version != 1 || !strings.EqualFold(state.Repo, repo) || state.RunID != runID || state.PreviousAttempt < 1 || state.ExpectedAttempt <= state.PreviousAttempt || state.ExpectedAttempt != state.PreviousAttempt+1 || state.HeadSHA == "" || !state.RequestAttempted || state.RequestedAt.IsZero() || (state.Mode != "failed" && state.Mode != "all") {
		return errors.New("rerun checkpoint identity or attempt is invalid; preserve it and inspect GitHub before retrying")
	}
	return nil
}
