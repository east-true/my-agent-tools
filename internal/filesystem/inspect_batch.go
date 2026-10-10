package filesystem

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type ReadRequest struct {
	Path     string           `json:"path"`
	Ranges   []LineRange      `json:"ranges,omitempty"`
	Patterns []string         `json:"patterns,omitempty"`
	Regex    bool             `json:"regex,omitempty"`
	Hash     bool             `json:"hash,omitempty"`
	Context  *int             `json:"context,omitempty"`
	Raw      bool             `json:"raw,omitempty"`
	Outline  bool             `json:"outline,omitempty"`
	Sections []SectionRequest `json:"sections,omitempty"`
}
type ReadBatch struct {
	Version int           `json:"version"`
	Files   []ReadRequest `json:"files"`
}
type readCursor struct {
	Version   int    `json:"v"`
	Selection string `json:"s"`
	Position  int    `json:"p"`
}
type readUnit struct {
	file, line int
	text       string
	match      bool
	heading    *Heading
}

func inspectBatch(ctx context.Context, options InspectOptions) (Inspection, error) {
	result := Inspection{Status: "ok", Files: []File{}, Complete: true}
	if options.Context < 0 || options.Context > 1000 || options.Start < 0 || options.End < 0 || options.End > 0 && (options.Start < 1 || options.End < options.Start) {
		return result, errors.New("invalid context or line range")
	}
	if len(options.Requests) > 0 && (len(options.Paths) > 0 || options.Start > 0 || options.End > 0) {
		return result, errors.New("--request cannot be combined with --path or --range")
	}
	if options.Start > 0 && options.End == 0 {
		options.End = options.Start
	}
	requests := map[string]ReadRequest{}
	scopeOptions := options.Options
	if len(options.Requests) > 0 {
		if len(options.Requests) > 200 {
			return result, errors.New("batch supports at most 200 file requests")
		}
		scopeOptions.Paths = []string{}
		for _, r := range options.Requests {
			name, err := cleanPath(r.Path)
			if err != nil {
				return result, err
			}
			if _, ok := requests[name]; ok {
				return result, errors.New("duplicate batch request path")
			}
			requests[name] = r
			scopeOptions.Paths = append(scopeOptions.Paths, name)
		}
	}
	s, err := openScope(scopeOptions)
	if err != nil {
		return result, err
	}
	defer s.root.Close()
	names, problems, err := s.files(ctx)
	if err != nil {
		return result, err
	}
	sort.Strings(names)
	result.Problems = problems
	files := []File{}
	fingerprints := []string{}
	total := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		r := ReadRequest{Path: name, Patterns: options.Patterns, Regex: options.Regex, Hash: options.Hash, Raw: options.Raw, Outline: options.Outline, Sections: options.Sections}
		contextLines := options.Context
		if options.Start > 0 {
			r.Ranges = []LineRange{{options.Start, options.End}}
		}
		if q, ok := requests[name]; ok {
			r = q
			r.Path = name
			if r.Patterns == nil {
				r.Patterns = options.Patterns
			}
			r.Regex = r.Regex || options.Regex
			r.Hash = r.Hash || options.Hash
			r.Raw = r.Raw || options.Raw
			r.Outline = r.Outline || options.Outline
			if r.Sections == nil {
				r.Sections = options.Sections
			}
			if q.Context != nil {
				contextLines = *q.Context
			}
		}
		if contextLines < 0 || contextLines > 1000 {
			return result, errors.New("context must be 0–1000")
		}
		if len(r.Sections) > 0 && (len(r.Ranges) > 0 || len(r.Patterns) > 0 || contextLines != 0) {
			return result, errors.New("section selection cannot be combined with ranges, patterns or context")
		}
		for _, section := range r.Sections {
			if strings.TrimSpace(section.Title) == "" || section.Occurrence < 0 {
				return result, errors.New("section title must be nonempty and occurrence must be nonnegative")
			}
		}
		for _, span := range r.Ranges {
			if span.Start < 1 || span.End < span.Start {
				return result, errors.New("ranges must be positive and ordered")
			}
		}
		patterns := []*regexp.Regexp{}
		for _, pattern := range r.Patterns {
			if pattern == "" {
				return result, errors.New("pattern must not be empty")
			}
			if !r.Regex {
				pattern = regexp.QuoteMeta(pattern)
			}
			rx, e := regexp.Compile(pattern)
			if e != nil {
				return result, e
			}
			patterns = append(patterns, rx)
		}
		// 명시한 raw 파일은 바로 작업할 수 있는 원문을 반환한다.
		// 해시만 반환해 추가 범위 조회가 필요해지는 동작을 피한다.
		wholeRaw := r.Raw && !r.Outline && len(r.Sections) == 0 && len(r.Ranges) == 0 && len(patterns) == 0 && len(scopeOptions.Paths) > 0
		info, e := s.root.Lstat(name)
		if e != nil {
			result.Problems = append(result.Problems, Problem{name, e.Error()})
			continue
		}
		item := File{Path: name, Bytes: info.Size()}
		fingerprint := fmt.Sprintf("%s:%d:%d:%d", name, info.Size(), info.ModTime().UnixNano(), info.Mode())
		if len(patterns) > 0 || len(r.Ranges) > 0 || r.Hash || r.Outline || len(r.Sections) > 0 || wholeRaw {
			data, _, e := s.read(name)
			if e != nil {
				result.Problems = append(result.Problems, Problem{name, e.Error()})
				continue
			}
			total += len(data)
			if total > 32<<20 {
				result.Problems = append(result.Problems, Problem{name, "inspection total byte limit exceeded"})
				break
			}
			fingerprint = name + ":" + digest(data)
			item.Bytes = int64(len(data))
			if r.Hash {
				item.SHA256 = digest(data)
			}
			if len(patterns) > 0 || len(r.Ranges) > 0 || r.Outline || len(r.Sections) > 0 || wholeRaw {
				if !validText(data) {
					result.Problems = append(result.Problems, Problem{name, "not UTF-8 text; metadata-only inspection is available"})
					continue
				}
				lines := textLines(data)
				kind, final := newlineInfo(data)
				item.LineEnding = kind
				item.FinalNewline = &final
				if r.Outline || len(r.Sections) > 0 {
					headings := markdownHeadings(lines)
					selected, spans, e := markdownSections(headings, r.Sections)
					if e != nil {
						result.Problems = append(result.Problems, Problem{name, e.Error()})
						continue
					}
					if r.Outline {
						item.Headings = headings
					} else {
						item.Headings = selected
					}
					if len(r.Sections) > 0 {
						r.Ranges = spans
					}
				}
				chosen := []int{}
				for i, line := range lines {
					if r.Outline && len(r.Sections) == 0 && len(r.Ranges) == 0 && len(patterns) == 0 {
						break
					}
					within := len(r.Ranges) == 0
					for _, span := range r.Ranges {
						if i+1 >= span.Start && i+1 <= span.End {
							within = true
							break
						}
					}
					if !within {
						continue
					}
					if len(patterns) == 0 {
						chosen = append(chosen, i+1)
						continue
					}
					for _, rx := range patterns {
						if rx.MatchString(line) {
							item.Matches = append(item.Matches, i+1)
							chosen = append(chosen, i+1)
							break
						}
					}
				}
				if len(patterns) > 0 && len(chosen) == 0 {
					fingerprints = append(fingerprints, fingerprint)
					continue
				}
				item.Ranges = selectedRanges(lines, chosen, contextLines)
				if wholeRaw && len(lines) == 0 {
					text := ""
					item.Ranges = []Range{{Start: 1, Text: &text}}
				}
				if r.Raw {
					exact := rawLines(string(data))
					for i := range item.Ranges {
						span := &item.Ranges[i]
						text := strings.Join(exact[span.Start-1:span.Start-1+len(span.Lines)], "")
						span.Text = &text
						span.Lines = nil
					}
				}
			}
		}
		files = append(files, item)
		fingerprints = append(fingerprints, fingerprint)
	}
	// 커서 공간을 예약하기 전에 전체 결과가 상한 안에 들어가는지 확인한다.
	if options.Cursor == "" {
		full := result
		full.Files = files
		if len(full.Problems) > 0 {
			full.Status = "partial"
			full.Complete = false
		}
		payload, _ := json.Marshal(full)
		if len(payload)+1 <= s.options.MaxOutputBytes {
			return full, nil
		}
	}
	// 이어 읽기를 정규화한 루트·정확한 요청·선택한 실제 바이트에 연결한다.
	query := options
	query.Cursor = ""
	query.Root = s.name
	proof, _ := json.Marshal(struct {
		Query        InspectOptions
		Fingerprints []string
		Problems     []Problem
	}{query, fingerprints, result.Problems})
	selection := digest(proof)
	units := []readUnit{}
	for i, file := range files {
		for j := range file.Headings {
			units = append(units, readUnit{file: i, heading: &file.Headings[j]})
		}
		if len(file.Ranges) == 0 && len(file.Headings) == 0 {
			units = append(units, readUnit{file: i})
			continue
		}
		for _, span := range file.Ranges {
			lines := span.Lines
			if span.Text != nil {
				lines = rawLines(*span.Text)
			}
			if span.Text != nil && len(lines) == 0 {
				units = append(units, readUnit{file: i, line: span.Start})
			}
			for j, line := range lines {
				n := span.Start + j
				k := sort.SearchInts(file.Matches, n)
				units = append(units, readUnit{file: i, line: n, text: line, match: k < len(file.Matches) && file.Matches[k] == n})
			}
		}
	}
	start := 0
	if options.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(options.Cursor)
		if e != nil || len(raw) > 1024 {
			return result, errors.New("invalid inspection cursor")
		}
		var cursor readCursor
		if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Selection != selection || cursor.Position < 0 || cursor.Position >= len(units) {
			return result, errors.New("cursor query or files changed; restart inspection")
		}
		start = cursor.Position
	}
	for pos := start; pos < len(units); pos++ {
		u := units[pos]
		source := files[u.file]
		oldLen := len(result.Files)
		var oldFile File
		var oldRange Range
		if oldLen > 0 {
			oldFile = result.Files[oldLen-1]
			if len(oldFile.Ranges) > 0 {
				oldRange = oldFile.Ranges[len(oldFile.Ranges)-1]
			}
		}
		if len(result.Files) == 0 || result.Files[len(result.Files)-1].Path != source.Path {
			result.Files = append(result.Files, File{Path: source.Path, Bytes: source.Bytes, SHA256: source.SHA256, LineEnding: source.LineEnding, FinalNewline: source.FinalNewline})
		}
		file := &result.Files[len(result.Files)-1]
		if u.heading != nil {
			file.Headings = append(file.Headings, *u.heading)
		}
		if u.line > 0 {
			raw := len(source.Ranges) > 0 && source.Ranges[0].Text != nil
			count := 0
			if len(file.Ranges) > 0 {
				last := file.Ranges[len(file.Ranges)-1]
				count = len(last.Lines)
				if last.Text != nil {
					count = len(rawLines(*last.Text))
				}
			}
			if len(file.Ranges) > 0 && file.Ranges[len(file.Ranges)-1].Start+count == u.line {
				last := &file.Ranges[len(file.Ranges)-1]
				if raw {
					text := *last.Text + u.text
					last.Text = &text
				} else {
					last.Lines = append(last.Lines, u.text)
				}
			} else {
				span := Range{Start: u.line, Lines: []string{u.text}}
				if raw {
					text := u.text
					span.Text = &text
					span.Lines = nil
				}
				file.Ranges = append(file.Ranges, span)
			}
			if u.match {
				file.Matches = append(file.Matches, u.line)
			}
		}
		result.NextCursor = ""
		result.Complete = len(result.Problems) == 0
		result.Status = "ok"
		if pos+1 < len(units) {
			raw, _ := json.Marshal(readCursor{1, selection, pos + 1})
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			result.Status = "page"
			result.Complete = false
		}
		if len(result.Problems) > 0 {
			result.Status = "partial"
			result.Complete = false
		}
		payload, _ := json.Marshal(result)
		if len(payload)+1 > s.options.MaxOutputBytes {
			result.Files = result.Files[:oldLen]
			if oldLen > 0 {
				result.Files[oldLen-1] = oldFile
				if len(oldFile.Ranges) > 0 {
					result.Files[oldLen-1].Ranges[len(oldFile.Ranges)-1] = oldRange
				}
			}
			if pos == start {
				result.NextCursor = ""
				result.Status = "partial"
				result.Complete = false
				result.Problems = append(result.Problems, Problem{".", "output budget cannot fit a result row and cursor; increase --max-output-bytes"})
				return result, nil
			}
			raw, _ := json.Marshal(readCursor{1, selection, pos})
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			result.Complete = false
			if len(result.Problems) == 0 {
				result.Status = "page"
			}
			return result, nil
		}
	}
	if len(result.Problems) > 0 {
		result.Status = "partial"
		result.Complete = false
	}
	return result, nil
}
