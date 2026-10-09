package github

import (
	"fmt"
	"strings"
)

type Policy struct {
	BodyLanguage string              `json:"body_language,omitempty"`
	LabelMap     map[string][]string `json:"label_map,omitempty"`
	IssueTypeMap map[string][]string `json:"issue_type_map,omitempty"`
}

type Config struct {
	GitHub Policy `json:"github"`
}

func DefaultPolicy() Policy {
	return Policy{
		BodyLanguage: "ko",
		LabelMap: map[string][]string{
			"fix": {"fix", "bug"}, "feat": {"feat", "feature", "enhancement"},
			"docs": {"docs", "documentation"}, "refactor": {"refactor"},
			"ci": {"ci"}, "test": {"test", "tests"}, "chore": {"chore"},
			"build": {"build"}, "perf": {"perf", "performance"}, "style": {"style"}, "revert": {"revert"},
		},
		IssueTypeMap: map[string][]string{
			"fix": {"Bug", "Task"}, "feat": {"Feature", "Task"},
			"docs": {"Task"}, "refactor": {"Task"}, "ci": {"Task"},
			"test": {"Task"}, "chore": {"Task"},
		},
	}
}

func (policy *Policy) Merge(config Policy) error {
	if config.BodyLanguage != "" {
		if config.BodyLanguage != "ko" && config.BodyLanguage != "any" {
			return fmt.Errorf("body_language must be 'ko' or 'any'")
		}
		policy.BodyLanguage = config.BodyLanguage
	}
	for kind, names := range config.LabelMap {
		policy.LabelMap[kind] = names
	}
	for kind, names := range config.IssueTypeMap {
		policy.IssueTypeMap[kind] = names
	}
	return nil
}

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type IssueType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     *bool  `json:"is_enabled,omitempty"`
}

type Selection struct {
	Labels    []string `json:"labels"`
	IssueType string   `json:"issue_type,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
	Notes     []string `json:"notes,omitempty"`
}

func (policy Policy) Select(spec Spec, labels []Label, types []IssueType, kind string) (Selection, error) {
	workType, _, _ := strings.Cut(spec.Title, ":")
	var availableLabels, availableTypes []string
	for _, item := range labels {
		availableLabels = append(availableLabels, item.Name)
	}
	for _, item := range types {
		if item.Enabled == nil || *item.Enabled {
			availableTypes = append(availableTypes, item.Name)
		}
	}
	selection := Selection{Labels: []string{}}
	addLabel := func(name, reason string) {
		if name != "" && !contains(selection.Labels, name) {
			selection.Labels = append(selection.Labels, name)
			selection.Reasons = append(selection.Reasons, fmt.Sprintf("label %s: %s", name, reason))
		}
	}
	if spec.Labels != nil {
		for _, requested := range *spec.Labels {
			name := match([]string{requested}, availableLabels)
			if name == "" {
				return Selection{}, fmt.Errorf("label %q does not exist", requested)
			}
			addLabel(name, "explicit input")
		}
	} else if name := matchLabel(policy.LabelMap[workType], availableLabels); name != "" {
		addLabel(name, "title type "+workType+" matches existing name")
	} else {
		if len(selection.Labels) == 0 {
			selection.Notes = append(selection.Notes, "no matching existing work label; no label will be created")
		}
	}
	issueType := ""
	if kind == "issue" {
		if spec.IssueType != "" {
			issueType = match([]string{spec.IssueType}, availableTypes)
			if issueType == "" {
				return Selection{}, fmt.Errorf("issue type %q is not available", spec.IssueType)
			}
			selection.Reasons = append(selection.Reasons, "issue type "+issueType+": explicit input")
		} else {
			candidates, ok := policy.IssueTypeMap[workType]
			if !ok {
				candidates = []string{"Task"}
			}
			issueType = match(candidates, availableTypes)
			if issueType == "" {
				selection.Notes = append(selection.Notes, "no matching enabled issue type; no type will be created")
			} else {
				selection.Reasons = append(selection.Reasons, "issue type "+issueType+": prefix "+workType+" mapping")
			}
		}
	}
	selection.IssueType = issueType
	return selection, nil
}

// Common label namespaces still follow prefix mapping; no content classification.
func matchLabel(candidates, available []string) string {
	if found := match(candidates, available); found != "" {
		return found
	}
	for _, candidate := range candidates {
		for _, name := range available {
			for _, namespace := range []string{"type:", "type/", "kind:", "kind/", "category:"} {
				if strings.HasPrefix(strings.ToLower(name), namespace) && strings.EqualFold(strings.TrimSpace(name[len(namespace):]), candidate) {
					return name
				}
			}
		}
	}
	return ""
}

func match(candidates, available []string) string {
	for _, candidate := range candidates {
		for _, name := range available {
			if strings.EqualFold(candidate, name) {
				return name
			}
		}
	}
	return ""
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
