package github

import (
	"fmt"
	"strings"
)

type SetupPlan struct {
	Repo                string   `json:"repo"`
	Config              Config   `json:"config"`
	AvailableLabels     []string `json:"available_labels"`
	AvailableIssueTypes []string `json:"available_issue_types"`
	Notes               []string `json:"notes,omitempty"`
}

// Setup proposes concrete existing names without changing the catalog or caller's policy.
func Setup(catalog Catalog, existing, overrides Policy, refresh bool) (SetupPlan, error) {
	plan := SetupPlan{Repo: catalog.Repository.Name, Notes: append([]string{}, catalog.Notes...), AvailableLabels: []string{}, AvailableIssueTypes: []string{}}
	policy := DefaultPolicy()
	if err := policy.Merge(existing); err != nil {
		return plan, err
	}
	if err := policy.Merge(overrides); err != nil {
		return plan, err
	}
	for _, label := range catalog.Labels {
		plan.AvailableLabels = append(plan.AvailableLabels, label.Name)
	}
	for _, kind := range catalog.IssueTypes {
		if kind.Enabled == nil || *kind.Enabled {
			plan.AvailableIssueTypes = append(plan.AvailableIssueTypes, kind.Name)
		}
	}
	proposed := Policy{BodyLanguage: policy.BodyLanguage, LabelMap: copyMappings(existing.LabelMap), IssueTypeMap: copyMappings(existing.IssueTypeMap)}
	for _, prefix := range Prefixes {
		selection, err := DefaultPolicy().Select(Spec{Title: prefix + ": setup"}, catalog.Labels, catalog.IssueTypes, "issue")
		if err != nil {
			return plan, err
		}
		if _, configured := proposed.LabelMap[prefix]; !configured || refresh {
			proposed.LabelMap[prefix] = append([]string{}, selection.Labels...)
		}
		if _, configured := proposed.IssueTypeMap[prefix]; !configured || refresh {
			proposed.IssueTypeMap[prefix] = []string{}
			if selection.IssueType != "" {
				proposed.IssueTypeMap[prefix] = []string{selection.IssueType}
			}
		}
	}
	for prefix, names := range overrides.LabelMap {
		resolved, err := resolveSetupNames(prefix, names, plan.AvailableLabels, "label")
		if err != nil {
			return plan, err
		}
		proposed.LabelMap[prefix] = resolved
	}
	for prefix, names := range overrides.IssueTypeMap {
		resolved, err := resolveSetupNames(prefix, names, plan.AvailableIssueTypes, "issue type")
		if err != nil {
			return plan, err
		}
		proposed.IssueTypeMap[prefix] = resolved
	}
	for _, prefix := range Prefixes {
		for _, mapping := range []struct {
			kind      string
			names     []string
			available []string
		}{
			{"label", proposed.LabelMap[prefix], plan.AvailableLabels}, {"issue type", proposed.IssueTypeMap[prefix], plan.AvailableIssueTypes},
		} {
			if len(mapping.names) == 0 {
				plan.Notes = append(plan.Notes, fmt.Sprintf("%s: no %s mapping; set explicitly or rerun with --refresh after updating GitHub", prefix, mapping.kind))
			}
			for _, name := range mapping.names {
				if match([]string{name}, mapping.available) == "" {
					plan.Notes = append(plan.Notes, fmt.Sprintf("%s: preserved %s candidate %q is not currently available", prefix, mapping.kind, name))
				}
			}
		}
	}
	plan.Config = Config{GitHub: proposed}
	return plan, nil
}

func copyMappings(source map[string][]string) map[string][]string {
	result := map[string][]string{}
	for prefix, names := range source {
		result[prefix] = append([]string{}, names...)
	}
	return result
}

func resolveSetupNames(prefix string, names, available []string, kind string) ([]string, error) {
	if !contains(Prefixes, prefix) {
		return nil, fmt.Errorf("unsupported prefix %q", prefix)
	}
	result := []string{}
	for _, requested := range names {
		name := match([]string{requested}, available)
		if name == "" {
			return nil, fmt.Errorf("%s %q is not available for prefix %s", kind, requested, prefix)
		}
		if !contains(result, name) {
			result = append(result, name)
		}
	}
	return result, nil
}

// ParseSetupMappings accepts repeated PREFIX=NAME candidates, or PREFIX= to disable.
func ParseSetupMappings(values []string) (map[string][]string, error) {
	mappings := map[string][]string{}
	for _, value := range values {
		prefix, name, ok := strings.Cut(value, "=")
		prefix, name = strings.TrimSpace(prefix), strings.TrimSpace(name)
		if !ok || !contains(Prefixes, prefix) {
			return nil, fmt.Errorf("expected supported PREFIX=NAME, got %q", value)
		}
		previous, exists := mappings[prefix]
		if exists && (name == "" || len(previous) == 0) {
			return nil, fmt.Errorf("cannot combine an empty mapping with other candidates for %s", prefix)
		}
		if name == "" {
			mappings[prefix] = []string{}
		} else {
			mappings[prefix] = append(previous, name)
		}
	}
	return mappings, nil
}
