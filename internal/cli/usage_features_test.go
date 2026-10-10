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

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type conversationCLIAPI struct {
	base  workflowCLIAPI
	calls int
	body  string
}

func (api *conversationCLIAPI) Do(ctx context.Context, method, endpoint string, payload, target any) (int, error) {
	api.calls++
	if strings.Contains(endpoint, "/issues/7/comments?") {
		data, _ := json.Marshal([]github.ConversationComment{{ID: 8, Body: api.body, URL: "https://github.com/owner/repo/pull/7#issuecomment-8", UpdatedAt: "now"}})
		return 0, json.Unmarshal(data, target)
	}
	return api.base.Do(ctx, method, endpoint, payload, target)
}

func TestReviewsSaveExactCollectionWithoutOverwritingOrSavingPartialData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saved.json")
	api := &conversationCLIAPI{base: workflowCLIAPI{body: strings.Repeat("exact review\n", 1000)}, body: "review unavailable notice\n"}
	runArgs := []string{"github", "pr", "reviews", "--repo", "owner/repo", "--number", "7", "--conversation", "--save-result", path, "--compact", "--json"}
	var out, stderr bytes.Buffer
	factory := func(context.Context, command.Runner) (github.API, error) {
		return api, nil
	}
	code := run(context.Background(), runArgs, nil, &out, &stderr, fakeRunner{}, factory)
	var value struct {
		github.ReviewResult
		Saved savedReviewResult `json:"saved_result"`
	}
	if err := json.Unmarshal(out.Bytes(), &value); err != nil || code != 0 || !value.Saved.Verified || api.calls != 4 {
		t.Fatal(code, out.String(), stderr.String(), err, api.calls)
	}
	data, err := os.ReadFile(path)
	if err != nil || value.Saved.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatal("saved proof mismatch", err)
	}
	var saved github.ReviewResult
	if json.Unmarshal(data, &saved) != nil || !saved.Complete || saved.Threads[0].Comments[0].Body != api.base.body || saved.Threads[0].Comments[0].DiffHunk == "" || saved.Conversation == nil || (*saved.Conversation)[0].Body != api.body {
		t.Fatal("saved source truncated", saved)
	}
	if value.Threads[0].Comments[0].Body != api.base.body {
		t.Fatal("compact source body truncated")
	}
	out.Reset()
	calls := api.calls
	code = run(context.Background(), runArgs, nil, &out, &stderr, fakeRunner{}, factory)
	after, _ := os.ReadFile(path)
	if code != 2 || api.calls != calls || !bytes.Equal(data, after) || !strings.Contains(out.String(), "destination already exists") {
		t.Fatal("existing destination collected again or overwrote source", code, out.String())
	}
	missing := filepath.Join(t.TempDir(), "partial.json")
	api.base.partial = true
	runArgs[9] = missing // save-result의 파일 경로
	out.Reset()
	code = run(context.Background(), runArgs, nil, &out, &stderr, fakeRunner{}, factory)
	if _, err := os.Stat(missing); code != 1 || !os.IsNotExist(err) {
		t.Fatal("partial result was saved", code, err, out.String())
	}
}

func TestInspectionConversationStateScopeAndCurrentEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	api := &conversationCLIAPI{base: workflowCLIAPI{body: "request"}, body: "ordinary exact comment"}
	args := []string{"github", "pr", "inspect", "--repo", "owner/repo", "--number", "7", "--sections", "reviews", "--conversation", "--state-file", path, "--json", "--compact=false"}
	factory := func(context.Context, command.Runner) (github.API, error) {
		return api, nil
	}
	var out, stderr bytes.Buffer
	if code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, factory); code != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
	out.Reset()
	if code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, factory); code != 0 || !strings.Contains(out.String(), `"status":"unchanged"`) || !strings.Contains(out.String(), "ordinary exact comment") {
		t.Fatal(code, out.String(), stderr.String())
	}
	data, _ := os.ReadFile(path)
	calls := api.calls
	out.Reset()
	args = append(args, "--conversation=false")
	if code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, factory); code != 2 || api.calls != calls {
		t.Fatal("different scope fetched or accepted", code, out.String(), api.calls, calls)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("scope mismatch advanced state")
	}
}

func TestFilesystemMarkdownAndJUnitCLI(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide\r\n## Build\r\nexact\r\n## Other\r\nskip\r\n"), 0600)
	os.WriteFile(filepath.Join(root, "TEST-x.xml"), []byte(`<testsuite name="x" tests="1"><testcase name="one"/></testsuite>`), 0600)
	for _, args := range [][]string{{"inspect", "--root", root, "--path", "guide.md", "--section", "Build", "--raw", "--hash", "--json"}, {"test-results", "--root", root, "--json"}, {"test-results", "--help"}} {
		var out, stderr bytes.Buffer
		code := runFilesystem(context.Background(), args, nil, &out, &stderr)
		if code != 0 {
			t.Fatal(args, code, out.String(), stderr.String())
		}
		if args[0] == "inspect" && (!strings.Contains(out.String(), `exact\r\n`) || strings.Contains(out.String(), "skip")) {
			t.Fatal("section scope/raw lost", out.String())
		}
		if args[0] == "test-results" && len(args) > 2 && (!strings.Contains(out.String(), `"tests":1`) || !strings.Contains(out.String(), `"execution_verified":false`)) {
			t.Fatal(out.String())
		}
	}
}
