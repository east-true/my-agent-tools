package github

import (
	"reflect"
	"testing"
)

func setupCatalog() Catalog {
	disabled := false
	return Catalog{Repository: Repository{Name: "owner/repo"},
		Labels:     []Label{{Name: "type: bug"}, {Name: "type: feature"}, {Name: "documentation"}, {Name: "team-feature"}},
		IssueTypes: []IssueType{{Name: "Feature"}, {Name: "Task"}, {Name: "Bug", Enabled: &disabled}}}
}

func TestSetupImportsConcreteExistingNamesAndEnabledTypes(t *testing.T) {
	plan, err := Setup(setupCatalog(), Policy{}, Policy{}, false)
	if err != nil {
		t.Fatal(err)
	}
	policy := plan.Config.GitHub
	if policy.BodyLanguage != "ko" || !reflect.DeepEqual(policy.LabelMap["fix"], []string{"type: bug"}) || !reflect.DeepEqual(policy.LabelMap["feat"], []string{"type: feature"}) {
		t.Fatal(policy)
	}
	if !reflect.DeepEqual(policy.IssueTypeMap["fix"], []string{"Task"}) || !reflect.DeepEqual(policy.IssueTypeMap["feat"], []string{"Feature"}) {
		t.Fatal("disabled type selected", policy)
	}
	if labels, ok := policy.LabelMap["ci"]; !ok || len(labels) != 0 || labels == nil {
		t.Fatal("unmatched prefix is not explicitly disabled", policy)
	}
	if len(plan.AvailableLabels) != 4 || len(plan.AvailableIssueTypes) != 2 {
		t.Fatal(plan)
	}
}

func TestSetupPreservesExistingChoicesAndRefreshIsExplicit(t *testing.T) {
	existing := Policy{BodyLanguage: "any", LabelMap: map[string][]string{"feat": {"team-feature"}, "fix": {}}, IssueTypeMap: map[string][]string{"feat": {"Task"}}}
	plan, err := Setup(setupCatalog(), existing, Policy{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Config.GitHub.BodyLanguage != "any" || !reflect.DeepEqual(plan.Config.GitHub.LabelMap["feat"], []string{"team-feature"}) || len(plan.Config.GitHub.LabelMap["fix"]) != 0 {
		t.Fatal("preferences overwritten", plan)
	}
	plan.Config.GitHub.LabelMap["feat"][0] = "modified"
	if existing.LabelMap["feat"][0] != "team-feature" {
		t.Fatal("planning mutated caller config")
	}
	refreshed, err := Setup(setupCatalog(), existing, Policy{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Config.GitHub.BodyLanguage != "any" || !reflect.DeepEqual(refreshed.Config.GitHub.LabelMap["feat"], []string{"type: feature"}) {
		t.Fatal(refreshed)
	}
}

func TestSetupOverridesValidateNamesPreserveOrderAndCanonicalCase(t *testing.T) {
	overrides := Policy{LabelMap: map[string][]string{"feat": {"TEAM-FEATURE", "Type: Feature", "team-feature"}}, IssueTypeMap: map[string][]string{"feat": {"task", "feature"}}}
	plan, err := Setup(setupCatalog(), Policy{}, overrides, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Config.GitHub.LabelMap["feat"], []string{"team-feature", "type: feature"}) || !reflect.DeepEqual(plan.Config.GitHub.IssueTypeMap["feat"], []string{"Task", "Feature"}) {
		t.Fatal(plan)
	}
	for _, override := range []Policy{{LabelMap: map[string][]string{"feat": {"missing"}}}, {IssueTypeMap: map[string][]string{"fix": {"Bug"}}}, {LabelMap: map[string][]string{"unknown": {"team-feature"}}}} {
		if _, err := Setup(setupCatalog(), Policy{}, override, false); err == nil {
			t.Fatal("invalid override accepted", override)
		}
	}
}

func TestSetupKeepsMissingUserChoicesVisible(t *testing.T) {
	plan, err := Setup(setupCatalog(), Policy{LabelMap: map[string][]string{"feat": {"old-label"}}}, Policy{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Config.GitHub.LabelMap["feat"][0] != "old-label" {
		t.Fatal("old mapping silently dropped")
	}
	found := false
	for _, note := range plan.Notes {
		if note == `feat: preserved label candidate "old-label" is not currently available` {
			found = true
		}
	}
	if !found {
		t.Fatal("unavailable existing choice was hidden", plan.Notes)
	}
}

func TestSetupMappingFlagsSupportRepeatsAndExplicitEmpty(t *testing.T) {
	parsed, err := ParseSetupMappings([]string{"feat=team-feature", "feat=type: feature", "fix="})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed["feat"], []string{"team-feature", "type: feature"}) || len(parsed["fix"]) != 0 {
		t.Fatal(parsed)
	}
	for _, values := range [][]string{{"feat"}, {"unknown=bug"}, {"feat=", "feat=enhancement"}, {"feat=enhancement", "feat="}} {
		if _, err := ParseSetupMappings(values); err == nil {
			t.Fatal("invalid mapping accepted", values)
		}
	}
}
