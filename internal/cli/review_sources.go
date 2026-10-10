package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"sort"
	"strings"

	"github.com/east-true/my-agent-tools/internal/filesystem"
	"github.com/east-true/my-agent-tools/internal/github"
)

type reviewSources struct {
	Paths   fsStrings
	Root    string
	Threads bool
}

func (options *reviewSources) register(flags *flag.FlagSet) {
	flags.Var(&options.Paths, "source-path", "also read this root-relative source file verbatim with SHA; repeatable")
	flags.StringVar(&options.Root, "source-root", ".", "local source root; local files are not asserted to match the remote head")
	flags.BoolVar(&options.Threads, "review-sources", false, "also read distinct paths from unresolved inline review threads; never infer paths from prose")
}

func (options reviewSources) validate() error {
	if options.Root != "." && len(options.Paths) == 0 && !options.Threads {
		return errors.New("--source-root requires --source-path or --review-sources")
	}
	return nil
}

func (options reviewSources) read(ctx context.Context, reviews *github.ReviewResult) (*filesystem.Inspection, error) {
	paths := append([]string{}, options.Paths...)
	if options.Threads && reviews != nil {
		for _, thread := range reviews.Threads {
			if !thread.Resolved {
				paths = append(paths, thread.Path)
			}
		}
	}
	if len(paths) == 0 {
		if options.Threads {
			return &filesystem.Inspection{Status: "ok", Complete: true, Files: []filesystem.File{}}, nil
		}
		return nil, nil
	}
	seen := map[string]bool{}
	selected := []string{}
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			selected = append(selected, path)
		}
	}
	sort.Strings(selected)
	value, err := filesystem.Inspect(ctx, filesystem.InspectOptions{Options: filesystem.Options{Root: options.Root, Paths: selected}, Raw: true, Hash: true})
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func attachReviewSources(value any, sources *filesystem.Inspection) (any, error) {
	if sources == nil {
		return value, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	object := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	object["source_files"] = sources
	if !sources.Complete {
		object["status"], object["complete"] = "partial", false
	}
	return object, nil
}

func includesReviews(sections string) bool {
	for _, section := range strings.Split(sections, ",") {
		if section == "reviews" {
			return true
		}
	}
	return false
}
