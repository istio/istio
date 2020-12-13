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

package mesh

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
)

func TestManifestDiffFromFiles(t *testing.T) {
	g := gomega.NewWithT(t)
	testDataDir = filepath.Join(operatorRootDir, "cmd/mesh/testdata/profile-diff")
	file1Path := filepath.Join(testDataDir, "profile1.yaml")
	file2Path := filepath.Join(testDataDir, "profile2.yaml")

	args := []string{"manifest", "diff", "--dry-run", file1Path, file2Path}

	rootCmd := GetRootCmd(args)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute istioctl profile command: %v", err)
	}
	output := out.String()

	diff, err := ioutil.ReadFile(filepath.Join(testDataDir, "diff.txt"))
	if err != nil {
		t.Fatalf("failed to read profile diff file: %v", err)
	}

	diffStr := string(diff)
	g.Expect(output).To(gomega.Equal(diffStr))
}

func TestManifestDiffFromDirs(t *testing.T) {
	g := gomega.NewWithT(t)
	testDataDir1 := filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifestdir1")
	testDataDir2 := filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifestdir2")

	args := []string{"manifest", "diff", "-r", "--dry-run", testDataDir1, testDataDir2}

	rootCmd := GetRootCmd(args)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute istioctl profile command: %v", err)
	}
	output := out.String()

	diff, err := ioutil.ReadFile(filepath.Join(testDataDir, "diff.txt"))
	if err != nil {
		t.Fatalf("failed to read profile diff file: %v", err)
	}

	diffStr := string(diff)
	g.Expect(output).To(gomega.Equal(diffStr))
}
