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

	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/version"
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
	runBinariesTest(t, func(t *testing.T, name string) {
		if nonGoBinaries.Contains(name) {
			return
		}
		if nonVersionBinaries.Contains(name) {
			return
		}
		cmd := path.Join(*releasedir, name)
		args := []string{"version", "-ojson"}
		if name == "istioctl" {
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

var (
	nonGoBinaries      = sets.New("ztunnel", "envoy")
	nonVersionBinaries = sets.New("client", "server")
)

// Test that flags do not get polluted with unexpected flags
func TestFlags(t *testing.T) {
	runBinariesTest(t, func(t *testing.T, name string) {
		if nonGoBinaries.Contains(name) {
			return
		}
		cmd := path.Join(*releasedir, name)
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

// Test that binary sizes do not bloat
func TestBinarySizes(t *testing.T) {
	cases := map[string]struct {
		minMb int64
		maxMb int64
	}{
		// TODO: shrink the ranges here once the active work to reduce binary size is complete
		// For now, having two small a range will result in lots of "merge conflicts"
		"istioctl":        {60, 92},
		"pilot-agent":     {20, 26},
		"pilot-discovery": {60, 100},
		"bug-report":      {60, 80},
		"client":          {15, 30},
		"server":          {15, 30},
		"envoy":           {60, 150},
		"ztunnel":         {10, 15},
	}

	runBinariesTest(t, func(t *testing.T, name string) {
		tt, f := cases[name]
		if !f {
			t.Fatalf("min/max binary size not specified for %v", name)
		}
		cmd := path.Join(*releasedir, name)
		fi, err := os.Stat(cmd)
		if err != nil {
			t.Fatal(err)
		}
		got := fi.Size() / (1000 * 1000)
		t.Logf("Actual size: %dmb. Range: [%dmb, %dmb]", got, tt.minMb, tt.maxMb)
		if got > tt.maxMb {
			t.Fatalf("Binary size of %dmb was greater than max allowed size %dmb", got, tt.maxMb)
		}
		if got < tt.minMb {
			t.Fatalf("Binary size of %dmb was smaller than min allowed size %dmb. This is very likely a good thing, "+
				"indicating the binary size has decreased. The test will fail to ensure you update the test thresholds to ensure "+
				"the improvements are 'locked in'.", got, tt.minMb)
		}
	})
}

// If this flag is present, it means "testing" was imported by code that is built by the binary
var denylistedFlags = []string{
	"--test.memprofilerate",
}

func runBinariesTest(t *testing.T, f func(t *testing.T, name string)) {
	t.Helper()
	if *releasedir == "" {
		t.Skip("release dir not set")
	}
	binariesToTest := strings.Split(*binaries, " ")
	if len(binariesToTest) == 0 {
		t.Fatal("No binaries to test. Pass the --binaries flag.")
	}
	for _, b := range binariesToTest {
		t.Run(b, func(t *testing.T) {
			f(t, b)
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
