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
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/log"
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

// RealDependencies implementation of interface Dependencies, which is used in production
type RealDependencies struct {
	NetworkNamespace string
	// Should generally be set to true anytime we are "jumping" from a shared iptables
	// context (the node, an agent container) into a pod to do iptables stuff,
	// as it's faster and reduces contention for legacy iptables versions that use file-based locking.
	UsePodScopedXtablesLock bool

	ForceIptablesBinary string
}

const iptablesVersionPattern = `v([0-9]+(\.[0-9]+)+)`

type IptablesVersion struct {
	DetectedBinary        string
	DetectedSaveBinary    string
	DetectedRestoreBinary string
	// the actual version
	Version *utilversion.Version
	// true if legacy mode, false if nf_tables
	Legacy bool
	// true if we detected that existing rules are present for this variant (legacy, nft, v6)
	ExistingRules bool
}

func (v IptablesVersion) CmdToString(cmd constants.IptablesCmd) string {
	switch cmd {
	case constants.IPTables:
		return v.DetectedBinary
	case constants.IPTablesSave:
		return v.DetectedSaveBinary
	case constants.IPTablesRestore:
		return v.DetectedRestoreBinary
	default:
		return ""
	}
}

// IsWriteCmd returns true for all command types that do write actions (and thus need a lock)
func (v IptablesVersion) IsWriteCmd(cmd constants.IptablesCmd) bool {
	switch cmd {
	case constants.IPTables:
		return true
	case constants.IPTablesRestore:
		return true
	default:
		return false
	}
}

// Constants for iptables commands
// These should not be used directly/assumed to be present, but should be contextually detected
const (
	iptablesBin        = "iptables"
	iptablesNftBin     = "iptables-nft"
	iptablesLegacyBin  = "iptables-legacy"
	ip6tablesBin       = "ip6tables"
	ip6tablesNftBin    = "ip6tables-nft"
	ip6tablesLegacyBin = "ip6tables-legacy"
	legacy             = "legacy"
	nft                = "nft"
)

// adding this function redirect to enable unit testing for DetectIptablesVersion
var shouldUseBinaryForContext = shouldUseBinaryForCurrentContext

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
// Q: Why not simply "use the host default binary" at $PATH/iptables?
// A: Because we are running in our own container and do not have access to the host default binary.
// We are using our local binaries to update host rules, and we must pick the right match.
//
// Basic selection logic is as follows:
// 1. Check if we have `iptables-legacy` binary in our $PATH and if it has any existing rules in the netns
// 2. If so, use `legacy` binary immediately
// 3. Otherwise, check if we have `iptables-nft` binary in our $PATH and if so, use `nft` binary set
// 4. Otherwise, see if we have `iptables` binary set, and use that.
func (r *RealDependencies) DetectIptablesVersion(ipV6 bool) (IptablesVersion, error) {
	// Begin detecting
	//
	// iptables variants all have ipv6 variants, so decide which set we're looking for
	var nftBin, legacyBin, plainBin string
	if ipV6 {
		nftBin = ip6tablesNftBin
		legacyBin = ip6tablesLegacyBin
		plainBin = ip6tablesBin
	} else {
		nftBin = iptablesNftBin
		legacyBin = iptablesLegacyBin
		plainBin = iptablesBin
	}

	// the user has specifically chosen an iptables
	// version so use that binary or fail
	if r.ForceIptablesBinary != "" {
		switch r.ForceIptablesBinary {
		case legacy:
			legVer, err := shouldUseBinaryForContext(legacyBin)
			if err != nil {
				log.Errorf("did not find iptables binary, error was %v: %+v", err, legVer)
				return IptablesVersion{}, err
			}
			return legVer, nil
		case nft:
			nftVer, err := shouldUseBinaryForContext(nftBin)
			if err != nil {
				log.Errorf("did not find iptables binary, error was %v: %+v", err, nftVer)
				return IptablesVersion{}, err
			}
			return nftVer, nil
		}
	}

	// 1. What binaries we have
	// 2. What binary we should use, based on existing rules defined in our current context.
	//
	// does the legacy binary set exist, and are legacy rules present?
	legVer, err := shouldUseBinaryForContext(legacyBin)
	if err == nil && legVer.ExistingRules {
		// if so, immediately use it
		return legVer, nil
	}
	// not critical, may find another.
	log.Debugf("did not find (or cannot use) iptables binary, error was %v: %+v", err, legVer)

	// Check again
	// does the nft binary set exist and seem usable?
	// (at this point we don't need to check for existing rules,
	// since we know legacy doesn't have any, and `nft` is usable, prefer `nft`)
	nftVer, err := shouldUseBinaryForContext(nftBin)
	if err == nil {
		// if so, immediately use it.
		return nftVer, nil
	}
	// not critical, may find another.
	log.Debugf("did not find (or cannot use) iptables binary, error was %v: %+v", err, nftVer)

	// regular non-suffixed binary set is our last resort.
	//
	// If the aliased/non-suffixed binary is available and appears to work where the others did not,
	// just use it. In practice this should *never* happen, as the non-suffixed binary is invariably
	// softlinked to one or the other binary.
	// Either way, this is our last option, so just propagate the error if it fails, we can't do anything either way.
	return shouldUseBinaryForContext(plainBin)
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
func (r *RealDependencies) Run(
	logger *log.Scope,
	quietLogging bool,
	cmd constants.IptablesCmd,
	iptVer *IptablesVersion,
	stdin io.ReadSeeker,
	args ...string,
) (*bytes.Buffer, error) {
	return r.executeXTables(logger, cmd, iptVer, quietLogging, stdin, args...)
}
