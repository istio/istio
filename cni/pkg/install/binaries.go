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

	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/util/sets"
)

// Copies/mirrors any files present in a single source dir to N number of target dirs
// and returns a set of the filenames copied.
func copyBinaries(srcDir string, targetDirs []string) (sets.String, error) {
	copiedFilenames := sets.String{}
	srcFiles, err := os.ReadDir(srcDir)
	if err != nil {
		return copiedFilenames, err
	}

	for _, f := range srcFiles {
		if f.IsDir() {
			continue
		}

		filename := f.Name()
		srcFilepath := filepath.Join(srcDir, filename)

		for _, targetDir := range targetDirs {
			if err := file.AtomicCopy(srcFilepath, targetDir, filename); err != nil {
				installLog.Errorf("Failed file copy of %s to %s: %s", srcFilepath, targetDir, err.Error())
				return copiedFilenames, err
			}
			installLog.Infof("Copied %s to %s.", filename, targetDir)
		}

		copiedFilenames.Insert(filename)
	}

	return copiedFilenames, nil
}
