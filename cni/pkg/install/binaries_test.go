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

package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyBinaries(t *testing.T) {
	cases := []struct {
		name           string
		srcFiles       map[string]string // {filename: contents, ...}
		existingFiles  map[string]string // {filename: contents, ...}
		expectedFiles  map[string]string // {filename: contents. ...}
		updateBinaries bool
		skipBinaries   []string
	}{
		{
			name:          "basic",
			srcFiles:      map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			expectedFiles: map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
		},
		{
			name:           "update binaries",
			updateBinaries: true,
			srcFiles:       map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			existingFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
			expectedFiles:  map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
		},
		{
			name:           "don't update binaries",
			updateBinaries: false,
			srcFiles:       map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			existingFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
			expectedFiles:  map[string]string{"istio-cni": "cni000", "istio-iptables": "iptables111"},
		},
		{
			name:          "skip binaries",
			skipBinaries:  []string{"istio-iptables"},
			srcFiles:      map[string]string{"istio-cni": "cni111", "istio-iptables": "iptables111"},
			expectedFiles: map[string]string{"istio-cni": "cni111"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srcDir := t.TempDir()
			for filename, contents := range c.srcFiles {
				err := os.WriteFile(filepath.Join(srcDir, filename), []byte(contents), os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			targetDir := t.TempDir()
			for filename, contents := range c.existingFiles {
				err := os.WriteFile(filepath.Join(targetDir, filename), []byte(contents), os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			err := copyBinaries(srcDir, []string{targetDir}, c.updateBinaries, c.skipBinaries)
			if err != nil {
				t.Fatal(err)
			}

			for filename, expectedContents := range c.expectedFiles {
				contents, err := os.ReadFile(filepath.Join(targetDir, filename))
				if err != nil {
					t.Fatal(err)
				}
				if string(contents) != expectedContents {
					t.Fatalf("target file contents don't match source file; actual: %s", string(contents))
				}
			}
		})
	}
}
