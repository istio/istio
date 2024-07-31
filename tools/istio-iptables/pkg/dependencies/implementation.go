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
	"fmt"
	"io"
	"istio.io/istio/pkg/log"
	"os"
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
	NetworkNamespace         string
	HostFilesystemPodNetwork bool
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
	iptablesBin              = "iptables"
	iptablesNftBin           = "iptables-nft"
	iptablesLegacyBin        = "iptables-legacy"
	ip6tablesBin             = "ip6tables"
	ip6tablesNftBin          = "ip6tables-nft"
	ip6tablesLegacyBin       = "ip6tables-legacy"
	iptablesRestoreBin       = "iptables-restore"
	ip6tablesRestoreBin      = "ip6tables-restore"
	ModulesFile              = "/proc/modules"
	iptablesLegacyModuleName = "ip_tables"
	iptablesNftModuleName    = "nf_tables"
)

func NewIpTablesVersion(bin string, versionOutput string) (*IptablesVersion, error) {
	var parsedVer *utilversion.Version
	if !strings.Contains(versionOutput, "unrecognized option") {
		parsedVer, parseErr := parseIptablesVer(versionOutput)
		if parseErr != nil {
			return nil, fmt.Errorf("iptables version %q is not a valid version string: %v", versionOutput, parseErr)
		}
	} else {
		log.Warnf("found iptables binary %s, but it does not appear to support the '--version' flag, assuming very old legacy version", bin)
		parsedVer = utilversion.MustParseGeneric("0.0.0")
	}
	version := &IptablesVersion{
		DetectedBinary:        bin,
		DetectedSaveBinary:    fmt.Sprintf("%s-save", bin),
		DetectedRestoreBinary: fmt.Sprintf("%s-restore", bin),
		Version:               parsedVer,
	}
	rulesDump, err := exec.Command(version.DetectedSaveBinary).CombinedOutput()
	if err != nil {
		return nil, err
	}
	if strings.Count(string(rulesDump), "\n") >= 3 {
		version.ExistingRules = true
	} else {
		version.ExistingRules = false
	}
	return version, nil
}

func LoadProcModules() (map[string]struct{}, error) {
	file, err := os.Open(ModulesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	modules := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) > 0 {
			moduleName := fields[0]
			modules[moduleName] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}

func getIptablesNFTBin(isIpV6 bool) string {
	if isIpV6 {
		return ip6tablesNftBin
	}
        return iptablesNftBin
}

func getIptablesLegacyBin(isIpV6 bool) string {
	if isIpV6 {
		return ip6tablesLegacyBin
	} else {
		return iptablesLegacyBin
	}
}

func getIptablesBin(isIpV6 bool) string {
	if isIpV6 {
		return ip6tablesBin
	} else {
		return iptablesBin
	}
}

func GetNFTVersion(modules map[string]struct{}, ipV6 bool) (*IptablesVersion, error) {
	if modules == nil {
		return nil, fmt.Errorf("nil modules map")
	}
	if _, found := modules[iptablesNftModuleName]; !found {
		return nil, fmt.Errorf("nft module not found")
	}

	nftBin := getIptablesNFTBin(ipV6)
	nftVersionOutput, nftErr := exec.Command(nftBin, "--version").CombinedOutput()
	if nftErr == nil {
		return NewIpTablesVersion(nftBin, string(nftVersionOutput))
	}
	baseBin := getIptablesBin(ipV6)
	versionOutput, err := exec.Command(baseBin, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("no existing nft commands")
	}
	if strings.Contains(string(versionOutput), "nf_tables") {
		return NewIpTablesVersion(baseBin, string(versionOutput))
	} else {
		return nil, fmt.Errorf("nft command not found(default iptables is not nft)")
	}
}

func GetLegacyVersion(modules map[string]struct{}, ipV6 bool) (*IptablesVersion, error) {
	if modules == nil {
		return nil, fmt.Errorf("nil modules map")
	}
	if _, found := modules[iptablesLegacyModuleName]; !found {
		return nil, fmt.Errorf("legacy module not found")
	}
	legacyBin := getIptablesLegacyBin(ipV6)
	legacyVersionOutput, legacyErr := exec.Command(legacyBin, "--version").CombinedOutput()
	if legacyErr == nil {
		return NewIpTablesVersion(legacyBin, string(legacyVersionOutput))
	}
	baseBin := getIptablesBin(ipV6)
	versionOutput, err := exec.Command(baseBin, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("no existing legacy commands")
	} else {
		if !strings.Contains(string(versionOutput), "nf_tables") {
			return NewIpTablesVersion(baseBin, string(versionOutput))
		} else {
			return nil, fmt.Errorf("legacy command not found(default iptables is not legacy)")
		}
	}
}

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
// 1. see if we have `nft` binary set in our $PATH
// 2. see if we have existing rules in `nft` in our netns
// 3. If so, use `nft` binary set
// 4. Otherwise, see if we have `legacy` binary set, and use that.
// 5. Otherwise, see if we have `iptables` binary set, and use that (detecting whether it's nft or legacy).
func (r *RealDependencies) DetectIptablesVersion(ipV6 bool) (IptablesVersion, error) {
	modules, err := LoadProcModules()
	if err != nil {
		log.Errorf("failed to load kernel modules from %s, error was: %s", ModulesFile, err.Error())
		return IptablesVersion{}, err
	}

	legacyVersion, err := GetLegacyVersion(modules, ipV6)
	nftVersion, err := GetNFTVersion(modules, ipV6)
	if legacyVersion != nil && nftVersion != nil {
		if legacyVersion.ExistingRules {
			log.Info("legacy & nft both exists but nft have rules, use legacy")
			return *legacyVersion, nil
		} else {
			log.Info("legacy & nft both exists but legacy do not have any rule, use nft by default")
			return *nftVersion, nil
		}
	} else {
		if nftVersion != nil {
			log.Info("use nft")
			return *nftVersion, nil
		} else {
			log.Info("use legacy")
			return *legacyVersion, nil
		}
	}
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
func (r *RealDependencies) Run(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) error {
	return r.executeXTables(cmd, iptVer, false, stdin, args...)
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (r *RealDependencies) RunQuietlyAndIgnore(cmd constants.IptablesCmd, iptVer *IptablesVersion, stdin io.ReadSeeker, args ...string) {
	_ = r.executeXTables(cmd, iptVer, true, stdin, args...)
}
