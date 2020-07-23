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

package util

import (
	"io/ioutil"
	"path/filepath"
	"testing"
)

func CopyExistingConfFiles(t *testing.T, targetDir string, confFiles ...string) {
	t.Helper()
	for _, f := range confFiles {
		data, err := ioutil.ReadFile(filepath.Join("testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		err = ioutil.WriteFile(filepath.Join(targetDir, f), data, 0644)
		if err != nil {
			t.Fatal(err)
		}
	}
}
