package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type cleanupCLIAPI struct{}

func (cleanupCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	sha := strings.Repeat("a", 40)
	var data string
	switch {
	case endpoint == "repos/owner/repo":
		data = `{"default_branch":"main"}`
	case strings.Contains(endpoint, "/branches?"):
		data = `[{"name":"main","commit":{"sha":"` + sha + `"}},{"name":"feat/finished","commit":{"sha":"` + sha + `"}}]`
	case strings.Contains(endpoint, "/pulls?"):
		data = `[{"number":1,"state":"closed","head":{"ref":"feat/finished","sha":"` + sha + `","repo":{"full_name":"owner/repo"}}}]`
	case endpoint == "graphql" && strings.HasPrefix(payload.(map[string]any)["query"].(string), "query CleanupIssues"):
		data = `{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
	default:
		return 0, errors.New("unexpected API request: " + method + " " + endpoint)
	}
	return 0, json.Unmarshal([]byte(data), target)
}

type cleanupCLIRunner struct {
	writes     int
	failDelete bool
	remote     string
}

func (runner *cleanupCLIRunner) Run(_ context.Context, _ []byte, _ string, args ...string) ([]byte, error) {
	sha := strings.Repeat("a", 40)
	switch args[0] {
	case "remote":
		runner.remote = args[len(args)-1]
		return []byte("https://github.com/owner/repo.git\n"), nil
	case "for-each-ref":
		if strings.Contains(args[1], "objectname") {
			if strings.HasPrefix(args[2], "refs/remotes/") {
				return nil, nil
			}
			return []byte("refs/heads/main\x00" + sha + "\x00\x00\nrefs/heads/feat/finished\x00" + sha + "\x00origin\x00refs/heads/feat/finished\n"), nil
		}
		return []byte("refs/heads/main\x00\x00\nrefs/heads/feat/finished\x00origin\x00refs/heads/feat/finished\n"), nil
	case "worktree":
		return []byte("worktree " + filepath.Join(os.TempDir(), "example") + "\x00branch refs/heads/main\x00\x00"), nil
	case "rev-parse":
		return []byte(filepath.Join(os.TempDir(), "example") + "\n"), nil
	case "merge-base":
		return nil, nil
	case "config":
		return nil, errors.New("no branch config")
	case "update-ref", "-c":
		runner.writes++
		if runner.failDelete {
			return nil, errors.New("deletion refused")
		}
		return nil, nil
	}
	return nil, errors.New("unexpected Git command")
}

func TestCleanupCLIPreviewApplyAndPartialJSON(t *testing.T) {
	for _, mode := range []string{"preview", "apply", "partial"} {
		t.Run(mode, func(t *testing.T) {
			runner := &cleanupCLIRunner{failDelete: mode == "partial"}
			args := []string{"github", "branch", "cleanup", "--json"}
			if mode != "preview" {
				args = append(args, "--apply")
			}
			var out, stderr bytes.Buffer
			code := run(context.Background(), args, nil, &out, &stderr, runner, func(context.Context, command.Runner) (github.API, error) { return cleanupCLIAPI{}, nil })
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err, out.String(), stderr.String())
			}
			if mode == "preview" && (code != 0 || runner.writes != 0 || result["status"] != "planned") {
				t.Fatal("preview wrote or failed", code, runner.writes, result)
			}
			if mode == "apply" && (code != 0 || runner.writes != 2 || result["status"] != "completed") {
				t.Fatal("apply failed", code, runner.writes, result)
			}
			if mode == "partial" && (code != 1 || runner.writes != 1 || result["status"] != "partial") {
				t.Fatal("partial failure hidden", code, runner.writes, result)
			}
		})
	}
}

func TestCleanupHelpAndInvalidFlagsNeedNoAuthentication(t *testing.T) {
	for _, args := range [][]string{{"github", "branch"}, {"github", "branch", "--help"}, {"github", "branch", "cleanup", "--help"}, {"github", "branch", "cleanup", "--apply", "--dry-run"}, {"github", "branch", "cleanup", "--scope", "invalid"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
			t.Fatal("unexpected authentication")
			return nil, nil
		})
		isHelp := len(args) == 2 || strings.Contains(strings.Join(args, " "), "--help")
		if isHelp && (code != 0 || !strings.Contains(out.String()+stderr.String(), "cleanup")) {
			t.Fatal(code, out.String(), stderr.String())
		}
		if !isHelp && code != 2 {
			t.Fatal("invalid flags accepted", code)
		}
	}
}

func TestCleanupResolvesSelectedRemote(t *testing.T) {
	runner := &cleanupCLIRunner{}
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"github", "branch", "cleanup", "--remote", "upstream", "--scope", "remote", "--json"}, nil, &out, &stderr, runner, func(context.Context, command.Runner) (github.API, error) { return cleanupCLIAPI{}, nil })
	if code != 0 || runner.remote != "upstream" || runner.writes != 0 {
		t.Fatal(code, out.String(), stderr.String(), runner)
	}
}
