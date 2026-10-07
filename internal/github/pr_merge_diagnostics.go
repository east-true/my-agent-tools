package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// JobLogAPI downloads bounded plain-text logs using an unauthenticated client
// for GitHub's short-lived signed URL. The URL and raw log are never returned.
type JobLogAPI interface {
	JobLog(context.Context, string, int64) (string, error)
}

func (api SDK) JobLog(ctx context.Context, repo string, jobID int64) (string, error) {
	owner, name, _ := strings.Cut(repo, "/")
	location, redirectResponse, err := api.Client.Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 0)
	if err != nil {
		if redirectResponse != nil {
			return "", fmt.Errorf("job log URL unavailable (HTTP %d); check Actions read access and log retention", redirectResponse.StatusCode)
		}
		return "", errors.New("job log URL unavailable; check Actions read access and log retention")
	}
	if location == nil || location.Scheme != "https" || location.Host == "" {
		return "", errors.New("job log redirect must use HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, "GET", location.String(), nil)
	if err != nil {
		return "", errors.New("invalid job log redirect")
	}
	// Do not reuse the authenticated SDK transport for a storage host.
	caller := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 5 {
			return errors.New("invalid job log redirect")
		}
		return nil
	}}
	response, err := caller.Do(request)
	if err != nil {
		return "", errors.New("job log download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("job log download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return "", errors.New("job log read failed")
	}
	return string(data), nil
}

func actionsJobID(link string) int64 {
	u, err := url.Parse(link)
	if err != nil || u.Host != "github.com" {
		return 0
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 7 || parts[2] != "actions" || parts[3] != "runs" || parts[5] != "job" {
		return 0
	}
	id, err := strconv.ParseInt(parts[6], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

var terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func compactText(value string, limit int) string {
	value = terminalEscape.ReplaceAllString(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func logEvidence(log string) string {
	var lines []string
	for _, line := range strings.Split(log, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "##[error]") || strings.Contains(lower, "::error") || strings.Contains(lower, "--- fail:") || strings.Contains(lower, "error:") || strings.Contains(lower, "assertionerror") || strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal:") {
			lines = append(lines, compactText(line, 350))
			if len(lines) == 5 {
				break
			}
		}
	}
	return compactText(strings.Join(lines, "\n"), 1500)
}

func (client Client) diagnoseChecks(ctx context.Context, repo string, reasons []MergeReason) {
	for i := range reasons {
		r := &reasons[i]
		if r.checkID == 0 {
			continue
		}
		var check mergeCheck
		if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/check-runs/%d", repo, r.checkID), nil, &check); err != nil {
			r.DiagnosticError = compactText(err.Error(), 350)
		} else {
			r.Details = compactText(strings.Join([]string{check.Output.Title, check.Output.Summary, check.Output.Text}, "\n"), 1000)
		}
		var annotations []struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			Level     string `json:"annotation_level"`
			Message   string `json:"message"`
		}
		if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/check-runs/%d/annotations?per_page=100", repo, r.checkID), nil, &annotations); err != nil {
			r.DiagnosticError = compactText(r.DiagnosticError+"\n"+err.Error(), 500)
		} else {
			for _, annotation := range annotations {
				if annotation.Level == "failure" {
					r.Details = compactText(r.Details+fmt.Sprintf("\n%s:%d: %s", annotation.Path, annotation.StartLine, annotation.Message), 1500)
				}
			}
		}
		if r.jobID != 0 {
			var job struct {
				Steps []struct {
					Name       string `json:"name"`
					Conclusion string `json:"conclusion"`
				} `json:"steps"`
			}
			if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/jobs/%d", repo, r.jobID), nil, &job); err != nil {
				r.DiagnosticError = compactText(r.DiagnosticError+"\n"+err.Error(), 500)
			} else {
				for _, step := range job.Steps {
					if step.Conclusion == "failure" {
						r.Details = compactText(r.Details+"\nFailed step: "+step.Name, 1500)
					}
				}
			}
			if api, ok := client.API.(JobLogAPI); ok {
				log, err := api.JobLog(ctx, repo, r.jobID)
				if err != nil {
					r.DiagnosticError = compactText(r.DiagnosticError+"\n"+err.Error(), 500)
				} else if evidence := logEvidence(log); evidence != "" {
					r.Details = compactText(r.Details+"\n"+evidence, 2000)
				}
			}
		}
		if r.Details == "" {
			r.Details = "No detailed failure evidence was available; inspect the linked check."
		}
	}
}
