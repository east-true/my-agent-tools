package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/east-true/my-agent-tools/internal/filesystem"
)

const fsHelp = `Usage:
  tools fs inspect --root DIR [--request FILE] [--pattern TEXT] [--range START:END] [--raw] [--hash] [--json]
  tools fs delta --root DIR --state-file FILE [--include-content] [--peek] [--comparisons N] [--json]
  tools fs apply --root DIR (--plan FILE | --spec FILE) [--save-plan FILE] [--apply] [--report-changes] [--json]
  tools fs test-results --root DIR [--include '**/TEST-*.xml'] [--after RFC3339] [--json]

inspect returns usable file-specific ranges; --raw preserves exact UTF-8 fragments and line endings.
inspect --outline reads top-level Markdown headings; --section TITLE reads an exact section without an outline first.
test-results reads JUnit counts and exact diagnostics. Report presence never verifies process completion.
delta records content hashes; repeat the same state file for added/modified/deleted files.
apply generates or validates preimages before writes and reads files back. Default: preview.
Patterns/paths/include/exclude flags may repeat; ** matches directory components.
Generated state/dependency directories and symlinks are skipped. No Git or authentication needed.
Use 'tools fs <command> --help' for options.
`

const fsApplyFormat = `
Plan JSON (version 1; old/new are exact UTF-8 fragments, count is required):
  {"version":1,"files":[{"path":"a.txt","sha256":"64_LOWERCASE_HEX_DIGITS","replacements":[{"old":"before\n","new":"after\n","count":1}]}]}
With --spec, omit existing-file sha256; --save-plan captures it before writes.
Creation: {"path":"new.txt","sha256":"absent","content":"created\n"}
For authorized edits use --apply --report-changes once; each applied file returns verified after read-back.
--save-plan also returns saved_plan: version,files,sha256,verified from actual saved bytes checked before and after apply.
Only when recounting is authorized: --spec FILE --recount 1:1 adjusts the first file's first replacement; other counts stay strict. Corrections are reported.
Shared spec replacements: {"version":1,"groups":[{"paths":["a.txt","b.txt"],"replacements":[{"old":"before","new":"after","count":1}]}]}
Groups expand after files, in input order. Counts and hashes are verified per file; saved plans use the original files schema. --recount indexes expanded files.
Independent file validation errors are returned together in diagnostics; no plan is saved and no source is written on failure.
`

type fsStrings []string

func (values *fsStrings) String() string { return strings.Join(*values, ",") }
func (values *fsStrings) Set(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	*values = append(*values, value)
	return nil
}

func runFilesystem(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, fsHelp)
		return 0
	}
	action := args[0]
	if action != "inspect" && action != "delta" && action != "apply" && action != "test-results" {
		fmt.Fprintf(stderr, "unknown fs command %q\n", action)
		return 2
	}
	flags := flag.NewFlagSet("tools fs "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := filesystem.Options{}
	flags.StringVar(&options.Root, "root", ".", "directory tree to access")
	flags.IntVar(&options.MaxFiles, "max-files", 200, "maximum selected files (partial results do not advance state)")
	flags.Int64Var(&options.MaxFileBytes, "max-file-bytes", 1<<20, "maximum bytes per file")
	flags.IntVar(&options.MaxOutputBytes, "max-output-bytes", 64<<10, "maximum inspect/delta result bytes")
	jsonOutput := flags.Bool("json", false, "emit structured JSON")
	var include, exclude, paths, patterns, contentKinds, recounts, sections fsStrings
	var hash, regex, content, reset, execute, peek, reportChanges, raw, outline bool
	comparisons := 1
	sectionOccurrence := 0
	var lineRange, stateFile, planFile, specFile, savePlan, requestFile, cursor string
	var saveReport, readReport, reportSHA string
	if action != "inspect" {
		flags.StringVar(&saveReport, "save-report", "", "save exact unabridged report without overwriting; overflow reports are otherwise saved under root/.tools/state/fs-reports")
		flags.StringVar(&readReport, "read-report", "", "read a saved observation only; never applies edits or advances a delta baseline")
		flags.StringVar(&reportSHA, "report-sha256", "", "expected SHA-256 required with --read-report")
		flags.Var(&patterns, "pattern", "select saved report diagnostic/change lines containing exact text")
		flags.StringVar(&lineRange, "range", "", "select saved diagnostic/change lines by 1-based inclusive START:END")
	}
	contextLines := 0
	afterTime := ""
	if action != "apply" {
		flags.Var(&include, "include", "include glob; repeatable (default all regular files)")
		flags.Var(&exclude, "exclude", "exclude glob; repeatable")
		flags.Var(&paths, "path", "select one root-relative file; repeatable")
	}
	if action == "apply" {
		flags.Var(&paths, "path", "select this path when reading a saved report; repeatable")
	}
	if action == "inspect" {
		flags.BoolVar(&outline, "outline", false, "list top-level ATX/single-line Setext headings outside fenced/indented code")
		flags.Var(&sections, "section", "read an exact section including children; duplicate titles use --section-occurrence or per-file --request")
		flags.IntVar(&sectionOccurrence, "section-occurrence", 0, "select this 1-based occurrence for every --section and selected file; default requires a unique heading")
		flags.Var(&patterns, "pattern", "literal text in file contents; use --path for a filename or --include for file globs; repeatable")
		flags.BoolVar(&regex, "regex", false, "patterns are Go regular expressions")
		flags.BoolVar(&hash, "hash", false, "include original-byte SHA-256")
		flags.StringVar(&lineRange, "range", "", "1-based inclusive START:END range")
		flags.StringVar(&requestFile, "request", "", "version 1 JSON with file-specific ranges/patterns, or - for stdin")
		flags.StringVar(&cursor, "cursor", "", "continue an unchanged query using next_cursor")
		flags.BoolVar(&raw, "raw", false, "return exact UTF-8 fragments; with an explicit path and no selector read the whole file")
	}
	if action == "delta" {
		flags.StringVar(&stateFile, "state-file", "", "baseline snapshot file (required)")
		flags.BoolVar(&content, "include-content", false, "include changed text ranges and store baseline text")
		flags.BoolVar(&reset, "reset", false, "replace a valid same-root baseline for a new selection")
		flags.BoolVar(&peek, "peek", false, "compare without advancing an existing baseline")
		flags.Var(&contentKinds, "content-kinds", "include text only for this change kind; added/modified/deleted, repeatable")
		flags.IntVar(&comparisons, "comparisons", 1, "real repeated comparisons (1–10); >1 requires --peek and deduplicates identical reports")
	}
	{
		flags.IntVar(&contextLines, "context", 0, "lines around matches or changed spans")
	}
	if action == "test-results" {
		flags.StringVar(&afterTime, "after", "", "optional RFC3339 report mtime lower bound; does not prove test execution")
	}
	if action == "apply" {
		flags.StringVar(&planFile, "plan", "", "version 1 JSON edit plan, or - for stdin")
		flags.BoolVar(&execute, "apply", false, "apply the fully validated plan (default preview)")
		flags.StringVar(&specFile, "spec", "", "hashless version 1 edit spec; create hashes internally, or - for stdin")
		flags.StringVar(&savePlan, "save-plan", "", "save a generated validated plan without overwriting an existing file")
		flags.BoolVar(&reportChanges, "report-changes", false, "include changed lines; applied files are always read back and verified")
		flags.Var(&recounts, "recount", "authorize recalculating exactly FILE_INDEX:REPLACEMENT_INDEX (1-based); --spec only, repeatable")
	}
	if action == "apply" || action == "delta" {
		flags.StringVar(&options.ReportPattern, "report-pattern", "", "return exact changed lines matching this literal text in the first response")
		flags.IntVar(&options.ReportContext, "report-context", 0, "lines around explicitly selected report matches (0–1000)")
	}
	if action == "test-results" {
		flags.StringVar(&options.ReportPattern, "diagnostic-pattern", "", "return exact diagnostic context matching literal text while preserving all counts and XML SHA")
		flags.IntVar(&options.ReportContext, "diagnostic-context", 0, "lines around explicitly selected diagnostic matches (0–1000)")
	}
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: tools fs %s [options]\n", action)
		flags.PrintDefaults()
		if action == "apply" {
			fmt.Fprint(stderr, fsApplyFormat)
		}
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	failure := func(err error) int {
		if *jsonOutput {
			value := map[string]any{"status": "error", "error": err.Error()}
			var diagnostic *filesystem.EditDiagnostic
			if errors.As(err, &diagnostic) {
				value["diagnostic"] = diagnostic
			}
			var diagnostics *filesystem.EditDiagnostics
			if errors.As(err, &diagnostics) && len(diagnostics.Diagnostics) > 1 {
				value["diagnostics"] = diagnostics.Diagnostics
			}
			encode(out, stderr, value)
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return 1
	}
	if flags.NArg() > 0 {
		return failure(errors.New("unexpected positional arguments"))
	}
	options.Include, options.Exclude, options.Paths = include, exclude, paths
	if readReport != "" {
		allowed := map[string]bool{"read-report": true, "report-sha256": true, "path": true, "range": true, "pattern": true, "context": true, "max-output-bytes": true, "json": true}
		var invalid string
		flags.Visit(func(option *flag.Flag) {
			if !allowed[option.Name] {
				invalid = option.Name
			}
		})
		if invalid != "" || len(patterns) > 1 || contextLines < 0 || contextLines > 1000 || options.MaxOutputBytes < 256 || options.MaxOutputBytes > 16<<20 {
			return failure(errors.New("read-report accepts only report SHA, path, one pattern, range, context (0–1000), output budget and json"))
		}
		start, end, err := parseEvidenceRange(lineRange)
		if err != nil {
			return failure(err)
		}
		pattern := ""
		if len(patterns) > 0 {
			pattern = patterns[0]
		}
		value, err := readFSReport(ctx, action, readReport, reportSHA, paths, start, end, pattern, contextLines, options.MaxOutputBytes)
		if err != nil {
			return failure(err)
		}
		return encode(out, stderr, value)
	}
	if action != "inspect" && (reportSHA != "" || lineRange != "" || len(patterns) > 0 || action == "apply" && len(paths) > 0) {
		return failure(errors.New("report selectors require --read-report"))
	}
	if options.ReportContext < 0 || options.ReportContext > 1000 || options.ReportContext > 0 && options.ReportPattern == "" {
		return failure(errors.New("report/diagnostic context must be 0–1000 and requires its pattern"))
	}
	if action == "apply" && options.ReportPattern != "" && !reportChanges {
		return failure(errors.New("report-pattern requires --report-changes"))
	}
	if action == "delta" && options.ReportPattern != "" && !content {
		return failure(errors.New("report-pattern requires --include-content"))
	}
	if saveReport != "" {
		if _, err := os.Lstat(saveReport); !errors.Is(err, os.ErrNotExist) {
			return failure(errors.New("save-report destination exists or is unavailable; choose a new file"))
		}
	}
	var archive fsReportArchive
	var captureErr error
	if action != "inspect" {
		options.CaptureAllReports = saveReport != ""
		captureFSReport(action, &options, &archive, &captureErr)
	}

	var result any
	var err error
	complete := true
	switch action {
	case "inspect":
		if sectionOccurrence < 0 || sectionOccurrence > 0 && len(sections) == 0 {
			return failure(errors.New("--section-occurrence must be positive and requires --section"))
		}
		opts := filesystem.InspectOptions{Options: options, Patterns: patterns, Regex: regex, Context: contextLines, Hash: hash, Raw: raw, Outline: outline}
		for _, title := range sections {
			opts.Sections = append(opts.Sections, filesystem.SectionRequest{Title: title, Occurrence: sectionOccurrence})
		}
		opts.Cursor = cursor
		if requestFile != "" {
			var batch filesystem.ReadBatch
			if e := readFSJSON(requestFile, in, &batch); e != nil {
				return failure(e)
			}
			if batch.Version != 1 || len(batch.Files) == 0 {
				return failure(errors.New("request version must be 1 with nonempty files"))
			}
			opts.Requests = batch.Files
		}
		if lineRange != "" {
			parts := strings.Split(lineRange, ":")
			if len(parts) != 2 {
				return failure(errors.New("range must be START:END"))
			}
			opts.Start, err = strconv.Atoi(parts[0])
			if err != nil {
				return failure(err)
			}
			opts.End, err = strconv.Atoi(parts[1])
			if err != nil {
				return failure(err)
			}
			if opts.Start < 1 || opts.End < opts.Start {
				return failure(errors.New("range must be positive and ordered"))
			}
		}
		value, e := filesystem.Inspect(ctx, opts)
		result, err, complete = value, e, value.Complete || value.Status == "page"
	case "test-results":
		opts := filesystem.TestResultOptions{Options: options}
		if afterTime != "" {
			opts.After, err = time.Parse(time.RFC3339Nano, afterTime)
			if err != nil {
				return failure(fmt.Errorf("--after must be RFC3339: %w", err))
			}
		}
		value, e := filesystem.ReadTestResults(ctx, opts)
		result, err, complete = value, e, value.Complete
	case "delta":
		if saveReport != "" && comparisons > 1 {
			return failure(errors.New("saved report recovery currently requires one delta observation; use --comparisons=1"))
		}
		if comparisons < 1 || comparisons > 10 {
			return failure(errors.New("--comparisons must be 1–10"))
		}
		if comparisons > 1 {
			options.CaptureReport = nil
		}
		value, e := filesystem.Delta(ctx, filesystem.DeltaOptions{Options: options, StateFile: stateFile, Content: content, Context: contextLines, Reset: reset, Peek: peek, ContentKinds: contentKinds, Comparisons: comparisons})
		result, err, complete = value, e, value.Complete
	case "apply":
		if (planFile == "") == (specFile == "") {
			return failure(errors.New("choose exactly one of --plan or --spec"))
		}
		if savePlan != "" && specFile == "" {
			return failure(errors.New("--save-plan requires --spec"))
		}
		if len(recounts) > 0 && specFile == "" {
			return failure(errors.New("--recount requires --spec"))
		}
		var plan filesystem.Plan
		var corrections []filesystem.EditDiagnostic
		var savedBytes []byte
		var savedDestination string
		var savedEvidence *filesystem.PlanEvidence
		input := planFile
		if specFile != "" {
			input = specFile
		}
		if e := readFSJSON(input, in, &plan); e != nil {
			return failure(e)
		}
		if specFile != "" {
			targets := []filesystem.CountTarget{}
			for _, value := range recounts {
				parts := strings.Split(value, ":")
				if len(parts) != 2 {
					return failure(errors.New("recount must be FILE_INDEX:REPLACEMENT_INDEX"))
				}
				file, e1 := strconv.Atoi(parts[0])
				replacement, e2 := strconv.Atoi(parts[1])
				if e1 != nil || e2 != nil || file < 1 || replacement < 1 {
					return failure(errors.New("recount indexes must be positive integers"))
				}
				targets = append(targets, filesystem.CountTarget{File: file, Replacement: replacement})
			}
			plan, corrections, err = filesystem.GeneratePlanWithRecounts(ctx, options, plan, targets)
			if err != nil {
				return failure(err)
			}
			if savePlan != "" {
				root, e := filepath.Abs(options.Root)
				if e != nil {
					return failure(e)
				}
				root, e = filepath.EvalSymlinks(root)
				if e != nil {
					return failure(e)
				}
				destination, e := filepath.Abs(savePlan)
				if e != nil {
					return failure(e)
				}
				parent, e := filepath.EvalSymlinks(filepath.Dir(destination))
				if e != nil {
					return failure(e)
				}
				destination = filepath.Join(parent, filepath.Base(destination))
				for _, edit := range plan.Files {
					if strings.EqualFold(destination, filepath.Join(root, filepath.FromSlash(edit.Path))) {
						return failure(errors.New("saved plan cannot be an edit destination"))
					}
				}
				var buffer bytes.Buffer
				if e := json.NewEncoder(&buffer).Encode(plan); e != nil {
					return failure(e)
				}
				savedBytes = buffer.Bytes()
				savedDestination = destination
				file, e := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if e != nil {
					return failure(e)
				}
				_, e = file.Write(savedBytes)
				if e == nil {
					e = file.Sync()
				}
				closeErr := file.Close()
				if e == nil {
					e = closeErr
				}
				if e != nil {
					os.Remove(destination)
					return failure(e)
				}
				if e := verifySavedPlan(destination, savedBytes); e != nil {
					return failure(e)
				}
				savedEvidence = &filesystem.PlanEvidence{SHA256: fmt.Sprintf("%x", sha256.Sum256(savedBytes)), Version: plan.Version, Files: len(plan.Files), Verified: true}
			}
		}
		if saveReport != "" {
			root, err := filepath.Abs(options.Root)
			if err != nil {
				return failure(err)
			}
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				return failure(err)
			}
			destination, err := filepath.Abs(saveReport)
			if err != nil {
				return failure(err)
			}
			parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
			if err != nil {
				return failure(err)
			}
			destination = filepath.Join(parent, filepath.Base(destination))
			for _, edit := range plan.Files {
				if strings.EqualFold(destination, filepath.Join(root, filepath.FromSlash(edit.Path))) {
					return failure(errors.New("saved report cannot be an edit destination"))
				}
			}
		}
		value, e := filesystem.ApplyWithReport(ctx, options, plan, execute, reportChanges)
		value.PlanFile = savePlan
		value.SavedPlan = savedEvidence
		value.Corrections = corrections
		if savedEvidence != nil {
			if e := verifySavedPlan(savedDestination, savedBytes); e != nil {
				savedEvidence.Verified = false
				value.Status = "partial"
				value.Complete = false
				savedEvidence.Error = e.Error()
			}
		}
		result, err, complete = value, e, value.Complete
	}
	if err != nil {
		return failure(err)
	}
	if action != "inspect" {
		object, e := reportObject(result)
		if e != nil {
			return failure(e)
		}
		overflow := false
		switch action {
		case "apply":
			overflow = object["report_complete"] == false
		case "delta":
			for _, change := range result.(filesystem.DeltaResult).Changes {
				overflow = overflow || change.RangesOmitted > 0
			}
		case "test-results":
			for _, report := range result.(filesystem.TestResults).Reports {
				overflow = overflow || report.DiagnosticsOmitted > 0
			}
		}
		if saveReport != "" || overflow && (action != "delta" || comparisons == 1) {
			// 메타데이터는 최종 상태를 사용하고 생략한 구간만 원문 관찰에서 복구한다.
			if len(archive.Payload) == 0 {
				archive.Payload, captureErr = json.Marshal(result)
			}
			if len(archive.Payload) > 0 {
				full, err := reportObject(json.RawMessage(archive.Payload))
				if err != nil {
					captureErr = err
				} else {
					keys := []string{"applied", "plan_file", "saved_plan", "corrections"}
					if action == "apply" || !overflow {
						keys = append(keys, "status", "complete", "state_updated")
					}
					for _, key := range keys {
						if object[key] != nil {
							full[key] = object[key]
						}
					}
					archive.Payload, captureErr = json.Marshal(full)
				}
			}
			if archive.Version == 0 {
				archive.Version, archive.Kind, archive.ObservedAt = 1, action, time.Now().UTC().Format(time.RFC3339Nano)
				archive.Options = options
				archive.Options.CaptureReport = nil
			}
			var proof *reportReference
			if captureErr != nil {
				proof = &reportReference{Path: saveReport, Error: captureErr.Error()}
			} else {
				automaticRoot := ""
				if saveReport == "" {
					root := archive.Options.Root
					if root == "" {
						root = options.Root
					}
					automaticRoot = root
					saveReport = filepath.Join(root, ".tools", "state", "fs-reports", fmt.Sprintf("%d-%x.json", time.Now().UnixNano(), sha256.Sum256(archive.Payload)))
				}
				proof = saveFSReport(saveReport, archive, automaticRoot)
			}
			object["saved_report"] = proof
			if !proof.Verified {
				object["status"], object["complete"], complete = "partial", false, false
			}
			result = object
		}
	}
	// JSON is the stable batch interface; human output uses the same result without a second interpretation.
	if encode(out, stderr, result) != 0 {
		return 1
	}
	if !complete {
		return 1
	}
	return 0
}

func verifySavedPlan(name string, expected []byte) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return errors.New("saved plan type or size changed")
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, expected) {
		return errors.New("saved plan bytes changed")
	}
	return nil
}

func readFSJSON(name string, in io.Reader, value any) error {
	reader := in
	if name != "-" {
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 32<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("input contains trailing JSON or exceeds size limit")
	}
	return nil
}
