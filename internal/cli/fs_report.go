package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/east-true/my-agent-tools/internal/filesystem"
)

const maxSavedEvidenceBytes = 128 << 20

type fsReportArchive struct {
	Version    int                `json:"version"`
	Kind       string             `json:"kind"`
	ObservedAt string             `json:"observed_at"`
	Options    filesystem.Options `json:"scope"`
	Files      map[string]string  `json:"file_sha256,omitempty"`
	Selection  bool               `json:"verify_selection"`
	Payload    json.RawMessage    `json:"result"`
}

type reportReference struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
}

func readEvidenceBytes(path, expected string) ([]byte, error) {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != 32 || strings.ToLower(expected) != expected {
		return nil, errors.New("expected report/evidence SHA-256 must be 64 lowercase hex characters")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSavedEvidenceBytes {
		return nil, errors.New("saved evidence must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("saved evidence changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSavedEvidenceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSavedEvidenceBytes || fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
		return nil, errors.New("saved evidence SHA-256 mismatch")
	}
	return data, nil
}

func saveFSReport(path string, archive fsReportArchive, rootDir string) *reportReference {
	proof := &reportReference{Path: path}
	data, err := json.Marshal(archive)
	data = append(data, '\n')
	if err == nil && len(data) > maxSavedEvidenceBytes {
		err = errors.New("saved report byte limit exceeded")
	}
	if err == nil {
		proof.Path, err = filepath.Abs(path)
	}
	var bounded *os.Root
	var relative string
	if err == nil && rootDir != "" {
		var rootPath string
		rootPath, err = filepath.Abs(rootDir)
		if err == nil {
			bounded, err = os.OpenRoot(rootPath)
		}
		if err == nil {
			defer bounded.Close()
			relative, err = filepath.Rel(rootPath, proof.Path)
			if err == nil && !filepath.IsLocal(relative) {
				err = errors.New("automatic report path escapes source root")
			}
		}
	}
	if err == nil {
		if bounded != nil {
			err = bounded.MkdirAll(filepath.Dir(relative), 0700)
		} else {
			err = os.MkdirAll(filepath.Dir(proof.Path), 0700)
		}
	}
	if err == nil {
		var file *os.File
		if bounded != nil {
			file, err = bounded.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		} else {
			file, err = os.OpenFile(proof.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
		if err == nil {
			_, err = file.Write(data)
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err == nil {
		proof.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		_, err = readEvidenceBytes(proof.Path, proof.SHA256)
	}
	proof.Verified = err == nil
	if err != nil {
		proof.Error = err.Error()
	}
	return proof
}

func reportObject(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	object := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	err = decoder.Decode(&object)
	return object, err
}

// selectedText은 원래 개행과 1-based 위치를 보존하며 구간들을 합치지 않는다.
func selectedText(text string, start, end int, pattern string, contextLines int) []map[string]any {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	keep := make([]bool, len(lines))
	for i, line := range lines {
		if start > 0 && (i+1 < start || i+1 > end) {
			continue
		}
		if pattern == "" || strings.Contains(line, pattern) {
			for j := max(0, i-contextLines); j <= min(len(lines)-1, i+contextLines); j++ {
				if start == 0 || j+1 >= start && j+1 <= end {
					keep[j] = true
				}
			}
		}
	}
	spans := []map[string]any{}
	for i := 0; i < len(lines); {
		if !keep[i] {
			i++
			continue
		}
		first := i
		for i < len(lines) && keep[i] {
			i++
		}
		spans = append(spans, map[string]any{"start": first + 1, "end": i, "text": strings.Join(lines[first:i], "")})
	}
	return spans
}

func selectSavedRanges(value any, start, end int, pattern string, contextLines int) []any {
	result := []any{}
	ranges, _ := value.([]any)
	for _, span := range ranges {
		object, ok := span.(map[string]any)
		if !ok {
			continue
		}
		number, ok := object["start"].(json.Number)
		if !ok {
			continue
		}
		first, err := number.Int64()
		if err != nil || first < 1 {
			continue
		}
		lines, _ := object["lines"].([]any)
		keep := map[int]bool{}
		for i, line := range lines {
			position := int(first) + i
			if start > 0 && (position < start || position > end) {
				continue
			}
			text, _ := line.(string)
			if pattern == "" || strings.Contains(text, pattern) {
				for j := max(0, i-contextLines); j <= min(len(lines)-1, i+contextLines); j++ {
					if start == 0 || int(first)+j >= start && int(first)+j <= end {
						keep[j] = true
					}
				}
			}
		}
		for i := 0; i < len(lines); {
			if !keep[i] {
				i++
				continue
			}
			a := i
			for i < len(lines) && keep[i] {
				i++
			}
			result = append(result, map[string]any{"start": int(first) + a, "lines": lines[a:i]})
		}
	}
	return result
}

func readFSReport(ctx context.Context, kind, path, expected string, paths []string, start, end int, pattern string, contextLines, limit int) (any, error) {
	data, err := readEvidenceBytes(path, expected)
	if err != nil {
		return nil, err
	}
	var archive fsReportArchive
	if err := json.Unmarshal(data, &archive); err != nil {
		return nil, err
	}
	if archive.Version != 1 || archive.Kind != kind {
		return nil, errors.New("saved report command/version mismatch")
	}
	currentVerified := false
	if kind != "apply" {
		if err := filesystem.VerifyReportFiles(ctx, archive.Options, archive.Files, archive.Selection); err != nil {
			return nil, err
		}
		currentVerified = true
	}
	value := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(archive.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	key := map[string]string{"test-results": "reports", "delta": "changes", "apply": "files"}[kind]
	items, _ := value[key].([]any)
	selected := []any{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := object["path"].(string)
		if len(paths) > 0 && !containsString(paths, name) {
			continue
		}
		if kind == "test-results" && (start > 0 || pattern != "") {
			diagnostics, _ := object["diagnostics"].([]any)
			found := []any{}
			for index, item := range diagnostics {
				diagnostic, ok := item.(map[string]any)
				if !ok {
					continue
				}
				details, _ := diagnostic["details"].(string)
				spans := selectedText(details, start, end, pattern, contextLines)
				message, _ := diagnostic["message"].(string)
				if len(spans) == 0 && (pattern == "" || !strings.Contains(message, pattern)) {
					continue
				}
				delete(diagnostic, "details")
				diagnostic["diagnostic_index"], diagnostic["details_ranges"], diagnostic["details_selected"] = index+1, spans, true
				found = append(found, diagnostic)
			}
			object["diagnostics"] = found
		} else if kind != "test-results" && (start > 0 || pattern != "") {
			for _, field := range []string{"ranges", "before"} {
				if object[field] != nil {
					object[field] = selectSavedRanges(object[field], start, end, pattern, contextLines)
				}
			}
			object["ranges_selected"] = true
		}
		selected = append(selected, object)
	}
	value[key] = selected
	if before, ok := value["before_files"].(map[string]any); ok {
		delete(value, "before_files")
		raw := []any{}
		for _, item := range selected {
			object := item.(map[string]any)
			name := object["path"].(string)
			text, ok := before[name].(string)
			if ok {
				raw = append(raw, map[string]any{"path": name, "before_sha256": object["before_sha256"], "ranges": selectedText(text, start, end, pattern, contextLines)})
			}
		}
		value["before_raw_files"] = raw
	}
	value["collection_complete"] = value["complete"]
	value["saved_observation"], value["current_files_verified"] = true, currentVerified
	value["observed_at"], value["report_file"], value["report_sha256"] = archive.ObservedAt, path, expected
	// delta 복구는 읽기 전용이며 기존 기준을 갱신하지 않는다.
	if kind == "delta" {
		value["state_updated"] = false
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(payload)+1 > limit {
		return nil, errors.New("selected saved report exceeds output budget; select --path/--range/--pattern or increase --max-output-bytes")
	}
	return value, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func captureFSReport(kind string, options *filesystem.Options, archive *fsReportArchive, captureErr *error) {
	options.CaptureReport = func(snapshot filesystem.ReportSnapshot) {
		payload, err := json.Marshal(snapshot.Value)
		*captureErr = err
		*archive = fsReportArchive{Version: 1, Kind: kind, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Options: snapshot.Options, Files: snapshot.Files, Selection: snapshot.Selection, Payload: payload}
	}
}

func parseEvidenceRange(value string) (int, int, error) {
	if value == "" {
		return 0, 0, nil
	}
	var start, end int
	if _, err := fmt.Sscanf(value, "%d:%d", &start, &end); err != nil || start < 1 || end < start || fmt.Sprintf("%d:%d", start, end) != value {
		return 0, 0, errors.New("range must be positive inclusive START:END")
	}
	return start, end, nil
}
