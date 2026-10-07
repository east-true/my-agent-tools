package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type ciCLIAPI struct {
	logError error
	success  bool
}

func (api ciCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	if method != "GET" || payload != nil {
		return 0, errors.New("unexpected write")
	}
	var data string
	switch {
	case endpoint == "repos/owner/repo/actions/runs/42":
		conclusion := "failure"
		if api.success {
			conclusion = "success"
		}
		data = `{"id":42,"run_attempt":1,"head_sha":"abc","status":"completed","conclusion":"` + conclusion + `"}`
	case strings.HasPrefix(endpoint, "repos/owner/repo/actions/runs/42/attempts/1/jobs?"):
		data = `{"jobs":[{"id":11,"name":"test","status":"completed","conclusion":"failure","steps":[{"number":2,"name":"build","conclusion":"failure"}]}]}`
		if api.success {
			data = `{"jobs":[]}`
		}
	default:
		return 0, errors.New("unexpected endpoint: " + endpoint)
	}
	return 0, json.Unmarshal([]byte(data), target)
}

func (api ciCLIAPI) CIJobLog(context.Context, string, int64, int64) (string, bool, error) {
	return "Error: unexpected runner failure\nFull context must remain", false, api.logError
}

func TestCIFailuresHelpAndInvalidInputDoNotAuthenticate(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 0}, {[]string{"--help"}, 0}, {[]string{"-h"}, 0},
		{[]string{"failures", "--help"}, 0}, {[]string{"failures", "-h"}, 0},
		{[]string{"rerun"}, 2}, {[]string{"failures"}, 2},
		{[]string{"failures", "--run", "-1", "--json"}, 2},
		{[]string{"failures", "--run", "42", "extra"}, 2},
		{[]string{"failures", "--run", "42", "--max-log-bytes", "0"}, 2},
		{[]string{"failures", "--run", "42", "--max-log-bytes", "134217729"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := runCIFailures(context.Background(), tc.args, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
				t.Fatal("help or invalid input attempted authentication")
				return nil, nil
			})
			if code != tc.code {
				t.Fatalf("code=%d want %d out=%s stderr=%s", code, tc.code, out.String(), stderr.String())
			}
		})
	}
}

func TestCIFailuresCLIOutputsAndPartialExit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		api    ciCLIAPI
		json   bool
		code   int
		status string
	}{
		{"json evidence", ciCLIAPI{}, true, 0, "ok"},
		{"text evidence", ciCLIAPI{}, false, 0, "ok"},
		{"partial", ciCLIAPI{logError: errors.New("logs expired")}, true, 1, "partial"},
		{"successful run", ciCLIAPI{success: true}, true, 0, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			args := []string{"failures", "--run", "42", "--repo", "owner/repo"}
			if tc.json {
				args = append(args, "--json")
			}
			code := runCIFailures(context.Background(), args, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return tc.api, nil })
			if code != tc.code || stderr.Len() != 0 {
				t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
			}
			if tc.json {
				var result github.CIFailureResult
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != tc.status {
					t.Fatalf("invalid JSON result: %s %v", out.String(), err)
				}
				if tc.api.success && (!strings.Contains(out.String(), `"failed_jobs":[]`) || !strings.Contains(out.String(), `"evidence":[]`)) {
					t.Fatalf("empty results are not arrays: %s", out.String())
				}
			} else if !strings.Contains(out.String(), "Full context must remain") || !strings.Contains(out.String(), "step 2 build") {
				t.Fatalf("missing text evidence: %s", out.String())
			}
		})
	}
}

func TestCIFailuresCLIRequiresRunIDRatherThanPRNumber(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runCIFailures(context.Background(), []string{"failures", "--run", "42", "--repo", "owner/repo", "--json"}, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return nil, errors.New("no credentials") })
	if code != 1 || !strings.Contains(out.String(), `"status":"error"`) || !strings.Contains(out.String(), "no credentials") {
		t.Fatalf("authentication error: %d %s %s", code, out.String(), stderr.String())
	}
}

func TestCIFailuresIsConnectedToMainRouter(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"github", "ci", "failures", "--run", "42", "--repo", "owner/repo", "--json"}, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return ciCLIAPI{}, nil })
	if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), `"failed_jobs"`) || !strings.Contains(out.String(), "Full context must remain") {
		t.Fatalf("router failed: %d %s %s", code, out.String(), stderr.String())
	}
}
