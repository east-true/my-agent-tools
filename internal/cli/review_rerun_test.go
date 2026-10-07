package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type reviewRerunCLIAPI struct {
	writes  int
	failure bool
	partial bool
	err     bool
	all     bool
}

func (api *reviewRerunCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	if api.err {
		return 0, errors.New("permission denied")
	}
	if method == "POST" && endpoint != "graphql" {
		api.writes++
		if strings.HasSuffix(endpoint, "/rerun") {
			api.all = true
		} else if !strings.HasSuffix(endpoint, "/rerun-failed-jobs") {
			return 0, errors.New("unexpected mutation: " + endpoint)
		}
		return 0, nil
	}
	var data string
	switch {
	case endpoint == "graphql":
		query := payload.(map[string]any)["query"].(string)
		if !strings.HasPrefix(query, "query PullRequestReviews") {
			return 0, errors.New("unexpected query")
		}
		data = `{"data":{"repository":{"pullRequest":{"url":"https://github.com/owner/repo/pull/7","head_sha":"abc","review_decision":"CHANGES_REQUESTED","reviewThreads":{"nodes":[{"id":"T1","path":"main.go","line":12,"original_line":12,"diff_side":"RIGHT","is_outdated":true,"comments":{"nodes":[{"id":"C1","body":"원문 요청 사항","author":null}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`
	case strings.Contains(endpoint, "/pulls/7/reviews?"):
		if api.partial {
			return 0, errors.New("review history unavailable")
		}
		data = `[{"id":1,"state":"CHANGES_REQUESTED","body":"요청 사항","user":{"login":"reviewer"}}]`
	case endpoint == "repos/owner/repo/pulls/7":
		data = `{"head":{"sha":"abc"}}`
	case endpoint == "repos/owner/repo/actions/runs/42":
		attempt, conclusion := 1, "failure"
		if api.writes > 0 {
			attempt = 2
			if !api.failure {
				conclusion = "success"
			}
		}
		data = fmt.Sprintf(`{"id":42,"run_attempt":%d,"head_sha":"abc","status":"completed","conclusion":%q}`, attempt, conclusion)
	case strings.Contains(endpoint, "/actions/runs/42/attempts/2/jobs?"):
		data = `{"jobs":[{"id":11,"name":"test","status":"completed","conclusion":"failure","steps":[]}]}`
	default:
		return 0, errors.New("unexpected endpoint: " + endpoint)
	}
	return 0, json.Unmarshal([]byte(data), target)
}

func (api *reviewRerunCLIAPI) CIJobLog(context.Context, string, int64, int64) (string, bool, error) {
	if api.partial {
		return "", false, errors.New("logs expired")
	}
	return "Error: retained failure evidence", false, nil
}

func TestReviewAndRerunHelpAndValidationBeforeAuthentication(t *testing.T) {
	for _, prefix := range [][]string{{"github", "pr", "reviews"}, {"github", "ci", "rerun"}} {
		type validationCase struct {
			args []string
			code int
		}
		cases := []validationCase{{[]string{"--help"}, 0}, {[]string{"-h"}, 0}, {nil, 2}, {[]string{"--json"}, 2}, {[]string{"--unexpected"}, 2}}
		if prefix[1] == "pr" {
			cases = append(cases, validationCase{[]string{"--number", "-1", "--json"}, 2}, validationCase{[]string{"--number", "7", "extra"}, 2})
		} else {
			for _, args := range [][]string{{"--run", "-1"}, {"--run", "42", "extra"}, {"--run", "42", "--timeout", "0s"}, {"--run", "42", "--interval", "-1s"}, {"--run", "42", "--max-log-bytes", "0"}, {"--run", "42", "--max-log-bytes", "134217729"}} {
				cases = append(cases, validationCase{args, 2})
			}
		}
		for _, tc := range cases {
			t.Run(strings.Join(append(append([]string{}, prefix...), tc.args...), " "), func(t *testing.T) {
				var out, stderr bytes.Buffer
				code := run(context.Background(), append(append([]string{}, prefix...), tc.args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
					t.Fatal("help or invalid input authenticated")
					return nil, nil
				})
				if code != tc.code {
					t.Fatalf("code=%d want=%d out=%s stderr=%s", code, tc.code, out.String(), stderr.String())
				}
			})
		}
	}
}

func TestReviewAndRerunRouterOutputsAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		args         []string
		api          reviewRerunCLIAPI
		code, writes int
	}{
		{"reviews", "ok", []string{"pr", "reviews", "--number", "7"}, reviewRerunCLIAPI{}, 0, 0},
		{"reviews partial", "partial", []string{"pr", "reviews", "--number", "7", "--all"}, reviewRerunCLIAPI{partial: true}, 1, 0},
		{"reviews error", "error", []string{"pr", "reviews", "--number", "7"}, reviewRerunCLIAPI{err: true}, 1, 0},
		{"rerun completed", "completed", []string{"ci", "rerun", "--run", "42"}, reviewRerunCLIAPI{}, 0, 1},
		{"rerun all no wait", "requested", []string{"ci", "rerun", "--run", "42", "--all", "--wait=false"}, reviewRerunCLIAPI{}, 0, 1},
		{"rerun dry run", "planned", []string{"ci", "rerun", "--run", "42", "--dry-run"}, reviewRerunCLIAPI{}, 0, 0},
		{"rerun failure", "failed", []string{"ci", "rerun", "--run", "42"}, reviewRerunCLIAPI{failure: true}, 1, 1},
		{"rerun evidence partial", "failed", []string{"ci", "rerun", "--run", "42", "--annotations=false"}, reviewRerunCLIAPI{failure: true, partial: true}, 1, 1},
		{"rerun error", "error", []string{"ci", "rerun", "--run", "42"}, reviewRerunCLIAPI{err: true}, 1, 0},
	} {
		for _, jsonOutput := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s json=%t", tc.name, jsonOutput), func(t *testing.T) {
				api := tc.api
				var out, stderr bytes.Buffer
				args := append([]string{"github"}, tc.args...)
				args = append(args, "--repo", "owner/repo")
				if jsonOutput {
					args = append(args, "--json")
				}
				code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return &api, nil })
				if code != tc.code || api.writes != tc.writes || (!strings.Contains(out.String(), tc.status) && !strings.Contains(stderr.String(), "permission denied")) {
					t.Fatalf("code=%d out=%s stderr=%s writes=%d", code, out.String(), stderr.String(), api.writes)
				}
				if jsonOutput {
					decoder := json.NewDecoder(&out)
					var result map[string]any
					if err := decoder.Decode(&result); err != nil || result["status"] != tc.status {
						t.Fatalf("invalid result=%+v err=%v", result, err)
					}
					if err := decoder.Decode(&result); err != io.EOF {
						t.Fatalf("output is not exactly one JSON result: %v", err)
					}
					if tc.name == "rerun failure" && !strings.Contains(fmt.Sprint(result["failure"]), "retained failure evidence") {
						t.Fatal("rerun lost failure evidence")
					}
					if tc.name == "rerun evidence partial" && result["failure"].(map[string]any)["complete"] != false {
						t.Fatal("partial evidence reported as complete")
					}
				} else if tc.name == "reviews" && (!strings.Contains(out.String(), "원문 요청 사항") || !strings.Contains(out.String(), "main.go:12") || !strings.Contains(out.String(), "@unknown")) {
					t.Fatalf("missing review text: %s", out.String())
				}
				if tc.name == "rerun all no wait" && !api.all {
					t.Fatal("--all did not select whole-workflow rerun")
				}
			})
		}
	}
}

func TestReviewAndRerunAuthenticationFailure(t *testing.T) {
	for _, args := range [][]string{{"github", "pr", "reviews", "--number", "7"}, {"github", "ci", "rerun", "--run", "42"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), append(args, "--repo", "owner/repo", "--json"), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return nil, errors.New("no credentials") })
		if code != 1 || !strings.Contains(out.String(), `"status":"error"`) || !strings.Contains(out.String(), "no credentials") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
		}
	}
}
