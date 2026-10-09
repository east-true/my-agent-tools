package github

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var commitSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

type DeltaOptions struct {
	Number       int
	Since        string
	IncludePatch bool
}

func (options DeltaOptions) Validate() error {
	if options.Number <= 0 || !commitSHA.MatchString(options.Since) {
		return errors.New("--number must be positive and --since must be a full 40-character commit SHA")
	}
	return nil
}

type DeltaFile struct {
	Filename         string  `json:"filename"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	PreviousFilename string  `json:"previous_filename,omitempty"`
	Patch            *string `json:"patch,omitempty"`
}

type DeltaResult struct {
	Status   string      `json:"status"`
	Repo     string      `json:"repo"`
	Number   int         `json:"number"`
	Since    string      `json:"since"`
	Head     string      `json:"head"`
	Complete bool        `json:"complete"`
	Files    []DeltaFile `json:"files"`
	Notes    []string    `json:"notes,omitempty"`
}

func (client Client) PullRequestDelta(ctx context.Context, repo string, options DeltaOptions) (DeltaResult, error) {
	result := DeltaResult{Status: "ok", Repo: repo, Number: options.Number, Since: options.Since, Files: []DeltaFile{}}
	if err := options.Validate(); err != nil {
		return result, err
	}
	options.Since = strings.ToLower(options.Since)
	result.Since = options.Since
	pr, err := client.mergePR(ctx, repo, options.Number)
	if err != nil {
		return result, err
	}
	result.Head = pr.HeadSHA
	if !commitSHA.MatchString(result.Head) {
		return result, errors.New("GitHub returned an invalid PR commit SHA")
	}
	if result.Head != result.Since {
		commits, total, last := 0, -1, ""
		seen := map[string]bool{}
		for page := 1; ; {
			var response struct {
				Status    string                 `json:"status"`
				Total     int                    `json:"total_commits"`
				Base      struct{ SHA string }   `json:"base_commit"`
				MergeBase struct{ SHA string }   `json:"merge_base_commit"`
				Commits   []struct{ SHA string } `json:"commits"`
				Files     []DeltaFile            `json:"files"`
			}
			next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/compare/%s...%s?per_page=100&page=%d", repo, result.Since, result.Head, page), nil, &response)
			if err != nil {
				return result, fmt.Errorf("compare PR commits: %w", err)
			}
			if response.Base.SHA != result.Since || response.MergeBase.SHA != result.Since || (response.Status != "ahead" && response.Status != "identical") {
				result.Status = "needs_refresh"
				result.Notes = append(result.Notes, "Baseline is not an ancestor of the current head; obtain a full diff or choose a new baseline.")
				return result, nil
			}
			if total < 0 {
				total = response.Total
			} else if total != response.Total {
				return result, errors.New("compare commit count changed during pagination")
			}
			commits += len(response.Commits)
			for _, commit := range response.Commits {
				if !commitSHA.MatchString(commit.SHA) || seen[commit.SHA] {
					return result, errors.New("compare response contains an invalid or repeated commit SHA")
				}
				seen[commit.SHA] = true
			}
			if len(response.Commits) > 0 {
				last = response.Commits[len(response.Commits)-1].SHA
			}
			if page == 1 {
				result.Files = response.Files
				if result.Files == nil {
					result.Files = []DeltaFile{}
				}
				for i := range result.Files {
					if result.Files[i].Filename == "" || result.Files[i].Status == "" || result.Files[i].Additions < 0 || result.Files[i].Deletions < 0 {
						return result, errors.New("compare response contains incomplete file metadata")
					}
					if !options.IncludePatch {
						result.Files[i].Patch = nil
					}
				}
			}
			if next == 0 {
				break
			}
			if next <= page {
				return result, errors.New("compare pagination did not advance")
			}
			page = next
		}
		if commits != total || last != result.Head {
			return result, errors.New("compare response is incomplete or does not match the current head")
		}
		if len(result.Files) >= 300 {
			result.Status = "partial"
			result.Notes = append(result.Notes, "GitHub compare may truncate files at 300; use a complete local Git diff.")
		}
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Filename < result.Files[j].Filename })
	current, err := client.mergePR(ctx, repo, options.Number)
	if err != nil {
		return result, err
	}
	if current.HeadSHA != result.Head {
		result.Status = "partial"
		result.Notes = append(result.Notes, "PR head changed during comparison; collect again for the new commit.")
	}
	result.Complete = result.Status == "ok"
	return result, nil
}
