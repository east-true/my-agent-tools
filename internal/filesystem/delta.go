package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DeltaOptions struct {
	Options
	StateFile    string
	Content      bool
	Context      int
	Reset        bool
	Peek         bool
	ContentKinds []string
	Comparisons  int
}
type fingerprint struct {
	SHA256 string  `json:"sha256"`
	Bytes  int64   `json:"bytes"`
	Mode   uint32  `json:"mode"`
	Text   *string `json:"text,omitempty"`
}
type selection struct {
	Include, Exclude, Paths []string
	MaxFiles                int
	MaxFileBytes            int64
	Content                 bool
}
type snapshot struct {
	Version   int                    `json:"version"`
	Root      string                 `json:"root"`
	Selection selection              `json:"selection"`
	Files     map[string]fingerprint `json:"files"`
}
type Change struct {
	BeforeRawRanges []Range `json:"before_raw_ranges,omitempty"`
	RawRanges       []Range `json:"raw_ranges,omitempty"`
	RangesOmitted   int     `json:"ranges_omitted,omitempty"`
	Path            string  `json:"path"`
	Kind            string  `json:"kind"`
	SHA256          string  `json:"sha256,omitempty"`
	BeforeSHA256    string  `json:"before_sha256,omitempty"`
	Ranges          []Range `json:"ranges,omitempty"`
	Before          []Range `json:"before,omitempty"`
	BeforeMode      *uint32 `json:"before_mode,omitempty"`
	Mode            *uint32 `json:"mode,omitempty"`
}
type DeltaResult struct {
	BaselineSHA256    string        `json:"baseline_sha256,omitempty"`
	ReportSelected    bool          `json:"report_selected,omitempty"`
	Tracked           int           `json:"tracked"`
	Status            string        `json:"status"`
	Changes           []Change      `json:"changes"`
	Unchanged         int           `json:"unchanged"`
	StateUpdated      bool          `json:"state_updated"`
	Complete          bool          `json:"complete"`
	Problems          []Problem     `json:"problems,omitempty"`
	Comparisons       int           `json:"comparisons,omitempty"`
	Consistent        *bool         `json:"consistent,omitempty"`
	BaselinePreserved *bool         `json:"baseline_preserved,omitempty"`
	OtherResults      []DeltaResult `json:"other_results,omitempty"`
	SnapshotSHA256    string        `json:"snapshot_sha256,omitempty"`
	observationSHA    string
}

func lockState(name string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(name+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("state is locked or unavailable; inspect .lock before retrying: %w", err)
	}
	return func() { file.Close(); os.Remove(name + ".lock") }, nil
}
func saveState(name string, data []byte) error {
	if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return errors.New("state destination must be a regular file")
	}
	file, err := os.CreateTemp(filepath.Dir(name), ".tools-fs-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), name)
	}
	return err
}
func sorted(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
func Delta(ctx context.Context, options DeltaOptions) (DeltaResult, error) {
	if options.Comparisons < 0 || options.Comparisons > 10 {
		return DeltaResult{}, errors.New("comparisons must be 1–10")
	}
	if options.Comparisons > 1 {
		return observeDeltas(ctx, options, Delta)
	}
	result := DeltaResult{Status: "ok", Changes: []Change{}, Complete: true}
	if options.ReportPattern != "" && !options.Content {
		return result, errors.New("report-pattern requires --include-content")
	}
	if options.StateFile == "" || options.Context < 0 || options.Context > 1000 || options.Peek && options.Reset {
		return result, errors.New("--state-file is required; context must be 0–1000")
	}
	kinds := map[string]bool{}
	for _, kind := range options.ContentKinds {
		if kind != "added" && kind != "modified" && kind != "deleted" {
			return result, errors.New("content kinds must be added, modified or deleted")
		}
		kinds[kind] = true
	}
	if len(kinds) > 0 && !options.Content {
		return result, errors.New("--content-kinds requires --include-content")
	}
	statePath, err := filepath.Abs(options.StateFile)
	if err != nil {
		return result, err
	}
	s, err := openScope(options.Options)
	if err != nil {
		return result, err
	}
	defer s.root.Close()
	if relative, err := filepath.Rel(s.name, statePath); err == nil && filepath.IsLocal(relative) {
		name := filepath.ToSlash(relative)
		s.options.Exclude = append(s.options.Exclude, name, name+".lock")
	}
	unlock, err := lockState(statePath)
	if err != nil {
		return result, err
	}
	defer unlock()
	if info, err := os.Lstat(statePath); err == nil && (!info.Mode().IsRegular() || info.Size() > 64<<20) {
		return result, errors.New("invalid state file type or size")
	}
	scopeSelection := selection{sorted(s.options.Include), sorted(s.options.Exclude), sorted(s.options.Paths), s.options.MaxFiles, s.options.MaxFileBytes, options.Content}
	current := snapshot{Version: 1, Root: s.name, Selection: scopeSelection, Files: map[string]fingerprint{}}
	var previous snapshot
	raw, err := os.ReadFile(statePath)
	initialized := errors.Is(err, os.ErrNotExist)
	if initialized && options.Peek {
		return result, errors.New("initialize the baseline before using --peek")
	}
	if err != nil && !initialized {
		return result, err
	}
	if !initialized {
		if json.Unmarshal(raw, &previous) != nil || previous.Version != 1 || previous.Files == nil {
			return result, errors.New("invalid state JSON; preserve it and use another file")
		}
		a, _ := json.Marshal(previous.Selection)
		b, _ := json.Marshal(scopeSelection)
		if previous.Root != s.name {
			return result, errors.New("state belongs to another root")
		}
		if !bytes.Equal(a, b) && !options.Reset {
			return result, errors.New("selection differs from baseline; use another state file or --reset")
		}
	}
	if options.Reset {
		initialized = true
		previous = snapshot{}
	}
	s.missingPaths = map[string]bool{}
	for _, name := range s.options.Paths {
		s.missingPaths[name] = true
	}
	names, problems, err := s.files(ctx)
	if err != nil {
		return result, err
	}
	result.Problems = problems
	total := 0
	for _, name := range names {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		data, mode, err := s.read(name)
		if err != nil {
			result.Problems = append(result.Problems, Problem{name, err.Error()})
			continue
		}
		total += len(data)
		if total > 32<<20 {
			result.Problems = append(result.Problems, Problem{name, "snapshot total byte limit exceeded"})
			break
		}
		item := fingerprint{SHA256: digest(data), Bytes: int64(len(data)), Mode: uint32(mode)}
		if options.Content && validText(data) {
			text := string(data)
			item.Text = &text
		}
		current.Files[name] = item
	}
	if len(result.Problems) > 0 {
		result.Status = "partial"
		result.Complete = false
		return result, nil
	}
	observation, _ := json.Marshal(current.Files)
	result.observationSHA = digest(observation)
	keys := map[string]bool{}
	for name := range current.Files {
		keys[name] = true
	}
	for name := range previous.Files {
		keys[name] = true
	}
	ordered := []string{}
	for name := range keys {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	result.Tracked = len(current.Files)
	for _, name := range ordered {
		if initialized {
			break
		}
		before, oldExists := previous.Files[name]
		after, newExists := current.Files[name]
		if oldExists && newExists && before.SHA256 == after.SHA256 && before.Mode == after.Mode {
			result.Unchanged++
			continue
		}
		item := Change{Path: name, SHA256: after.SHA256, BeforeSHA256: before.SHA256, Kind: "modified"}
		if oldExists && newExists && before.Mode != after.Mode {
			oldMode, newMode := before.Mode, after.Mode
			item.BeforeMode = &oldMode
			item.Mode = &newMode
		}
		if !oldExists {
			item.Kind = "added"
		} else if !newExists {
			item.Kind = "deleted"
		}
		if options.Content && (len(kinds) == 0 || kinds[item.Kind]) {
			oldLines, newLines := []string{}, []string{}
			if before.Text != nil {
				oldLines = textLines([]byte(*before.Text))
			}
			if after.Text != nil {
				newLines = textLines([]byte(*after.Text))
			}
			if before.Text != nil || after.Text != nil {
				item.Before, item.Ranges = changedRanges(oldLines, newLines, options.Context)
			}
		}
		result.Changes = append(result.Changes, item)
	}
	if initialized {
		result.Status = "initialized"
	} else if len(result.Changes) == 0 {
		result.Status = "unchanged"
	}
	files := map[string]string{}
	for name, item := range current.Files {
		files[name] = fmt.Sprintf("%s:%o", item.SHA256, item.Mode)
	}
	for name := range previous.Files {
		if _, exists := current.Files[name]; !exists {
			files[name] = "absent"
		}
	}
	for _, name := range s.options.Paths {
		if _, exists := current.Files[name]; !exists {
			files[name] = "absent"
		}
	}
	if options.Peek {
		actual, readErr := baselineBytes(statePath)
		preserved := readErr == nil && bytes.Equal(raw, actual)
		result.BaselinePreserved = &preserved
		result.BaselineSHA256 = digest(raw)
		if !preserved {
			result.Status, result.Complete = "partial", false
			result.Problems = append(result.Problems, Problem{statePath, "baseline changed or became unavailable during observation"})
		}
	}
	payload, _ := json.Marshal(result)
	beforeRaw := map[string]string{}
	for _, change := range result.Changes {
		if old := previous.Files[change.Path]; old.Text != nil {
			beforeRaw[change.Path] = *old.Text
		}
	}
	s.captureReport(struct {
		DeltaResult
		BeforeFiles map[string]string `json:"before_files,omitempty"`
	}{result, beforeRaw}, files, true, len(payload)+1 > s.options.MaxOutputBytes)
	if options.ReportPattern != "" {
		result.ReportSelected = true
		for i := range result.Changes {
			change := &result.Changes[i]
			change.Before, change.Ranges = nil, nil
			if len(kinds) > 0 && !kinds[change.Kind] {
				continue
			}
			if item := previous.Files[change.Path]; item.Text != nil {
				change.BeforeRawRanges = selectRawReportRanges(*item.Text, options.ReportPattern, options.ReportContext)
			}
			if item := current.Files[change.Path]; item.Text != nil {
				change.RawRanges = selectRawReportRanges(*item.Text, options.ReportPattern, options.ReportContext)
			}
		}
		payload, _ = json.Marshal(result)
	}
	if len(payload) > s.options.MaxOutputBytes {
		for i := range result.Changes {
			result.Changes[i].RangesOmitted = len(result.Changes[i].Ranges) + len(result.Changes[i].Before) + len(result.Changes[i].BeforeRawRanges) + len(result.Changes[i].RawRanges)
			result.Changes[i].Ranges, result.Changes[i].Before = nil, nil
			result.Changes[i].BeforeRawRanges, result.Changes[i].RawRanges = nil, nil
		}
		result.Status = "partial"
		result.Complete = false
		result.Problems = []Problem{{".", "delta output limit exceeded; baseline was not advanced"}}
		return result, nil
	}
	if options.Peek {
		return result, nil
	}
	payload, err = json.Marshal(current)
	if err != nil {
		return result, err
	}
	if len(payload) > 64<<20 {
		return result, errors.New("state byte limit exceeded")
	}
	if !initialized && strings.TrimSpace(string(raw)) == string(payload) {
		return result, nil
	}
	if err := saveState(statePath, payload); err != nil {
		return result, err
	}
	result.StateUpdated = true
	return result, nil
}
