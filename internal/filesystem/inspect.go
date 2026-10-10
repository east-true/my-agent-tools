package filesystem

import (
	"context"
	"strings"
	"unicode/utf8"
)

type InspectOptions struct {
	Options
	Patterns            []string
	Regex               bool
	Context, Start, End int
	Hash                bool
	Requests            []ReadRequest
	Cursor              string
	Raw                 bool
	Outline             bool
	Sections            []SectionRequest
}
type Range struct {
	Start int      `json:"start"`
	Lines []string `json:"lines,omitempty"`
	Text  *string  `json:"text,omitempty"`
}
type File struct {
	Path         string    `json:"path"`
	Bytes        int64     `json:"bytes"`
	SHA256       string    `json:"sha256,omitempty"`
	Matches      []int     `json:"matches,omitempty"`
	Ranges       []Range   `json:"ranges,omitempty"`
	LineEnding   string    `json:"line_ending,omitempty"`
	FinalNewline *bool     `json:"final_newline,omitempty"`
	Headings     []Heading `json:"headings,omitempty"`
}
type Inspection struct {
	Status     string    `json:"status"`
	Files      []File    `json:"files"`
	Problems   []Problem `json:"problems,omitempty"`
	Complete   bool      `json:"complete"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

func textLines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if len(data) == 0 {
		return []string{}
	}
	return strings.Split(s, "\n")
}
func validText(data []byte) bool { return utf8.Valid(data) && !strings.ContainsRune(string(data), 0) }
func selectedRanges(lines []string, matches []int, context int) []Range {
	out := []Range{}
	for _, line := range matches {
		start := max(1, line-context)
		end := min(len(lines), line+context)
		if len(out) > 0 {
			last := &out[len(out)-1]
			oldEnd := last.Start + len(last.Lines) - 1
			if start <= oldEnd+1 {
				if end > oldEnd {
					last.Lines = append(last.Lines, lines[oldEnd:end]...)
				}
				continue
			}
		}
		out = append(out, Range{Start: start, Lines: append([]string{}, lines[start-1:end]...)})
	}
	return out
}
func Inspect(ctx context.Context, options InspectOptions) (Inspection, error) {
	return inspectBatch(ctx, options)
}

func rawLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func newlineInfo(data []byte) (string, bool) {
	text := string(data)
	lf := strings.Count(text, "\n")
	crlf := strings.Count(text, "\r\n")
	kind := "none"
	if lf > 0 {
		kind = "lf"
		if crlf == lf {
			kind = "crlf"
		} else if crlf > 0 {
			kind = "mixed"
		}
	}
	return kind, strings.HasSuffix(text, "\n")
}
