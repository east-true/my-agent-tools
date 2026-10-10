package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type TestResultOptions struct {
	Options
	After time.Time
}

type TestCounts struct {
	Tests    int `json:"tests"`
	Failures int `json:"failures"`
	Errors   int `json:"errors"`
	Skipped  int `json:"skipped"`
}

func (counts *TestCounts) add(other TestCounts) {
	counts.Tests += other.Tests
	counts.Failures += other.Failures
	counts.Errors += other.Errors
	counts.Skipped += other.Skipped
}

type TestSuiteResult struct {
	Name string `json:"name"`
	TestCounts
}

type TestDiagnostic struct {
	DiagnosticIndex int     `json:"diagnostic_index,omitempty"`
	DetailsSelected bool    `json:"details_selected,omitempty"`
	DetailsRanges   []Range `json:"details_ranges,omitempty"`
	Suite           string  `json:"suite"`
	Name            string  `json:"name"`
	ClassName       string  `json:"classname,omitempty"`
	Kind            string  `json:"kind"`
	Message         string  `json:"message,omitempty"`
	Type            string  `json:"type,omitempty"`
	Details         string  `json:"details,omitempty"`
}

type TestReport struct {
	Path                   string            `json:"path"`
	SHA256                 string            `json:"sha256"`
	ModifiedAt             string            `json:"modified_at"`
	TestCounts             `json:"-"`        // 합계는 하위 suite와 최상위 집계로 반환한다.
	Suites                 []TestSuiteResult `json:"suites"`
	Diagnostics            []TestDiagnostic  `json:"diagnostics,omitempty"`
	DiagnosticsOmitted     int               `json:"diagnostics_omitted,omitempty"`
	DiagnosticsUnavailable bool              `json:"diagnostics_unavailable,omitempty"`
}

type TestResults struct {
	DiagnosticsSelected bool   `json:"diagnostics_selected,omitempty"`
	Status              string `json:"status"`
	Complete            bool   `json:"complete"`
	ExecutionVerified   bool   `json:"execution_verified"`
	ReportCount         int    `json:"report_count"`
	TestCounts
	Reports  []TestReport `json:"reports"`
	Problems []Problem    `json:"problems,omitempty"`
}

type xmlDiagnostic struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Details string `xml:",chardata"`
}
type xmlTestCase struct {
	Name      string          `xml:"name,attr"`
	ClassName string          `xml:"classname,attr"`
	Failures  []xmlDiagnostic `xml:"failure"`
	Errors    []xmlDiagnostic `xml:"error"`
	Skipped   []xmlDiagnostic `xml:"skipped"`
}
type xmlSuite struct {
	Name     string        `xml:"name,attr"`
	Tests    *int          `xml:"tests,attr"`
	Failures *int          `xml:"failures,attr"`
	Errors   *int          `xml:"errors,attr"`
	Skipped  *int          `xml:"skipped,attr"`
	Cases    []xmlTestCase `xml:"testcase"`
	Suites   []xmlSuite    `xml:"testsuite"`
}

func (suite xmlSuite) collect(report *TestReport, parent string, depth int) (TestCounts, error) {
	counts := TestCounts{}
	if depth > 100 {
		return counts, errors.New("JUnit suite nesting limit exceeded")
	}
	name := suite.Name
	if parent != "" {
		if name != "" {
			name = parent + "/" + name
		} else {
			name = parent
		}
	}
	for _, test := range suite.Cases {
		counts.Tests++
		if len(test.Failures) > 0 {
			counts.Failures++
		}
		if len(test.Errors) > 0 {
			counts.Errors++
		}
		if len(test.Skipped) > 0 {
			counts.Skipped++
		}
		for _, group := range []struct {
			kind   string
			values []xmlDiagnostic
		}{{"failure", test.Failures}, {"error", test.Errors}, {"skipped", test.Skipped}} {
			for _, detail := range group.values {
				report.Diagnostics = append(report.Diagnostics, TestDiagnostic{Suite: name, Name: test.Name, ClassName: test.ClassName, Kind: group.kind, Message: detail.Message, Type: detail.Type, Details: detail.Details})
			}
		}
	}
	own := counts
	for _, child := range suite.Suites {
		other, err := child.collect(report, name, depth+1)
		if err != nil {
			return counts, err
		}
		counts.add(other)
	}
	for _, metric := range []struct {
		name     string
		declared *int
		actual   *int
	}{{"tests", suite.Tests, &counts.Tests}, {"failures", suite.Failures, &counts.Failures}, {"errors", suite.Errors, &counts.Errors}, {"skipped", suite.Skipped, &counts.Skipped}} {
		if metric.declared == nil {
			continue
		}
		if *metric.declared < 0 || *metric.declared > 1000000000 {
			return counts, fmt.Errorf("invalid JUnit %s count", metric.name)
		}
		if len(suite.Cases)+len(suite.Suites) > 0 && *metric.declared != *metric.actual {
			return counts, fmt.Errorf("JUnit %s count differs from testcase/child suite evidence", metric.name)
		}
		*metric.actual = *metric.declared
	}
	if counts.Failures > counts.Tests || counts.Errors > counts.Tests || counts.Skipped > counts.Tests {
		return counts, errors.New("JUnit outcome count exceeds test count")
	}
	if len(suite.Suites) == 0 {
		if len(suite.Cases) == 0 && counts.Failures+counts.Errors+counts.Skipped > 0 {
			report.DiagnosticsUnavailable = true
		}
		report.Suites = append(report.Suites, TestSuiteResult{name, counts})
	} else if own.Tests > 0 {
		report.Suites = append(report.Suites, TestSuiteResult{name, own})
	}
	return counts, nil
}

func parseTestReport(data []byte, report *TestReport) error {
	// DecodeElement의 재귀 할당 전에 깊이를 제한한다.
	// 지원하지 않는 구조를 조용히 0개 테스트로 처리하지 않고 오류로 반환한다.
	check := xml.NewDecoder(bytes.NewReader(data))
	parents := []string{}
	for {
		token, err := check.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if len(parents) >= 100 {
				return errors.New("JUnit XML nesting limit exceeded")
			}
			if len(parents) > 0 {
				parent := parents[len(parents)-1]
				allowed := true
				switch parent {
				case "testsuites":
					allowed = token.Name.Local == "testsuite"
				case "testsuite":
					allowed = token.Name.Local == "testsuite" || token.Name.Local == "testcase" || token.Name.Local == "properties" || token.Name.Local == "system-out" || token.Name.Local == "system-err"
				case "testcase":
					allowed = token.Name.Local == "failure" || token.Name.Local == "error" || token.Name.Local == "skipped" || token.Name.Local == "properties" || token.Name.Local == "system-out" || token.Name.Local == "system-err"
				case "failure", "error", "skipped":
					allowed = false
				}
				if !allowed {
					return fmt.Errorf("unsupported JUnit element %s inside %s", token.Name.Local, parent)
				}
			}
			parents = append(parents, token.Name.Local)
		case xml.EndElement:
			parents = parents[:len(parents)-1]
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *xml.StartElement
	for root == nil {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if start, ok := token.(xml.StartElement); ok {
			root = &start
		}
		if text, ok := token.(xml.CharData); ok && len(bytes.TrimSpace(text)) > 0 {
			return errors.New("text outside JUnit root")
		}
	}
	if root.Name.Local != "testsuite" && root.Name.Local != "testsuites" {
		return errors.New("expected JUnit testsuite or testsuites root")
	}
	var suite xmlSuite
	if err := decoder.DecodeElement(&suite, root); err != nil {
		return err
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if _, ok := token.(xml.StartElement); ok {
			return errors.New("multiple XML roots")
		}
		if text, ok := token.(xml.CharData); ok && len(bytes.TrimSpace(text)) > 0 {
			return errors.New("text after JUnit root")
		}
	}
	counts, err := suite.collect(report, "", 0)
	if err != nil {
		return err
	}
	report.TestCounts = counts
	return nil
}

// ReadTestResults는 선택한 보고서를 읽는다. 완전성은 유효한 보고서 판독의 범위이며
// 이번 테스트 프로세스의 실행 완료를 증명하지 않는다.
func ReadTestResults(ctx context.Context, options TestResultOptions) (TestResults, error) {
	result := TestResults{Status: "observed", Complete: true, Reports: []TestReport{}}
	if len(options.Include) == 0 && len(options.Paths) == 0 {
		options.Include = []string{"**/TEST-*.xml"}
	}
	s, err := openScope(options.Options)
	if err != nil {
		return result, err
	}
	defer s.root.Close()
	names, problems, err := s.files(ctx)
	if err != nil {
		return result, err
	}
	result.Problems = problems
	sort.Strings(names)
	if len(names) == 0 {
		result.Problems = append(result.Problems, Problem{".", "no JUnit reports selected; this is not a successful zero-test execution"})
	}
	total := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		before, err := s.root.Stat(name)
		if err != nil {
			result.Problems = append(result.Problems, Problem{name, err.Error()})
			continue
		}
		data, _, err := s.read(name)
		if err != nil {
			result.Problems = append(result.Problems, Problem{name, err.Error()})
			continue
		}
		total += len(data)
		if total > 32<<20 {
			result.Problems = append(result.Problems, Problem{name, "report total byte limit exceeded"})
			break
		}
		info, err := s.root.Stat(name)
		if err != nil {
			result.Problems = append(result.Problems, Problem{name, err.Error()})
			continue
		}
		if !os.SameFile(before, info) || before.Size() != info.Size() || before.Mode() != info.Mode() || !before.ModTime().Equal(info.ModTime()) {
			result.Problems = append(result.Problems, Problem{name, "report changed during observation"})
			continue
		}
		report := TestReport{Path: name, SHA256: digest(data), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano), Suites: []TestSuiteResult{}}
		if err := parseTestReport(data, &report); err != nil {
			result.Problems = append(result.Problems, Problem{name, err.Error()})
			continue
		}
		if !options.After.IsZero() && info.ModTime().Before(options.After) {
			result.Problems = append(result.Problems, Problem{name, "report predates --after; current execution is unverified"})
		}
		if report.DiagnosticsUnavailable {
			result.Problems = append(result.Problems, Problem{name, "JUnit report declares outcomes without testcase diagnostic evidence"})
		}
		result.Reports = append(result.Reports, report)
		result.TestCounts.add(report.TestCounts)
	}
	result.ReportCount = len(result.Reports)
	if len(result.Problems) > 0 {
		result.Status = "partial"
		result.Complete = false
	}
	files := map[string]string{}
	for _, report := range result.Reports {
		files[report.Path] = report.SHA256
	}
	payload, _ := json.Marshal(result)
	s.captureReport(result, files, false, len(payload)+1 > s.options.MaxOutputBytes)
	if options.ReportPattern != "" {
		result.DiagnosticsSelected = true
		for i := range result.Reports {
			selected := []TestDiagnostic{}
			for index, diagnostic := range result.Reports[i].Diagnostics {
				ranges := selectRawReportRanges(diagnostic.Details, options.ReportPattern, options.ReportContext)
				if len(ranges) == 0 && !strings.Contains(diagnostic.Message, options.ReportPattern) {
					continue
				}
				diagnostic.Details, diagnostic.DetailsSelected, diagnostic.DiagnosticIndex, diagnostic.DetailsRanges = "", true, index+1, ranges
				selected = append(selected, diagnostic)
			}
			result.Reports[i].Diagnostics = selected
		}
		payload, _ = json.Marshal(result)
	}
	if len(payload)+1 > s.options.MaxOutputBytes {
		result.Status, result.Complete = "partial", false
		for i := range result.Reports {
			if len(result.Reports[i].Diagnostics) > 0 {
				result.Reports[i].DiagnosticsUnavailable = true
			}
			result.Reports[i].DiagnosticsOmitted = len(result.Reports[i].Diagnostics)
			result.Reports[i].Diagnostics = nil
			result.Reports[i].Suites = nil
		}
		result.Problems = append(result.Problems, Problem{".", "output budget exceeded; diagnostics/suites omitted, increase --max-output-bytes to read complete evidence"})
	}
	return result, nil
}
