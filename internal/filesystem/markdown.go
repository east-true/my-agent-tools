package filesystem

import (
	"fmt"
	"regexp"
	"strings"
)

// Heading의 위치는 원본 파일 기준이며 End는 하위 섹션을 포함한다.
type Heading struct {
	Title      string `json:"title"`
	Level      int    `json:"level"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Occurrence int    `json:"occurrence"`
}

type SectionRequest struct {
	Title      string `json:"title"`
	Occurrence int    `json:"occurrence,omitempty"`
}

var htmlBlockStart = regexp.MustCompile(`(?i)^</?(address|article|aside|base|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h[1-6]|head|header|hr|html|iframe|legend|li|link|main|menu|nav|ol|p|param|section|summary|table|tbody|td|tfoot|th|thead|title|tr|ul)(?:[\s/>]|$)`)

// markdownHeadings는 ATX와 단일 행 Setext 제목을 찾는다.
// 코드 블록과 인식한 HTML 블록은 제외하며 inline markup을 렌더링하지 않는다.
// 인용문·목록 내부와 여러 행 Setext를 해석하는 전체 Markdown 파서는 아니다.
func markdownHeadings(lines []string) []Heading {
	result := []Heading{}
	occurrences := map[string]int{}
	stack := []int{}
	fence := byte(0)
	fenceLength := 0
	previousParagraph := false
	paragraphStart := -1
	htmlEnd := ""
	htmlBlank := false
	add := func(title string, level, start int) {
		for len(stack) > 0 && result[stack[len(stack)-1]].Level >= level {
			result[stack[len(stack)-1]].End = start - 1
			stack = stack[:len(stack)-1]
		}
		occurrences[title]++
		stack = append(stack, len(result))
		result = append(result, Heading{title, level, start, len(lines), occurrences[title]})
	}
	for i, line := range lines {
		lower := strings.ToLower(strings.TrimLeft(line, " "))
		if htmlEnd != "" {
			if strings.Contains(lower, htmlEnd) {
				htmlEnd = ""
			}
			previousParagraph = false
			continue
		}
		if htmlBlank {
			if strings.TrimSpace(line) == "" {
				htmlBlank = false
			}
			previousParagraph = false
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent >= 4 || strings.HasPrefix(line, "\t") {
			previousParagraph = false
			continue
		}
		text := strings.TrimLeft(line, " ")
		if fence != 0 {
			n := 0
			for n < len(text) && text[n] == fence {
				n++
			}
			if n >= fenceLength && strings.TrimSpace(text[n:]) == "" {
				fence = 0
			}
			previousParagraph = false
			continue
		}
		if strings.HasPrefix(lower, "<!--") {
			if !strings.Contains(lower, "-->") {
				htmlEnd = "-->"
			}
			previousParagraph = false
			continue
		}
		for _, tag := range []string{"script", "style", "pre", "textarea"} {
			if strings.HasPrefix(lower, "<"+tag) && len(lower) > len(tag)+1 && strings.ContainsAny(lower[len(tag)+1:len(tag)+2], " \t>") {
				if !strings.Contains(lower, "</"+tag+">") {
					htmlEnd = "</" + tag + ">"
				}
				previousParagraph = false
				break
			}
		}
		if htmlEnd != "" {
			continue
		}
		if htmlBlockStart.MatchString(lower) {
			htmlBlank = true
			previousParagraph = false
			continue
		}
		if len(text) > 0 && (text[0] == '`' || text[0] == '~') {
			n := 0
			for n < len(text) && text[n] == text[0] {
				n++
			}
			if n >= 3 && (text[0] != '`' || !strings.Contains(text[n:], "`")) {
				fence, fenceLength = text[0], n
				previousParagraph = false
				continue
			}
		}
		n := 0
		for n < len(text) && text[n] == '#' {
			n++
		}
		if n >= 1 && n <= 6 && (n == len(text) || text[n] == ' ' || text[n] == '\t') {
			title := strings.TrimSpace(text[n:])
			end := len(title)
			for end > 0 && title[end-1] == '#' {
				end--
			}
			if end < len(title) && (end == 0 || title[end-1] == ' ' || title[end-1] == '\t') {
				title = strings.TrimSpace(title[:end])
			}
			add(title, n, i+1)
			previousParagraph = false
			continue
		}
		trim := strings.TrimSpace(text)
		if previousParagraph && trim != "" && (strings.Trim(trim, "=") == "" || strings.Trim(trim, "-") == "") {
			level := 1
			if trim[0] == '-' {
				level = 2
			}
			if paragraphStart == i-1 {
				add(strings.TrimSpace(lines[i-1]), level, i)
			}
			previousParagraph = false
			continue
		}
		paragraph := trim != "" && !strings.HasPrefix(trim, ">") && !strings.HasPrefix(trim, "<") &&
			!strings.HasPrefix(trim, "- ") && !strings.HasPrefix(trim, "* ") && !strings.HasPrefix(trim, "+ ") &&
			strings.Trim(trim, "-*_ ") != ""
		if paragraph && !previousParagraph {
			paragraphStart = i
		}
		previousParagraph = paragraph
	}
	return result
}

func markdownSections(headings []Heading, requests []SectionRequest) ([]Heading, []LineRange, error) {
	selected := []Heading{}
	spans := []LineRange{}
	seen := map[int]bool{}
	for _, request := range requests {
		matches := []Heading{}
		for _, heading := range headings {
			if heading.Title == request.Title && (request.Occurrence == 0 || heading.Occurrence == request.Occurrence) {
				matches = append(matches, heading)
			}
		}
		if len(matches) != 1 {
			return nil, nil, fmt.Errorf("section %q matches %d headings; select a unique title or explicit occurrence in --request", request.Title, len(matches))
		}
		if !seen[matches[0].Start] {
			seen[matches[0].Start] = true
			selected = append(selected, matches[0])
			spans = append(spans, LineRange{Start: matches[0].Start, End: matches[0].End})
		}
	}
	return selected, spans, nil
}
