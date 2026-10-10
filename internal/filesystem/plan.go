package filesystem

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CountTarget explicitly authorizes recounting one replacement in the input spec.
// Both positions are 1-based; all other counts remain strict.
type CountTarget struct {
	File        int
	Replacement int
}

// GeneratePlan captures exact original-byte hashes without asking the caller to copy them.
// Creation remains explicit ("absent"); an unexpectedly missing edit never becomes a creation.
func GeneratePlan(ctx context.Context, options Options, spec Plan) (Plan, error) {
	plan, _, err := GeneratePlanWithRecounts(ctx, options, spec, nil)
	return plan, err
}

func GeneratePlanWithRecounts(ctx context.Context, options Options, spec Plan, targets []CountTarget) (Plan, []EditDiagnostic, error) {
	var err error
	spec, err = expandSpec(spec)
	if err != nil {
		return Plan{}, nil, err
	}
	selected := map[[2]int]bool{}
	selectedFiles := map[int]bool{}
	for _, target := range targets {
		if target.File < 1 || target.File > len(spec.Files) {
			return Plan{}, nil, errors.New("recount file index is outside the spec")
		}
		edit := spec.Files[target.File-1]
		if target.Replacement < 1 || target.Replacement > len(edit.Replacements) || edit.SHA256 == "absent" || edit.Content != nil {
			return Plan{}, nil, editError(edit.Path, fmt.Errorf("recount replacement %d must select an existing-file replacement", target.Replacement))
		}
		key := [2]int{target.File - 1, target.Replacement - 1}
		if selected[key] {
			return Plan{}, nil, editError(edit.Path, errors.New("duplicate recount target"))
		}
		selected[key] = true
		selectedFiles[target.File-1] = true
	}
	s, err := openScope(options)
	if err != nil {
		return Plan{}, nil, err
	}
	defer s.root.Close()
	plan := Plan{Version: 1, Files: append([]Edit{}, spec.Files...)}
	seen := map[string]bool{}
	total := 0
	sourceBytes := 0
	corrections := []EditDiagnostic{}
	diagnostics := &EditDiagnostics{}
	collect := func(name string, err error) {
		var diagnostic *EditDiagnostic
		if errors.As(editError(name, err), &diagnostic) {
			diagnostics.Diagnostics = append(diagnostics.Diagnostics, *diagnostic)
		}
	}
	if len(plan.Files) > s.options.MaxFiles {
		return Plan{}, nil, errors.New("spec exceeds maximum selected files")
	}
	for i := range plan.Files {
		if err := ctx.Err(); err != nil {
			return Plan{}, nil, err
		}
		edit := plan.Files[i]
		name, err := cleanPath(edit.Path)
		if err != nil {
			collect(edit.Path, err)
			continue
		}
		if seen[name] {
			collect(name, errors.New("duplicate spec path"))
			continue
		}
		seen[name] = true
		size := 0
		fileCorrections := []EditDiagnostic{}
		err = func() error {
			if edit.SHA256 == "" {
				data, _, err := s.read(name)
				if err != nil {
					return err
				}
				sourceBytes += len(data)
				if sourceBytes > 32<<20 {
					return errors.New("batch source byte limit exceeded")
				}
				edit.SHA256 = digest(data)
				if selectedFiles[i] {
					if !validText(data) {
						return errors.New("recount requires UTF-8 text")
					}
					edit.Replacements = append([]Replacement{}, edit.Replacements...)
					text := string(data)
					for j := range edit.Replacements {
						replacement := &edit.Replacements[j]
						if replacement.Old == "" || replacement.Count < 1 {
							return errors.New("replacement requires nonempty old and positive exact count")
						}
						actual := strings.Count(text, replacement.Old)
						if actual != replacement.Count {
							expected := replacement.Count
							diagnostic := EditDiagnostic{Path: name, Code: "replacement_count", Replacement: j + 1, Expected: &expected, Actual: &actual, Message: "replacement count differs from plan"}
							if !selected[[2]int{i, j}] || actual == 0 {
								return &diagnostic
							}
							fileCorrections = append(fileCorrections, diagnostic)
							replacement.Count = actual
						}
						text = strings.ReplaceAll(text, replacement.Old, replacement.New)
					}
				}
			} else if edit.SHA256 != "absent" {
				return errors.New("spec hashes must be omitted for existing files or absent for explicit creation; use --plan for expected hashes")
			}
			item, err := s.prepare(edit)
			if err != nil {
				return err
			}
			size = len(item.data)
			return nil
		}()
		if sourceBytes > 32<<20 {
			return Plan{}, nil, err
		}
		if err != nil {
			collect(name, err)
			continue
		}
		plan.Files[i] = edit
		corrections = append(corrections, fileCorrections...)
		total += size
		if total > 32<<20 {
			return Plan{}, nil, errors.New("batch byte limit exceeded")
		}
	}
	if len(diagnostics.Diagnostics) > 0 {
		return Plan{}, nil, diagnostics
	}
	return plan, corrections, nil
}

func expandSpec(spec Plan) (Plan, error) {
	if spec.Version != 1 || len(spec.Files) > 200 || len(spec.Groups) > 200 {
		return Plan{}, errors.New("spec version must be 1 with 1–200 expanded files")
	}
	result := Plan{Version: 1, Files: append([]Edit{}, spec.Files...)}
	for _, group := range spec.Groups {
		if len(group.Paths) == 0 || len(group.Replacements) == 0 || len(group.Paths) > 200-len(result.Files) {
			return Plan{}, errors.New("each group requires paths and replacements; spec supports at most 200 expanded files")
		}
		for _, name := range group.Paths {
			result.Files = append(result.Files, Edit{Path: name, Replacements: append([]Replacement{}, group.Replacements...)})
		}
	}
	if len(result.Files) == 0 {
		return Plan{}, errors.New("spec version must be 1 with 1–200 expanded files")
	}
	return result, nil
}
