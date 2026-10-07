package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
	sdk "github.com/google/go-github/v92/github"
)

type fakeRunner struct{}

func (fakeRunner) Run(context.Context, []byte, string, ...string) ([]byte, error) {
	return nil, errors.New("no external commands available")
}

type fakeAPI struct {
	writes        int
	partial       bool
	branchFailure bool
}

func (api *fakeAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	data := "{}"
	if endpoint == "graphql" {
		query := payload.(map[string]any)["query"].(string)
		if strings.HasPrefix(query, "query RepositoryTemplates") {
			return 0, json.Unmarshal([]byte(`{"data":{"repository":{"templates":[]}}}`), target)
		}
		api.writes++
		data = `{"data":{"createLinkedBranch":{"linkedBranch":{"ref":{"name":"1-feat-add-cli"}}}}}`
		if api.branchFailure {
			data = `{"errors":[{"message":"branch creation denied"}]}`
		}
		return 0, json.Unmarshal([]byte(data), target)
	}
	if method != "GET" {
		api.writes++
		data = `{"number":1,"node_id":"I_1","html_url":"https://github.com/owner/repo/issues/1","labels":[{"name":"enhancement"}],"assignees":[{"login":"tester"}],"type":{"name":"Feature"}}`
		if api.partial {
			data = `{"number":1,"html_url":"https://github.com/owner/repo/issues/1"}`
		}
	} else {
		switch {
		case endpoint == "repos/owner/repo":
			data = `{"full_name":"owner/repo","default_branch":"main","permissions":{"push":true}}`
		case strings.Contains(endpoint, "/labels?"):
			data = `[{"name":"enhancement"}]`
		case strings.HasSuffix(endpoint, "/issue-types"):
			data = `[{"name":"Feature"}]`
		case endpoint == "user":
			data = `{"login":"tester"}`
		case strings.HasSuffix(endpoint, "/git/ref/heads/main"):
			data = `{"object":{"sha":"abc123"}}`
		case strings.Contains(endpoint, "/git/ref/heads/"):
			return 0, &sdk.ErrorResponse{Response: &http.Response{StatusCode: 404}, Message: "Not Found"}
		}
	}
	return 0, json.Unmarshal([]byte(data), target)
}

func invoke(t *testing.T, api *fakeAPI, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(input), &out, &stderr, fakeRunner{},
		func(context.Context, command.Runner) (github.API, error) { return api, nil })
	return code, out.String(), stderr.String()
}

func TestHelpNeedsNoAuthentication(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--help"}, {"-h"},
		{"github"}, {"github", "--help"}, {"github", "-h"},
		{"github", "setup", "--help"}, {"github", "setup", "-h"},
		{"github", "issue"}, {"github", "issue", "--help"}, {"github", "issue", "-h"},
		{"github", "pr"}, {"github", "pr", "--help"}, {"github", "pr", "-h"},
		{"github", "issue", "create", "--help"}, {"github", "issue", "create", "-h"},
		{"github", "pr", "create", "--help"}, {"github", "pr", "create", "-h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{},
				func(context.Context, command.Runner) (github.API, error) {
					t.Fatal("help attempted authentication")
					return nil, errors.New("unavailable")
				})
			output := out.String() + stderr.String()
			if code != 0 {
				t.Fatalf("code=%d help=%s", code, output)
			}
			for _, prefix := range []string{"feat", "fix", "refactor", "docs", "ci", "test", "chore", "build", "perf", "style", "revert"} {
				if !strings.Contains(output, prefix) {
					t.Errorf("help missing prefix %q: %s", prefix, output)
				}
			}
			if len(args) >= 3 && args[1] == "pr" && args[2] == "create" && !strings.Contains(output, "verification") {
				t.Errorf("PR help missing verification fields: %s", output)
			}
			if len(args) == 2 || (len(args) == 3 && args[2] != "create") {
				if args[1] == "issue" && !strings.Contains(output, "issue branch --number") {
					t.Errorf("issue help missing branch command: %s", output)
				}
			}
		})
	}
}

func TestDryRunViaStdinDoesNotWrite(t *testing.T) {
	api := &fakeAPI{}
	code, out, stderr := invoke(t, api, `{"title":"feat: add cli","summary":"기능 추가"}`, "github", "issue", "create", "--repo", "owner/repo", "--file", "-", "--dry-run")
	if code != 0 || api.writes != 0 || stderr != "" {
		t.Fatalf("code=%d writes=%d output=%s error=%s", code, api.writes, out, stderr)
	}
	var result struct {
		Status string      `json:"status"`
		Plan   github.Plan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Status != "planned" || result.Plan.Payload["type"] != "Feature" {
		t.Fatalf("invalid JSON plan: %s error=%v", out, err)
	}
}

func TestCreationAndPartialJSONOutput(t *testing.T) {
	for _, partial := range []bool{false, true} {
		api := &fakeAPI{partial: partial}
		code, out, stderr := invoke(t, api, `{"title":"feat: add cli","body":"내용"}`, "github", "issue", "create", "--repo", "owner/repo", "--file", "-", "--json")
		var result github.Result
		wantWrites := 2
		if partial {
			wantWrites = 1
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || result.Number != 1 || api.writes != wantWrites || stderr != "" {
			t.Fatalf("bad create result: code=%d out=%s stderr=%s err=%v", code, out, stderr, err)
		}
		if partial && (code != 1 || result.Status != "partial" || result.Error == "") {
			t.Fatalf("partial should retain URL and error: %s", out)
		}
		if !partial && (code != 0 || result.Status != "created") {
			t.Fatalf("successful creation failed: %s", out)
		}
	}
}

func TestPrefixTitleAndBodyFileDriveWholeWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("기능 추가"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	code, out, stderr := invoke(t, api, "", "github", "issue", "create", "--repo", "owner/repo", "--prefix", "feat", "--title", "Add CLI!", "--body-file", path, "--json")
	var result github.Result
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if code != 0 || api.writes != 2 || result.BranchInfo == nil || !result.BranchInfo.Linked || result.Selection.IssueType != "Feature" || stderr != "" {
		t.Fatalf("code=%d writes=%d out=%s err=%s", code, api.writes, out, stderr)
	}
}

func TestBranchFailureRetainsIssueAndReportsPartial(t *testing.T) {
	api := &fakeAPI{branchFailure: true}
	code, out, _ := invoke(t, api, `{"prefix":"feat","title":"add cli","summary":"기능 추가"}`, "github", "issue", "create", "--repo", "owner/repo", "--file", "-", "--json")
	var result github.Result
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if code != 1 || result.Status != "partial" || result.URL == "" || !strings.Contains(result.Error, "branch creation denied") || api.writes != 2 {
		t.Fatalf("code=%d writes=%d result=%+v", code, api.writes, result)
	}
}

func TestInvalidInputHasUsageExitAndNoMutation(t *testing.T) {
	api := &fakeAPI{}
	code, out, _ := invoke(t, api, `{"titel":"feat: typo"}`, "github", "issue", "create", "--repo", "owner/repo", "--file", "-", "--json")
	if code != 2 || api.writes != 0 || !strings.Contains(out, "unknown field") {
		t.Fatalf("code=%d output=%s writes=%d", code, out, api.writes)
	}
}

func TestInvalidTitleIsRejectedBeforeAuthentication(t *testing.T) {
	for _, kind := range []string{"issue", "pr"} {
		for _, title := range []string{"로그인 추가", "add 로그인"} {
			t.Run(kind+"/"+title, func(t *testing.T) {
				input, err := json.Marshal(github.Spec{Prefix: "feat", Title: title, Body: "기능 추가"})
				if err != nil {
					t.Fatal(err)
				}
				var out, stderr bytes.Buffer
				code := run(context.Background(), []string{"github", kind, "create", "--repo", "owner/repo", "--file", "-", "--json"}, strings.NewReader(string(input)), &out, &stderr, fakeRunner{},
					func(context.Context, command.Runner) (github.API, error) {
						t.Fatal("invalid title attempted authentication or remote access")
						return nil, nil
					})
				if code != 2 || !strings.Contains(out.String(), "English") {
					t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
				}
			})
		}
	}
}

func TestExplicitConfigSupportsOtherProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(path, []byte(`{"github":{"body_language":"any","label_map":{"feat":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	code, out, stderr := invoke(t, api, `{"title":"feat: add cli","summary":"Add a command."}`, "github", "issue", "create", "--repo", "owner/repo", "--file", "-", "--dry-run", "--config", path)
	if code != 0 || !strings.Contains(out, "## Summary") || !strings.Contains(out, `"labels":[]`) {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
	}
}
