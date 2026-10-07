package github

import (
	"strings"
	"testing"
)

func TestSpecRenderingPreservesLiteralCommands(t *testing.T) {
	command := "printf '%s\\n' \"$(touch should-not-exist)\" `literal`"
	body, err := (Spec{Title: "fix: preserve input", Summary: "입력을 보존합니다.", Changes: []string{"줄바꿈 보존"},
		Issue: 12, Verification: []Verification{{Command: command, Result: "not-run", Details: "미실행"}}}).Validate("pr", "ko")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{command, "## 검증", "not-run: 미실행", "Closes #12"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q: %s", want, body)
		}
	}
}

func TestSpecRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		spec Spec
		kind string
	}{
		{"non-english title", Spec{Title: "fix: 한글 제목", Body: "내용"}, "issue"},
		{"missing Korean prose", Spec{Title: "fix: good title", Summary: "English prose"}, "issue"},
		{"missing body", Spec{Title: "fix: good title"}, "issue"},
		{"mixed body", Spec{Title: "fix: good title", Body: "내용", Summary: "요약"}, "issue"},
		{"PR-only field", Spec{Title: "fix: good title", Body: "내용", Head: "fix/good-title"}, "issue"},
		{"issue-only field", Spec{Title: "fix: good title", Body: "내용", IssueType: "Bug"}, "pr"},
		{"unknown verification result", Spec{Title: "fix: good title", Summary: "내용", Verification: []Verification{{Command: "test", Result: "maybe"}}}, "pr"},
		{"missing failure reason", Spec{Title: "fix: good title", Summary: "내용", Verification: []Verification{{Command: "test", Result: "failed"}}}, "pr"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.spec.Validate(test.kind, "ko"); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestTitleCaseIsPreservedForIssuesAndPRs(t *testing.T) {
	for _, kind := range []string{"issue", "pr"} {
		for _, title := range []string{"Add API / OAuth!", "API"} {
			spec := Spec{Prefix: "feat", Title: title, Body: "기능 추가"}
			if err := spec.ApplyPrefix(); err != nil {
				t.Fatal(err)
			}
			if _, err := spec.Validate(kind, "ko"); err != nil {
				t.Fatalf("%s rejected %q: %v", kind, title, err)
			}
			if spec.Title != "feat: "+title {
				t.Fatalf("%s changed title: %q", kind, spec.Title)
			}
		}
	}
}

func TestJSONRejectsUnknownFieldsAndTrailingObjects(t *testing.T) {
	for _, input := range []string{`{"titel":"fix: typo"}`, `{} {}`, `{} garbage`, `null`, `[]`} {
		var spec Spec
		if err := Decode(strings.NewReader(input), &spec); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

func TestEnglishPolicyAndMissingVerification(t *testing.T) {
	body, err := (Spec{Title: "feat: add command", Summary: "Add a command."}).Validate("pr", "any")
	if err != nil || !strings.Contains(body, "Not run: no verification results supplied.") {
		t.Fatalf("body=%q error=%v", body, err)
	}
}

func TestPolicyOnlySelectsExistingMetadata(t *testing.T) {
	policy := DefaultPolicy()
	if err := policy.Merge(Policy{LabelMap: map[string][]string{"feat": {"type: feature", "enhancement"}}}); err != nil {
		t.Fatal(err)
	}
	disabled := false
	labels := []Label{{Name: "Enhancement"}}
	types := []IssueType{{Name: "Feature", Enabled: &disabled}, {Name: "Task"}}
	selected, err := policy.Select(Spec{Title: "feat: add command"}, labels, types, "issue")
	if err != nil || len(selected.Labels) != 1 || selected.Labels[0] != "Enhancement" || selected.IssueType != "Task" {
		t.Fatalf("selection=%v err=%v", selected, err)
	}
	empty := []string{}
	selected, err = policy.Select(Spec{Title: "feat: add command", Labels: &empty}, labels, types, "issue")
	if err != nil || len(selected.Labels) != 0 {
		t.Fatalf("explicit empty labels not honored: %v %v", selected, err)
	}
	missing := []string{"unknown"}
	if _, err := policy.Select(Spec{Title: "feat: add command", Labels: &missing}, labels, types, "issue"); err == nil {
		t.Fatal("nonexistent label accepted")
	}
	if _, err := policy.Select(Spec{Title: "feat: add command", IssueType: "Unknown"}, labels, types, "issue"); err == nil {
		t.Fatal("nonexistent issue type accepted")
	}
}

func TestRemoteParsing(t *testing.T) {
	for _, remote := range []string{"git@github.com:owner/repo.git", "https://github.com/owner/repo.git", "ssh://git@github.com/owner/repo.git"} {
		got, err := repoFromRemote(remote)
		if err != nil || got != "owner/repo" {
			t.Errorf("%s -> %q, %v", remote, got, err)
		}
	}
	if _, err := repoFromRemote("https://example.com/owner/repo.git"); err == nil {
		t.Fatal("unsupported host accepted")
	}
}
