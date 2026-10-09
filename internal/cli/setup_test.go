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

type setupRootRunner struct{ root string }

func (runner setupRootRunner) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	if name == "git" && len(args) > 0 && args[0] == "rev-parse" {
		return []byte(runner.root + "\n"), nil
	}
	return nil, errors.New("unexpected command")
}

func invokeSetup(t *testing.T, root string, api github.API, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"github", "setup", "--repo", "owner/repo"}, args...), nil, &out, &stderr, setupRootRunner{root}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	return code, out.String(), stderr.String()
}

func TestSetupCLIWritesConfigAtGitRootAndDoesNotMutateGitHub(t *testing.T) {
	root := t.TempDir()
	api := &fakeAPI{}
	code, out, stderr := invokeSetup(t, root, api, "--json", "--body-language", "any")
	if code != 0 || api.writes != 0 || stderr != "" {
		t.Fatal(code, out, stderr, api.writes)
	}
	data, err := os.ReadFile(filepath.Join(root, ".tools.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config github.Config
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.GitHub.BodyLanguage != "any" || config.GitHub.LabelMap["feat"][0] != "enhancement" {
		t.Fatal(config)
	}
	policy := github.DefaultPolicy()
	if err := policy.Merge(config.GitHub); err != nil {
		t.Fatal("generated config cannot be loaded", err)
	}
	first := append([]byte{}, data...)
	code, out, stderr = invokeSetup(t, root, api, "--json")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	data, err = os.ReadFile(filepath.Join(root, ".tools.json"))
	if err != nil || !bytes.Equal(first, data) {
		t.Fatal("repeat setup was not idempotent", err)
	}
}

func TestSetupCLIPreviewAndInvalidNamesDoNotWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	api := &fakeAPI{}
	code, out, stderr := invokeSetup(t, root, api, "--config", path, "--dry-run", "--json")
	if code != 0 || !strings.Contains(out, `"status":"planned"`) || stderr != "" || api.writes != 0 {
		t.Fatal(code, out, stderr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote config", err)
	}
	code, out, _ = invokeSetup(t, root, api, "--config", path, "--set-label", "feat=missing", "--json")
	if code != 2 || !strings.Contains(out, "not available") || api.writes != 0 {
		t.Fatal(code, out, api.writes)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid override wrote config", err)
	}
}

func TestSetupCLIExistingConfigPreservedAndExplicitOptionsWin(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".tools.json")
	original := []byte(`{"github":{"body_language":"any","label_map":{"feat":[],"fix":["user-label"]},"issue_type_map":{"feat":["Feature"]}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	code, out, stderr := invokeSetup(t, root, api, "--set-label", "feat=ENHANCEMENT", "--json")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	data, _ := os.ReadFile(path)
	var config github.Config
	_ = json.Unmarshal(data, &config)
	if config.GitHub.BodyLanguage != "any" || config.GitHub.LabelMap["feat"][0] != "enhancement" || config.GitHub.LabelMap["fix"][0] != "user-label" {
		t.Fatal(config)
	}
	code, out, stderr = invokeSetup(t, root, api, "--refresh", "--json")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	data, _ = os.ReadFile(path)
	_ = json.Unmarshal(data, &config)
	if len(config.GitHub.LabelMap["fix"]) != 0 || config.GitHub.BodyLanguage != "any" {
		t.Fatal("refresh failed", config)
	}
}

func TestSetupRefusesMalformedAndChangedExistingFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".tools.json")
	original := []byte(`{"github":{"label_map":42}}`)
	_ = os.WriteFile(path, original, 0o600)
	code, _, _ := invokeSetup(t, root, &fakeAPI{}, "--json")
	if code != 2 {
		t.Fatal("malformed config overwritten")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("malformed config changed")
	}
	original = []byte(`{"github":{"body_language":"ko"}}`)
	_ = os.WriteFile(path, original, 0o600)
	changed := []byte(`{"github":{"body_language":"any"}}`)
	_ = os.WriteFile(path, changed, 0o600)
	if err := writeSetupConfig(path, []byte(`{}`), original, true, 0o600); err == nil {
		t.Fatal("concurrent user edits overwritten")
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, changed) {
		t.Fatal("new settings lost")
	}
	if err := writeSetupConfig(path, []byte(`{}`), nil, false, 0o600); err == nil {
		t.Fatal("newly appeared config overwritten")
	}
}

func TestSetupHelpAndInvalidFlagsDoNotAuthenticate(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--set-label", "unknown=bug"}, {"--body-language", "invalid"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), append([]string{"github", "setup"}, args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
			t.Fatal("help/invalid args authenticated")
			return nil, nil
		})
		if args[0] == "--help" || args[0] == "-h" {
			if code != 0 || !strings.Contains(stderr.String(), "set-label") {
				t.Fatal(code, stderr.String())
			}
		} else if code != 2 {
			t.Fatal(code, stderr.String())
		}
	}
}
