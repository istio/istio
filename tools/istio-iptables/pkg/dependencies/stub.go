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

package dependencies

import (
	"io"
	"os"
	"fmt"
	"strings"

	"istio.io/istio/pkg/env"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

var DryRunFilePath = env.Register("DRY_RUN_FILE_PATH", "", "If provided, StdoutStubDependencies will write the input from stdin to the given file.")

// TODO BML replace DIY mocks/state with something better
type DependenciesStub struct {
	ExecutedNormally     []string
	ExecutedQuietly      []string
	ExecutedAll          []string
}

func (s *DependenciesStub) Run(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) error {
	s.execute(false /*quietly*/, cmd, iptVer, args...)
	_ = writeAllToDryRunPath(stdin)
	return nil
}

func (s *DependenciesStub) RunQuietlyAndIgnore(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) {
	s.execute(true /*quietly*/, cmd, iptVer, args...)
	_ = writeAllToDryRunPath(stdin)
}

// TODO BML this stub can be smarter
func (s *DependenciesStub) DetectIptablesVersion(overrideVersion string, ipV6 bool) (IptablesVersion, error) {
	if ipV6 {

			return IptablesVersion{
				DetectedBinary:        "ip6tables",
				DetectedSaveBinary:    "ip6tables-save",
				DetectedRestoreBinary: "ip6tables-restore",
			}, nil
	}
	return IptablesVersion{
		DetectedBinary:        "iptables",
		DetectedSaveBinary:    "iptables-save",
		DetectedRestoreBinary: "iptables-restore",
	}, nil
}

func (s *DependenciesStub) execute(quietly bool, cmd constants.IptablesCmd, iptVer *IptablesVersion, args ...string) {
	cmdline := strings.Join(append([]string{iptVer.CmdToString(cmd)}, args...), " ")
	s.ExecutedAll = append(s.ExecutedAll, cmdline)
	if quietly {
		s.ExecutedQuietly = append(s.ExecutedQuietly, cmdline)
	} else {
		s.ExecutedNormally = append(s.ExecutedNormally, cmdline)
	}
}

// TODO BML this is more than a stub actually needs to do, we should be able to drop this testing hack
func writeAllToDryRunPath(stdin io.ReadSeeker) error {
	path := DryRunFilePath.Get()
	if path != "" {
		// Print the input into the given output file.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("unable to open dry run output file %v: %v", path, err)
		}

		defer f.Close()
		if stdin != nil {
			if _, err = io.Copy(f, stdin); err != nil {
				return fmt.Errorf("unable to write dry run output file: %v", err)
			}
		}
	}
	return nil
}
