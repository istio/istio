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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

var DryRunFilePath = env.Register("DRY_RUN_FILE_PATH", "", "If provided, StdoutStubDependencies will write the input from stdin to the given file.")

// TODO BML replace DIY mocks/state with something better
type DependenciesStub struct {
	ExecutedNormally       []string
	ExecutedQuietly        []string
	ExecutedStdin          []string
	ExecutedAll            []string
	ForceIPv4DetectionFail bool
	ForceIPv6DetectionFail bool
}

func (s *DependenciesStub) Run(logger *log.Scope,
	quietLogging bool,
	cmd constants.IptablesCmd,
	iptVer *IptablesVersion,
	stdin io.ReadSeeker,
	args ...string,
) (*bytes.Buffer, error) {
	s.execute(quietLogging, cmd, iptVer, stdin, args...)
	_ = s.writeAllToDryRunPath()
	return &bytes.Buffer{}, nil
}

func (s *DependenciesStub) DetectIptablesVersion(ipV6 bool) (IptablesVersion, error) {
	if ipV6 {
		if s.ForceIPv6DetectionFail {
			return IptablesVersion{}, fmt.Errorf("ip6tables binary not found")
		}
		return IptablesVersion{
			DetectedBinary:        "ip6tables",
			DetectedSaveBinary:    "ip6tables-save",
			DetectedRestoreBinary: "ip6tables-restore",
		}, nil
	}
	if s.ForceIPv4DetectionFail {
		return IptablesVersion{}, fmt.Errorf("iptables binary not found")
	}
	return IptablesVersion{
		DetectedBinary:        "iptables",
		DetectedSaveBinary:    "iptables-save",
		DetectedRestoreBinary: "iptables-restore",
	}, nil
}

func (s *DependenciesStub) execute(quietly bool, cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) {
	// We are either getting iptables rules as a `stdin` blob in `iptables-save` format (if this is a restore)
	if stdin != nil {
		buf := bufio.NewScanner(stdin)
		for buf.Scan() {
			stdincmd := buf.Text()
			s.ExecutedAll = append(s.ExecutedAll, stdincmd)
			s.ExecutedStdin = append(s.ExecutedStdin, stdincmd)
		}
	} else {
		// ...or as discrete individual commands
		cmdline := strings.Join(append([]string{iptVer.CmdToString(cmd)}, args...), " ")
		s.ExecutedAll = append(s.ExecutedAll, cmdline)
		if quietly {
			s.ExecutedQuietly = append(s.ExecutedQuietly, cmdline)
		} else {
			s.ExecutedNormally = append(s.ExecutedNormally, cmdline)
		}
	}
}

// TODO BML this is more than a stub actually needs to do, we should be able to drop this testing hack
// and skip writing to a file, but some tests are not *actually* doing unit testing and need this.
func (s *DependenciesStub) writeAllToDryRunPath() error {
	path := DryRunFilePath.Get()
	if path != "" {
		// Print the input into the given output file.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("unable to open dry run output file %v: %v", path, err)
		}

		defer f.Close()

		for _, line := range s.ExecutedAll {
			_, err := f.WriteString(line + "\n")
			if err != nil {
				return fmt.Errorf("unable to write lines to dry run output file %v: %v", path, err)
			}
		}
	}
	return nil
}
