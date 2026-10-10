package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactFragmentsCanBeAppliedWithoutAnotherRead(t *testing.T) {
	for _, original := range []string{"one\r\ntwo\r\nthree\r\n", "one\r\ntwo\nthree", "one\ntwo\nthree\n"} {
		t.Run(fmt.Sprintf("%x", digest([]byte(original))[:4]), func(t *testing.T) {
			root, _ := fixture(t)
			put(t, root, "a.txt", original)
			r, e := Inspect(context.Background(), InspectOptions{Options: Options{Root: root, Paths: []string{"a.txt"}}, Start: 1, End: 2, Hash: true, Raw: true})
			if e != nil || !r.Complete || r.Files[0].Ranges[0].Text == nil || len(r.Files[0].Ranges[0].Lines) != 0 {
				t.Fatal(r, e)
			}
			file := r.Files[0]
			old := *file.Ranges[0].Text
			new := strings.ReplaceAll(strings.ReplaceAll(old, "one", "ONE"), "two", "TWO")
			if file.FinalNewline == nil || *file.FinalNewline != strings.HasSuffix(original, "\n") {
				t.Fatal(file)
			}
			plan := Plan{Version: 1, Files: []Edit{{Path: file.Path, SHA256: file.SHA256, Replacements: []Replacement{{Old: old, New: new, Count: 1}}}}}
			applied, e := ApplyWithReport(context.Background(), Options{Root: root}, plan, true, true)
			got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
			if e != nil || !applied.Complete || !applied.Files[0].Verified || string(got) != strings.Replace(original, old, new, 1) {
				t.Fatal(applied, string(got), e)
			}
		})
	}
}

func TestCompletePayloadFitsWithoutReservingAnUnneededCursor(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", strings.Repeat("readable line\n", 20))
	opts := InspectOptions{Options: Options{Root: root, Paths: []string{"a.txt"}}, Start: 1, End: 20}
	whole, e := Inspect(context.Background(), opts)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(whole)
	opts.MaxOutputBytes = len(raw) + 1
	result, e := Inspect(context.Background(), opts)
	if e != nil || !result.Complete || result.NextCursor != "" || len(result.Files[0].Ranges[0].Lines) != 20 {
		t.Fatal(result, e)
	}
}

func TestGeneratedPlanProvidesSpecificReplacementDiagnostics(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "safe")
	put(t, root, "b.txt", "x x")
	spec := Plan{Version: 1, Files: []Edit{{Path: "a.txt", Replacements: []Replacement{{Old: "safe", New: "edited", Count: 1}}}, {Path: "b.txt", Replacements: []Replacement{{Old: "x x", New: "x x", Count: 1}, {Old: "x", New: "y", Count: 1}}}}}
	_, e := GeneratePlan(context.Background(), Options{Root: root}, spec)
	var d *EditDiagnostic
	if !errors.As(e, &d) || d.Path != "b.txt" || d.Replacement != 2 || *d.Expected != 1 || *d.Actual != 2 {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "safe" {
		t.Fatal("modified source while planning")
	}
}

func TestRepeatedDeltaScansPreserveOneReportAndShowDifferentObservations(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "zero\n")
	opts := DeltaOptions{Options: Options{Root: root, Include: []string{"*.txt"}}, StateFile: filepath.Join(root, "baseline.json"), Content: true}
	if _, e := Delta(context.Background(), opts); e != nil {
		t.Fatal(e)
	}
	baseline, _ := os.ReadFile(opts.StateFile)
	put(t, root, "a.txt", "one\n")
	opts.Peek = true
	opts.Comparisons = 2
	equal, e := Delta(context.Background(), opts)
	if e != nil || !equal.Complete || equal.Comparisons != 2 || equal.Consistent == nil || !*equal.Consistent || !*equal.BaselinePreserved || len(equal.OtherResults) != 0 || len(equal.Changes) != 1 {
		t.Fatal(equal, e)
	}
	calls := 0
	v, e := observeDeltas(context.Background(), opts, func(ctx context.Context, single DeltaOptions) (DeltaResult, error) {
		calls++
		r, e := Delta(ctx, single)
		if calls == 1 {
			put(t, root, "a.txt", "two\n")
		}
		return r, e
	})
	if e != nil || calls != 2 || v.Complete || *v.Consistent || !*v.BaselinePreserved || len(v.OtherResults) != 1 || v.Changes[0].Ranges[0].Lines[0] != "one" || v.OtherResults[0].Changes[0].Ranges[0].Lines[0] != "two" {
		t.Fatal(v, e)
	}
	after, _ := os.ReadFile(opts.StateFile)
	if string(after) != string(baseline) {
		t.Fatal("repeat changed baseline")
	}
	opts.Peek = false
	if _, e := Delta(context.Background(), opts); e == nil {
		t.Fatal("repeated comparisons advanced baseline")
	}
}

func TestRawPaginationPreservesEveryByteAndFinalNewline(t *testing.T) {
	root, _ := fixture(t)
	text := strings.Repeat("first\r\nsecond\n", 25) + "last"
	put(t, root, "a.txt", text)
	opts := InspectOptions{Options: Options{Root: root, Paths: []string{"a.txt"}, MaxOutputBytes: 450}, Start: 1, End: 51, Raw: true}
	got := ""
	pages := 0
	for {
		r, e := Inspect(context.Background(), opts)
		if e != nil {
			t.Fatal(e)
		}
		encoded, _ := json.Marshal(r)
		if len(encoded)+1 > 450 {
			t.Fatal("raw page exceeds budget", len(encoded))
		}
		for _, f := range r.Files {
			if f.LineEnding != "mixed" || *f.FinalNewline {
				t.Fatal(f)
			}
			for _, span := range f.Ranges {
				if span.Text == nil || len(span.Lines) != 0 {
					t.Fatal(span)
				}
				got += *span.Text
			}
		}
		pages++
		if pages > 100 {
			t.Fatal("cursor did not advance")
		}
		if r.NextCursor == "" {
			if !r.Complete {
				t.Fatal(r)
			}
			break
		}
		opts.Cursor = r.NextCursor
	}
	if pages < 2 || got != text {
		t.Fatal(pages, got)
	}
}
