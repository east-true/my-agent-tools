package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemDispatchNeverAuthenticatesAndReportsPartialResults(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\n한국어\n"), 0600)
	for _, args := range [][]string{{"fs"}, {"fs", "inspect", "--help"}, {"fs", "inspect", "--root", root, "--pattern", "한국어", "--json"}, {"fs", "delta", "--root", root, "--state-file", filepath.Join(root, ".tools/state/s.json"), "--json"}, {"fs", "apply", "--root", root, "--plan", "-", "--json"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(`{"version":1,"files":[{"path":"created.txt","sha256":"absent","content":"new\n"}]}`), &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
			t.Fatal("filesystem called GitHub authentication")
			return nil, errors.New("unavailable")
		})
		if code != 0 {
			t.Fatal(args, code, out.String(), stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	code := runFilesystem(context.Background(), []string{"inspect", "--root", root, "--path", "missing.txt", "--json"}, nil, &out, &stderr)
	if code != 1 || !strings.Contains(out.String(), `"complete":false`) {
		t.Fatal(code, out.String(), stderr.String())
	}
	out.Reset()
	code = runFilesystem(context.Background(), []string{"apply", "--root", root, "--plan", "-", "--apply", "--json"}, strings.NewReader(`{"version":1,"files":[{"path":"../outside.txt","sha256":"absent","content":"new"}]}`), &out, &stderr)
	if code != 1 || !strings.Contains(out.String(), `"status":"blocked"`) {
		t.Fatal(code, out.String())
	}
}

func TestSharedSpecCLIReportsAllFileErrorsAndSavesOnlyValidatedExpandedPlan(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("limit=8\r\nkeep\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	saved := filepath.Join(root, "plan.json")
	args := []string{"apply", "--root", root, "--spec", "-", "--save-plan", saved, "--apply", "--report-changes", "--json"}
	var out, stderr bytes.Buffer
	code := runFilesystem(context.Background(), args, strings.NewReader(`{"version":1,"groups":[{"paths":["a.txt","b.txt"],"replacements":[{"old":"limit=8","new":"limit=64","count":2}]}]}`), &out, &stderr)
	var failure struct {
		Diagnostics []struct {
			Path   string `json:"path"`
			Actual int    `json:"actual_count"`
		}
	}
	if json.Unmarshal(out.Bytes(), &failure) != nil || code != 1 || len(failure.Diagnostics) != 2 || failure.Diagnostics[0].Path != "a.txt" || failure.Diagnostics[1].Path != "b.txt" || failure.Diagnostics[1].Actual != 1 {
		t.Fatal(code, out.String(), stderr.String())
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatal("invalid batch saved a plan", err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		got, _ := os.ReadFile(filepath.Join(root, name))
		if string(got) != "limit=8\r\nkeep\r\n" {
			t.Fatal("failed batch wrote", name)
		}
	}
	out.Reset()
	stderr.Reset()
	code = runFilesystem(context.Background(), args, strings.NewReader(`{"version":1,"groups":[{"paths":["a.txt","b.txt"],"replacements":[{"old":"limit=8","new":"limit=64","count":1}]}]}`), &out, &stderr)
	var response struct {
		Complete  bool
		SavedPlan struct {
			Verified bool
			Files    int
		} `json:"saved_plan"`
		Files []struct{ Verified bool }
	}
	if json.Unmarshal(out.Bytes(), &response) != nil || code != 0 || !response.Complete || !response.SavedPlan.Verified || response.SavedPlan.Files != 2 || len(response.Files) != 2 || !response.Files[0].Verified || !response.Files[1].Verified {
		t.Fatal(code, out.String(), stderr.String())
	}
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Files []struct {
			Path         string
			SHA256       string
			Replacements []struct{ Count int }
		}
	}
	if json.Unmarshal(raw, &plan) != nil || len(plan.Files) != 2 || strings.Contains(string(raw), `"groups"`) {
		t.Fatal("saved plan format changed", string(raw))
	}
	wanted := fmt.Sprintf("%x", sha256.Sum256([]byte("limit=8\r\nkeep\r\n")))
	for _, file := range plan.Files {
		if file.SHA256 != wanted || file.Replacements[0].Count != 1 {
			t.Fatal("saved preimage/count lost", file)
		}
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		got, _ := os.ReadFile(filepath.Join(root, name))
		if string(got) != "limit=64\r\nkeep\r\n" {
			t.Fatal("group apply changed other bytes", name)
		}
	}
}

func TestFilesystemSpecSavesExpectedHashesAndAppliesWithVerifiedLines(t *testing.T) {
	root := t.TempDir()
	original := []byte("limit=08\r\nkeep\r\n")
	os.WriteFile(filepath.Join(root, "a.txt"), original, 0600)
	saved := filepath.Join(root, "plan.json")
	var out, stderr bytes.Buffer
	code := runFilesystem(context.Background(), []string{"apply", "--root", root, "--spec", "-", "--save-plan", saved, "--apply", "--report-changes", "--json"}, strings.NewReader(`{"version":1,"files":[{"path":"a.txt","replacements":[{"old":"limit=08","new":"limit=64","count":1}]}]}`), &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), `"verified":true`) || !strings.Contains(out.String(), `"limit=64"`) {
		t.Fatal(code, out.String(), stderr.String())
	}
	raw, _ := os.ReadFile(saved)
	var response struct {
		SavedPlan struct {
			SHA256   string `json:"sha256"`
			Version  int    `json:"version"`
			Files    int    `json:"files"`
			Verified bool   `json:"verified"`
		} `json:"saved_plan"`
	}
	if json.Unmarshal(out.Bytes(), &response) != nil || !response.SavedPlan.Verified || response.SavedPlan.Version != 1 || response.SavedPlan.Files != 1 || response.SavedPlan.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal("saved plan evidence does not match real bytes", out.String())
	}
	var plan struct {
		Files []struct {
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &plan) != nil || len(plan.Files) != 1 || plan.Files[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256(original)) {
		t.Fatal(string(raw))
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "limit=64\r\nkeep\r\n" {
		t.Fatal(string(got))
	}
	// An existing saved plan is never overwritten, and no new edits run on that failure.
	out.Reset()
	code = runFilesystem(context.Background(), []string{"apply", "--root", root, "--spec", "-", "--save-plan", saved, "--apply", "--json"}, strings.NewReader(`{"version":1,"files":[{"path":"a.txt","replacements":[{"old":"limit=64","new":"limit=99","count":1}]}]}`), &out, &stderr)
	if code != 1 {
		t.Fatal(code, out.String())
	}
	got, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	if !bytes.Equal(got, []byte("limit=64\r\nkeep\r\n")) {
		t.Fatal("edited after plan save failure")
	}
}

func TestSavedPlanVerificationRejectsSameSizeChangesAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "plan.json")
	original := []byte("original")
	if err := os.WriteFile(name, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifySavedPlan(name, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifySavedPlan(name, original); err == nil {
		t.Fatal("accepted modified plan with same size")
	}
	if err := verifySavedPlan(root, original); err == nil {
		t.Fatal("accepted a directory as a plan")
	}
}

func TestAuthorizedRecountCompletesWithoutChangingInputOrOtherCounts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.txt")
	specFile := filepath.Join(root, "spec.json")
	planFile := filepath.Join(root, "plan.json")
	os.WriteFile(source, []byte("x x\nkeep\n"), 0600)
	spec := []byte(`{"version":1,"files":[{"path":"a.txt","replacements":[{"old":"x","new":"X","count":1}]}]}`)
	os.WriteFile(specFile, spec, 0600)
	var out, stderr bytes.Buffer
	code := runFilesystem(context.Background(), []string{"apply", "--root", root, "--spec", specFile, "--recount", "1:1", "--save-plan", planFile, "--apply", "--report-changes", "--json"}, nil, &out, &stderr)
	var result struct {
		Complete    bool `json:"complete"`
		Corrections []struct {
			Path     string `json:"path"`
			Expected int    `json:"expected_count"`
			Actual   int    `json:"actual_count"`
		} `json:"corrections"`
		SavedPlan struct {
			Verified bool `json:"verified"`
		} `json:"saved_plan"`
	}
	if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || !result.Complete || !result.SavedPlan.Verified || len(result.Corrections) != 1 || result.Corrections[0].Path != "a.txt" || result.Corrections[0].Expected != 1 || result.Corrections[0].Actual != 2 {
		t.Fatal(code, out.String(), stderr.String())
	}
	got, _ := os.ReadFile(source)
	input, _ := os.ReadFile(specFile)
	if string(got) != "X X\nkeep\n" || !bytes.Equal(input, spec) {
		t.Fatal("incorrect final source or modified input spec")
	}
	out.Reset()
	code = runFilesystem(context.Background(), []string{"apply", "--root", root, "--plan", planFile, "--recount", "1:1", "--apply", "--json"}, nil, &out, &stderr)
	if code != 1 {
		t.Fatal("recount bypassed expected-hash plan rules")
	}
}
