package github

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type CIOccurrence struct {
	JobID      int64  `json:"job_id"`
	JobName    string `json:"job_name"`
	StepNumber int    `json:"step_number"`
	StepName   string `json:"step_name"`
	URL        string `json:"url"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type CIDiagnostic struct {
	Path         string `json:"path"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	Message      string `json:"message"`
	EvidenceLine int    `json:"evidence_line"`
}

type CIEvidence struct {
	Kind              string          `json:"kind"`
	Truncated         bool            `json:"truncated,omitempty"`
	Diagnostics       []CIDiagnostic  `json:"diagnostics,omitempty"`
	ExpectedArguments map[string]int  `json:"expected_arguments,omitempty"`
	MissingMethods    []string        `json:"missing_interface_methods,omitempty"`
	ExitCodes         []int           `json:"exit_codes,omitempty"`
	Lines             []string        `json:"lines,omitempty"`
	Notice            string          `json:"notice,omitempty"`
	Occurrences       []CIOccurrence  `json:"occurrences"`
	Tests             []CITestFailure `json:"tests,omitempty"`
	LogVariants       []CILogVariant  `json:"log_variants,omitempty"`
}

type CITestFailure struct {
	Framework    string `json:"framework"`
	Name         string `json:"name"`
	Path         string `json:"path,omitempty"`
	Message      string `json:"message,omitempty"`
	EvidenceLine int    `json:"evidence_line"`
}

type CILogVariant struct {
	Lines       []string       `json:"lines"`
	Occurrences []CIOccurrence `json:"occurrences"`
}

var (
	ciANSI       = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	ciTimestamp  = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\S+\s*`)
	ciLogTime    = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\S+[ \t]`)
	ciGoLocation = regexp.MustCompile(`^(?:##\[error\])?(.+\.go):(\d+):(\d+):\s*(.+)$`)
	ciCall       = regexp.MustCompile(`in call to ([A-Za-z_][A-Za-z0-9_./]*)`)
	ciMissing    = regexp.MustCompile(`missing method ([A-Za-z_][A-Za-z0-9_]*)`)
	ciExit       = regexp.MustCompile(`Process completed with exit code (-?\d+)\.`)
	ciError      = regexp.MustCompile(`(?i)(^--- FAIL:|^FAIL(?:\s|$)|^panic:|^fatal:|^Traceback|^.*\.(?:go|py|ts|tsx|js|rs|c|cpp):\d+(?::\d+)?:\s|^.*(?:AssertionError|ModuleNotFoundError|ImportError|SyntaxError|TypeError|ReferenceError):|^npm ERR!|^##\[error\]|^Error:)`)
	ciGoTest     = regexp.MustCompile(`^--- FAIL:\s+(\S+)(?:\s+\([0-9.]+s\))?$`)
	ciPyTest     = regexp.MustCompile(`^FAILED\s+(.+?)(?:\s+-\s+(.+))?$`)
)

// Extract only facts supported by complete Go compiler evidence. Other failures
// keep every line; a matching error marker alone is not a root-cause diagnosis.
func ciExtractEvidence(lines []string, truncated bool) CIEvidence {
	evidence := CIEvidence{Kind: "go_compiler", ExpectedArguments: map[string]int{}, Truncated: truncated}
	missing := map[string]bool{}
	exits := map[int]bool{}
	seenDiagnostics := map[string]bool{}
	unsupported := false
	currentCall := ""
	for i, raw := range lines {
		message := ciANSI.ReplaceAllString(strings.TrimPrefix(raw, "\ufeff"), "")
		message = strings.TrimSpace(ciTimestamp.ReplaceAllString(message, ""))
		if match := ciGoLocation.FindStringSubmatch(message); match != nil {
			line, lineErr := strconv.Atoi(match[2])
			column, columnErr := strconv.Atoi(match[3])
			if lineErr != nil || columnErr != nil || line <= 0 || column <= 0 {
				unsupported = true
			}
			if !seenDiagnostics[message] {
				evidence.Diagnostics = append(evidence.Diagnostics, CIDiagnostic{Path: match[1], Line: line, Column: column, Message: match[4], EvidenceLine: i + 1})
				seenDiagnostics[message] = true
			}
			currentCall = ""
			if call := ciCall.FindStringSubmatch(match[4]); call != nil {
				currentCall = call[1]
			}
		} else if strings.HasPrefix(message, "want ") {
			count, err := ciParameterCount(strings.TrimSpace(strings.TrimPrefix(message, "want ")))
			previous, exists := evidence.ExpectedArguments[currentCall]
			if err != nil || currentCall == "" || (exists && count != previous) {
				unsupported = true
			} else {
				evidence.ExpectedArguments[currentCall] = count
			}
		} else if exit := ciExit.FindStringSubmatch(message); exit != nil {
			code, err := strconv.Atoi(exit[1])
			if err != nil {
				unsupported = true
			} else {
				exits[code] = true
			}
		} else if ciError.MatchString(message) && !(message == "FAIL" || strings.HasPrefix(message, "FAIL\t") || strings.HasPrefix(message, "FAIL ")) {
			unsupported = true
		}
		for _, method := range ciMissing.FindAllStringSubmatch(message, -1) {
			missing[method[1]] = true
		}
	}
	for method := range missing {
		evidence.MissingMethods = append(evidence.MissingMethods, method)
	}
	sort.Strings(evidence.MissingMethods)
	for exit := range exits {
		evidence.ExitCodes = append(evidence.ExitCodes, exit)
	}
	sort.Ints(evidence.ExitCodes)
	if truncated || unsupported || len(evidence.Diagnostics) == 0 || len(exits) != 1 || exits[0] {
		fallback := CIEvidence{Kind: "log", Truncated: truncated, Lines: lines, Notice: "Unrecognized or incomplete compiler evidence; full available log retained. No root cause inferred."}
		for i, line := range lines {
			message := ciNormalizedLine(line)
			if match := ciGoTest.FindStringSubmatch(message); match != nil {
				fallback.Tests = append(fallback.Tests, CITestFailure{Framework: "go", Name: match[1], EvidenceLine: i + 1})
			} else if match := ciPyTest.FindStringSubmatch(message); match != nil && strings.Contains(match[1], "::") {
				path, name, _ := strings.Cut(match[1], "::")
				fallback.Tests = append(fallback.Tests, CITestFailure{Framework: "pytest", Name: name, Path: path, Message: match[2], EvidenceLine: i + 1})
			}
		}
		if len(fallback.Tests) != 0 {
			fallback.Kind = "test_failure"
			fallback.Notice = "Test identities are extracted from explicit failure markers; full available context retained. No root cause inferred."
		}
		return fallback
	}
	return evidence
}

func ciAppendEvidence(all *[]CIEvidence, evidence CIEvidence) {
	key := ciEvidenceKey(evidence)
	for i := range *all {
		if ciEvidenceKey((*all)[i]) == key {
			if len(evidence.Lines) > 0 && strings.Join(evidence.Lines, "\n") != strings.Join((*all)[i].Lines, "\n") {
				matched := false
				for j := range (*all)[i].LogVariants {
					if strings.Join((*all)[i].LogVariants[j].Lines, "\n") == strings.Join(evidence.Lines, "\n") {
						(*all)[i].LogVariants[j].Occurrences = append((*all)[i].LogVariants[j].Occurrences, evidence.Occurrences...)
						matched = true
						break
					}
				}
				if !matched {
					(*all)[i].LogVariants = append((*all)[i].LogVariants, CILogVariant{Lines: evidence.Lines, Occurrences: evidence.Occurrences})
				}
			}
			(*all)[i].Occurrences = append((*all)[i].Occurrences, evidence.Occurrences...)
			return
		}
	}
	*all = append(*all, evidence)
}

func ciNormalizedLine(line string) string {
	return strings.TrimSpace(ciTimestamp.ReplaceAllString(ciANSI.ReplaceAllString(strings.TrimPrefix(line, "\ufeff"), ""), ""))
}

func ciEvidenceKey(evidence CIEvidence) string {
	evidence.Occurrences, evidence.LogVariants = nil, nil
	if len(evidence.Lines) > 0 {
		lines := make([]string, len(evidence.Lines))
		for i, line := range evidence.Lines {
			lines[i] = ciLogTime.ReplaceAllString(ciANSI.ReplaceAllString(strings.TrimPrefix(line, "\ufeff"), ""), "")
		}
		evidence.Lines = lines
	}
	encoded, _ := json.Marshal(evidence)
	return string(encoded)
}

func ciParameterCount(signature string) (int, error) {
	if !strings.HasPrefix(signature, "(") {
		return 0, fmt.Errorf("expected parameter list")
	}
	var stack []byte
	var quote byte
	escaped := false
	count, start := 0, 1
	for i := 0; i < len(signature); i++ {
		ch := signature[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if ch == '\\' && quote != '`' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '(', '[', '{':
			stack = append(stack, ch)
		case ')', ']', '}':
			want := map[byte]byte{')': '(', ']': '[', '}': '{'}[ch]
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return 0, fmt.Errorf("unbalanced parameter list")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				if strings.TrimSpace(signature[i+1:]) != "" {
					return 0, fmt.Errorf("text after parameter list")
				}
				if strings.TrimSpace(signature[start:i]) != "" {
					count++
				} else if count != 0 {
					return 0, fmt.Errorf("empty parameter")
				}
				return count, nil
			}
		case ',':
			if len(stack) == 1 {
				if strings.TrimSpace(signature[start:i]) == "" {
					return 0, fmt.Errorf("empty parameter")
				}
				count++
				start = i + 1
			}
		}
	}
	return 0, fmt.Errorf("truncated parameter list")
}
