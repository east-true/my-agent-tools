package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMarkdownSectionsPreserveRawBytesAndDisambiguateTitles(t *testing.T) {
	root, _ := fixture(t)
	text := "# Guide\r\nintro\r\n## Build ###\r\nbuild\r\n```sh\r\n# not a heading\r\n```\r\n### Verify\r\ncheck\r\n## Build\r\nother\r\n\r\nSetext\r\n======\r\nlast"
	put(t, root, "guide.md", text)
	opts := InspectOptions{Options: Options{Root: root, Paths: []string{"guide.md"}}, Outline: true, Hash: true}
	r, err := Inspect(context.Background(), opts)
	if err != nil || !r.Complete || len(r.Files) != 1 || len(r.Files[0].Ranges) != 0 {
		t.Fatal(r, err)
	}
	want := []Heading{{"Guide", 1, 1, 12, 1}, {"Build", 2, 3, 9, 1}, {"Verify", 3, 8, 9, 1}, {"Build", 2, 10, 12, 2}, {"Setext", 1, 13, 15, 1}}
	if !reflect.DeepEqual(r.Files[0].Headings, want) {
		t.Fatal(r.Files[0].Headings)
	}
	opts.Outline = false
	opts.Raw = true
	opts.Sections = []SectionRequest{{Title: "Build"}}
	r, err = Inspect(context.Background(), opts)
	if err != nil || r.Complete || len(r.Problems) != 1 {
		t.Fatal("ambiguous section accepted", r, err)
	}
	opts.Sections[0].Occurrence = 1
	r, err = Inspect(context.Background(), opts)
	exact := "## Build ###\r\nbuild\r\n```sh\r\n# not a heading\r\n```\r\n### Verify\r\ncheck\r\n"
	if err != nil || !r.Complete || len(r.Files[0].Ranges) != 1 || *r.Files[0].Ranges[0].Text != exact || r.Files[0].Ranges[0].Start != 3 || r.Files[0].SHA256 != digest([]byte(text)) {
		t.Fatal(r, err)
	}
	opts.Sections = []SectionRequest{{Title: "Setext"}}
	r, err = Inspect(context.Background(), opts)
	if err != nil || !r.Complete || *r.Files[0].Ranges[0].Text != "Setext\r\n======\r\nlast" {
		t.Fatal("final newline changed", r, err)
	}
}

func TestMarkdownOutlineAndSectionPaginationRetainsEveryHeadingAndLine(t *testing.T) {
	root, _ := fixture(t)
	text := "# Root\n"
	for i := 0; i < 12; i++ {
		text += "## Title\n" + strings.Repeat("content ", 12) + "\n"
	}
	put(t, root, "g.md", text)
	opts := InspectOptions{Options: Options{Root: root, Paths: []string{"g.md"}, MaxOutputBytes: 900}, Outline: true, Raw: true, Sections: []SectionRequest{{Title: "Root"}}}
	gotHeadings := []Heading{}
	gotText := ""
	pages := 0
	for {
		r, err := Inspect(context.Background(), opts)
		if err != nil || len(r.Problems) > 0 {
			t.Fatal(r, err)
		}
		for _, file := range r.Files {
			gotHeadings = append(gotHeadings, file.Headings...)
			for _, span := range file.Ranges {
				gotText += *span.Text
			}
		}
		pages++
		if pages > 100 {
			t.Fatal("cursor did not advance")
		}
		if r.Complete {
			break
		}
		if r.NextCursor == "" {
			t.Fatal("page lost continuation", r)
		}
		opts.Cursor = r.NextCursor
	}
	if pages < 2 || len(gotHeadings) != 13 || gotText != text {
		t.Fatal("pagination lost data", pages, len(gotHeadings), gotText)
	}
	put(t, root, "g.md", text+"changed\n")
	if _, err := Inspect(context.Background(), opts); err == nil {
		t.Fatal("changed file accepted old cursor")
	}
}

func TestOutlineKeepsExplicitRangeAndPatterns(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "g.md", "# Heading\nfirst\nsecond\n")
	opts := InspectOptions{Options: Options{Root: root, Paths: []string{"g.md"}}, Outline: true, Start: 2, End: 2}
	r, err := Inspect(context.Background(), opts)
	if err != nil || !r.Complete || len(r.Files[0].Ranges) != 1 || !reflect.DeepEqual(r.Files[0].Ranges[0].Lines, []string{"first"}) {
		t.Fatal(r, err)
	}
}

func TestExplicitRawFileIsUsableWithoutAnotherRangeQuery(t *testing.T) {
	for _, text := range []string{"retry_budget=2\r\ntimeout=30\r\n", "mixed\r\nlast\nno newline", ""} {
		t.Run(text, func(t *testing.T) {
			root, _ := fixture(t)
			put(t, root, "settings.py", text)
			for _, batch := range []bool{false, true} {
				opts := InspectOptions{Options: Options{Root: root, Paths: []string{"settings.py"}}, Raw: true, Hash: true}
				if batch {
					opts.Paths = nil
					opts.Requests = []ReadRequest{{Path: "settings.py", Raw: true, Hash: true}}
				}
				r, err := Inspect(context.Background(), opts)
				if err != nil || !r.Complete || len(r.Files) != 1 || len(r.Files[0].Ranges) != 1 || r.Files[0].Ranges[0].Text == nil || *r.Files[0].Ranges[0].Text != text || r.Files[0].SHA256 != digest([]byte(text)) {
					t.Fatal("explicit raw request returned metadata only", r, err)
				}
			}
		})
	}
}

func TestJUnitNestedSuitesCountsAndAllDiagnostics(t *testing.T) {
	root, _ := fixture(t)
	text := `<testsuites tests="3" failures="1" errors="1" skipped="1"><testsuite name="outer" tests="3" failures="1" errors="1" skipped="1"><testsuite name="inner" tests="2" failures="1" errors="0" skipped="1"><testcase name="fail" classname="Example"><failure message="expected &lt;2&gt;" type="Assertion"><![CDATA[line one
line two <exact>]]></failure><failure message="second">extra detail</failure></testcase><testcase name="skip"><skipped message="disabled"/></testcase></testsuite><testcase name="error"><error message="boom">full trace</error></testcase></testsuite></testsuites>`
	put(t, root, "build/test-results/test/TEST-example.xml", text)
	r, err := ReadTestResults(context.Background(), TestResultOptions{Options: Options{Root: root}})
	if err != nil || !r.Complete || r.ExecutionVerified || r.ReportCount != 1 || r.TestCounts != (TestCounts{3, 1, 1, 1}) || len(r.Reports[0].Suites) != 2 || len(r.Reports[0].Diagnostics) != 4 {
		t.Fatal(r, err)
	}
	d := r.Reports[0].Diagnostics[1]
	if d.Message != "expected <2>" || d.Details != "line one\nline two <exact>" || d.Suite != "outer/inner" || r.Reports[0].SHA256 != digest([]byte(text)) {
		t.Fatal(d, r.Reports[0])
	}
}

func TestJUnitMissingMalformedStaleAndInconsistentReportsRemainIncomplete(t *testing.T) {
	for _, text := range []string{"", `<testsuite>`, `<not-junit/>`, `<testsuite><unknown/></testsuite>`, `<testsuite tests="-1"/>`, `<testsuite tests="2"><testcase name="one"/></testsuite>`, `<testsuite/><testsuite/>`, `<testsuite><testcase><failure><b>lost</b></failure></testcase></testsuite>`} {
		t.Run(text, func(t *testing.T) {
			root, _ := fixture(t)
			if text != "" {
				put(t, root, "TEST-invalid.xml", text)
			}
			r, err := ReadTestResults(context.Background(), TestResultOptions{Options: Options{Root: root}})
			if err != nil || r.Complete || len(r.Problems) == 0 || r.ExecutionVerified || r.ReportCount != 0 {
				t.Fatal(r, err)
			}
		})
	}
	root, _ := fixture(t)
	put(t, root, "TEST-old.xml", `<testsuite name="old" tests="2" failures="0"/>`)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "TEST-old.xml"), old, old); err != nil {
		t.Fatal(err)
	}
	r, err := ReadTestResults(context.Background(), TestResultOptions{Options: Options{Root: root}, After: time.Now().Add(-time.Minute)})
	if err != nil || r.Complete || r.Tests != 2 || r.ReportCount != 1 || r.ExecutionVerified {
		t.Fatal("stale evidence silently accepted/discarded", r, err)
	}
}

func TestJUnitDiagnosticBudgetIsExplicitAndDoesNotClaimSuccess(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "TEST-long.xml", `<testsuite tests="1" failures="1"><testcase name="f"><failure>`+strings.Repeat("trace\n", 1000)+`</failure></testcase></testsuite>`)
	r, err := ReadTestResults(context.Background(), TestResultOptions{Options: Options{Root: root, MaxOutputBytes: 1000}})
	if err != nil || r.Complete || r.Failures != 1 || r.ReportCount != 1 || len(r.Reports[0].Diagnostics) != 0 || len(r.Problems) != 1 {
		t.Fatal(r, err)
	}
}
