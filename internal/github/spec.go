package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Verification struct {
	Command string `json:"command"`
	Result  string `json:"result"`
	Details string `json:"details,omitempty"`
}

// Spec contains authored facts. Verification commands are only rendered, never executed.
type Spec struct {
	Prefix       string         `json:"prefix,omitempty"`
	Title        string         `json:"title"`
	Body         string         `json:"body,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	Changes      []string       `json:"changes,omitempty"`
	Acceptance   []string       `json:"acceptance,omitempty"`
	Verification []Verification `json:"verification,omitempty"`
	Labels       *[]string      `json:"labels,omitempty"`
	IssueType    string         `json:"issue_type,omitempty"`
	Issue        int            `json:"issue,omitempty"`
	Base         string         `json:"base,omitempty"`
	Head         string         `json:"head,omitempty"`
	Draft        bool           `json:"draft,omitempty"`
}

func Decode(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return errors.New("expected a JSON object")
	}
	object := json.NewDecoder(bytes.NewReader(raw))
	object.DisallowUnknownFields()
	if err := object.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one JSON object")
	}
	return nil
}

var titlePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*: [\x20-\x7e]+$`)
var titleWord = regexp.MustCompile(`[A-Za-z0-9]`)

var Prefixes = []string{"feat", "fix", "refactor", "docs", "ci", "test", "chore", "build", "perf", "style", "revert"}

// ApplyPrefix adds only the selected prefix; authored title text is never rewritten.
func (spec *Spec) ApplyPrefix() error {
	prefix := spec.Prefix
	if existing, _, ok := strings.Cut(spec.Title, ": "); ok && contains(Prefixes, strings.ToLower(existing)) {
		if prefix != "" && prefix != existing {
			return errors.New("prefix conflicts with the prefix already in title")
		}
		if !contains(Prefixes, existing) {
			return errors.New("title prefix must be lowercase")
		}
		spec.Prefix = existing
		return nil
	}
	if !contains(Prefixes, prefix) {
		return fmt.Errorf("choose prefix: %s", strings.Join(Prefixes, ", "))
	}
	spec.Title = prefix + ": " + spec.Title
	return nil
}

func (spec Spec) Validate(kind string, language string) (string, error) {
	if !titlePattern.MatchString(spec.Title) || spec.Title != strings.TrimSpace(spec.Title) {
		return "", errors.New("title must be English '<type>: <summary>' with an ASCII summary")
	}
	_, summary, _ := strings.Cut(spec.Title, ": ")
	if summary != strings.TrimSpace(summary) {
		return "", errors.New("title summary must not have leading or trailing whitespace")
	}
	if !titleWord.MatchString(summary) {
		return "", errors.New("title summary must contain English letters or numbers")
	}
	if kind == "issue" && (spec.Issue != 0 || spec.Base != "" || spec.Head != "" || spec.Draft) {
		return "", errors.New("issue, base, head and draft are PR-only fields")
	}
	if kind == "pr" && (spec.IssueType != "" || len(spec.Acceptance) > 0) {
		return "", errors.New("issue_type and acceptance are issue-only fields")
	}
	if spec.Issue < 0 {
		return "", errors.New("issue must be a positive issue number")
	}
	if spec.Body != "" && (spec.Summary != "" || len(spec.Changes)+len(spec.Acceptance)+len(spec.Verification) > 0) {
		return "", errors.New("use body or structured summary/changes/acceptance/verification, not both")
	}
	for _, items := range [][]string{spec.Changes, spec.Acceptance} {
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				return "", errors.New("changes and acceptance entries must not be blank")
			}
		}
	}
	for _, check := range spec.Verification {
		if strings.TrimSpace(check.Command) == "" {
			return "", errors.New("verification command must not be blank")
		}
		switch check.Result {
		case "passed", "failed", "not-run":
		default:
			return "", errors.New("verification result must be passed, failed or not-run")
		}
		if check.Result != "passed" && strings.TrimSpace(check.Details) == "" {
			return "", errors.New("failed/not-run verification requires details")
		}
	}
	body := strings.TrimSpace(spec.Body)
	if body == "" {
		if strings.TrimSpace(spec.Summary) == "" {
			return "", errors.New("body or summary is required")
		}
		var rendered strings.Builder
		heading := "변경 요약"
		if kind == "issue" {
			heading = "배경"
		}
		if language == "any" {
			heading = "Summary"
		}
		fmt.Fprintf(&rendered, "## %s\n\n%s\n", heading, strings.TrimSpace(spec.Summary))
		section(&rendered, label(language, "작업 내용", "Changes"), spec.Changes, false)
		section(&rendered, label(language, "완료 기준", "Acceptance criteria"), spec.Acceptance, true)
		if kind == "pr" || len(spec.Verification) > 0 {
			fmt.Fprintf(&rendered, "\n## %s\n\n", label(language, "검증", "Verification"))
			if len(spec.Verification) == 0 {
				fmt.Fprintln(&rendered, label(language, "- 미실행: 검증 결과가 제공되지 않았습니다.", "- Not run: no verification results supplied."))
			}
			for _, check := range spec.Verification {
				// Fenced blocks preserve literal shell text, including backticks and substitutions.
				fence := "```"
				for strings.Contains(check.Command, fence) {
					fence += "`"
				}
				fmt.Fprintf(&rendered, "%sshell\n%s\n%s\n\n- %s", fence, check.Command, fence, check.Result)
				if check.Details != "" {
					fmt.Fprintf(&rendered, ": %s", check.Details)
				}
				fmt.Fprint(&rendered, "\n\n")
			}
		}
		body = strings.TrimSpace(rendered.String())
	}
	// Check authored prose rather than Korean headings generated by the renderer.
	prose := spec.Body + spec.Summary + strings.Join(spec.Changes, "") + strings.Join(spec.Acceptance, "")
	if language == "ko" && !hasHangul(prose) {
		return "", errors.New("body must contain Korean prose (configure body_language as 'any' for other projects)")
	}
	if kind == "pr" && spec.Issue > 0 {
		body += fmt.Sprintf("\n\nCloses #%d", spec.Issue)
	}
	return body, nil
}

func section(out *strings.Builder, title string, items []string, checklist bool) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "\n## %s\n\n", title)
	for _, item := range items {
		prefix := "- "
		if checklist {
			prefix = "- [ ] "
		}
		fmt.Fprintf(out, "%s%s\n", prefix, item)
	}
}

func label(language, korean, english string) string {
	if language == "any" {
		return english
	}
	return korean
}

func hasHangul(value string) bool {
	for _, r := range value {
		if r >= 0xAC00 && r <= 0xD7A3 || r >= 0x1100 && r <= 0x11FF || r >= 0x3130 && r <= 0x318F {
			return true
		}
	}
	return false
}
