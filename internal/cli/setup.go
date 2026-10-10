package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type setupMappings []string

func (values *setupMappings) String() string         { return strings.Join(*values, ", ") }
func (values *setupMappings) Set(value string) error { *values = append(*values, value); return nil }

func runSetup(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	flags := flag.NewFlagSet("tools github setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current origin)")
	configPath := flags.String("config", "", "config to create/update (default: Git root .tools.json, then current directory)")
	dryRun := flags.Bool("dry-run", false, "preview fetched mappings without writing a file")
	jsonOutput := flags.Bool("json", false, "emit structured settings and available names")
	refresh := flags.Bool("refresh", false, "recalculate supported prefix mappings; explicit --set options take precedence")
	bodyLanguage := flags.String("body-language", "", "ko or any (default: preserve existing, otherwise ko)")
	var labels, types setupMappings
	flags.Var(&labels, "set-label", "PREFIX=existing-label; repeat for ordered candidates; PREFIX= disables")
	flags.Var(&types, "set-issue-type", "PREFIX=enabled-type; repeat for ordered candidates; PREFIX= disables")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github setup [options]\n\nFetch existing GitHub labels/types and save concrete prefix mappings locally.\nExisting preferences are preserved; --refresh recalculates mappings.\nGitHub labels/types are never created or updated.")
		flags.PrintDefaults()
		fmt.Fprintf(stderr, "\nAvailable prefixes:\n  %s\n", strings.Join(github.Prefixes, ", "))
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error, code int) int {
		if *jsonOutput {
			_ = encode(out, stderr, map[string]any{"status": "error", "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return code
	}
	if flags.NArg() != 0 {
		return fail(errors.New("unexpected positional arguments"), 2)
	}
	labelMap, err := github.ParseSetupMappings(labels)
	if err != nil {
		return fail(err, 2)
	}
	typeMap, err := github.ParseSetupMappings(types)
	if err != nil {
		return fail(err, 2)
	}
	if *bodyLanguage != "" && *bodyLanguage != "ko" && *bodyLanguage != "any" {
		return fail(errors.New("--body-language must be ko or any"), 2)
	}
	path := resolvePolicyPath(ctx, runner, *configPath)
	if strings.TrimSpace(path) == "" {
		return fail(errors.New("config path is empty"), 2)
	}
	existing, original, exists, mode, err := readSetupConfig(path)
	if err != nil {
		return fail(err, 2)
	}
	validation := github.DefaultPolicy()
	if err := validation.Merge(existing.GitHub); err != nil {
		return fail(err, 2)
	}
	client := github.Client{Runner: runner}
	resolved, err := client.ResolveRepo(ctx, *repo)
	if err != nil {
		return fail(err, 1)
	}
	client.API, err = newAPI(ctx, runner)
	if err != nil {
		return fail(err, 1)
	}
	catalog, err := client.Context(ctx, resolved)
	if err != nil {
		return fail(err, 1)
	}
	plan, err := github.Setup(catalog, existing.GitHub, github.Policy{BodyLanguage: *bodyLanguage, LabelMap: labelMap, IssueTypeMap: typeMap}, *refresh)
	if err != nil {
		return fail(err, 2)
	}
	plan.Repo = resolved
	data, err := json.MarshalIndent(plan.Config, "", "  ")
	if err != nil {
		return fail(err, 1)
	}
	data = append(data, '\n')
	status := "planned"
	var saved *setupEvidence
	var verifyErr error
	if !*dryRun {
		if err := writeSetupConfig(path, data, original, exists, mode); err != nil {
			return fail(err, 1)
		}
		status = "saved"
		saved, verifyErr = verifySetupConfig(path, data, original, exists, mode)
		if verifyErr != nil {
			status = "partial"
		}
	}
	if *jsonOutput {
		value := map[string]any{"status": status, "path": path, "plan": plan}
		if saved != nil {
			value["saved_config"] = saved
		}
		if code := encode(out, stderr, value); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(out, "%s: %s (repo: %s)\n", status, path, resolved)
		if *dryRun {
			fmt.Fprintln(out, string(data))
		} else {
			fmt.Fprintf(out, "saved=true changed=%t verified=%t sha256=%s mode=%s\n", saved.Changed, saved.Verified, saved.SHA256, saved.Mode)
		}
		for _, note := range plan.Notes {
			fmt.Fprintln(out, "note:", note)
		}
	}
	if verifyErr != nil {
		fmt.Fprintln(stderr, "saved config verification:", verifyErr)
		return 1
	}
	return 0
}

type setupEvidence struct {
	Saved         bool   `json:"saved"`
	Changed       bool   `json:"changed"`
	Verified      bool   `json:"verified"`
	SHA256        string `json:"sha256,omitempty"`
	Mode          string `json:"mode,omitempty"`
	ModePreserved bool   `json:"mode_preserved"`
	Error         string `json:"error,omitempty"`
}

func verifySetupConfig(path string, expected, original []byte, existed bool, mode os.FileMode) (*setupEvidence, error) {
	proof := &setupEvidence{Saved: true, Changed: !existed || !bytes.Equal(original, expected)}
	_, actual, exists, actualMode, err := readSetupConfig(path)
	if err == nil && !exists {
		err = errors.New("saved config disappeared")
	}
	if err == nil {
		proof.SHA256 = fmt.Sprintf("%x", sha256.Sum256(actual))
		proof.Mode = fmt.Sprintf("%04o", actualMode.Perm())
		proof.ModePreserved = !existed || actualMode.Perm() == mode.Perm()
		if !bytes.Equal(actual, expected) || !proof.ModePreserved {
			err = errors.New("saved config bytes or original permissions differ after read-back")
		}
	}
	proof.Verified = err == nil
	if err != nil {
		proof.Error = err.Error()
	}
	return proof, err
}

func readSetupConfig(path string) (github.Config, []byte, bool, os.FileMode, error) {
	var config github.Config
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil, false, 0o644, nil
	}
	if err != nil {
		return config, nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return config, nil, false, 0, errors.New("config must be a regular file; symbolic links are not replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, nil, false, 0, err
	}
	if err := github.Decode(bytes.NewReader(data), &config); err != nil {
		return config, nil, false, 0, fmt.Errorf("existing config: %w", err)
	}
	return config, data, true, info.Mode().Perm(), nil
}

func writeSetupConfig(path string, data, original []byte, existed bool, mode os.FileMode) error {
	// Existing user edits are checked before replacing the complete file.
	check := func() error {
		_, current, exists, currentMode, err := readSetupConfig(path)
		if err != nil {
			return err
		}
		if exists != existed || !bytes.Equal(current, original) || existed && currentMode.Perm() != mode.Perm() {
			return errors.New("config changed during setup; rerun to preserve the new settings")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	if existed && bytes.Equal(data, original) {
		return nil
	}
	if !existed {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(path)
			return writeErr
		}
		if closeErr != nil {
			_ = os.Remove(path)
			return closeErr
		}
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tools-setup-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	_, writeErr := temporary.Write(data)
	if writeErr == nil {
		writeErr = temporary.Sync()
	}
	closeErr := temporary.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := check(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
