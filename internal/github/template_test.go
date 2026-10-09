package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestExistingTemplateIsFoundAndAppliedAutomatically(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"repository":{"templates":[{"filename":"feature_request.md","body":"<!-- instruction -->\n## 설명\n\n## 변경 내용\n\n## 완료 기준"}]}}}`)
			return
		}
		commonResponse(w, r)
	})
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "issue", Spec{Prefix: "feat", Title: "add feature", Summary: "기능 추가", Changes: []string{"변경 사항"}, Acceptance: []string{"완료 조건"}}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Payload["body"].(string)
	for _, want := range []string{"## 설명", "기능 추가", "## 변경 내용", "- 변경 사항", "## 완료 기준", "- [ ] 완료 조건"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "instruction") || strings.Contains(body, "## 배경") || len(f.writes()) != 0 || plan.Template != "feature_request.md" {
		t.Fatalf("template not applied correctly: %+v", plan)
	}
}

func TestPrefixMappingDoesNotClassifyBody(t *testing.T) {
	selection, err := DefaultPolicy().Select(Spec{Title: "fix: handle login", Body: "인증과 DB를 수정하며 테스트합니다."}, []Label{{Name: "type: bug"}, {Name: "database"}, {Name: "auth"}}, []IssueType{{Name: "Bug"}, {Name: "Feature"}}, "issue")
	if err != nil || len(selection.Labels) != 1 || selection.Labels[0] != "type: bug" || selection.IssueType != "Bug" {
		t.Fatalf("unexpected selection: %+v error=%v", selection, err)
	}
}

func TestPrefixSelectionPreservesAuthoredTitle(t *testing.T) {
	spec := Spec{Prefix: "feat", Title: "Add API / Login!"}
	if err := spec.ApplyPrefix(); err != nil {
		t.Fatal(err)
	}
	if spec.Title != "feat: Add API / Login!" {
		t.Fatal(spec.Title)
	}
	if err := spec.ApplyPrefix(); err != nil || spec.Title != "feat: Add API / Login!" {
		t.Fatalf("prefix applied twice: %+v error=%v", spec, err)
	}
	for _, spec := range []Spec{{Prefix: "feat", Title: "fix: login"}, {Prefix: "unknown", Title: "login"}} {
		if err := spec.ApplyPrefix(); err == nil {
			t.Fatalf("accepted %+v", spec)
		}
	}
}

func TestTitleSymbolsBecomeBranchSlug(t *testing.T) {
	spec := Spec{Prefix: "feat", Title: "[API] Add OAuth / Login! (#4)", Body: "기능 추가"}
	if err := spec.ApplyPrefix(); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Validate("issue", "ko"); err != nil {
		t.Fatal(err)
	}
	kind, slug, err := branchParts(spec.Title)
	if err != nil || kind != "feat" || slug != "api-add-oauth-login-4" {
		t.Fatalf("kind=%s slug=%s err=%v", kind, slug, err)
	}
	if spec.Title != "feat: [API] Add OAuth / Login! (#4)" {
		t.Fatalf("branch derivation changed title: %s", spec.Title)
	}
}

func TestInvalidTitlesAreRejectedWithoutRewriting(t *testing.T) {
	for _, title := range []string{"로그인 추가", "add 로그인", "add café", " add login", "add login "} {
		t.Run(title, func(t *testing.T) {
			spec := Spec{Prefix: "feat", Title: title, Body: "기능 추가"}
			if err := spec.ApplyPrefix(); err != nil {
				t.Fatal(err)
			}
			if _, err := spec.Validate("issue", "ko"); err == nil {
				t.Fatalf("accepted invalid title %q", title)
			}
			if spec.Title != "feat: "+title {
				t.Fatalf("rewrote authored title: %q", spec.Title)
			}
		})
	}
}

func TestTemplatePreservesFencedVerificationCommand(t *testing.T) {
	command := "printf '%s' '\n# literal comment\n'"
	source := "## 검증\n\n```shell\n" + command + "\n```\n\n- passed"
	body := applyTemplate(Template{Body: "## 테스트"}, source)
	if !strings.Contains(body, command) || !strings.Contains(body, "```shell") {
		t.Fatalf("command changed: %s", body)
	}
}
