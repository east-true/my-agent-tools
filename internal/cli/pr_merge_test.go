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

type mergeCLIAPI struct {
	calls    int
	writes   int
	failure  bool
	apiError bool
}

func (api *mergeCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	api.calls++
	if api.apiError {
		return 0, errors.New("permission denied")
	}
	data := `[]`
	switch {
	case endpoint == "graphql":
		query := payload.(map[string]any)["query"].(string)
		if !strings.HasPrefix(query, "query MergePullRequest") {
			return 0, errors.New("unexpected query")
		}
		data = `{"data":{"repository":{"pullRequest":{"url":"https://github.com/owner/repo/pull/7","state":"OPEN","headRefOid":"head1","baseRefOid":"base1","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","reviewDecision":"APPROVED"}}}}`
	case method == "PUT":
		api.writes++
		data = `{"status":"merged","details":{"sha":"merged1"}}`
	case strings.Contains(endpoint, "/check-runs?"):
		data = `{"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"success"}]}`
	case strings.Contains(endpoint, "/statuses?"):
		if api.failure {
			data = `[{"id":1,"context":"external-ci","state":"failure","description":"login test failed"}]`
		}
	default:
		return 0, errors.New("unexpected endpoint: " + endpoint)
	}
	return 0, json.Unmarshal([]byte(data), target)
}

func invokeMergeCLI(t *testing.T, api *mergeCLIAPI, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"github", "pr", "merge"}, args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	return code, out.String(), stderr.String()
}

func TestMergeHelpAndValidationBeforeAuthentication(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0}, {[]string{"-h"}, 0}, {nil, 2},
		{[]string{"--number", "-1"}, 2}, {[]string{"--number", "7", "--timeout", "0s"}, 2},
		{[]string{"--number", "7", "--timeout", "-1m"}, 2}, {[]string{"--number", "7", "--interval", "0s"}, 2},
		{[]string{"--number", "7", "--method", "fast"}, 2}, {[]string{"--number", "7", "unexpected"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := run(context.Background(), append([]string{"github", "pr", "merge"}, tc.args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
				t.Fatal("invalid input or help authenticated")
				return nil, errors.New("unavailable")
			})
			if code != tc.code {
				t.Fatalf("code=%d want=%d out=%s stderr=%s", code, tc.code, out.String(), stderr.String())
			}
		})
	}
}

func TestMergeCLIEmitsExactlyOneFinalJSON(t *testing.T) {
	for _, tc := range []struct {
		name, status      string
		failure, apiError bool
		code              int
		writes            int
	}{
		{"merged", "merged", false, false, 0, 1}, {"failure", "blocked", true, false, 1, 0}, {"API error", "error", false, true, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &mergeCLIAPI{failure: tc.failure, apiError: tc.apiError}
			code, out, stderr := invokeMergeCLI(t, api, "--number", "7", "--repo", "owner/repo", "--json")
			var result github.MergeResult
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("output contains intermediate or invalid JSON: %s %v", out, err)
			}
			if code != tc.code || result.Status != tc.status || stderr != "" || api.writes != tc.writes {
				t.Fatalf("code=%d result=%+v stderr=%s writes=%d", code, result, stderr, api.writes)
			}
			if tc.failure && (len(result.Reasons) != 1 || result.Reasons[0].Summary != "login test failed") {
				t.Fatalf("missing failure summary: %+v", result)
			}
		})
	}
}

func TestMergeCLITextAndMethod(t *testing.T) {
	for _, failure := range []bool{false, true} {
		api := &mergeCLIAPI{failure: failure}
		code, out, stderr := invokeMergeCLI(t, api, "--number", "7", "--repo", "owner/repo", "--method", "rebase")
		want := 0
		if failure {
			want = 1
		}
		if code != want || stderr != "" || !strings.Contains(out, "https://github.com/owner/repo/pull/7") {
			t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
		}
		if failure && (!strings.Contains(out, "login test failed") || !strings.Contains(out, "next:")) {
			t.Fatalf("missing diagnosis: %s", out)
		}
	}
}
