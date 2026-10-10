package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecCollectsIndependentErrorsWithoutGuessingDependentCounts(t *testing.T) {
	root, _ := fixture(t)
	for _, name := range []string{"a.txt", "b.txt", "valid.txt"} {
		put(t, root, name, "x\r\nkeep\r\n")
	}
	spec := Plan{Version: 1, Files: []Edit{
		{Path: "a.txt", Replacements: []Replacement{{Old: "x", New: "y", Count: 2}, {Old: "y", New: "z", Count: 4}}},
		{Path: "valid.txt", Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}},
		{Path: "b.txt", Replacements: []Replacement{{Old: "missing", New: "Y", Count: 1}}},
		{Path: "../outside.txt", Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}},
	}}
	original, _ := json.Marshal(spec)
	plan, err := GeneratePlan(context.Background(), Options{Root: root}, spec)
	var all *EditDiagnostics
	var first *EditDiagnostic
	if err == nil || len(plan.Files) != 0 || !errors.As(err, &all) || !errors.As(err, &first) || len(all.Diagnostics) != 3 || first.Path != "a.txt" || first.Replacement != 1 || *first.Actual != 1 {
		t.Fatal(plan, err)
	}
	if all.Diagnostics[1].Path != "b.txt" || *all.Diagnostics[1].Actual != 0 || all.Diagnostics[2].Path != "../outside.txt" {
		t.Fatal(all)
	}
	for _, name := range []string{"a.txt", "b.txt", "valid.txt"} {
		got, _ := os.ReadFile(filepath.Join(root, name))
		if string(got) != "x\r\nkeep\r\n" {
			t.Fatal("invalid batch wrote", name)
		}
	}
	after, _ := json.Marshal(spec)
	if string(original) != string(after) {
		t.Fatal("caller spec mutated")
	}
}

func TestSharedSpecExpandsPerFileHashesCountsAndCanonicalPlan(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "first.txt", "x\n")
	put(t, root, "a.txt", "x\r\n")
	put(t, root, "b.txt", "x x\r\n")
	spec := Plan{Version: 1, Files: []Edit{{Path: "first.txt", Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}}, Groups: []EditGroup{{Paths: []string{"a.txt", "b.txt"}, Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}}}
	original, _ := json.Marshal(spec)
	_, err := GeneratePlan(context.Background(), Options{Root: root}, spec)
	var diagnostic *EditDiagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Path != "b.txt" || *diagnostic.Actual != 2 {
		t.Fatal("group count not independently validated", err)
	}
	plan, corrections, err := GeneratePlanWithRecounts(context.Background(), Options{Root: root}, spec, []CountTarget{{File: 3, Replacement: 1}})
	if err != nil || len(plan.Groups) != 0 || len(plan.Files) != 3 || len(corrections) != 1 || corrections[0].Path != "b.txt" || plan.Files[1].Replacements[0].Count != 1 || plan.Files[2].Replacements[0].Count != 2 {
		t.Fatal(plan, corrections, err)
	}
	if plan.Files[0].Path != "first.txt" || plan.Files[1].SHA256 != digest([]byte("x\r\n")) || plan.Files[2].SHA256 != digest([]byte("x x\r\n")) {
		t.Fatal("expanded order/hash lost", plan)
	}
	after, _ := json.Marshal(spec)
	if string(after) != string(original) {
		t.Fatal("group input mutated")
	}
	raw, _ := json.Marshal(plan)
	if strings.Contains(string(raw), "groups") {
		t.Fatal("saved plan is not canonical", string(raw))
	}
	result, err := ApplyWithReport(context.Background(), Options{Root: root}, plan, true, true)
	if err != nil || !result.Complete || !result.ReportComplete || result.Applied != 3 {
		t.Fatal(result, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(got) != "X X\r\n" {
		t.Fatal("group apply lost exact bytes", string(got))
	}
}

func TestSharedSpecRejectsDuplicateScopeAndInvalidGroups(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "x")
	for _, groups := range [][]EditGroup{
		{{Paths: []string{"a.txt", "a.txt"}, Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}},
		{{Paths: []string{"a.txt"}}},
		{{Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}},
		{{Paths: make([]string, 201), Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}},
	} {
		spec := Plan{Version: 1, Groups: groups}
		if _, err := GeneratePlan(context.Background(), Options{Root: root}, spec); err == nil {
			t.Fatal("accepted invalid group", groups)
		}
		if _, err := Apply(context.Background(), Options{Root: root}, spec, true); err == nil {
			t.Fatal("accepted groups as a hash-bound plan")
		}
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "x" {
		t.Fatal("invalid groups wrote source")
	}
}
