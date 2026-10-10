package filesystem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

type Replacement struct {
	Old   string `json:"old"`
	New   string `json:"new"`
	Count int    `json:"count"`
}
type Edit struct {
	Path         string        `json:"path"`
	SHA256       string        `json:"sha256"`
	Content      *string       `json:"content,omitempty"`
	Replacements []Replacement `json:"replacements,omitempty"`
}
type Plan struct {
	Version int         `json:"version"`
	Files   []Edit      `json:"files"`
	Groups  []EditGroup `json:"groups,omitempty"`
}

// EditGroup is spec-only shorthand for identical replacements on existing files.
// Saved plans always expand groups into individual, hash-bound files.
type EditGroup struct {
	Paths        []string      `json:"paths"`
	Replacements []Replacement `json:"replacements"`
}
type ApplyFile struct {
	BeforeMode    *uint32         `json:"before_mode,omitempty"`
	Mode          *uint32         `json:"mode,omitempty"`
	ModePreserved *bool           `json:"mode_preserved,omitempty"`
	RangesOmitted int             `json:"ranges_omitted,omitempty"`
	Path          string          `json:"path"`
	Status        string          `json:"status"`
	SHA256        string          `json:"sha256,omitempty"`
	Error         string          `json:"error,omitempty"`
	Verified      bool            `json:"verified,omitempty"`
	Ranges        []Range         `json:"ranges,omitempty"`
	Diagnostic    *EditDiagnostic `json:"diagnostic,omitempty"`
}
type ApplyResult struct {
	ReportSelected bool             `json:"report_selected,omitempty"`
	Status         string           `json:"status"`
	Files          []ApplyFile      `json:"files"`
	Applied        int              `json:"applied"`
	Complete       bool             `json:"complete"`
	ReportComplete bool             `json:"report_complete"`
	PlanFile       string           `json:"plan_file,omitempty"`
	SavedPlan      *PlanEvidence    `json:"saved_plan,omitempty"`
	Corrections    []EditDiagnostic `json:"corrections,omitempty"`
}

// PlanEvidence describes the actual saved bytes without duplicating the edit plan.
type PlanEvidence struct {
	SHA256   string `json:"sha256"`
	Version  int    `json:"version"`
	Files    int    `json:"files"`
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
}
type prepared struct {
	edit      Edit
	data      []byte
	mode      os.FileMode
	temporary string
	original  []byte
}

func (s *scope) prepare(edit Edit) (prepared, error) {
	item := prepared{edit: edit, mode: 0644}
	name, err := cleanPath(edit.Path)
	if err != nil {
		return item, err
	}
	item.edit.Path = name
	if edit.SHA256 == "absent" {
		if edit.Content == nil || len(edit.Replacements) > 0 {
			return item, errors.New("creation requires content and no replacements")
		}
		if err := s.regularPath(name, true); err != nil {
			return item, err
		}
		if _, err := s.root.Lstat(name); err == nil {
			return item, errors.New("creation destination exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return item, err
		}
		item.data = []byte(*edit.Content)
	} else {
		if len(edit.SHA256) != 64 {
			return item, errors.New("expected SHA-256 must be 64 lowercase hex characters or absent")
		}
		decoded, err := hex.DecodeString(edit.SHA256)
		if err != nil || len(decoded) != 32 || strings.ToLower(edit.SHA256) != edit.SHA256 {
			return item, errors.New("invalid expected SHA-256")
		}
		data, mode, err := s.read(name)
		if err != nil {
			return item, err
		}
		if digest(data) != edit.SHA256 {
			return item, errors.New("file changed since inspection; refresh the plan")
		}
		if !validText(data) {
			return item, errors.New("batch edits require UTF-8 text")
		}
		item.mode = mode
		item.original = append([]byte{}, data...)
		if edit.Content != nil {
			if len(edit.Replacements) > 0 {
				return item, errors.New("choose content or replacements")
			}
			data = []byte(*edit.Content)
		} else {
			if len(edit.Replacements) == 0 {
				return item, errors.New("edit requires content or replacements")
			}
			text := string(data)
			for index, replacement := range edit.Replacements {
				if replacement.Old == "" || replacement.Count < 1 {
					return item, errors.New("replacement requires nonempty old and positive exact count")
				}
				actual := strings.Count(text, replacement.Old)
				if actual != replacement.Count {
					expected := replacement.Count
					return item, &EditDiagnostic{Path: name, Code: "replacement_count", Replacement: index + 1, Expected: &expected, Actual: &actual, Message: "replacement count differs from plan"}
				}
				text = strings.ReplaceAll(text, replacement.Old, replacement.New)
			}
			data = []byte(text)
		}
		item.data = data
	}
	if !validText(item.data) || int64(len(item.data)) > s.options.MaxFileBytes {
		return item, errors.New("new content must be bounded UTF-8 text without NUL")
	}
	return item, nil
}
func (s *scope) stage(item *prepared) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	item.temporary = path.Join(path.Dir(item.edit.Path), ".tools-fs-"+hex.EncodeToString(nonce)+".tmp")
	file, err := s.root.OpenFile(item.temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(item.data); err == nil {
		err = file.Chmod(item.mode)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
func (s *scope) commit(item prepared) error {
	if item.edit.SHA256 == "absent" {
		if err := s.regularPath(item.edit.Path, true); err != nil {
			return err
		}
		return s.root.Link(item.temporary, item.edit.Path)
	}
	data, mode, err := s.read(item.edit.Path)
	if err != nil {
		return err
	}
	if digest(data) != item.edit.SHA256 || mode != item.mode {
		return errors.New("file changed before replacement; refresh the plan")
	}
	return s.root.Rename(item.temporary, item.edit.Path)
}
func Apply(ctx context.Context, options Options, plan Plan, execute bool) (ApplyResult, error) {
	return ApplyWithReport(ctx, options, plan, execute, false)
}
func ApplyWithReport(ctx context.Context, options Options, plan Plan, execute, report bool) (ApplyResult, error) {
	result := ApplyResult{Status: "planned", Files: []ApplyFile{}, Complete: true, ReportComplete: true}
	if len(plan.Groups) > 0 {
		return result, errors.New("groups are spec-only; use --spec to generate an expanded hash-bound plan")
	}
	if plan.Version != 1 || len(plan.Files) == 0 || len(plan.Files) > 200 {
		return result, errors.New("plan version must be 1 with 1–200 files")
	}
	s, err := openScope(options)
	if err != nil {
		return result, err
	}
	defer s.root.Close()
	if len(plan.Files) > s.options.MaxFiles {
		return result, errors.New("plan exceeds maximum selected files")
	}
	if execute {
		lock, err := s.root.OpenFile(".tools-fs-apply.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return result, fmt.Errorf("apply is locked or unavailable; inspect .tools-fs-apply.lock: %w", err)
		}
		defer func() { lock.Close(); s.root.Remove(".tools-fs-apply.lock") }()
	}
	items := []prepared{}
	seen := map[string]bool{}
	total := 0
	for _, edit := range plan.Files {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if total > 32<<20 {
			result.Files = append(result.Files, ApplyFile{Path: edit.Path, Status: "rejected", Error: "batch byte limit exceeded"})
			continue
		}
		item, err := s.prepare(edit)
		if seen[edit.Path] {
			err = errors.New("duplicate plan path")
		}
		seen[edit.Path] = true
		entry := ApplyFile{Path: edit.Path, Status: "validated"}
		total += len(item.data)
		if total > 32<<20 {
			err = errors.New("batch byte limit exceeded")
		}
		if err != nil {
			entry.Status = "rejected"
			entry.Error = err.Error()
			var diagnostic *EditDiagnostic
			if errors.As(err, &diagnostic) {
				entry.Diagnostic = diagnostic
			}
			result.Status = "blocked"
			result.Complete = false
		} else {
			entry.SHA256 = digest(item.data)
		}
		if err == nil && item.edit.SHA256 != "absent" {
			mode := uint32(item.mode)
			entry.BeforeMode = &mode
		}
		result.Files = append(result.Files, entry)
		items = append(items, item)
	}
	if report && result.Complete && !execute {
		for i, item := range items {
			_, result.Files[i].Ranges = changedRanges(textLines(item.original), textLines(item.data), 0)
		}
		payload, _ := json.Marshal(result)
		s.captureReport(result, nil, false, len(payload)+1 > s.options.MaxOutputBytes)
		selectApplyReport(&result, s.options)
		boundApplyReport(&result, s.options.MaxOutputBytes)
	}
	if !result.Complete || !execute {
		return result, nil
	}
	defer func() {
		for _, item := range items {
			if item.temporary != "" {
				s.root.Remove(item.temporary)
			}
		}
	}()
	for i := range items {
		if err := s.stage(&items[i]); err != nil {
			result.Status = "blocked"
			result.Complete = false
			result.Files[i].Status = "failed"
			result.Files[i].Error = err.Error()
			return result, nil
		}
	}
	result.Status = "applied"
	for i, item := range items {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			err = s.commit(item)
		}
		if err != nil {
			result.Status = "partial"
			result.Complete = false
			result.Files[i].Status = "failed"
			result.Files[i].Error = err.Error()
			for j := i + 1; j < len(items); j++ {
				result.Files[j].Status = "not_applied"
			}
			return result, nil
		}
		result.Files[i].Status = "applied"
		result.Applied++
		// Read back actual bytes and native permissions while the cooperative lock is held.
		actual, mode, verifyErr := s.read(item.edit.Path)
		if verifyErr == nil {
			actualMode := uint32(mode)
			result.Files[i].Mode = &actualMode
			if item.edit.SHA256 != "absent" {
				preserved := mode == item.mode
				result.Files[i].ModePreserved = &preserved
			}
		}
		if verifyErr == nil && (digest(actual) != digest(item.data) || item.edit.SHA256 != "absent" && mode != item.mode) {
			verifyErr = errors.New("read-back bytes or existing permissions differ from the prepared edit")
		}
		if verifyErr != nil {
			result.Status = "partial"
			result.Complete = false
			result.Files[i].Status = "verification_failed"
			result.Files[i].Error = verifyErr.Error()
			for j := i + 1; j < len(items); j++ {
				result.Files[j].Status = "not_applied"
			}
			return result, nil
		}
		result.Files[i].Verified = true
		result.Files[i].SHA256 = digest(actual)
		if report {
			_, result.Files[i].Ranges = changedRanges(textLines(item.original), textLines(actual), 0)
		}
	}
	payload, _ := json.Marshal(result)
	s.captureReport(result, nil, false, len(payload)+1 > s.options.MaxOutputBytes)
	selectApplyReport(&result, s.options)
	boundApplyReport(&result, s.options.MaxOutputBytes)
	return result, nil
}

func boundApplyReport(result *ApplyResult, limit int) {
	// Mutation status is always retained. Large changed-line reports may be omitted.
	for i := len(result.Files) - 1; i >= 0; i-- {
		payload, _ := json.Marshal(result)
		if len(payload) <= limit {
			return
		}
		if len(result.Files[i].Ranges) > 0 {
			result.Files[i].RangesOmitted = len(result.Files[i].Ranges)
			result.Files[i].Ranges = nil
			result.ReportComplete = false
		}
	}
	payload, _ := json.Marshal(result)
	if len(payload) > limit {
		result.ReportComplete = false
	}
}

func selectApplyReport(result *ApplyResult, options Options) {
	if options.ReportPattern == "" {
		return
	}
	result.ReportSelected = true
	for i := range result.Files {
		result.Files[i].Ranges = selectLineReportRanges(result.Files[i].Ranges, options.ReportPattern, options.ReportContext)
	}
}
