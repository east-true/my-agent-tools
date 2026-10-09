package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type cleanupTestRunner struct {
	directoryRunner
	remote   string
	commands [][]string
	before   func([]string) error
}

func (runner *cleanupTestRunner) Run(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	runner.commands = append(runner.commands, append([]string(nil), args...))
	if runner.before != nil {
		if err := runner.before(args); err != nil {
			return nil, err
		}
	}
	// Only the push operation is redirected to an isolated bare repository.
	if len(args) >= 3 && args[0] == "-c" && args[2] == "push" {
		args = append([]string{"-c", "remote.origin.url=" + runner.remote, "-c", "remote.origin.pushurl=" + runner.remote}, args...)
	}
	return runner.directoryRunner.Run(ctx, input, name, args...)
}

type cleanupWorld struct {
	t            *testing.T
	client       Client
	runner       *cleanupTestRunner
	remoteRunner directoryRunner
	prs          []cleanupPR
	protected    map[string]bool
	issues       map[int]string
	links        map[int][]string
	base         string
	fixture      *fixture
}

func newCleanupWorld(t *testing.T) *cleanupWorld {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	setup := directoryRunner{dir: root}
	call := func(args ...string) {
		t.Helper()
		if _, err := setup.Run(context.Background(), nil, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	remote, local := filepath.Join(root, "remote.git"), filepath.Join(root, "local")
	call("init", "--bare", remote)
	call("init", "--initial-branch=main", local)
	runner := &cleanupTestRunner{directoryRunner: directoryRunner{dir: local}, remote: remote}
	w := &cleanupWorld{t: t, runner: runner, remoteRunner: directoryRunner{dir: remote}, protected: map[string]bool{}, issues: map[int]string{}, links: map[int][]string{}}
	w.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial")
	w.base = w.git("rev-parse", "HEAD")
	w.git("remote", "add", "origin", "https://github.com/owner/repo.git")
	w.git("push", remote, "main")
	w.git("update-ref", "refs/remotes/origin/main", w.base)
	w.fixture = newFixture(t, func(out http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			fmt.Fprint(out, `{"full_name":"owner/repo","default_branch":"main"}`)
		case "/repos/owner/repo/branches":
			data, err := w.remoteRunner.Run(context.Background(), nil, "git", "for-each-ref", "--format=%(refname:strip=2)%00%(objectname)", "refs/heads/")
			if err != nil {
				t.Error(err)
				out.WriteHeader(500)
				return
			}
			branches := []cleanupRemoteBranch{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				fields := strings.Split(line, "\x00")
				branch := cleanupRemoteBranch{Name: fields[0], Protected: w.protected[fields[0]]}
				branch.Commit.SHA = fields[1]
				branches = append(branches, branch)
			}
			_ = json.NewEncoder(out).Encode(branches)
		case "/repos/owner/repo/pulls":
			_ = json.NewEncoder(out).Encode(w.prs)
		case "/graphql":
			nodes := []map[string]any{}
			for number, state := range w.issues {
				if state != "closed" {
					continue
				}
				linked := []map[string]any{}
				for _, name := range w.links[number] {
					linked = append(linked, map[string]any{"ref": map[string]any{"name": name, "repository": map[string]any{"nameWithOwner": "owner/repo"}}})
				}
				nodes = append(nodes, map[string]any{"number": number, "id": fmt.Sprintf("I_%d", number), "linkedBranches": map[string]any{"nodes": linked, "pageInfo": map[string]any{"hasNextPage": false}}})
			}
			_ = json.NewEncoder(out).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false}}}}})
		default:
			if strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/") {
				var number int
				_, _ = fmt.Sscanf(r.URL.Path, "/repos/owner/repo/issues/%d", &number)
				if state, ok := w.issues[number]; ok {
					_ = json.NewEncoder(out).Encode(map[string]any{"number": number, "state": state})
					return
				}
				out.WriteHeader(404)
				fmt.Fprint(out, `{"message":"Not Found"}`)
				return
			}
			t.Errorf("unexpected cleanup API call: %s", r.URL)
			out.WriteHeader(500)
		}
	})
	w.client = w.fixture.client
	w.client.Runner = runner
	runner.commands = nil
	return w
}

func (w *cleanupWorld) git(args ...string) string {
	w.t.Helper()
	out, err := w.runner.directoryRunner.Run(context.Background(), nil, "git", args...)
	if err != nil {
		w.t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (w *cleanupWorld) branch(name string, local, remote bool) {
	w.t.Helper()
	if local {
		w.git("branch", name, w.base)
	}
	if remote {
		w.git("push", w.runner.remote, w.base+":refs/heads/"+name)
	}
	if local {
		w.git("update-ref", "refs/remotes/origin/"+name, w.base)
		w.git("config", "branch."+name+".remote", "origin")
		w.git("config", "branch."+name+".merge", "refs/heads/"+name)
	}
}

func (w *cleanupWorld) pr(number int, name, state string, merged bool) {
	pr := cleanupPR{Number: number, State: state}
	pr.Head.Ref, pr.Head.SHA = name, w.base
	pr.Head.Repo = &struct {
		Name string `json:"full_name"`
	}{Name: "owner/repo"}
	if merged {
		when := "2026-10-07T00:00:00Z"
		pr.MergedAt = &when
	}
	w.prs = append(w.prs, pr)
}

func (w *cleanupWorld) plan(scope string) CleanupPlan {
	w.t.Helper()
	plan, err := w.client.PlanCleanup(context.Background(), "owner/repo", CleanupOptions{Remote: "origin", Scope: scope})
	if err != nil {
		w.t.Fatal(err)
	}
	return plan
}

func cleanupTarget(t *testing.T, plan CleanupPlan, scope, name string) CleanupTarget {
	t.Helper()
	for _, target := range plan.Targets {
		if target.Scope == scope && target.Name == name {
			return target
		}
	}
	t.Fatalf("missing target %s %s: %+v", scope, name, plan)
	return CleanupTarget{}
}

func TestCleanupPreviewTerminalStatesAndProtections(t *testing.T) {
	w := newCleanupWorld(t)
	for _, name := range []string{"feat/merged", "fix/closed", "feat/open", "7-fix-issue", "feat/linked", "feat/protected", "feat/worktree", "feat/fork", "feat/advanced", "feat/unknown"} {
		w.branch(name, true, true)
	}
	w.pr(1, "feat/merged", "closed", true)
	w.pr(2, "fix/closed", "closed", false)
	w.pr(3, "feat/open", "open", false)
	w.pr(4, "feat/protected", "closed", true)
	w.pr(5, "feat/worktree", "closed", true)
	w.pr(6, "feat/fork", "closed", true)
	w.prs[len(w.prs)-1].Head.Repo.Name = "someone/fork"
	w.pr(7, "feat/advanced", "closed", true)
	w.prs[len(w.prs)-1].Head.SHA = "old-sha"
	w.issues[7] = "closed"
	w.issues[9] = "closed"
	w.links[9] = []string{"feat/linked"}
	w.protected["feat/protected"] = true
	worktree := filepath.Join(t.TempDir(), "worktree")
	w.git("worktree", "add", worktree, "feat/worktree")
	w.git("worktree", "lock", worktree)
	plan := w.plan("both")
	for _, name := range []string{"feat/merged", "fix/closed", "7-fix-issue", "feat/linked"} {
		for _, scope := range []string{"remote", "local"} {
			if target := cleanupTarget(t, plan, scope, name); !target.Eligible {
				t.Errorf("finished branch kept: %+v", target)
			}
		}
	}
	for _, name := range []string{"main", "feat/open", "feat/protected", "feat/worktree", "feat/fork", "feat/advanced", "feat/unknown"} {
		for _, scope := range []string{"remote", "local"} {
			if target := cleanupTarget(t, plan, scope, name); target.Eligible {
				t.Errorf("unsafe candidate: %+v", target)
			}
		}
	}
	for _, args := range w.runner.commands {
		if args[0] == "fetch" || args[0] == "update-ref" || args[0] == "push" || args[0] == "config" {
			t.Errorf("preview mutated Git: %v", args)
		}
	}
	if len(w.fixture.writes()) != 0 {
		t.Fatal("preview mutated GitHub")
	}
}

func TestCleanupOpenPRWinsOverClosedIssueAndEarlierClosedPR(t *testing.T) {
	w := newCleanupWorld(t)
	name := "17-fix-login"
	w.branch(name, true, true)
	w.issues[17] = "closed"
	w.pr(1, name, "closed", true)
	w.pr(2, name, "open", false)
	for _, target := range w.plan("both").Targets {
		if target.Name == name && (target.Eligible || target.Skip != "open PR") {
			t.Errorf("open work deleted: %+v", target)
		}
	}
}

func TestCleanupRetainsUnpublishedLocalCommits(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/merged"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	w.git("switch", name)
	w.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "unpublished")
	w.git("switch", "main")
	plan := w.plan("both")
	if !cleanupTarget(t, plan, "remote", name).Eligible || cleanupTarget(t, plan, "local", name).Eligible {
		t.Fatal("publication guard incorrect", plan)
	}
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(err, result)
	}
	w.git("show-ref", "--verify", "refs/heads/"+name)
	if _, err := w.remoteRunner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/"+name); err == nil {
		t.Fatal("remote finished branch retained")
	}
}

func TestCleanupRemovesLocalAfterSquashMergeAndAlreadyDeletedRemote(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/squashed"
	w.branch(name, true, false)
	w.git("switch", name)
	w.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "feature")
	sha := w.git("rev-parse", "HEAD")
	w.git("switch", "main")
	w.git("update-ref", "refs/remotes/origin/"+name, sha)
	w.pr(1, name, "closed", true)
	w.prs[0].Head.SHA = sha
	plan := w.plan("both")
	if !cleanupTarget(t, plan, "local", name).Eligible || !cleanupTarget(t, plan, "tracking", "origin/"+name).Eligible {
		t.Fatal("squash cleanup refused", plan)
	}
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(err, result)
	}
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/origin/" + name} {
		if _, err := w.runner.directoryRunner.Run(context.Background(), nil, "git", "show-ref", "--verify", ref); err == nil {
			t.Fatal("ref retained", ref)
		}
	}
}

func TestCleanupApplyDeletesSelectedRefsAndPreservesUnknownWork(t *testing.T) {
	w := newCleanupWorld(t)
	name := "fix/closed"
	w.branch(name, true, true)
	w.pr(1, name, "closed", false)
	w.branch("feat/unknown", true, true)
	result, err := w.client.ApplyCleanup(context.Background(), w.plan("both"))
	if err != nil {
		t.Fatal(err, result)
	}
	deleted := 0
	for _, action := range result.Actions {
		if action.Status == "deleted" {
			deleted++
		}
	}
	if deleted != 2 {
		t.Fatal("wrong deletion count", result)
	}
	for _, runner := range []directoryRunner{w.runner.directoryRunner, w.remoteRunner} {
		if _, err := runner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/"+name); err == nil {
			t.Fatal("closed branch retained")
		}
		if _, err := runner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/feat/unknown"); err != nil {
			t.Fatal("unknown branch deleted", err)
		}
	}
}

func TestCleanupRechecksReopenedPRBeforeWriting(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/merged"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	plan := w.plan("both")
	w.prs[0].State = "open"
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range result.Actions {
		if action.Status == "deleted" {
			t.Fatal("reopened work deleted", result)
		}
	}
	w.git("show-ref", "--verify", "refs/heads/"+name)
}

func TestCleanupRechecksReopenedIssueAndBranchSettings(t *testing.T) {
	w := newCleanupWorld(t)
	name := "17-fix-login"
	w.branch(name, true, true)
	w.issues[17] = "closed"
	plan := w.plan("both")
	w.issues[17] = "open"
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range result.Actions {
		if action.Status == "deleted" {
			t.Fatal("reopened issue work deleted", result)
		}
	}
	w.issues[17] = "closed"
	result, err = w.client.ApplyCleanup(context.Background(), w.plan("both"))
	if err != nil {
		t.Fatal(err, result)
	}
	if _, err := w.runner.directoryRunner.Run(context.Background(), nil, "git", "config", "--local", "--get", "branch."+name+".remote"); err == nil {
		t.Fatal("deleted branch upstream settings retained")
	}
}

func TestCleanupWorktreeCreatedAfterRefreshProtectsRemoteAlias(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/merged"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	w.git("branch", "alias", name)
	w.git("config", "branch.alias.remote", "origin")
	w.git("config", "branch.alias.merge", "refs/heads/"+name)
	plan := w.plan("both")
	calls := 0
	w.runner.before = func(args []string) error {
		if args[0] == "worktree" {
			calls++
			if calls == 2 {
				w.git("worktree", "add", filepath.Join(t.TempDir(), "new-worktree"), "alias")
			}
		}
		return nil
	}
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(err, result)
	}
	for _, action := range result.Actions {
		if action.Status == "deleted" {
			t.Fatal("checked-out work deleted", result)
		}
	}
}

func TestCleanupClosedIssueAndLinkedBranchPagination(t *testing.T) {
	f := newFixture(t, func(out http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(payload.Query, "query CleanupIssueBranches"):
			if payload.Variables["cursor"] != "links-next" {
				t.Error("wrong nested cursor", payload)
			}
			fmt.Fprint(out, `{"data":{"node":{"linkedBranches":{"nodes":[{"ref":{"name":"feat/second","repository":{"nameWithOwner":"owner/repo"}}},{"ref":{"name":"feat/foreign","repository":{"nameWithOwner":"other/repo"}}}],"pageInfo":{"hasNextPage":false}}}}}`)
		case payload.Variables["cursor"] == nil:
			fmt.Fprint(out, `{"data":{"repository":{"issues":{"nodes":[{"id":"I_1","number":1,"linkedBranches":{"nodes":[{"ref":{"name":"feat/first","repository":{"nameWithOwner":"owner/repo"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"links-next"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"issues-next"}}}}}`)
		case payload.Variables["cursor"] == "issues-next":
			fmt.Fprint(out, `{"data":{"repository":{"issues":{"nodes":[{"id":"I_2","number":2,"linkedBranches":{"nodes":[{"ref":null}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}`)
		default:
			t.Error("unexpected cursor", payload)
		}
	})
	links, closed, err := f.client.cleanupClosedIssues(context.Background(), "owner/repo")
	if err != nil || len(links) != 2 || len(links["feat/first"]) != 1 || len(links["feat/second"]) != 1 || !closed[1] || !closed[2] {
		t.Fatal(links, closed, err)
	}
	if len(f.writes()) != 0 {
		t.Fatal("pagination mutated GitHub")
	}
}

func TestCleanupIncompleteMetadataRefusesPlanning(t *testing.T) {
	for _, body := range []string{
		`{"errors":[{"message":"permission denied"}]}`,
		`{"data":{"repository":null}}`,
		`{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}}}}`,
		`{"data":{"repository":{"issues":{"nodes":[{"id":"I_1","number":1,"linkedBranches":{"nodes":[],"pageInfo":{"hasNextPage":true}}}],"pageInfo":{"hasNextPage":false}}}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t, func(out http.ResponseWriter, r *http.Request) { fmt.Fprint(out, body) })
			if _, _, err := f.client.cleanupClosedIssues(context.Background(), "owner/repo"); err == nil {
				t.Fatal("incomplete metadata accepted")
			}
		})
	}
}

func TestCleanupRESTPagination(t *testing.T) {
	f := newFixture(t, func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			out.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/branches?per_page=100&page=2>; rel="next"`, r.Host))
			fmt.Fprint(out, `[{"name":"first","commit":{"sha":"first-sha"}}]`)
		} else {
			fmt.Fprint(out, `[{"name":"second","commit":{"sha":"second-sha"}}]`)
		}
	})
	items, err := cleanupPages[cleanupRemoteBranch](context.Background(), f.client, "repos/owner/repo/branches")
	if err != nil || len(items) != 2 || items[1].Name != "second" {
		t.Fatal(items, err)
	}
}

func TestCleanupLeaseAndLocalCompareAndSwapRejectConcurrentMoves(t *testing.T) {
	for _, scope := range []string{"remote", "local"} {
		t.Run(scope, func(t *testing.T) {
			w := newCleanupWorld(t)
			name := "feat/merged"
			w.branch(name, true, true)
			w.pr(1, name, "closed", true)
			plan := w.plan(scope)
			// Make a distinct commit without moving the candidate until deletion.
			w.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "new work")
			newSHA := w.git("rev-parse", "HEAD")
			fired := false
			w.runner.before = func(args []string) error {
				if fired {
					return nil
				}
				if scope == "remote" && len(args) > 2 && args[2] == "push" {
					fired = true
					w.git("push", w.runner.remote, newSHA+":refs/heads/"+name)
				}
				if scope == "local" && args[0] == "update-ref" && strings.Contains(strings.Join(args, " "), "refs/heads/"+name) {
					fired = true
					w.git("update-ref", "refs/heads/"+name, newSHA)
				}
				return nil
			}
			result, err := w.client.ApplyCleanup(context.Background(), plan)
			if err == nil || result.Status != "partial" || !fired {
				t.Fatal("concurrent move did not reject deletion", result, err)
			}
			runner := w.runner.directoryRunner
			if scope == "remote" {
				runner = w.remoteRunner
			}
			out, err := runner.Run(context.Background(), nil, "git", "rev-parse", "refs/heads/"+name)
			if err != nil || strings.TrimSpace(string(out)) != newSHA {
				t.Fatal("new work lost", string(out), err)
			}
		})
	}
}

func TestCleanupRemoteFailureRetainsLocalAndReportsPartial(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/merged"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	w.runner.before = func(args []string) error {
		if len(args) > 2 && args[2] == "push" {
			return errors.New("remote permission denied")
		}
		return nil
	}
	result, err := w.client.ApplyCleanup(context.Background(), w.plan("both"))
	if err == nil || result.Status != "partial" {
		t.Fatal("failure hidden", result, err)
	}
	w.git("show-ref", "--verify", "refs/heads/"+name)
	for _, action := range result.Actions {
		if action.Scope == "local" && action.Name == name && action.Status != "skipped" {
			t.Fatal("last local copy lost", result)
		}
	}
}

func TestCleanupRejectsMismatchedPushURLAndProtectsAliases(t *testing.T) {
	w := newCleanupWorld(t)
	name := "feat/merged"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	w.git("config", "remote.origin.pushurl", "https://github.com/other/repo.git")
	if _, err := w.client.PlanCleanup(context.Background(), "owner/repo", CleanupOptions{Remote: "origin", Scope: "both"}); err == nil {
		t.Fatal("different push repository accepted")
	}
	w.git("config", "--unset", "remote.origin.pushurl")
	w.git("branch", "alias", name)
	w.git("config", "branch.alias.remote", "origin")
	w.git("config", "branch.alias.merge", "refs/heads/"+name)
	worktree := filepath.Join(t.TempDir(), "alias-worktree")
	w.git("worktree", "add", worktree, "alias")
	w.git("worktree", "lock", worktree)
	if cleanupTarget(t, w.plan("both"), "remote", name).Eligible {
		t.Fatal("remote of checked-out alias was eligible")
	}
}

func TestCleanupExplicitProtectionAndForeignUpstream(t *testing.T) {
	w := newCleanupWorld(t)
	name := "release/stable"
	w.branch(name, true, true)
	w.pr(1, name, "closed", true)
	options := CleanupOptions{Remote: "origin", Scope: "both", Protect: []string{"release/*"}}
	plan, err := w.client.PlanCleanup(context.Background(), "owner/repo", options)
	if err != nil {
		t.Fatal(err)
	}
	if cleanupTarget(t, plan, "remote", name).Eligible {
		t.Fatal("protected pattern ignored")
	}
	w.git("remote", "add", "fork", "https://github.com/other/repo.git")
	w.git("config", "branch."+name+".remote", "fork")
	if cleanupTarget(t, w.plan("both"), "local", name).Eligible {
		t.Fatal("other remote's local branch eligible")
	}
}
