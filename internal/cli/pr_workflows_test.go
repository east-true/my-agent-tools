package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type workflowCLIAPI struct {
	body    string
	partial bool
	writes  int
}

func (api *workflowCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	if endpoint != "graphql" && method != "GET" {
		api.writes++
		return 0, errors.New("unexpected mutation")
	}
	head := strings.Repeat("b", 40)
	var data any
	switch {
	case endpoint == "graphql":
		query := payload.(map[string]any)["query"].(string)
		if strings.HasPrefix(query, "query PullRequestReviews") {
			if api.partial {
				data = map[string]any{"errors": []map[string]any{{"message": "reviews unavailable"}}}
				break
			}
			data = map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"url": "https://github.com/owner/repo/pull/7", "head_sha": head, "review_decision": "APPROVED", "reviewThreads": map[string]any{"nodes": []map[string]any{{"id": "T1", "path": "main.go", "line": 12, "comments": map[string]any{"nodes": []map[string]any{{"id": "C1", "body": api.body, "diff_hunk": strings.Repeat("+context\n", 500)}}, "pageInfo": map[string]any{"hasNextPage": false}}}}, "pageInfo": map[string]any{"hasNextPage": false}}}}}}
		} else {
			data = map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"url": "https://github.com/owner/repo/pull/7", "state": "OPEN", "isDraft": false, "merged": false, "headRefOid": head, "baseRefOid": "base", "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "reviewDecision": "APPROVED"}}}}
		}
	case strings.Contains(endpoint, "/check-runs?"):
		data = map[string]any{"check_runs": []map[string]any{{"id": 1, "name": "test", "status": "completed", "conclusion": "success"}}}
	case strings.Contains(endpoint, "/statuses?") || strings.Contains(endpoint, "/reviews?"):
		data = []any{}
	case endpoint == "repos/owner/repo/pulls/7":
		data = map[string]any{"head": map[string]any{"sha": head}}
	case strings.Contains(endpoint, "/compare/"):
		data = map[string]any{"status": "ahead", "base_commit": map[string]any{"sha": strings.Repeat("a", 40)}, "merge_base_commit": map[string]any{"sha": strings.Repeat("a", 40)}, "commits": []map[string]any{{"sha": head}}, "total_commits": 1, "files": []map[string]any{{"filename": "main.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "+example"}}}
	default:
		return 0, fmt.Errorf("unexpected endpoint %s", endpoint)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	return 0, json.Unmarshal(encoded, target)
}

func invokeWorkflowCLI(t *testing.T, api github.API, args ...string) (int, []byte, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"github", "pr"}, args...), strings.NewReader("본문"), &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	return code, out.Bytes(), stderr.String()
}

func TestWorkflowHelpAndInputValidationNeverAuthenticate(t *testing.T) {
	for _, action := range []string{"inspect", "delta", "submit"} {
		for _, args := range [][]string{{"--help"}, {"-h"}, nil, {"--number", "-1"}, {"--unknown"}, {"--compact", "--artifact-dir", ""}} {
			t.Run(action+strings.Join(args, " "), func(t *testing.T) {
				var out, stderr bytes.Buffer
				code := run(context.Background(), append([]string{"github", "pr", action}, args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
					t.Fatal("help/invalid input authenticated")
					return nil, nil
				})
				want := 2
				if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
					want = 0
				}
				if code != want {
					t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
				}
			})
		}
	}
}

func TestInspectCLIUpdatesStateOnlyAfterCompleteDeliveredOutput(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	api := &workflowCLIAPI{body: "original request"}
	args := []string{"inspect", "--number", "7", "--repo", "owner/repo", "--state-file", state, "--artifact-dir", filepath.Join(dir, "evidence"), "--json"}
	code, data, stderr := invokeWorkflowCLI(t, api, args...)
	if code != 0 || stderr != "" || !bytes.Contains(data, []byte("original request")) || !bytes.Contains(data, []byte(`"diff_hunk"`)) {
		t.Fatalf("first result: code=%d data=%s stderr=%s", code, data, stderr)
	}
	before, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	code, data, stderr = invokeWorkflowCLI(t, api, args...)
	if code != 0 || !bytes.Contains(data, []byte(`"status":"unchanged"`)) || !bytes.Contains(data, []byte("original request")) || !bytes.Contains(data, []byte(`"outstanding"`)) || !bytes.Contains(data, []byte(`"attention_required":true`)) {
		t.Fatalf("repeated result: code=%d data=%s stderr=%s", code, data, stderr)
	}
	api.partial = true
	code, data, stderr = invokeWorkflowCLI(t, api, args...)
	retained, _ := os.ReadFile(state)
	if code != 1 || !bytes.Contains(data, []byte(`"status":"partial"`)) || !bytes.Equal(before, retained) {
		t.Fatalf("partial overwrote state: code=%d data=%s stderr=%s", code, data, stderr)
	}
	api.partial = false
	api.body = "changed request"
	var failures bytes.Buffer
	code = run(context.Background(), append([]string{"github", "pr"}, args...), nil, brokenWorkflowWriter{}, &failures, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	retained, _ = os.ReadFile(state)
	if code != 1 || !bytes.Equal(before, retained) {
		t.Fatal("failed output advanced the observation cursor")
	}
	code, data, stderr = invokeWorkflowCLI(t, api, args...)
	if code != 0 || !bytes.Contains(data, []byte(`"status":"changed"`)) || !bytes.Contains(data, []byte("changed request")) || bytes.Contains(data, []byte("original request")) {
		t.Fatalf("changed result: code=%d data=%s stderr=%s", code, data, stderr)
	}
	if api.writes != 0 {
		t.Fatal("read-only inspection mutated GitHub")
	}
	code, data, _ = invokeWorkflowCLI(t, api, append(args, "--full")...)
	if code != 0 || !bytes.Contains(data, []byte(`"threads"`)) {
		t.Fatalf("full snapshot missing: %s", data)
	}
}

type brokenWorkflowWriter struct{}

func (brokenWorkflowWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDeltaCLIConnectsAndDefaultsToMetadata(t *testing.T) {
	api := &workflowCLIAPI{}
	args := []string{"delta", "--number", "7", "--repo", "owner/repo", "--since", strings.Repeat("a", 40), "--json"}
	code, data, stderr := invokeWorkflowCLI(t, api, args...)
	if code != 0 || stderr != "" || bytes.Contains(data, []byte(`"patch"`)) || !bytes.Contains(data, []byte(`"complete":true`)) {
		t.Fatalf("delta: code=%d data=%s stderr=%s", code, data, stderr)
	}
	code, data, _ = invokeWorkflowCLI(t, api, append(args, "--include-patch")...)
	if code != 0 || !bytes.Contains(data, []byte(`"patch":"+example"`)) {
		t.Fatalf("explicit patch missing: %s", data)
	}
}

func TestSubmitSpecPrefixAndInputSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pr.json")
	if err := os.WriteFile(path, []byte(`{"prefix":"fix","title":"improve workflow","body":"수정 내용"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := github.Spec{Prefix: "fix"}
	if err := readSubmitSpec(path, "", nil, &spec); err != nil || spec.Body != "수정 내용" {
		t.Fatalf("spec=%+v err=%v", spec, err)
	}
	for _, input := range []struct {
		file, body string
		spec       github.Spec
	}{{path, "body.md", github.Spec{}}, {path, "", github.Spec{Prefix: "feat"}}, {"", "", github.Spec{Title: "title"}}} {
		if err := readSubmitSpec(input.file, input.body, nil, &input.spec); err == nil {
			t.Fatal("accepted conflicting or missing input")
		}
	}
}

type submitDryRunCLI struct{ root string }

func (runner submitDryRunCLI) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	if name != "git" {
		return nil, errors.New("unexpected command")
	}
	switch args[0] {
	case "remote":
		return []byte("https://github.com/owner/repo.git"), nil
	case "symbolic-ref":
		return []byte("fix/workflows"), nil
	case "status":
		return nil, nil
	case "rev-parse":
		if len(args) > 1 && args[1] == "--show-toplevel" {
			return []byte(runner.root), nil
		}
		return []byte(strings.Repeat("a", 40)), nil
	}
	return nil, errors.New("unexpected git operation")
}

func TestSubmitDryRunNeedsNoAuthenticationOrGitHubCalls(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"github", "pr", "submit", "--repo", "owner/repo", "--file", "-", "--dry-run"}, strings.NewReader(`{"prefix":"fix","title":"improve workflows","body":"수정 내용"}`), &out, &stderr, submitDryRunCLI{root: t.TempDir()}, func(context.Context, command.Runner) (github.API, error) {
		t.Fatal("local dry-run attempted authentication")
		return nil, nil
	})
	if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), `"status":"planned"`) || !strings.Contains(out.String(), `"push_attempted":false`) {
		t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
	}
}
