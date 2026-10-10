package filesystem

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Options struct {
	ReportPattern           string               `json:"-"`
	ReportContext           int                  `json:"-"`
	CaptureReport           func(ReportSnapshot) `json:"-"`
	CaptureAllReports       bool                 `json:"-"`
	Root                    string
	Include, Exclude, Paths []string
	MaxFiles                int
	MaxFileBytes            int64
	MaxOutputBytes          int
}

type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type scope struct {
	missingPaths map[string]bool
	root         *os.Root
	name         string
	options      Options
}

func openScope(options Options) (*scope, error) {
	if options.Root == "" {
		options.Root = "."
	}
	if options.MaxFiles == 0 {
		options.MaxFiles = 200
	}
	if options.MaxFileBytes == 0 {
		options.MaxFileBytes = 1 << 20
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = 64 << 10
	}
	if options.ReportContext < 0 || options.ReportContext > 1000 {
		return nil, errors.New("report context must be 0–1000")
	}
	if options.MaxFiles < 1 || options.MaxFiles > 10000 || options.MaxFileBytes < 1 || options.MaxFileBytes > 16<<20 || options.MaxOutputBytes < 256 || options.MaxOutputBytes > 16<<20 {
		return nil, errors.New("invalid limits: files 1–10000, file bytes 1–16777216, output bytes 256–16777216")
	}
	for _, p := range append(append([]string{}, options.Include...), options.Exclude...) {
		for _, part := range strings.Split(p, "/") {
			if part != "**" {
				if _, err := path.Match(part, ""); err != nil {
					return nil, err
				}
			}
		}
	}
	name, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, err
	}
	name, err = filepath.EvalSymlinks(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &scope{root: root, name: name, options: options}, nil
}

func cleanPath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.ContainsAny(name, "\x00\r\n") || !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) || name == "." {
		return "", errors.New("path must be a root-relative slash-separated file name without . or .. components")
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".tools-fs-") {
			return "", errors.New("reserved filesystem workflow path")
		}
	}
	return name, nil
}

func match(pattern, name string) bool {
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(name))
		return ok
	}
	var walk func([]string, []string) bool
	walk = func(p, n []string) bool {
		if len(p) == 0 {
			return len(n) == 0
		}
		if p[0] == "**" {
			return walk(p[1:], n) || len(n) > 0 && walk(p, n[1:])
		}
		if len(n) == 0 {
			return false
		}
		ok, _ := path.Match(p[0], n[0])
		return ok && walk(p[1:], n[1:])
	}
	return walk(strings.Split(pattern, "/"), strings.Split(name, "/"))
}
func (s *scope) excluded(name string) bool {
	for _, part := range strings.Split(name, "/") {
		switch part {
		case ".git", ".tools", ".codex", ".claude", ".agents", ".aws", "node_modules", "dist", "artifacts", "__pycache__":
			return true
		}
		if strings.HasPrefix(part, ".tools-fs-") {
			return true
		}
	}
	for _, p := range s.options.Exclude {
		if match(p, name) {
			return true
		}
	}
	return false
}
func (s *scope) selected(name string) bool {
	if len(s.options.Include) == 0 {
		return true
	}
	for _, p := range s.options.Include {
		if match(p, name) {
			return true
		}
	}
	return false
}

func (s *scope) files(ctx context.Context) ([]string, []Problem, error) {
	names := []string{}
	problems := []Problem{}
	if len(s.options.Paths) > 0 {
		seen := map[string]bool{}
		for _, name := range s.options.Paths {
			name, err := cleanPath(name)
			if err != nil {
				return nil, nil, err
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			if s.excluded(name) || !s.selected(name) {
				problems = append(problems, Problem{name, "excluded by selection"})
				continue
			}
			if err := s.regularPath(name, false); err != nil {
				if s.missingPaths[name] && errors.Is(err, os.ErrNotExist) {
					continue
				}
				problems = append(problems, Problem{name, err.Error()})
				continue
			}
			names = append(names, name)
		}
	} else {
		err := fs.WalkDir(s.root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				problems = append(problems, Problem{name, err.Error()})
				return nil
			}
			if name == "." {
				return nil
			}
			if s.excluded(name) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				if s.selected(name) {
					problems = append(problems, Problem{name, "symbolic link skipped"})
				}
				return nil
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				if s.selected(name) {
					problems = append(problems, Problem{name, "not a regular file"})
				}
				return nil
			}
			if !d.IsDir() && s.selected(name) {
				names = append(names, name)
				if len(names) > s.options.MaxFiles {
					return fs.SkipAll
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(names) > s.options.MaxFiles {
		names = names[:s.options.MaxFiles]
		problems = append(problems, Problem{".", "file limit reached; selection is incomplete"})
	}
	return names, problems, nil
}

func (s *scope) regularPath(name string, absent bool) error {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) && absent && i == len(parts)-1 {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symbolic links are not accepted")
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return errors.New("not a regular file")
			}
		} else if !info.IsDir() {
			return errors.New("parent is not a directory")
		}
	}
	return nil
}
func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func (s *scope) read(name string) ([]byte, os.FileMode, error) {
	if err := s.regularPath(name, false); err != nil {
		return nil, 0, err
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !before.Mode().IsRegular() {
		return nil, 0, errors.New("not a regular file")
	}
	if before.Size() > s.options.MaxFileBytes {
		return nil, 0, errors.New("file byte limit exceeded")
	}
	data, err := io.ReadAll(io.LimitReader(file, s.options.MaxFileBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(data)) > s.options.MaxFileBytes {
		return nil, 0, errors.New("file byte limit exceeded")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, 0, errors.New("file changed while reading; retry")
	}
	return data, before.Mode().Perm(), nil
}
