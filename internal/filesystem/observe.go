package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

func baselineBytes(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, errors.New("invalid baseline type or size")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if len(data) > 64<<20 {
		return nil, errors.New("baseline byte limit exceeded")
	}
	return data, err
}

func observeDeltas(ctx context.Context, options DeltaOptions, scan func(context.Context, DeltaOptions) (DeltaResult, error)) (DeltaResult, error) {
	if !options.Peek || options.Reset {
		return DeltaResult{}, errors.New("repeated comparisons require --peek and cannot reset the baseline")
	}
	before, err := baselineBytes(options.StateFile)
	if err != nil {
		return DeltaResult{}, err
	}
	count := options.Comparisons
	options.Comparisons = 1
	first := DeltaResult{}
	consistent := true
	seen := map[string]bool{}
	for i := 0; i < count; i++ {
		value, err := scan(ctx, options)
		if err != nil {
			return first, err
		}
		if i == 0 {
			first = value
			seen[value.observationSHA] = true
		}
		first.Comparisons = i + 1
		if !value.Complete {
			first.Complete = false
			consistent = false
			if i > 0 {
				first.OtherResults = append(first.OtherResults, value)
			}
			break
		}
		if value.observationSHA != first.observationSHA {
			consistent = false
			if !seen[value.observationSHA] {
				value.SnapshotSHA256 = value.observationSHA
				first.OtherResults = append(first.OtherResults, value)
				seen[value.observationSHA] = true
			}
		}
	}
	after, err := baselineBytes(options.StateFile)
	preserved := err == nil && bytes.Equal(before, after)
	first.Consistent = &consistent
	first.BaselinePreserved = &preserved
	if !consistent || !preserved {
		if first.Status != "partial" {
			first.Status = "unstable"
		}
		first.Complete = false
		first.SnapshotSHA256 = first.observationSHA
		if !preserved {
			first.Problems = append(first.Problems, Problem{options.StateFile, "baseline changed or became unavailable during comparison"})
		}
	}
	limit := options.MaxOutputBytes
	if limit == 0 {
		limit = 64 << 10
	}
	payload, _ := json.Marshal(first)
	if len(payload)+1 > limit {
		first.Status = "partial"
		first.Complete = false
		first.Changes = nil
		first.OtherResults = nil
		first.Problems = append(first.Problems, Problem{".", "comparison report exceeds output limit; baseline was not advanced; increase the output budget"})
	}
	return first, nil
}
