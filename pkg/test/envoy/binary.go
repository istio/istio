// Copyright Istio Authors
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

package envoy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
)

var (
	envoyFileNamePattern = regexp.MustCompile("^envoy$|^envoy-[a-f0-9]+$|^envoy-debug-[a-f0-9]+$")
)

// FindBinary searches for an Envoy debug binary under ISTIO_OUT. If the ISTIO_OUT environment variable
// is not set, the default location under GOPATH is assumed. If ISTIO_OUT contains multiple debug binaries,
// the most recent file is used.
func FindBinary() (string, error) {
	binPaths, err := findBinaries()
	if err != nil {
		return "", err
	}

	if len(binPaths) == 0 {
		return "", fmt.Errorf("unable to locate an Envoy binary under dir %s", env.IstioOut)
	}

	latestBinPath, err := findMostRecentFile(binPaths)
	if err != nil {
		return "", err
	}

	return latestBinPath, nil
}

// FindBinaryOrFail calls FindBinary and fails the given test if an error occurs.
func FindBinaryOrFail(t test.Failer) string {
	p, err := FindBinary()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func findBinaries() ([]string, error) {
	binPaths := make([]string, 0)
	err := filepath.Walk(env.LocalOut, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !f.IsDir() && envoyFileNamePattern.MatchString(f.Name()) {
			binPaths = append(binPaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return binPaths, nil
}

func findMostRecentFile(filePaths []string) (string, error) {
	latestFilePath := ""
	latestFileTime := int64(0)
	for _, filePath := range filePaths {
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			// Should never happen
			return "", err
		}
		fileTime := fileInfo.ModTime().Unix()
		if fileTime > latestFileTime {
			latestFileTime = fileTime
			latestFilePath = filePath
		}
	}
	return latestFilePath, nil
}
