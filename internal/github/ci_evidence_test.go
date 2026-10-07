package github

import (
	"strings"
	"testing"
)

func TestCIParameterCountHandlesNestedTypesAndRejectsIncompleteLists(t *testing.T) {
	for _, tc := range []struct {
		input string
		count int
	}{
		{"()", 0},
		{"(int, func(a string, b ...any), map[string]struct{Tag string `json:\"a,b\"`}, []byte)", 4},
		{"(func(a, b int), [2]int)", 2},
	} {
		count, err := ciParameterCount(tc.input)
		if err != nil || count != tc.count {
			t.Fatalf("%s count=%d err=%v", tc.input, count, err)
		}
	}
	for _, input := range []string{"(int, func(a string)", "(map[string)int)", "(int,)", "(,int)", "(int) extra", "int", "(struct{Tag string `unterminated})"} {
		if _, err := ciParameterCount(input); err == nil {
			t.Errorf("accepted malformed signature: %s", input)
		}
	}
}

func TestCICompilerEvidenceFallbackPreservesUnknownAndMalformedErrors(t *testing.T) {
	for _, log := range []string{
		"src/file.go:5:3: too many arguments in call to pkg.Call\nwant (int, func(a int)\nProcess completed with exit code 1.",
		"src/file.go:5:3: compile error\npanic: other failure\nProcess completed with exit code 1.",
		"src/file.go:5:3: compile error\nProcess completed with exit code 0.",
		"expected: 10\nactual: 20\n--- FAIL: TestEquality",
		"Error: network disconnected\nAdditional runner evidence",
		"src/file.go:5:3: compile error\nProcess completed with exit code 1.\nProcess completed with exit code 2.",
	} {
		lines := strings.Split(log, "\n")
		evidence := ciExtractEvidence(lines, false)
		if evidence.Kind != "log" || strings.Join(evidence.Lines, "\n") != log {
			t.Fatalf("fallback lost evidence: %+v", evidence)
		}
	}
}

func TestCIEvidenceNormalizesBOMANSIAndPreservesDifferentMatrixErrors(t *testing.T) {
	log := "\ufeff2026-10-07T00:00:00Z \x1b[31m##[error]src/file.go:5:3: compile error\x1b[0m\n2026-10-07T00:00:00Z FAIL\tpackage\t0.5s\n2026-10-07T00:00:00Z ##[error]Process completed with exit code 2."
	evidence := ciExtractEvidence(strings.Split(log, "\n"), false)
	if evidence.Kind != "go_compiler" || evidence.Diagnostics[0].Path != "src/file.go" || evidence.ExitCodes[0] != 2 {
		t.Fatalf("normalization lost compiler error: %+v", evidence)
	}
	var all []CIEvidence
	evidence.Occurrences = []CIOccurrence{{JobID: 1}}
	ciAppendEvidence(&all, evidence)
	evidence.Occurrences = []CIOccurrence{{JobID: 2}}
	ciAppendEvidence(&all, evidence)
	distinct := ciExtractEvidence(strings.Split(strings.ReplaceAll(log, "compile error", "different error"), "\n"), false)
	distinct.Occurrences = []CIOccurrence{{JobID: 3}}
	ciAppendEvidence(&all, distinct)
	if len(all) != 2 || len(all[0].Occurrences) != 2 || len(all[1].Occurrences) != 1 {
		t.Fatalf("incorrect deduplication: %+v", all)
	}
}

func TestCIStepMappingFallsBackWhenBoundariesAreUnknown(t *testing.T) {
	job := CIJob{Steps: []CIStep{{Name: "test", Conclusion: "failure", StartedAt: "2026-10-07T00:00:01Z", CompletedAt: "2026-10-07T00:00:02Z"}}}
	for _, log := range []string{ciCompilerLog + "untimestamped evidence\n", strings.ReplaceAll(ciCompilerLog, "00:00:01", "00:00:09")} {
		sections := ciLogSections(job, log)
		if len(sections) != 1 || sections[0].startLine != 1 || sections[0].step.Number != 0 || strings.Join(sections[0].lines, "\n")+"\n" != log {
			t.Fatalf("guessed unknown step boundaries: %+v", sections)
		}
	}
}
