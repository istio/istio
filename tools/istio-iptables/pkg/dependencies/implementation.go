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
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// XTablesExittype is the exit type of xtables commands.
type XTablesExittype int

// Learn from `xtables_exittype` of iptables.
// `XTF_ONLY_ONCE`, `XTF_NO_INVERT`, `XTF_BAD_VALUE`, `XTF_ONE_ACTION` will eventually turned out to be a
// parameter problem with explicit error message. Thus, we do not need to support them here.
const (
	// XTablesOtherProblem indicates a problem of other type in xtables
	XTablesOtherProblem XTablesExittype = iota + 1
	// XTablesParameterProblem indicates a parameter problem in xtables
	XTablesParameterProblem
	// XTablesVersionProblem indicates a version problem in xtables
	XTablesVersionProblem
	// XTablesResourceProblem indicates a resource problem in xtables
	XTablesResourceProblem
)

var exittypeToString = map[XTablesExittype]string{
	XTablesOtherProblem:     "xtables other problem",
	XTablesParameterProblem: "xtables parameter problem",
	XTablesVersionProblem:   "xtables version problem",
	XTablesResourceProblem:  "xtables resource problem",
}

// XTablesCmds is the set of all the xtables-related commands currently supported.
var XTablesCmds = sets.New(
	constants.IPTABLES,
	constants.IP6TABLES,
	constants.IPTABLESRESTORE,
	constants.IP6TABLESRESTORE,
	constants.IPTABLESSAVE,
	constants.IP6TABLESSAVE,
)

// XTablesWriteCmds contains all xtables commands that do write actions (and thus need a lock)
var XTablesWriteCmds = sets.New(
	constants.IPTABLES,
	constants.IP6TABLES,
	constants.IPTABLESRESTORE,
	constants.IP6TABLESRESTORE,
)

// RealDependencies implementation of interface Dependencies, which is used in production
type RealDependencies struct {
	IptablesVersion  IptablesVersion
	NetworkNamespace string
	CNIMode          bool
}

const iptablesVersionPattern = `v([0-9]+(\.[0-9]+)+)`

type IptablesVersion struct {
	// the actual version
	version *utilversion.Version
	// true if legacy mode, false if nf_tables
	legacy bool
}

func DetectIptablesVersion(ver string) (IptablesVersion, error) {
	if ver == "" {
		var err error
		verb, err := exec.Command("iptables", "--version").CombinedOutput()
		if err != nil {
			return IptablesVersion{}, err
		}
		ver = string(verb)
	}
	// Legacy will have no marking or 'legacy', so just look for nf_tables
	nft := strings.Contains(ver, "nf_tables")
	versionMatcher := regexp.MustCompile(iptablesVersionPattern)
	match := versionMatcher.FindStringSubmatch(ver)
	if match == nil {
		return IptablesVersion{}, fmt.Errorf("no iptables version found: %q", ver)
	}
	version, err := utilversion.ParseGeneric(match[1])
	if err != nil {
		return IptablesVersion{}, fmt.Errorf("iptables version %q is not a valid version string: %v", match[1], err)
	}
	return IptablesVersion{version: version, legacy: !nft}, nil
}

// transformToXTablesErrorMessage returns an updated error message with explicit xtables error hints, if applicable.
func transformToXTablesErrorMessage(stderr string, err error) string {
	ee, ok := err.(*exec.ExitError)
	if !ok {
		// Not common, but can happen if file not found error, etc
		return err.Error()
	}
	exitcode := ee.ExitCode()
	if errtypeStr, ok := exittypeToString[XTablesExittype(exitcode)]; ok {
		// The original stderr is something like:
		// `prog_name + prog_vers: error hints`
		// `(optional) try help information`.
		// e.g.,
		// `iptables 1.8.4 (legacy): Couldn't load target 'ISTIO_OUTPUT':No such file or directory`
		// `Try 'iptables -h' or 'iptables --help' for more information.`
		// Reusing the `error hints` and optional `try help information` parts of the original stderr to form
		// an error message with explicit xtables error information.
		errStrParts := strings.SplitN(stderr, ":", 2)
		errStr := stderr
		if len(errStrParts) > 1 {
			errStr = errStrParts[1]
		}
		return fmt.Sprintf("%v: %v", errtypeStr, strings.TrimSpace(errStr))
	}

	return stderr
}

// Run runs a command
func (r *RealDependencies) Run(cmd string, stdin io.ReadSeeker, args ...string) (err error) {
	if XTablesCmds.Contains(cmd) {
		err = r.executeXTables(cmd, false, stdin, args...)
	} else {
		err = r.execute(cmd, false, stdin, args...)
	}
	return err
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (r *RealDependencies) RunQuietlyAndIgnore(cmd string, stdin io.ReadSeeker, args ...string) {
	if XTablesCmds.Contains(cmd) {
		_ = r.executeXTables(cmd, true, stdin, args...)
	} else {
		_ = r.execute(cmd, true, stdin, args...)
	}
}
