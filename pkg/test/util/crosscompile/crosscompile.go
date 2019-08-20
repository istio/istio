// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package crosscompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	GoosLinux   = "linux"
	GoarchAmd64 = "amd64"
)

// Config for a cross-compile operation.
type Config struct {
	GOOS    string
	GOARCH  string
	SrcRoot string
	SrcPath string
	OutDir  string
}

// LinuxAmd64 is a utility function that returns a copy of this config with GOOS="linux" and GOARCH="amd64".
func (c Config) LinuxAmd64() Config {
	out := c
	out.GOOS = GoosLinux
	out.GOARCH = GoarchAmd64
	return out
}

// Do cross-compiles the given app for the target OS and ARCH and returns the path to the binary.
func Do(cfg Config) (string, error) {
	// Lookup the go command.
	goCmd, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}

	filename := filepath.Base(cfg.SrcPath)
	outFile := filepath.Join(cfg.OutDir, filename)

	// Convert srcPath to a relative path.
	if !strings.HasPrefix(cfg.SrcPath, "./") {
		// Get the path to build relative to IstioSrc.
		relativePath, err := filepath.Rel(cfg.SrcRoot, cfg.SrcPath)
		if err != nil {
			return "", fmt.Errorf("failed building Go application %s. Source must be under %s",
				cfg.SrcPath, cfg.SrcRoot)
		}
		cfg.SrcPath = "./" + relativePath
	}

	// Run `go build` relative to IstioSrc.
	cmd := &exec.Cmd{
		Path: goCmd,
		Args: []string{
			goCmd,
			"build",
			"-o",
			outFile,
			cfg.SrcPath,
		},
		Dir: cfg.SrcRoot,
	}

	// Setup cross-compile on the target platform.
	cmd.Env = append(os.Environ(),
		"GOOS="+cfg.GOOS,
		"GOARCH="+cfg.GOARCH,
		"CGO_ENABLED=0",
	)

	var out []byte
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed running go build. Error: %v, Output: %s", err, string(out))
	}

	return outFile, nil
}
