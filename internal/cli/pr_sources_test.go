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

func TestReviewSourceBundlePreservesRawPreimageAndSavedEvidence(t *testing.T) {
	root := t.TempDir()
	source := []byte("retry_budget=8\r\ntimeout=30\r\n# 그대로")
	os.WriteFile(filepath.Join(root, "settings.py"), source, 0640)
	api := &conversationCLIAPI{base: workflowCLIAPI{body: "exact request"}}
	factory := func(context.Context, command.Runner) (github.API, error) {
		return api, nil
	}
	for _, path := range []string{"settings.py", "missing.py"} {
		var out, stderr bytes.Buffer
		saved := filepath.Join(root, path+".json")
		args := []string{"github", "pr", "reviews", "--repo", "owner/repo", "--number", "7", "--source-root", root, "--source-path", path, "--save-result", saved, "--json"}
		code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, factory)
		var value reviewOutput
		if err := json.Unmarshal(out.Bytes(), &value); err != nil || value.Sources == nil || len(value.Threads) == 0 {
			t.Fatal(code, out.String(), stderr.String(), err)
		}
		if path == "missing.py" {
			if code != 1 || value.Complete || value.Sources.Complete || len(value.Sources.Problems) == 0 {
				t.Fatal("missing source accepted", value)
			}
			if _, err := os.Stat(saved); !os.IsNotExist(err) {
				t.Fatal("incomplete source bundle saved", err)
			}
			continue
		}
		data, err := os.ReadFile(saved)
		var evidence reviewOutput
		if code != 0 || err != nil || json.Unmarshal(data, &evidence) != nil || !value.Saved.Verified || !evidence.Complete || evidence.Sources == nil || evidence.Saved != nil {
			t.Fatal(code, out.String(), err)
		}
		file := evidence.Sources.Files[0]
		if file.SHA256 == "" || !bytes.Equal([]byte(*file.Ranges[0].Text), source) || !strings.Contains(out.String(), "exact request") {
			t.Fatal("source or review bytes lost", evidence)
		}
	}
}

func TestInvalidSourceSelectionFailsBeforeAuthentication(t *testing.T) {
	var out, stderr bytes.Buffer
	called := false
	factory := func(context.Context, command.Runner) (github.API, error) {
		called = true
		return nil, nil
	}
	code := run(context.Background(), []string{"github", "pr", "reviews", "--repo", "owner/repo", "--number", "7", "--source-path", "../outside"}, nil, &out, &stderr, fakeRunner{}, factory)
	if code != 2 || called {
		t.Fatal(code, called, out.String(), stderr.String())
	}
}

func TestMarkdownCommonOccurrenceAvoidsRequestFileAndKeepsAmbiguityErrors(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"a.md", "b.md"} {
		os.WriteFile(filepath.Join(root, path), []byte("# Guide\r\n## Build\r\nfirst\r\n## Build\r\nsecond\r\n"), 0600)
	}
	args := []string{"inspect", "--root", root, "--path", "a.md", "--path", "b.md", "--outline", "--section", "Build", "--raw", "--hash", "--json"}
	var out, stderr bytes.Buffer
	if code := runFilesystem(context.Background(), args, nil, &out, &stderr); code != 1 {
		t.Fatal("ambiguous title accepted", code, out.String())
	}
	out.Reset()
	if code := runFilesystem(context.Background(), append(args, "--section-occurrence", "2"), nil, &out, &stderr); code != 0 || strings.Contains(out.String(), `first\r\n`) || strings.Count(out.String(), `second\r\n`) != 2 {
		t.Fatal("common occurrence or raw bytes lost", code, out.String(), stderr.String())
	}
}
