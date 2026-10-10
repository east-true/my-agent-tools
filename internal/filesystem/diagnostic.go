package filesystem

import (
	"errors"
	"fmt"
	"strings"
)

type EditDiagnostic struct {
	Path        string `json:"path"`
	Code        string `json:"code"`
	Replacement int    `json:"replacement_index,omitempty"`
	Expected    *int   `json:"expected_count,omitempty"`
	Actual      *int   `json:"actual_count,omitempty"`
	Message     string `json:"message"`
}

// EditDiagnostics collects independent file errors without returning a usable
// plan or making writes. Unwrap retains the existing single-diagnostic contract.
type EditDiagnostics struct {
	Diagnostics []EditDiagnostic `json:"diagnostics"`
}

func (d *EditDiagnostics) Error() string {
	messages := make([]string, len(d.Diagnostics))
	for i := range d.Diagnostics {
		messages[i] = d.Diagnostics[i].Error()
	}
	return strings.Join(messages, "; ")
}

func (d *EditDiagnostics) Unwrap() []error {
	result := make([]error, len(d.Diagnostics))
	for i := range d.Diagnostics {
		result[i] = &d.Diagnostics[i]
	}
	return result
}

func (d *EditDiagnostic) Error() string {
	if d.Replacement > 0 {
		return fmt.Sprintf("%s replacement %d: %s (expected %d, actual %d)", d.Path, d.Replacement, d.Message, *d.Expected, *d.Actual)
	}
	return fmt.Sprintf("%s: %s", d.Path, d.Message)
}

func editError(path string, err error) error {
	if err == nil {
		return nil
	}
	var d *EditDiagnostic
	if errors.As(err, &d) {
		return err
	}
	return &EditDiagnostic{Path: path, Code: "invalid_edit", Message: err.Error()}
}
