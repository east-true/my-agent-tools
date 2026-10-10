package filesystem

import (
	"strings"
)

// selectRawReportRanges는 원래 개행과 위치를 유지한 명시적 원문 선택이다.
func selectRawReportRanges(text, pattern string, contextLines int) []Range {
	lines := rawLines(text)
	keep := make([]bool, len(lines))
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			for j := max(0, i-contextLines); j <= min(len(lines)-1, i+contextLines); j++ {
				keep[j] = true
			}
		}
	}
	result := []Range{}
	for i := 0; i < len(lines); {
		if !keep[i] {
			i++
			continue
		}
		first := i
		for i < len(lines) && keep[i] {
			i++
		}
		text := strings.Join(lines[first:i], "")
		result = append(result, Range{Start: first + 1, Text: &text})
	}
	return result
}

func selectLineReportRanges(ranges []Range, pattern string, contextLines int) []Range {
	result := []Range{}
	for _, span := range ranges {
		matches := []int{}
		for i, line := range span.Lines {
			if strings.Contains(line, pattern) {
				matches = append(matches, i+1)
			}
		}
		for _, selected := range selectedRanges(span.Lines, matches, contextLines) {
			selected.Start += span.Start - 1
			result = append(result, selected)
		}
	}
	return result
}
