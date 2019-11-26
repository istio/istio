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

package curl

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Masterminds/semver"

	"istio.io/istio/pkg/test"
)

// VersionInfo provides the detailed information for the version of curl on the system.
type VersionInfo struct {
	// Version of curl.
	Version *semver.Version

	// Protocols supported by this version of curl.
	Protocols []string

	// Features supported by this version of curl.
	Features []string
}

// RequireMinVersionOrFail calls RequireMinVersion. If it returns an error, fails the given test.
func RequireMinVersionOrFail(t test.Failer, min *semver.Version) {
	t.Helper()
	if err := RequireMinVersion(min); err != nil {
		t.Fatal(err)
	}
}

// RequireMinVersion returns an error if the version of curl on the system does not meet the specified minimum.
func RequireMinVersion(min *semver.Version) error {
	v, err := GetVersion()
	if err != nil {
		return err
	}
	if v.Version.LessThan(min) {
		return fmt.Errorf("curl version %s less than min required %s. OS details: %s",
			v.Version.String(), min.String(), getOSInfo())
	}
	return nil
}

// GetVersion gets the version of curl on the system.
func GetVersion() (VersionInfo, error) {
	result, err := exec.Command("curl", "--version").CombinedOutput()
	if err != nil {
		return VersionInfo{}, fmt.Errorf("failed running curl, check $PATH. %s %v", string(result), err)
	}

	lines := strings.Split(string(result), "\n")

	// Parse the first line.
	line := lines[0]
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return VersionInfo{}, fmt.Errorf("failed parsing curl version. Unexpected field count on line %s", line)
	}
	if fields[0] != "curl" {
		return VersionInfo{}, fmt.Errorf("failed parsing curl version. Unexpected first field on line: %s", line)
	}
	sver, err := semver.NewVersion(fields[1])
	if err != nil {
		return VersionInfo{}, fmt.Errorf("failed parsing curl version: %v", err)
	}

	v := VersionInfo{
		Version: sver,
	}

	// Parse the remaining lines
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "Protocols:") {
			v.Protocols = strings.Fields(line)[1:]
		}
		if strings.HasPrefix(line, "Features:") {
			v.Features = strings.Fields(line)[1:]
		}
	}

	return v, nil
}

func getOSInfo() string {
	switch runtime.GOOS {
	case "linux":
		// Not using uname because on containers it will give info about the kernel, not the
		// container.
		out, err := exec.Command("cat", "/etc/os-release").Output()
		if err != nil {
			return fmt.Sprintf("failed to retrieve OS info: %v", err)
		}
		return string(out)
	case "mac":
		out, err := exec.Command("sw_vers").Output()
		if err != nil {
			return fmt.Sprintf("failed to retrieve OS info: %v", err)
		}
		return string(out)
	default:
		return fmt.Sprintf("Unable to retrieve info for OS: %s", runtime.GOOS)
	}
}
