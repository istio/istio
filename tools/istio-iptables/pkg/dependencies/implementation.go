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
	detectedBinary string
	// the actual version
	version *utilversion.Version
	// true if legacy mode, false if nf_tables
	legacy bool
}


// Constants for iptables commands
const (
	IPTABLES_BIN         = "iptables" //TODO check usages, nothing should use this directly but below
	IPTABLES_NFT_BIN     = "iptables-nft"
	IPTABLES_LEGACY_BIN  = "iptables-legacy"
	IP6TABLES_BIN        = "ip6tables"
	IP6TABLES_NFT_BIN    = "ip6tables-nft"
	IP6TABLES_LEGACY_BIN = "ip6tables-legacy"
)

// It is not sufficient to check for the presence of one binary or the other in $PATH -
// we must choose a binary that is
// 1. Available in our $PATH
// 2. Matches where rules are actually defined in the netns we're operating in
// (legacy or nft, with a preference for the latter if both present)
//
// This is designed to handle situations where, for instance, the host has nft-defined rules, and our default container
// binary is `legacy`, or vice-versa - we must match the binaries we have in our $PATH to what rules are actually defined
// in our current netns context.
//
// Basic selection logic is as follows:
// 1. see if we have `nft` binary in our $PATH
// 2. see if we have existing rules in `nft` in our netns
// 3. If so, use `nft`
// 4. Otherwise, see if we have `legacy` binary, and use that.
// 5. Otherwise, see if we have `iptables` binary, and use that (detecting whether it's nft or legacy).
func DetectIptablesVersion(overrideVersion string) (IptablesVersion, error) {
	// If an override version string is defined, we do no detection and assume you are telling
	// us to use something that's actually in $PATH
	if overrideVersion != "" {
		// Legacy will have no marking or 'legacy', so just look for nf_tables
		nft := strings.Contains(overrideVersion, "nf_tables")
		parsedVer, err := parseIptablesVer(string(overrideVersion))
		if err != nil {
			return IptablesVersion{}, fmt.Errorf("iptables version %q is not a valid version string: %v", overrideVersion, err)
		}
		return IptablesVersion{version: parsedVer, legacy: !nft}, nil
	} else {
		// does the nft binary exist?
		ver, err := shouldUseBinaryForCurrentContext(IPTABLES_NFT_BIN)
		if err == nil {
			// if so, use it.
			return ver, nil
		} else {
			// does the legacy binary exist?
			ver, err := shouldUseBinaryForCurrentContext(IPTABLES_LEGACY_BIN)
			if err == nil {
				// if so, use it
				return ver, err
			} else {
				// regular non-suffixed binary is our last resort - if it's not there,
				// propagate the error, we can't do anything
				return shouldUseBinaryForCurrentContext(IPTABLES_BIN)
			}
		}
	}
}

func shouldUseBinaryForCurrentContext(iptablesBin string) (IptablesVersion, error) {
	// We assume that whatever `iptablesXXX` binary you pass us also has a `iptablesXXX-save` binary
	// which should always be true for any valid iptables installation (we use both in our iptables code anyway)
	iptablesSaveBin := fmt.Sprintf("%s-save", iptablesBin)
	// does the "xx-save" binary exist?
	rawIptablesVer, execErr := exec.Command(iptablesSaveBin, "--version").CombinedOutput()
	if execErr == nil {
		// if it seems to, use it to dump the rules in our netns, and see if any rules exist there
		rulesDump, _ := exec.Command(iptablesSaveBin).CombinedOutput()
		// `xx-save` should return _no_ output (0 lines) if no rules are defined in this netns for that binary variant.
		// `xx-save` should return at least 3 output lines if at least one rule is defined in this netns for that binary variant.
		if strings.Count(string(rulesDump), "\n") >= 3 {
			// binary exists and rules exist in its tables in this netns -> we should use this binary for this netns.
			parsedVer, err := parseIptablesVer(string(rawIptablesVer))
			if err != nil {
				return IptablesVersion{}, fmt.Errorf("iptables version %q is not a valid version string: %v", rawIptablesVer, err)
			}
			// Legacy will have no marking or 'legacy', so just look for nf_tables
			isNft := strings.Contains(string(rawIptablesVer), "nf_tables")
			return IptablesVersion{detectedBinary: iptablesBin, version: parsedVer, legacy: !isNft}, nil
		}
	}
	return IptablesVersion{}, fmt.Errorf("iptables save binary: %s either not present, or has no rules defined in current netns", iptablesSaveBin)
}

// TODO BML verify this won't choke on "-save" binaries having slightly diff. version string prefixes
func parseIptablesVer(rawVer string) (*utilversion.Version, error) {
	versionMatcher := regexp.MustCompile(iptablesVersionPattern)
	match := versionMatcher.FindStringSubmatch(rawVer)
	if match == nil {
		return nil, fmt.Errorf("no iptables version found for: %q", rawVer)
	}
	version, err := utilversion.ParseGeneric(match[1])
	if err != nil {
		return nil, fmt.Errorf("iptables version %q is not a valid version string: %v", match[1], err)
	}
	return version, nil
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
func (r *RealDependencies) Run(cmd string, stdin io.ReadSeeker, args ...string) error {
	return r.executeXTables(cmd, false, stdin, args...)
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (r *RealDependencies) RunQuietlyAndIgnore(cmd string, stdin io.ReadSeeker, args ...string) {
	_ = r.executeXTables(cmd, true, stdin, args...)
}
