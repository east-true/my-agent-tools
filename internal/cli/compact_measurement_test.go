package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// Fixed synthetic inputs measure serialized output, not agent task usage.
func compactMeasurementFixtures() map[string]any {
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = fmt.Sprintf("2026-10-08T00:00:00Z processing item %03d: dependency cache ready", i)
	}
	lines[210] = "2026-10-08T00:00:00Z ERROR sample_test.go:12: expected 200, actual 500"
	return map[string]any{
		"ci_log":     map[string]any{"status": "ok", "complete": true, "evidence": []any{map[string]any{"kind": "log", "lines": lines}}},
		"review":     map[string]any{"status": "ok", "complete": true, "threads": []any{map[string]any{"id": "T1", "comments": []any{map[string]any{"id": "C1", "body": strings.Repeat("수정 요청: 로그인 실패 시 오류 메시지와 재시도 버튼을 표시하세요. ", 200), "diff_hunk": strings.Repeat("+ return error\n", 200)}}}}},
		"long_line":  map[string]any{"status": "partial", "complete": false, "evidence": []any{map[string]any{"kind": "log", "lines": []string{"ERROR " + strings.Repeat("dependency lookup failed; ", 500)}}}},
		"whitespace": map[string]any{"status": "ok", "diff_hunk": strings.Repeat(" ", 5000)},
		"short":      map[string]any{"status": "ok", "complete": true, "number": 7},
	}
}

func TestCompactOutputMeasurements(t *testing.T) {
	reportDir := os.Getenv("TOOLS_COMPACT_MEASUREMENT_DIR")
	artifactDir := reportDir
	if artifactDir == "" {
		artifactDir = t.TempDir()
	}
	type measurement struct {
		Fixture      string `json:"fixture"`
		Encoding     string `json:"encoding"`
		InputSHA256  string `json:"input_sha256"`
		OutputSHA256 string `json:"output_sha256"`
		BeforeBytes  int    `json:"before_bytes"`
		AfterBytes   int    `json:"after_bytes"`
		BeforeTokens int    `json:"before_tokens"`
		AfterTokens  int    `json:"after_tokens"`
		Compacted    bool   `json:"compacted"`
	}
	rows := []measurement{}
	fixtures := compactMeasurementFixtures()
	for _, encoding := range []tokenizer.Encoding{tokenizer.O200kBase, tokenizer.Cl100kBase} {
		codec, err := tokenizer.Get(encoding)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"ci_log", "review", "long_line", "whitespace", "short"} {
			value := fixtures[name]
			before, err := compactJSON(value)
			if err != nil {
				t.Fatal(err)
			}
			projected, err := compactValue(value, compactFlags{Enabled: true, Dir: filepath.Join(artifactDir, "evidence", string(encoding), name), Encoding: string(encoding)})
			if err != nil {
				t.Fatal(err)
			}
			after, err := compactJSON(projected)
			if err != nil {
				t.Fatal(err)
			}
			beforeTokens, err := codec.Count(string(before))
			if err != nil {
				t.Fatal(err)
			}
			afterTokens, err := codec.Count(string(after))
			if err != nil {
				t.Fatal(err)
			}
			compacted := string(before) != string(after)
			if afterTokens > beforeTokens || (compacted && afterTokens == beforeTokens) || compacted != (name != "short" && name != "whitespace" && name != "review") {
				t.Fatalf("%s/%s: tokens=%d->%d compacted=%t", name, encoding, beforeTokens, afterTokens, compacted)
			}
			rows = append(rows, measurement{name, string(encoding), fmt.Sprintf("%x", sha256.Sum256(before)), fmt.Sprintf("%x", sha256.Sum256(after)), len(before), len(after), beforeTokens, afterTokens, compacted})
			t.Logf("%s/%s: tokens=%d->%d bytes=%d->%d", name, encoding, beforeTokens, afterTokens, len(before), len(after))
		}
	}
	if reportDir != "" {
		report := map[string]any{"scope": "fixed synthetic serialized JSON output, without trailing newline; includes absolute evidence reference and SHA-256", "tokenizer": "github.com/tiktoken-go/tokenizer v0.8.1", "model_calls": 0, "agent_task_tokens_measured": false, "artifact_root": reportDir, "measurements": rows}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(reportDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(reportDir, "compact-output.json"), append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
