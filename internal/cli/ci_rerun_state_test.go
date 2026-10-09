package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type statefulRerunAPI struct {
	reviewRerunCLIAPI
	directory string
}

func (api *statefulRerunAPI) StateDirectory() string { return api.directory }

func TestCIRerunDefaultCheckpointPreventsDuplicateRequestAndResumes(t *testing.T) {
	api := &statefulRerunAPI{directory: t.TempDir()}
	invoke := func(args ...string) (int, string) {
		var out, stderr bytes.Buffer
		arguments := append([]string{"github", "ci", "rerun", "--run", "42", "--repo", "owner/repo", "--json"}, args...)
		code := run(context.Background(), arguments, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
		return code, out.String() + stderr.String()
	}
	code, output := invoke("--all", "--wait=false")
	if code != 0 || api.writes != 1 {
		t.Fatalf("code=%d output=%s", code, output)
	}
	path := defaultRerunState(api, "owner/repo", 42)
	state, err := readRerunCheckpoint(path, "owner/repo", 42)
	if err != nil || state == nil || !state.Accepted || state.Terminal || state.Mode != "all" || state.ExpectedAttempt != 2 {
		t.Fatalf("checkpoint=%+v err=%v", state, err)
	}
	code, output = invoke()
	if code != 2 || api.writes != 1 || !strings.Contains(output, "--resume") {
		t.Fatal("unresolved request was submitted again")
	}
	code, output = invoke("--resume", "--all=false")
	if code != 2 || api.writes != 1 {
		t.Fatal("conflicting saved mode accepted")
	}
	code, output = invoke("--resume")
	if code != 0 || api.writes != 1 || !strings.Contains(output, `"resumed":true`) || !strings.Contains(output, `"status":"completed"`) {
		t.Fatalf("resume failed: code=%d output=%s", code, output)
	}
	state, err = readRerunCheckpoint(path, "owner/repo", 42)
	if err != nil || !state.Terminal {
		t.Fatal("completion was not saved")
	}
	// A blocked fresh invocation must not turn the old completed checkpoint
	// back into an unresolved request.
	api.err = true
	_, _ = invoke()
	state, err = readRerunCheckpoint(path, "owner/repo", 42)
	if err != nil || !state.Terminal {
		t.Fatal("preflight failure changed an earlier checkpoint")
	}
}

func TestCIRerunRejectsCorruptAndForeignCheckpointWithoutWrites(t *testing.T) {
	for _, mode := range []string{"corrupt", "foreign", "missing", "locked"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if mode == "corrupt" {
				_ = os.WriteFile(path, []byte(`{"invalid"`), 0600)
			}
			if mode == "foreign" {
				state := github.CIRerunCheckpoint{Version: 1, Repo: "other/repo", RunID: 42}
				data, _ := json.Marshal(state)
				_ = os.WriteFile(path, data, 0600)
			}
			if mode == "locked" {
				_ = os.WriteFile(path+".lock", nil, 0600)
			}
			api := &reviewRerunCLIAPI{}
			var out, stderr bytes.Buffer
			code := run(context.Background(), []string{"github", "ci", "rerun", "--run", "42", "--repo", "owner/repo", "--resume", "--state-file", path, "--json"}, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
			if code == 0 || api.writes != 0 {
				t.Fatalf("unsafe checkpoint used: code=%d out=%s", code, out.String())
			}
		})
	}
}
