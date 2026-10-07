package github

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

type Template struct {
	Filename string `json:"filename"`
	Name     string `json:"name,omitempty"`
	Body     string `json:"body,omitempty"`
}

func (client Client) templates(ctx context.Context, repo, kind, prefix string) (*Template, error) {
	owner, name, _ := strings.Cut(repo, "/")
	field := "issueTemplates"
	if kind == "pr" {
		field = "pullRequestTemplates"
	}
	query := fmt.Sprintf(`query RepositoryTemplates($owner:String!,$name:String!){repository(owner:$owner,name:$name){templates:%s{filename body}}}`, field)
	var response struct {
		graphErrors
		Data struct {
			Repository *struct {
				Templates []Template `json:"templates"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"owner": owner, "name": name}}, &response); err != nil {
		return nil, fmt.Errorf("read existing Markdown templates: %w", err)
	}
	if err := response.err(); err != nil {
		return nil, err
	}
	if response.Data.Repository == nil {
		return nil, errors.New("GitHub returned no repository when reading templates")
	}
	available := response.Data.Repository.Templates
	candidates := []string{prefix}
	candidates = append(candidates, DefaultPolicy().LabelMap[prefix]...)
	for _, candidate := range candidates {
		for i := range available {
			filename := strings.ToLower(path.Base(available[i].Filename))
			words := strings.FieldsFunc(filename, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
			if contains(words, candidate) {
				return &available[i], nil
			}
		}
	}
	for i := range available {
		filename := strings.ToLower(path.Base(available[i].Filename))
		if filename == "issue_template.md" || filename == "pull_request_template.md" || filename == "default.md" {
			return &available[i], nil
		}
	}
	if len(available) == 1 && kind == "pr" {
		return &available[0], nil
	}
	return nil, nil
}

type bodySection struct{ heading, content string }

func bodySections(body string) []bodySection {
	var sections []bodySection
	current := bodySection{}
	var fence byte
	fenceLength := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			count := 0
			for count < len(trimmed) && trimmed[count] == trimmed[0] {
				count++
			}
			if fence == 0 && count >= 3 {
				fence, fenceLength = trimmed[0], count
			} else if fence == trimmed[0] && count >= fenceLength && strings.TrimSpace(trimmed[count:]) == "" {
				fence, fenceLength = 0, 0
			}
			current.content += line + "\n"
			continue
		}
		if fence == 0 && regexp.MustCompile(`^#{1,6} `).MatchString(line) {
			if current.heading != "" || strings.TrimSpace(current.content) != "" {
				sections = append(sections, current)
			}
			current = bodySection{heading: line}
		} else {
			current.content += line + "\n"
		}
	}
	if current.heading != "" || strings.TrimSpace(current.content) != "" {
		sections = append(sections, current)
	}
	return sections
}

func sectionKey(heading string) string {
	name := strings.ToLower(strings.TrimSpace(strings.TrimLeft(heading, "#")))
	for key, names := range map[string][]string{
		"summary":      {"summary", "description", "배경", "변경 요약", "요약", "설명"},
		"changes":      {"changes", "작업 내용", "변경 내용", "구현 내용"},
		"acceptance":   {"acceptance criteria", "완료 기준"},
		"verification": {"verification", "test plan", "검증", "테스트"},
	} {
		for _, candidate := range names {
			if name == candidate {
				return key
			}
		}
	}
	return name
}

// Reuse existing Markdown headings while retaining every authored section.
func applyTemplate(template Template, body string) string {
	format := strings.TrimSpace(regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(template.Body, ""))
	if format == "" {
		return body
	}
	if strings.Contains(format, "{{body}}") {
		return strings.ReplaceAll(format, "{{body}}", body)
	}
	source := bodySections(body)
	used := make([]bool, len(source))
	var out strings.Builder
	for index, section := range bodySections(format) {
		fmt.Fprintln(&out, section.heading)
		content := strings.TrimSpace(section.content)
		if content != "" {
			fmt.Fprintf(&out, "\n%s\n", content)
		}
		for i, authored := range source {
			if !used[i] && (sectionKey(section.heading) == sectionKey(authored.heading) || (index == 0 && authored.heading == "")) {
				fmt.Fprintf(&out, "\n%s\n", strings.TrimSpace(authored.content))
				used[i] = true
			}
		}
		fmt.Fprintln(&out)
	}
	for i, section := range source {
		if !used[i] {
			fmt.Fprintf(&out, "%s\n\n%s\n\n", section.heading, strings.TrimSpace(section.content))
		}
	}
	return strings.TrimSpace(out.String())
}
