package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
	"github.com/tiktoken-go/tokenizer"
)

func TestCompactRejectsByteSavingsThatIncreaseTokens(t *testing.T) {
	value := map[string]any{"status": "ok", "diff_hunk": strings.Repeat(" ", 5000)}
	raw, _ := compactJSON(value)
	result, err := compactValue(value, compactFlags{Enabled: true, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := compactJSON(result)
	if !bytes.Equal(actual, raw) {
		t.Fatal("token-cheap whitespace was replaced by a more expensive evidence reference")
	}
	for _, encoding := range []tokenizer.Encoding{tokenizer.O200kBase, tokenizer.Cl100kBase} {
		long := map[string]any{"status": "failed", "raw_details": strings.Repeat("구체적인 수정 요청을 확인하고 반영하세요. ", 500)}
		before, _ := compactJSON(long)
		projected, err := compactValue(long, compactFlags{Enabled: true, Dir: t.TempDir(), Encoding: string(encoding)})
		if err != nil {
			t.Fatal(err)
		}
		after, _ := compactJSON(projected)
		codec, _ := tokenizer.Get(encoding)
		a, _ := codec.Count(string(before))
		b, _ := codec.Count(string(after))
		if b >= a || len(after) >= len(before) {
			t.Fatalf("encoding=%s before=%d after=%d", encoding, a, b)
		}
	}
}

func TestEvidenceRetentionOnlyDeletesVerifiedManagedFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(label string, age time.Duration) string {
		data := []byte(`{"status":"ok","label":` + fmt.Sprintf("%q", label) + `}`)
		path := filepath.Join(dir, fmt.Sprintf("%x.json", sha256.Sum256(data)))
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return path
	}
	old := write("expired", 40*24*time.Hour)
	older := write("count overflow", 3*time.Hour)
	newer := write("recent", time.Hour)
	current := write("current", 0)
	foreign := filepath.Join(dir, strings.Repeat("1", 64)+".json")
	if err := os.WriteFile(foreign, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneEvidence(compactFlags{Dir: dir, Retention: 30 * 24 * time.Hour, Limit: 2}, current); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{old, older} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unpruned managed evidence %s", path)
		}
	}
	for _, path := range []string{newer, current, foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("removed protected evidence %s", path)
		}
	}
}

type defaultCleanupMergeAPI struct {
	mergeCLIAPI
	sourceReads int
}

func (api *defaultCleanupMergeAPI) Do(ctx context.Context, method, endpoint string, payload, target any) (int, error) {
	if endpoint == "repos/owner/repo/pulls/7" {
		api.sourceReads++
		return 0, json.Unmarshal([]byte(`{"number":7,"head":{"ref":"fix/workflows","sha":"head1","repo":{"full_name":"fork/repo"}}}`), target)
	}
	return api.mergeCLIAPI.Do(ctx, method, endpoint, payload, target)
}

func TestMergeDefaultContinuesToCleanupAndFinishIsRemoved(t *testing.T) {
	api := &defaultCleanupMergeAPI{}
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"github", "pr", "merge", "--number", "7", "--repo", "owner/repo", "--json"}, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	if code != 0 || api.sourceReads != 1 || !strings.Contains(out.String(), `"merged":true`) || !strings.Contains(out.String(), "cleanup skipped") {
		t.Fatalf("code=%d out=%s stderr=%s reads=%d", code, out.String(), stderr.String(), api.sourceReads)
	}
	out.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"github", "pr", "finish", "--help"}, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
		t.Fatal("removed command authenticated")
		return nil, nil
	})
	if code != 2 {
		t.Fatalf("duplicate finish command still exists: code=%d", code)
	}
}

func TestInspectionScopeCannotReuseAnotherSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := inspectionItems(stateFixtureResult())
	state.Sections = "checks"
	data, _ := json.Marshal(state)
	if err := atomicJSONFile(path, data); err != nil {
		t.Fatal(err)
	}
	if _, err := readInspectionState(path, "owner/repo", 7, "reviews"); err == nil {
		t.Fatal("accepted another selection's observation cursor")
	}
}

func TestInspectionFailureCacheHonorsCollectionOptions(t *testing.T) {
	state := inspectionItems(stateFixtureResult())
	state.Annotations, state.MaxLogBytes = true, 8<<20
	raw, err := json.Marshal(github.CIFailureResult{Repo: state.Repo, Complete: true, Run: github.CIRun{ID: 42, Attempt: 2, HeadSHA: state.HeadSHA, Status: "completed", Conclusion: "failure"}})
	if err != nil {
		t.Fatal(err)
	}
	state.Items["failure:42:2"] = raw
	for _, mode := range []string{"same options", "annotations changed", "limit changed", "partial evidence"} {
		t.Run(mode, func(t *testing.T) {
			options := github.InspectOptions{Annotations: true, MaxLogBytes: 8 << 20}
			copy := state
			if mode == "annotations changed" {
				options.Annotations = false
			} else if mode == "limit changed" {
				options.MaxLogBytes = 16 << 20
			} else if mode == "partial evidence" {
				copy.Items = map[string]json.RawMessage{"failure:42:2": json.RawMessage(`{"complete":false}`)}
			}
			reuseInspectionEvidence(&copy, &options)
			if (len(options.CachedFailures) == 1) != (mode == "same options") {
				t.Fatalf("wrong cache reuse for %s: %+v", mode, options.CachedFailures)
			}
		})
	}
}
