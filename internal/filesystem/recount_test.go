package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitRecountCapturesOriginalHashWithoutMutatingSpec(t *testing.T) {
	root, _ := fixture(t)
	original := "x x\nkeep\n"
	put(t, root, "a.txt", original)
	spec := Plan{Version: 1, Files: []Edit{{Path: "a.txt", Replacements: []Replacement{{Old: "x", New: "X", Count: 1}}}}}
	plan, corrections, err := GeneratePlanWithRecounts(context.Background(), Options{Root: root}, spec, []CountTarget{{File: 1, Replacement: 1}})
	if err != nil || len(corrections) != 1 || corrections[0].Path != "a.txt" || corrections[0].Replacement != 1 || *corrections[0].Expected != 1 || *corrections[0].Actual != 2 || spec.Files[0].Replacements[0].Count != 1 || plan.Files[0].Replacements[0].Count != 2 || plan.Files[0].SHA256 != digest([]byte(original)) {
		t.Fatal(plan, corrections, spec, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != original {
		t.Fatal("recount changed source before apply")
	}
	result, err := Apply(context.Background(), Options{Root: root}, plan, true)
	got, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || !result.Complete || string(got) != "X X\nkeep\n" {
		t.Fatal(result, string(got), err)
	}
}

func TestRecountRejectsUnselectedMismatchesAndZeroMatches(t *testing.T) {
	for _, entry := range []struct {
		name         string
		replacements []Replacement
		targets      []CountTarget
	}{
		{"unselected", []Replacement{{Old: "x", New: "X", Count: 1}, {Old: "y", New: "Y", Count: 1}}, []CountTarget{{File: 1, Replacement: 1}}},
		{"zero", []Replacement{{Old: "missing", New: "X", Count: 1}}, []CountTarget{{File: 1, Replacement: 1}}},
		{"invalid count", []Replacement{{Old: "x", New: "X", Count: 0}}, []CountTarget{{File: 1, Replacement: 1}}},
		{"duplicate target", []Replacement{{Old: "x", New: "X", Count: 1}}, []CountTarget{{File: 1, Replacement: 1}, {File: 1, Replacement: 1}}},
		{"unknown index", []Replacement{{Old: "x", New: "X", Count: 1}}, []CountTarget{{File: 2, Replacement: 1}}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root, _ := fixture(t)
			original := "x x y y\n"
			put(t, root, "a.txt", original)
			spec := Plan{Version: 1, Files: []Edit{{Path: "a.txt", Replacements: entry.replacements}}}
			_, _, err := GeneratePlanWithRecounts(context.Background(), Options{Root: root}, spec, entry.targets)
			if err == nil {
				t.Fatal("accepted an unauthorized or invalid recount")
			}
			got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
			if string(got) != original {
				t.Fatal("invalid batch wrote source")
			}
			for i, r := range spec.Files[0].Replacements {
				if r.Count != entry.replacements[i].Count {
					t.Fatal("changed caller spec")
				}
			}
		})
	}
}

func TestRecountUsesSequentialReplacementStateAndRemainsHashBound(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "x x")
	spec := Plan{Version: 1, Files: []Edit{{Path: "a.txt", Replacements: []Replacement{{Old: "x", New: "y", Count: 2}, {Old: "y", New: "z", Count: 1}}}}}
	plan, corrections, err := GeneratePlanWithRecounts(context.Background(), Options{Root: root}, spec, []CountTarget{{File: 1, Replacement: 2}})
	if err != nil || len(corrections) != 1 || plan.Files[0].Replacements[1].Count != 2 || spec.Files[0].Replacements[1].Count != 1 {
		t.Fatal(plan, corrections, err)
	}
	put(t, root, "a.txt", "x q")
	result, err := Apply(context.Background(), Options{Root: root}, plan, true)
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || result.Complete || string(got) != "x q" {
		t.Fatal("recount lost original SHA precondition", result, string(got), err)
	}
}
