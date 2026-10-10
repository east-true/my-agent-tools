package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBatchRangesPaginationAndStaleCursor(t *testing.T) {
	root, _ := fixture(t)
	lines := []string{}
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("source line %02d", i))
	}
	put(t, root, "a.txt", strings.Join(lines, "\r\n")+"\r\n")
	put(t, root, "b.txt", "first\nneedle here\nlast\n")
	options := InspectOptions{Options: Options{Root: root, MaxOutputBytes: 450}, Requests: []ReadRequest{
		{Path: "a.txt", Ranges: []LineRange{{2, 8}, {20, 26}}, Hash: true},
		{Path: "b.txt", Patterns: []string{"needle"}},
	}}
	got := []string{}
	cursor := ""
	pages := 0
	for {
		options.Cursor = cursor
		r, err := Inspect(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(r)
		if len(raw) > 450 {
			t.Fatal("unbounded page", len(raw))
		}
		for _, f := range r.Files {
			for _, span := range f.Ranges {
				for i, text := range span.Lines {
					got = append(got, fmt.Sprintf("%s:%d:%s", f.Path, span.Start+i, text))
				}
			}
		}
		pages++
		if pages > 20 {
			t.Fatal("cursor made no progress")
		}
		if r.NextCursor == "" {
			if !r.Complete {
				t.Fatal(r)
			}
			break
		}
		if pages == 1 {
			original, _ := os.ReadFile(filepath.Join(root, "a.txt"))
			put(t, root, "a.txt", strings.Replace(string(original), "source line 01", "SOURCE LINE 01", 1))
			options.Cursor = r.NextCursor
			if _, e := Inspect(context.Background(), options); e == nil {
				t.Fatal("accepted a changed file")
			}
			put(t, root, "a.txt", string(original))
		}
		cursor = r.NextCursor
	}
	want := []string{}
	for _, span := range []LineRange{{2, 8}, {20, 26}} {
		for i := span.Start; i <= span.End; i++ {
			want = append(want, fmt.Sprintf("a.txt:%d:source line %02d", i, i))
		}
	}
	want = append(want, "b.txt:2:needle here")
	if pages < 2 || !reflect.DeepEqual(got, want) {
		t.Fatal(pages, got, want)
	}
}

func TestSparseChangedRangesAndContextMerge(t *testing.T) {
	old := []string{}
	for i := 1; i <= 100; i++ {
		old = append(old, fmt.Sprintf("line %03d", i))
	}
	next := append([]string{}, old...)
	next[1] = "first edit"
	next[89] = "second edit"
	before, after := changedRanges(old, next, 0)
	if len(before) != 2 || len(after) != 2 || after[0].Start != 2 || after[1].Start != 90 || len(after[0].Lines) != 1 || len(after[1].Lines) != 1 {
		t.Fatal(before, after)
	}
	insert := append([]string{}, old[:10]...)
	insert = append(insert, "inserted")
	insert = append(insert, old[10:50]...)
	insert = append(insert, old[51:]...)
	a, b := changedRanges(old, insert, 0)
	if len(a) != 1 || a[0].Start != 51 || len(b) != 1 || b[0].Start != 11 || b[0].Lines[0] != "inserted" {
		t.Fatal(a, b)
	}
	next = append([]string{}, old...)
	next[10] = "a"
	next[13] = "b"
	_, after = changedRanges(old, next, 2)
	if len(after) != 1 || after[0].Start != 9 || len(after[0].Lines) != 8 {
		t.Fatal(after)
	}
	repeated := strings.Split(strings.Repeat("same\n", 100), "\n")
	next = append([]string{}, repeated...)
	next[1] = "first"
	next[89] = "second"
	_, after = changedRanges(repeated, next, 0)
	if len(after) != 2 {
		t.Fatal("repeated-line edits were merged", after)
	}
}

func TestPeekCanRepeatWithoutConsumingBaseline(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "before\n")
	options := DeltaOptions{Options: Options{Root: root, Include: []string{"*.txt"}}, StateFile: filepath.Join(root, "snapshot.json"), Content: true}
	options.Peek = true
	if _, e := Delta(context.Background(), options); e == nil {
		t.Fatal("peek initialized baseline")
	}
	options.Peek = false
	if _, e := Delta(context.Background(), options); e != nil {
		t.Fatal(e)
	}
	baseline, _ := os.ReadFile(options.StateFile)
	put(t, root, "a.txt", "after\n")
	options.Peek = true
	for i := 0; i < 2; i++ {
		r, e := Delta(context.Background(), options)
		if e != nil || len(r.Changes) != 1 || r.StateUpdated {
			t.Fatal(r, e)
		}
		raw, _ := os.ReadFile(options.StateFile)
		if string(raw) != string(baseline) {
			t.Fatal("peek advanced baseline")
		}
	}
	options.Peek = false
	r, e := Delta(context.Background(), options)
	if e != nil || !r.StateUpdated {
		t.Fatal(r, e)
	}
}

func TestGeneratedPlanPreviewApplyAndBoundedReadbackReport(t *testing.T) {
	root, _ := fixture(t)
	original := "one\r\nuntouched\r\n"
	put(t, root, "a.txt", original)
	text := strings.Repeat("new line\n", 100)
	spec := Plan{Version: 1, Files: []Edit{{Path: "a.txt", Replacements: []Replacement{{Old: "one", New: "two", Count: 1}}}, {Path: "new.txt", SHA256: "absent", Content: &text}}}
	plan, e := GeneratePlan(context.Background(), Options{Root: root}, spec)
	if e != nil || plan.Files[0].SHA256 != digest([]byte(original)) || spec.Files[0].SHA256 != "" {
		t.Fatal(plan, spec, e)
	}
	preview, e := ApplyWithReport(context.Background(), Options{Root: root}, plan, false, true)
	if e != nil || preview.Applied != 0 || preview.Files[0].Verified || preview.Files[0].Ranges[0].Lines[0] != "two" {
		t.Fatal(preview, e)
	}
	if _, e := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(e) {
		t.Fatal("preview mutated")
	}
	result, e := ApplyWithReport(context.Background(), Options{Root: root, MaxOutputBytes: 600}, plan, true, true)
	if e != nil || !result.Complete || result.ReportComplete || result.Applied != 2 {
		t.Fatal(result, e)
	}
	for _, f := range result.Files {
		actual, _ := os.ReadFile(filepath.Join(root, f.Path))
		if !f.Verified || f.SHA256 != digest(actual) {
			t.Fatal(f)
		}
	}
	missing := Plan{Version: 1, Files: []Edit{{Path: "missing.txt", Content: &text}}}
	if _, e := GeneratePlan(context.Background(), Options{Root: root}, missing); e == nil {
		t.Fatal("implicitly created missing edit")
	}
	put(t, root, "a.txt", "user changed\n")
	r, e := Apply(context.Background(), Options{Root: root}, plan, true)
	if e != nil || r.Complete || r.Applied != 0 {
		t.Fatal(r, e)
	}
}

func TestDeltaContentKindProjectionPreservesAllChangeFactsAndBaseline(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "before\n")
	put(t, root, "gone.txt", "deleted body\n")
	opts := DeltaOptions{Options: Options{Root: root, Include: []string{"*.txt"}}, StateFile: filepath.Join(root, "baseline.json"), Content: true}
	if _, e := Delta(context.Background(), opts); e != nil {
		t.Fatal(e)
	}
	baseline, _ := os.ReadFile(opts.StateFile)
	put(t, root, "a.txt", "after\n")
	put(t, root, "new.txt", "new body\n")
	os.Remove(filepath.Join(root, "gone.txt"))
	opts.Peek = true
	all, e := Delta(context.Background(), opts)
	if e != nil {
		t.Fatal(e)
	}
	opts.ContentKinds = []string{"modified"}
	filtered, e := Delta(context.Background(), opts)
	if e != nil || !filtered.Complete || len(filtered.Changes) != 3 {
		t.Fatal(filtered, e)
	}
	for i, c := range filtered.Changes {
		full := all.Changes[i]
		if c.Path != full.Path || c.Kind != full.Kind || c.SHA256 != full.SHA256 || c.BeforeSHA256 != full.BeforeSHA256 {
			t.Fatal("projection lost change facts", c, full)
		}
		if c.Kind == "modified" {
			if !reflect.DeepEqual(c.Ranges, full.Ranges) || !reflect.DeepEqual(c.Before, full.Before) {
				t.Fatal(c, full)
			}
		} else if len(c.Ranges) != 0 || len(c.Before) != 0 {
			t.Fatal("unrequested content emitted", c)
		}
	}
	after, _ := os.ReadFile(opts.StateFile)
	if string(after) != string(baseline) {
		t.Fatal("projection consumed baseline")
	}
	opts.ContentKinds = []string{"unknown"}
	if _, e := Delta(context.Background(), opts); e == nil {
		t.Fatal("invalid content kind accepted")
	}
}
