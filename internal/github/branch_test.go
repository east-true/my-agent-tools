package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueNumberAndSanitizedTitleCreateLinkedBranch(t *testing.T) {
	branch := "17-fix-handle-api-login-42"
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			var payload struct {
				Query     string `json:"query"`
				Variables struct {
					Input struct {
						Name    string `json:"name"`
						OID     string `json:"oid"`
						IssueID string `json:"issueId"`
					} `json:"input"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if strings.HasPrefix(payload.Query, "query RepositoryTemplates") {
				fmt.Fprint(w, `{"data":{"repository":{"templates":[]}}}`)
				return
			}
			input := payload.Variables.Input
			if input.Name != branch || input.OID != "base-sha" || input.IssueID != "I_17" {
				t.Errorf("wrong linked branch input: %+v", input)
			}
			fmt.Fprintf(w, `{"data":{"createLinkedBranch":{"linkedBranch":{"ref":{"name":%q}}}}}`, branch)
			return
		}
		if commonResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/heads/main":
			fmt.Fprint(w, `{"object":{"sha":"base-sha"}}`)
		case "/repos/owner/repo/issues":
			fmt.Fprint(w, `{"number":17,"node_id":"I_17","html_url":"https://github.com/owner/repo/issues/17","labels":[{"name":"bug"}],"assignees":[{"login":"tester"}],"type":{"name":"Bug"}}`)
		case "/repos/owner/repo/git/ref/heads/" + branch:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "issue", Spec{Prefix: "fix", Title: "Handle API / login! (#42)", Body: "인증 오류 수정"}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Payload["title"] != "fix: Handle API / login! (#42)" || plan.Selection.IssueType != "Bug" {
		t.Fatalf("wrong plan: %+v", plan)
	}
	if err := f.client.PrepareBranch(context.Background(), &plan, false); err != nil {
		t.Fatal(err)
	}
	if len(f.writes()) != 0 {
		t.Fatal("planning wrote to GitHub")
	}
	result, err := f.client.Create(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.client.CompleteBranch(context.Background(), plan, &result); err != nil {
		t.Fatal(err)
	}
	if result.Branch != branch || result.BranchInfo == nil || !result.BranchInfo.Linked || result.BranchInfo.CheckedOut || len(f.writes()) != 2 {
		t.Fatalf("bad result: %+v writes=%v", result, f.writes())
	}
}

func TestExistingLinkedBranchCanBeResumedWithoutRecreatingIssue(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"node":{"linkedBranches":{"nodes":[{"ref":{"name":"17-fix-login","repository":{"nameWithOwner":"owner/repo"}}}],"pageInfo":{"hasNextPage":false}}}}}`)
			return
		}
		if commonResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/issues/17":
			fmt.Fprint(w, `{"number":17,"node_id":"I_17","title":"fix: login","html_url":"https://github.com/owner/repo/issues/17"}`)
		case "/repos/owner/repo/git/ref/heads/main":
			fmt.Fprint(w, `{"object":{"sha":"base-sha"}}`)
		case "/repos/owner/repo/git/ref/heads/17-fix-login":
			fmt.Fprint(w, `{"ref":"refs/heads/17-fix-login"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	})
	plan, result, err := f.client.ExistingIssuePlan(context.Background(), "owner/repo", 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.client.PrepareBranch(context.Background(), &plan, false); err != nil {
		t.Fatal(err)
	}
	if err := f.client.CompleteBranch(context.Background(), plan, &result); err != nil {
		t.Fatal(err)
	}
	if result.BranchInfo == nil || !result.BranchInfo.Reused || len(f.writes()) != 0 {
		t.Fatalf("resume mutated remote objects: %+v writes=%v", result, f.writes())
	}
}

type failingCheckout struct{}

func (failingCheckout) Run(context.Context, []byte, string, ...string) ([]byte, error) {
	return nil, errors.New("checkout refused")
}

func TestCheckoutFailureRetainsCreatedBranch(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		fmt.Fprint(w, `{"data":{"createLinkedBranch":{"linkedBranch":{"ref":{"name":"17-fix-login"}}}}}`)
	})
	f.client.Runner = failingCheckout{}
	path := filepath.Join(t.TempDir(), "17-fix-login")
	plan := Plan{Repo: "owner/repo", WorkType: "fix", Slug: "login", BranchPlan: &BranchPlan{OID: "base", Checkout: true, WorktreePath: path}}
	result := Result{Number: 17, IssueNodeID: "I_17", URL: "https://github.com/owner/repo/issues/17"}
	err := f.client.CompleteBranch(context.Background(), plan, &result)
	if err == nil || result.BranchInfo == nil || !result.BranchInfo.Linked || result.BranchInfo.CheckedOut || result.BranchInfo.WorktreePath != path || result.URL == "" {
		t.Fatalf("created objects lost: %+v error=%v", result, err)
	}
}

type directoryRunner struct{ dir string }

func (runner directoryRunner) Run(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = runner.dir
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return out, nil
}

func TestWorktreeTracksRemoteBranchAndPreservesUserFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote, local := filepath.Join(root, "remote.git"), filepath.Join(root, "local")
	runner := directoryRunner{dir: root}
	git := func(args ...string) string {
		t.Helper()
		out, err := runner.Run(context.Background(), nil, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--bare", remote)
	git("init", local)
	git("-C", local, "symbolic-ref", "HEAD", "refs/heads/main")
	git("-C", local, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial")
	git("-C", local, "remote", "add", "origin", remote)
	git("-C", local, "push", "origin", "HEAD:refs/heads/17-fix-login")
	userFile := filepath.Join(local, "user-work.txt")
	if err := os.WriteFile(userFile, []byte("keep this work"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := Client{Runner: directoryRunner{dir: local}}
	path := filepath.Join(local+".worktrees", "17-fix-login")
	actual, reused, err := client.worktreeBranch(context.Background(), "17-fix-login", path)
	if err != nil || actual != path || reused {
		t.Fatal(err)
	}
	if git("-C", local, "branch", "--show-current") != "main" || git("-C", path, "branch", "--show-current") != "17-fix-login" || git("-C", path, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/17-fix-login" {
		t.Fatal("wrong branch or upstream")
	}
	if data, err := os.ReadFile(userFile); err != nil || string(data) != "keep this work" {
		t.Fatal("user file was changed")
	}
	if actual, reused, err := client.worktreeBranch(context.Background(), "17-fix-login", path); err != nil || actual != path || !reused {
		t.Fatalf("repeat worktree failed: path=%s reused=%t error=%v", actual, reused, err)
	}
}
