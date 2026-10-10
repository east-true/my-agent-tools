package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tiktoken-go/tokenizer"
)

type compactFlags struct {
	Enabled   bool
	Dir       string
	Encoding  string
	Retention time.Duration
	Limit     int
}

func (options *compactFlags) register(flags *flag.FlagSet, enabled bool) {
	flags.BoolVar(&options.Enabled, "compact", enabled, "reduce long evidence; preserve the full result in a local file only when output shrinks")
	flags.StringVar(&options.Dir, "artifact-dir", ".tools/state/evidence", "directory for full evidence preserved by --compact")
	flags.StringVar(&options.Encoding, "token-encoding", "o200k_base", "token encoding used to verify compact output: o200k_base or cl100k_base")
	flags.DurationVar(&options.Retention, "artifact-retention", 30*24*time.Hour, "expiry for preserved evidence; 0 disables expiry")
	flags.IntVar(&options.Limit, "artifact-limit", 200, "maximum managed evidence files; 0 disables count limit")
}

func (options compactFlags) validate() error {
	if options.Enabled && strings.TrimSpace(options.Dir) == "" {
		return errors.New("--artifact-dir must not be empty")
	}
	if options.Retention < 0 || options.Limit < 0 {
		return errors.New("artifact retention and limit must not be negative")
	}
	if options.Encoding != "" && options.Encoding != "o200k_base" && options.Encoding != "cl100k_base" {
		return errors.New("--token-encoding must be o200k_base or cl100k_base")
	}
	return nil
}

var compactError = regexp.MustCompile(`(?i)(error|panic|fatal|fail|traceback|exception|assert|want|expected|actual)`)

func compactLogLines(lines []any) ([]any, bool) {
	result := make([]any, len(lines))
	truncated := false
	for i, line := range lines {
		result[i] = line
		if text, ok := line.(string); ok && len([]rune(text)) > 1200 {
			result[i] = string([]rune(text)[:1200])
			truncated = true
		}
	}
	return result, truncated
}

// compactProjection keeps exact source snippets and their line numbers. It
// never invents a summary, and the full JSON remains available by digest.
func compactProjection(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, field := range item {
			// 편집용 원문과 좌표는 조회한 바이트 그대로 반환한다.
			if key == "source_files" {
				result[key] = field
				continue
			}
			if key == "log_variants" {
				if variants, ok := field.([]any); ok && len(variants) > 0 {
					result["log_variants_omitted"] = len(variants)
				}
				continue
			}
			if key == "lines" {
				if lines, ok := field.([]any); ok && len(lines) > 30 {
					keep := map[int]bool{}
					for i, line := range lines {
						if text, ok := line.(string); ok && compactError.MatchString(text) {
							for j := max(0, i-3); j <= min(len(lines)-1, i+3); j++ {
								keep[j] = true
							}
							if len(keep) >= 60 {
								break
							}
						}
					}
					// Keep both ends even when no supported error marker exists.
					for i := 0; i < 5; i++ {
						keep[i], keep[len(lines)-1-i] = true, true
					}
					var excerpts []map[string]any
					for i := 0; i < len(lines); {
						if !keep[i] {
							i++
							continue
						}
						start := i
						for i < len(lines) && keep[i] {
							i++
						}
						selected, truncated := compactLogLines(lines[start:i])
						excerpt := map[string]any{"start_line": start + 1, "lines": selected}
						if truncated {
							excerpt["lines_truncated"] = true
						}
						excerpts = append(excerpts, excerpt)
					}
					result["excerpts"], result["omitted_lines"] = excerpts, len(lines)-len(keep)
					continue
				}
				if lines, ok := field.([]any); ok {
					selected, truncated := compactLogLines(lines)
					result[key] = selected
					if truncated {
						result["lines_truncated"] = true
					}
					continue
				}
			}
			// Review requests, code hunks and diagnostic messages remain exact so
			// fixing code never requires reopening an artifact for these fields.
			if text, ok := field.(string); ok && (key == "raw_details" || key == "summary" || key == "text") && len([]rune(text)) > 1200 {
				result[key], result[key+"_truncated"] = string([]rune(text)[:1200]), true
				continue
			}
			result[key] = compactProjection(field)
		}
		compactReviewMetadata(result)
		return result
	case []any:
		result := make([]any, len(item))
		for i, child := range item {
			result[i] = compactProjection(child)
		}
		return result
	default:
		return value
	}
}

// Review coordinates and timestamps often repeat. Keep meaningful differences
// and the current line (including null); compactValue preserves the exact full
// result before this projection is emitted.
func compactReviewMetadata(thread map[string]any) {
	comments, ok := thread["comments"].([]any)
	if !ok || thread["id"] == nil || thread["path"] == nil {
		return
	}
	if _, ok := thread["is_outdated"].(bool); !ok {
		return
	}
	if _, ok := thread["is_resolved"].(bool); !ok {
		return
	}
	for _, key := range []string{"start_line", "original_line", "original_start_line", "start_diff_side"} {
		if value, exists := thread[key]; exists && (value == nil || value == "") {
			delete(thread, key)
		}
	}
	for _, pair := range [][2]string{{"original_line", "line"}, {"original_start_line", "start_line"}, {"start_diff_side", "diff_side"}} {
		original, exists := thread[pair[0]]
		current, present := thread[pair[1]]
		equal := false
		switch value := original.(type) {
		case json.Number:
			equal = current == value
		case string:
			equal = current == value
		}
		if exists && present && equal {
			delete(thread, pair[0])
		}
	}
	for _, value := range comments {
		comment, ok := value.(map[string]any)
		if !ok {
			continue
		}
		created, ok := comment["created_at"].(string)
		if ok && created != "" && comment["updated_at"] == created {
			delete(comment, "updated_at")
		}
	}
}

func compactValue(value any, options compactFlags) (any, error) {
	if !options.Enabled {
		return value, nil
	}
	raw, err := compactJSON(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	projection := compactProjection(object).(map[string]any)
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if strings.TrimSpace(options.Dir) == "" {
		return nil, fmt.Errorf("--artifact-dir must not be empty")
	}
	path, err := filepath.Abs(filepath.Join(options.Dir, digest+".json"))
	if err != nil {
		return nil, err
	}
	projection["evidence_file"], projection["evidence_sha256"] = path, digest
	reduced, err := compactJSON(projection)
	if err != nil {
		return nil, err
	}
	// Evidence references themselves cost tokens. Keep short results unchanged.
	if len(reduced) >= len(raw) {
		return value, nil
	}
	encoding := options.Encoding
	if encoding == "" {
		encoding = "o200k_base"
	}
	codec, err := tokenizer.Get(tokenizer.Encoding(encoding))
	if err != nil {
		return nil, err
	}
	before, err := codec.Count(string(raw))
	if err != nil {
		return nil, err
	}
	after, err := codec.Count(string(reduced))
	if err != nil {
		return nil, err
	}
	if after >= before {
		return value, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := atomicJSONFile(path, raw); err != nil {
		return nil, err
	}
	if err := pruneEvidence(options, path); err != nil {
		return nil, err
	}
	return projection, nil
}

var managedEvidenceName = regexp.MustCompile(`^[a-f0-9]{64}\.json$`)

func pruneEvidence(options compactFlags, current string) error {
	if options.Retention == 0 && options.Limit == 0 {
		return nil
	}
	entries, err := os.ReadDir(options.Dir)
	if err != nil {
		return err
	}
	type artifact struct {
		path     string
		modified time.Time
	}
	files := []artifact{}
	for _, entry := range entries {
		if !managedEvidenceName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		path, err := filepath.Abs(filepath.Join(options.Dir, entry.Name()))
		if err != nil {
			return err
		}
		if path == current {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, artifact{path: path, modified: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modified.Equal(files[j].modified) {
			return files[i].path < files[j].path
		}
		return files[i].modified.After(files[j].modified)
	})
	kept := 1
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(bytes.TrimSuffix(data, []byte("\n")))) + ".json"
		if filepath.Base(file.path) != digest {
			continue
		}
		aged := options.Retention > 0 && time.Since(file.modified) > options.Retention
		if !aged && (options.Limit == 0 || kept < options.Limit) {
			kept++
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return err
		}
	}
	return nil
}

func compactJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func atomicJSONFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tools-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Close()
	} else {
		file.Close()
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	return err
}

func encodeCompact(out, stderr io.Writer, value any, options compactFlags) int {
	projected, err := compactValue(value, options)
	if err != nil {
		// A mutation may already have completed. Preserve its actual result.
		_ = encode(out, stderr, value)
		fmt.Fprintln(stderr, "preserve compact evidence:", err)
		return 1
	}
	return encode(out, stderr, projected)
}
