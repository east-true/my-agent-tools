package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

type creationBranchRunner struct {
	branch string
	err    bool
	reads  int
}

func (runner *creationBranchRunner) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	runner.reads++
	if name != "git" || len(args) != 4 || args[0] != "symbolic-ref" || args[3] != "HEAD" || runner.err {
		return nil, errors.New("branch unavailable")
	}
	return []byte(runner.branch + "\n"), nil
}

func TestPRCreationReturnsFinalLocalBranchAndPreservesCreatedURLOnFailure(t *testing.T) {
	for _, scenario := range []string{"implicit", "explicit fork", "changed", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/repos/owner/repo/pulls" {
					t.Fatal("unexpected additional API call", r.URL)
				}
				fmt.Fprint(w, `{"number":8,"html_url":"pr-url","head":{"ref":"feat/add-cli","sha":"abc"},"base":{"ref":"main"}}`)
			})
			runner := &creationBranchRunner{branch: "feat/add-cli", err: scenario == "unavailable"}
			if scenario == "changed" {
				runner.branch = "main"
			}
			f.client.Runner = runner
			plan := Plan{Kind: "pr", Repo: "owner/repo", LocalHead: "feat/add-cli", Payload: map[string]any{"head": "feat/add-cli", "base": "main"}}
			if scenario == "explicit fork" {
				plan.LocalHead = ""
				plan.Payload["head"] = "fork:feat/add-cli"
			}
			result, err := f.client.Create(context.Background(), plan)
			if result.Number != 8 || result.URL != "pr-url" || result.PRContext == nil || result.PRContext.HeadSHA != "abc" || result.PRContext.BaseRef != "main" || len(f.writes()) != 1 {
				t.Fatal(result, err)
			}
			if scenario == "explicit fork" {
				if err != nil || runner.reads != 0 || result.PRContext.LocalBranchVerified || result.PRContext.RequestedHead != "fork:feat/add-cli" {
					t.Fatal("remote head treated as local proof", result, err, runner.reads)
				}
				return
			}
			if runner.reads != 1 || result.PRContext.LocalBranchVerified != (scenario == "implicit") || (err == nil) != (scenario == "implicit") {
				t.Fatal(result, err, runner.reads)
			}
			if err != nil && result.Status != "partial" {
				t.Fatal("created PR was lost", result)
			}
		})
	}
}
