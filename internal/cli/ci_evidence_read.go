package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/east-true/my-agent-tools/internal/github"
)

type evidenceReadFlags struct {
	File    string
	SHA256  string
	Run     int64
	Attempt int
	Job     int64
	Range   string
	Pattern string
	Context int
	Limit   int
}

func (options *evidenceReadFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&options.File, "read-evidence", "", "read saved CI evidence offline; never authenticates, collects, reruns, submits or merges")
	flags.StringVar(&options.SHA256, "evidence-sha256", "", "expected SHA-256 of --read-evidence")
	flags.Int64Var(&options.Run, "evidence-run", 0, "saved workflow run ID")
	flags.IntVar(&options.Attempt, "evidence-attempt", 0, "saved workflow attempt")
	flags.Int64Var(&options.Job, "job", 0, "saved job ID")
	flags.StringVar(&options.Range, "range", "", "1-based inclusive original job-log START:END")
	flags.StringVar(&options.Pattern, "pattern", "", "literal text to select from saved job evidence")
	flags.IntVar(&options.Context, "context", 0, "original lines around selected matches (0–1000)")
	flags.IntVar(&options.Limit, "evidence-output-bytes", 64<<10, "maximum selected evidence output bytes")
}

func (options evidenceReadFlags) selected() bool {
	return options.File != "" || options.SHA256 != "" || options.Run != 0 || options.Attempt != 0 || options.Job != 0 || options.Range != "" || options.Pattern != "" || options.Context != 0
}

func (options evidenceReadFlags) run(flags *flag.FlagSet, out, stderr io.Writer) int {
	fail := func(err error) int {
		_ = encode(out, stderr, map[string]any{"status": "error", "error": err.Error()})
		return 2
	}
	allowed := map[string]bool{"read-evidence": true, "evidence-sha256": true, "evidence-run": true, "evidence-attempt": true, "job": true, "range": true, "pattern": true, "context": true, "evidence-output-bytes": true, "json": true}
	var invalid string
	flags.Visit(func(option *flag.Flag) {
		if !allowed[option.Name] {
			invalid = option.Name
		}
	})
	if flags.NArg() != 0 || invalid != "" {
		return fail(errors.New("offline evidence reading accepts only saved evidence selectors; operation flags are forbidden"))
	}
	if options.File == "" || options.Run <= 0 || options.Attempt <= 0 || options.Job <= 0 || options.Context < 0 || options.Context > 1000 || options.Limit < 256 || options.Limit > 16<<20 {
		return fail(errors.New("read-evidence requires file, SHA, positive run/attempt/job and valid limits"))
	}
	start, end, err := parseEvidenceRange(options.Range)
	if err != nil {
		return fail(err)
	}
	data, err := readEvidenceBytes(options.File, options.SHA256)
	if err != nil {
		return fail(err)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fail(err)
	}
	var candidates []github.CIFailureResult
	var walk func(any)
	walk = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			if item["run"] != nil && item["evidence"] != nil && item["repo"] != nil {
				data, _ := json.Marshal(item)
				var failure github.CIFailureResult
				if json.Unmarshal(data, &failure) == nil && failure.Run.ID == options.Run && failure.Run.Attempt == options.Attempt {
					candidates = append(candidates, failure)
				}
				return
			}
			for _, child := range item {
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(value)
	if len(candidates) != 1 {
		return fail(errors.New("saved evidence must contain one unambiguous selected run attempt"))
	}
	failure := candidates[0]
	found := false
	segments := []any{}
	for _, evidence := range failure.Evidence {
		for _, occurrence := range evidence.Occurrences {
			if occurrence.JobID != options.Job {
				continue
			}
			lines := evidence.Lines
			variant := false
			for _, alternative := range evidence.LogVariants {
				for _, location := range alternative.Occurrences {
					if location == occurrence {
						lines, variant = alternative.Lines, true
					}
				}
			}
			if len(lines) == 0 {
				continue
			}
			found = true
			// 자료의 시작 위치를 바꾸지 않고 job 원래 좌표로 선택한다.
			keep := make([]bool, len(lines))
			for i, line := range lines {
				position := occurrence.StartLine + i
				if start > 0 && (position < start || position > end) {
					continue
				}
				if options.Pattern == "" || strings.Contains(line, options.Pattern) {
					for j := max(0, i-options.Context); j <= min(len(lines)-1, i+options.Context); j++ {
						if start == 0 || occurrence.StartLine+j >= start && occurrence.StartLine+j <= end {
							keep[j] = true
						}
					}
				}
			}
			ranges := []any{}
			for i := 0; i < len(lines); {
				if !keep[i] {
					i++
					continue
				}
				a := i
				for i < len(lines) && keep[i] {
					i++
				}
				ranges = append(ranges, map[string]any{"start_line": occurrence.StartLine + a, "end_line": occurrence.StartLine + i - 1, "lines": lines[a:i]})
			}
			if len(ranges) > 0 {
				segments = append(segments, map[string]any{"kind": evidence.Kind, "occurrence": occurrence, "variant": variant, "collection_truncated": evidence.Truncated, "ranges": ranges})
			}
		}
	}
	if !found {
		return fail(errors.New("selected job has no retained log evidence in this snapshot"))
	}
	result := map[string]any{"status": "observed", "saved_observation": true, "fresh_state_verified": false, "repo": failure.Repo, "run": failure.Run, "job_id": options.Job, "collection_complete": failure.Complete, "evidence_file": options.File, "evidence_sha256": options.SHA256, "segments": segments}
	payload, err := json.Marshal(result)
	if err != nil {
		return fail(err)
	}
	if len(payload)+1 > options.Limit {
		return fail(fmt.Errorf("selected evidence exceeds %d bytes; narrow range/pattern or increase --evidence-output-bytes", options.Limit))
	}
	return encode(out, stderr, result)
}
