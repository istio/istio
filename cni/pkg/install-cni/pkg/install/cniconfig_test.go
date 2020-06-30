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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	testutils "istio.io/istio/pilot/test/util"
)

func TestGetDefaultCNINetwork(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	cases := []struct {
		name            string
		dir             string
		inFilename      string
		outFilename     string
		fileContents    string
		expectedFailure bool
	}{
		{
			name:            "inexistent directory",
			dir:             "/inexistent/directory",
			expectedFailure: true,
		},
		{
			name:            "empty directory",
			dir:             tempDir,
			expectedFailure: true,
		},
		{
			name:            "empty file",
			dir:             tempDir,
			expectedFailure: true,
			inFilename:      "empty.conf",
		},
		{
			name:            "regular file",
			dir:             tempDir,
			expectedFailure: false,
			inFilename:      "regular.conf",
			outFilename:     "regular.conf",
			fileContents: `
{
	"cniVersion": "0.3.1",
	"name": "istio-cni",
	"type": "istio-cni"
}`,
		},
		{
			name:            "another regular file",
			dir:             tempDir,
			expectedFailure: false,
			inFilename:      "regular2.conf",
			outFilename:     "regular.conf",
			fileContents: `
{
	"cniVersion": "0.3.1",
	"name": "istio-cni",
	"type": "istio-cni"
}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.fileContents != "" {
				err = ioutil.WriteFile(filepath.Join(c.dir, c.inFilename), []byte(c.fileContents), 0644)
				if err != nil {
					t.Fatal(err)
				}
			}

			result, err := getDefaultCNINetwork(c.dir)
			if (c.expectedFailure && err == nil) || (!c.expectedFailure && err != nil) {
				t.Fatalf("expected failure: %t, got %v", c.expectedFailure, err)
			}

			if c.fileContents != "" {
				if c.outFilename != result {
					t.Fatalf("expected %s, got %s", c.outFilename, result)
				}
			}
		})
	}
}

func TestTransformCNIPluginIntoList(t *testing.T) {
	cases := []struct {
		name   string
		inFile string
	}{
		{
			name:   "regular network file",
			inFile: "bridge.conf",
		},
		{
			name:   "list network file",
			inFile: "list.conflist",
		},
	}

	istioConf := testutils.ReadFile("testdata/istio-cni.conf", t)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			existingConfFilename := "testdata/" + c.inFile
			existingConf := testutils.ReadFile(existingConfFilename, t)

			output, err := transformCNIPluginIntoList(istioConf, existingConf)
			if err != nil {
				t.Fatal(err)
			}

			goldenFilename := existingConfFilename + ".golden"
			golden := testutils.ReadFile(goldenFilename, t)
			testutils.CompareBytes(output, golden, goldenFilename, t)
		})
	}
}
