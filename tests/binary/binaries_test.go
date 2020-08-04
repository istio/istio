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

package binary

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"istio.io/pkg/version"
)

var (
	binaries   *string
	releasedir *string
)

func TestMain(m *testing.M) {
	releasedir = flag.String("base-dir", "", "directory for binaries")
	binaries = flag.String("binaries", "", "space separated binaries to test")
	flag.Parse()
	os.Exit(m.Run())
}

func TestVersion(t *testing.T) {
	binariesToTest := strings.Split(*binaries, " ")
	if len(binariesToTest) == 0 {
		t.Fatal("No binaries to test. Pass the --binaries flag.")
	}
	for _, b := range binariesToTest {
		cmd := path.Join(*releasedir, b)
		t.Run(b, func(t *testing.T) {
			if b == "istiod" {
				// Currently istiod is not using CLI flags - version/etc will be included in a
				// detached file.
				return
			}

			args := []string{"version", "-ojson"}
			if b == "istioctl" {
				args = append(args, "--remote=false")
			}

			out, err := exec.Command(cmd, args...).Output()
			if err != nil {
				t.Fatalf("--version failed with error: %v. Output: %v", err, string(out))
			}

			var resp version.Version
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("Failed to marshal to json: %v. Output: %v", err, string(out))
			}

			verInfo := resp.ClientVersion

			validateField(t, "Version", verInfo.Version)
			validateField(t, "GitRevision", verInfo.GitRevision)
			validateField(t, "GolangVersion", verInfo.GolangVersion)
			validateField(t, "BuildStatus", verInfo.BuildStatus)
			validateField(t, "GitTag", verInfo.GitTag)
		})
	}
}

var (
	// If this flag is present, it means "testing" was imported by code that is built by the binary
	denylistedFlags = []string{
		"--test.memprofilerate",
	}
)

// Test that flags do not get polluted with unexpected flags
func TestFlags(t *testing.T) {
	binariesToTest := strings.Split(*binaries, " ")
	if len(binariesToTest) == 0 {
		t.Fatal("No binaries to test. Pass the --binaries flag.")
	}
	for _, b := range binariesToTest {
		cmd := path.Join(*releasedir, b)
		t.Run(b, func(t *testing.T) {
			if b == "istiod" {
				// Currently istiod is not using CLI flags - version/etc will be included in a
				// detached file.
				return
			}
			out, err := exec.Command(cmd, "--help").Output()
			if err != nil {
				t.Fatalf("--help failed with error: %v. Output: %v", err, string(out))
			}

			for _, denylist := range denylistedFlags {
				if strings.Contains(string(out), denylist) {
					t.Fatalf("binary contains unexpected flags: %v", string(out))
				}
			}
		})
	}
}

// TODO we may be able to do more validation of fields here, but because it changes based on the environment this is not easy
// For now ensuring the fields even get populated is most important.
func validateField(t *testing.T, field, s string) {
	t.Helper()
	if s == "unknown" || s == "" {
		t.Errorf("Field %v was invalid. Got: %v.", field, s)
	}
}
