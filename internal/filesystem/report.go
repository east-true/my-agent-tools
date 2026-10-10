package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ReportSnapshot은 출력 상한 적용 전의 관찰을 저장하는 읽기 전용 자료다.
// 저장된 apply 결과는 현재 파일 상태를 보장하지 않는다.
type ReportSnapshot struct {
	Value     any
	Options   Options
	Files     map[string]string
	Selection bool
}

func (s *scope) captureReport(value any, files map[string]string, selection, overflow bool) {
	if s.options.CaptureReport == nil || !overflow && !s.options.CaptureAllReports {
		return
	}
	options := s.options
	options.Root, options.CaptureReport = s.name, nil
	s.options.CaptureReport(ReportSnapshot{Value: value, Options: options, Files: files, Selection: selection})
}

// VerifyReportFiles는 기존 관찰과 새 파일 내용을 섞기 전에 SHA와 선택 범위를 확인한다.
func VerifyReportFiles(ctx context.Context, options Options, files map[string]string, selection bool) error {
	s, err := openScope(options)
	if err != nil {
		return err
	}
	defer s.root.Close()
	s.missingPaths = map[string]bool{}
	for name, hash := range files {
		s.missingPaths[name] = hash == "absent"
	}
	if selection {
		names, problems, err := s.files(ctx)
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			return errors.New("saved report selection cannot be verified")
		}
		expected := []string{}
		for name, hash := range files {
			if hash != "absent" {
				expected = append(expected, name)
			}
		}
		sort.Strings(expected)
		sort.Strings(names)
		if a, _ := json.Marshal(names); string(a) != mustJSON(expected) {
			return errors.New("files selected by the saved observation changed")
		}
	}
	for name, expected := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := cleanPath(name); err != nil {
			return err
		}
		if expected == "absent" {
			if err := s.verifyAbsentPath(name); err != nil {
				return err
			}
			if _, err := s.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("saved absent file changed: %s", name)
			}
			continue
		}
		data, mode, err := s.read(name)
		parts := strings.Split(expected, ":")
		if err != nil || digest(data) != parts[0] || len(parts) > 1 && fmt.Sprintf("%o", mode) != parts[1] {
			return fmt.Errorf("saved report file changed or is unavailable: %s", name)
		}
	}
	return nil
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// ValidatePaths는 파일 내용 재조회 없이 루트·명시 경로의 입력 계약을 확인한다.
func ValidatePaths(options Options) error {
	s, err := openScope(options)
	if err != nil {
		return err
	}
	defer s.root.Close()
	for _, name := range options.Paths {
		if _, err := cleanPath(name); err != nil {
			return err
		}
	}
	return nil
}

func (s *scope) verifyAbsentPath(name string) error {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("saved absent path has a symbolic-link ancestor")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return errors.New("saved absent path has a non-directory ancestor")
		}
	}
	return errors.New("saved absent file now exists")
}
