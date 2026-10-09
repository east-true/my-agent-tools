// Build all supported binaries using only Go, on Linux, macOS or Windows.
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	var checksums strings.Builder
	for _, target := range []struct{ os, arch string }{
		{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	} {
		name := "tools"
		if target.os == "windows" {
			name += ".exe"
		}
		relative := filepath.Join(target.os+"-"+target.arch, name)
		path := filepath.Join("dist", relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", path, "./cmd/tools")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GOOS=") && !strings.HasPrefix(value, "GOARCH=") && !strings.HasPrefix(value, "CGO_ENABLED=") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS="+target.os, "GOARCH="+target.arch, "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s/%s: %w", target.os, target.arch, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&checksums, "%x  %s\n", sha256.Sum256(data), filepath.ToSlash(relative))
		fmt.Println(path)
	}
	return os.WriteFile(filepath.Join("dist", "SHA256SUMS"), []byte(checksums.String()), 0o644)
}
