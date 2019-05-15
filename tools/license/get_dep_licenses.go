// Copyright 2018 Istio Authors
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

// Binary get_dep_licenses outputs aggrerate license information for all transitive Istio dependencies.
// This tool requires https://github.com/benbalter/licensee to work.
// Usage:
//   1) Generate complete dump of every license, suitable for including in release build/binary image:
//      go run get_dep_licenses.go --branch release-1.0.1
//   2) CSV format output with one package per line:
//      go run get_dep_licenses.go --summary --branch release-1.0.1
//   3) Detailed info about how closely each license matches official text:
//      go run get_dep_licenses.go --match-detail --branch release-1.0.1
//   4) Use a different branch from the current one. Will do git checkout to that branch and back to the current on completion.
//      This can only be used from inside Istio repo:
//      go run get_dep_licenses.go --branch release-1.0.1 --checkout
//   5) Check if all licenses are Google approved. Outputs lists of restricted, reciprocal, missing, and unknown status licenses.
//      go run get_dep_licenses.go --check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// maxLevelsToLicense is the maximum levels to go up to the root to find
	// license in parent directories.
	maxLevelsToLicense = 8
)

type licenseType int

const (
	// licenseTypeApproved is definitely ok to use and modify.
	licenseTypeApproved licenseType = iota
	// licenseTypeReciprocal can be used but not modified.
	licenseTypeReciprocal
	// licenseTypeRestricted
	licenseTypeRestricted
)

var (
	// licenseStrToType are code that's definitely ok to use and modify.
	licenseStrToType = map[string]licenseType{
		// licenseTypeApproved
		"Apache-2.0":   licenseTypeApproved,
		"ISC":          licenseTypeApproved,
		"AFL-2.1":      licenseTypeApproved,
		"AFL-3.0":      licenseTypeApproved,
		"Artistic-1.0": licenseTypeApproved,
		"Artistic-2.0": licenseTypeApproved,
		"Apache-1.1":   licenseTypeApproved,
		"BSD-1-Clause": licenseTypeApproved,
		"BSD-2-Clause": licenseTypeApproved,
		"BSD-3-Clause": licenseTypeApproved,
		"FTL":          licenseTypeApproved,
		"LPL-1.02":     licenseTypeApproved,
		"MS-PL":        licenseTypeApproved,
		"MIT":          licenseTypeApproved,
		"NCSA":         licenseTypeApproved,
		"OpenSSL":      licenseTypeApproved,
		"PHP-3.0":      licenseTypeApproved,
		"TCP-wrappers": licenseTypeApproved,
		"W3C":          licenseTypeApproved,
		"Xnet":         licenseTypeApproved,
		"Zlib":         licenseTypeApproved,
		// licenseTypeReciprocal
		"CC0-1.0":  licenseTypeReciprocal,
		"APSL-2.0": licenseTypeReciprocal,
		"CDDL-1.0": licenseTypeReciprocal,
		"CDDL-1.1": licenseTypeReciprocal,
		"CPL-1.0":  licenseTypeReciprocal,
		"EPL-1.0":  licenseTypeReciprocal,
		"IPL-1.0":  licenseTypeReciprocal,
		"MPL-1.0":  licenseTypeReciprocal,
		"MPL-1.1":  licenseTypeReciprocal,
		"MPL-2.0":  licenseTypeReciprocal,
		"Ruby":     licenseTypeReciprocal,
		// licenseTypeRestricted
		"GPL-1.0-only":      licenseTypeRestricted,
		"GPL-1.0-or-later":  licenseTypeRestricted,
		"GPL-2.0-only":      licenseTypeRestricted,
		"GPL-2.0-or-later":  licenseTypeRestricted,
		"GPL-3.0-only":      licenseTypeRestricted,
		"GPL-3.0-or-later":  licenseTypeRestricted,
		"LGPL-2.0-only":     licenseTypeRestricted,
		"LGPL-2.0-or-later": licenseTypeRestricted,
		"LGPL-2.1-only":     licenseTypeRestricted,
		"LGPL-2.1-or-later": licenseTypeRestricted,
		"LGPL-3.0-only":     licenseTypeRestricted,
		"LGPL-3.0-or-later": licenseTypeRestricted,
		"NPL-1.0":           licenseTypeRestricted,
		"NPL-1.1":           licenseTypeRestricted,
		"OSL-1.0":           licenseTypeRestricted,
		"OSL-1.1":           licenseTypeRestricted,
		"OSL-2.0":           licenseTypeRestricted,
		"OSL-2.1":           licenseTypeRestricted,
		"OSL-3.0":           licenseTypeRestricted,
		"QPL-1.0":           licenseTypeRestricted,
		"Sleepycat":         licenseTypeRestricted,
	}
	// knownUnknownLicenses are either missing or unknown to licensee, but were manually copied and /or reviewed
	// and are considered ok, so the tool will not complain about these.
	knownUnknownLicenses = map[string]bool{
		"github.com/jmespath/go-jmespath":                                         true,
		"github.com/alicebob/gopher-json":                                         true,
		"istio.io/istio/vendor/github.com/dchest/siphash":                         true,
		"istio.io/istio/vendor/github.com/signalfx/com_signalfx_metrics_protobuf": true,
	}
	// Ignore package paths that don't start with this.
	mustStartWith = []string{
		"istio.io/istio/vendor",
		"vendor",
	}
	// After ignoring anything not in mustStartWith, further exclude anything with prefix below.
	skipPrefixes = []string{
		"istio.io/istio/vendor/github.com/gogo",
		"vendor/golang_org",
	}
	// root is the root of Go src code.
	root = filepath.Join(os.Getenv("GOPATH"), "src")
	// istioSubdir is the subdir from src root where istio source is found.
	istioSubdir = "istio.io/istio"
	// istioRoot is the path we expect to find istio source under.
	istioRoot = filepath.Join(root, istioSubdir)
	// istioReleaseBranch is the branch to generate licenses for.
	istioReleaseBranch = ""
)

// LicenseInfo describes a license.
type LicenseInfo struct {
	packageName       string
	path              string
	url               string
	licenseeOutput    string
	licenseTypeString string
	licenseText       string
	exact             bool
	confidence        string
}

// LicenseInfos is a slice of LicenseInfo.
type LicenseInfos []*LicenseInfo

// Len implements the sort.Interface interface.
func (s LicenseInfos) Len() int {
	return len(s)
}

// Less implements the sort.Interface interface.
func (s LicenseInfos) Less(i, j int) bool {
	return s[i].packageName < s[j].packageName
}

// Swap implements the sort.Interface interface.
func (s LicenseInfos) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func main() {
	var summary, checkout, matchDetail, check bool
	flag.BoolVar(&summary, "summary", false, "Generate a summary report.")
	flag.BoolVar(&checkout, "checkout", false, "Checkout target branch, return to current branch on completion. Can only use from inside Istio git repo.")
	flag.BoolVar(&matchDetail, "match_detail", false, "Show information about match closeness for inexact matches.")
	flag.BoolVar(&check, "check", false, "Check licenses to see if they are Google approved. Exits with error if any unapproved licenses are found, "+
		"but success does not imply all licenses are approved.")
	flag.StringVar(&istioReleaseBranch, "branch", "", "Istio release branch to use.")
	flag.Parse()

	// Verify inputs.
	if summary && matchDetail {
		log.Fatal("--summary and --match_detail cannot both be set.")
	}

	if istioReleaseBranch == "" && !check {
		var err error
		istioReleaseBranch, err = runBash("git", "rev-parse", "HEAD")
		if err != nil {
			log.Fatalf("Could not get current commit: %s", err)
		}
		istioReleaseBranch = strings.TrimSpace(istioReleaseBranch)
	}

	// Everything happens from istio root.
	if err := os.Chdir(istioRoot); err != nil {
		log.Fatalf("Could not chdir to Istio root at %s", istioRoot)
	}

	// Handle git checkouts if the release branch we want != current branch
	var prevBranch string
	if checkout {
		// Save git branch to return to later.
		pb, err := runBash("git", "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			log.Fatalf("Could not get current branch: %s", err)
		}
		prevBranch = strings.TrimSpace(pb)

		// Need to switch to branch we're getting the licenses for.
		_, err = runBash("git", "checkout", istioReleaseBranch)
		if err != nil {
			log.Fatalf("Could not git checkout %s: %s", istioReleaseBranch, err)
		}
	}
	defer func() {
		if checkout {
			// Get back to original branch.

			_, err := exec.Command("git", "checkout", prevBranch).Output()
			if err != nil {
				log.Fatalf("Could not git checkout back to original branch %s.", prevBranch)
			}
		}
	}()

	// List all the deps in vendor.
	out, err := runBash("go", "list", "-f", `'{{ join .Deps  "\n"}}'`, "./vendor/...")
	if err != nil {
		log.Fatal(out)
	}
	outv := strings.Split(out, "\n")
	outv, skipv := filter(dedup(outv))
	sort.Strings(outv)
	sort.Strings(skipv)
	var missing []string

	// TODO: detect multiple licenses.
	licensePath := make(map[string]string)
	for _, p := range outv {
		lf, err := findLicenseFile(p)
		if err != nil || lf == nil {
			if !knownUnknownLicenses[p] {
				missing = append(missing, p)
			}
			continue
		}
		licensePath[p] = lf[0]
	}

	licenseTypes := make(map[string][]string)
	var reciprocalList, restrictedList, missingList []string
	unknownMap := make(map[string]string)
	var licenses, exact, inexact LicenseInfos
	for p, lp := range licensePath {
		linfo := &LicenseInfo{}
		if matchDetail || summary || check {
			// This requires the external licensee program.
			linfo, err = getLicenseeInfo(lp)
			if err != nil {
				log.Printf("licensee error: %s", err)
				continue
			}
		}
		linfo.packageName = strings.TrimPrefix(p, istioSubdir+"/vendor/")
		linfo.licenseText = readFile(lp)
		linfo.path = lp
		linfo.url = pathToURL(lp)
		licenses = append(licenses, linfo)
		ltypeStr := linfo.licenseTypeString
		if linfo.exact {
			licenseTypes[ltypeStr] = append(licenseTypes[ltypeStr], p)
			exact = append(exact, linfo)
		} else {
			inexact = append(inexact, linfo)
		}

		log.Printf("Checking %s\n", linfo.packageName)
		lt, ok := licenseStrToType[ltypeStr]
		switch {
		// No license was found by licensee.
		case ltypeStr == "":
			missingList = append(missingList, linfo.packageName)
		// License was found but not in a definite category.
		case !ok:
			if !knownUnknownLicenses[linfo.packageName] {
				unknownMap[linfo.packageName] = ltypeStr
			}
		case lt == licenseTypeApproved:
		case lt == licenseTypeReciprocal:
			reciprocalList = append(reciprocalList, linfo.packageName)
		case lt == licenseTypeRestricted:
			restrictedList = append(restrictedList, linfo.packageName)
		}
	}

	if check {
		exitCode := 0
		if len(reciprocalList) > 0 {
			fmt.Println("===========================================================")
			fmt.Println("The following packages have reciprocal licenses (code may")
			fmt.Println("be used but not modified):")
			fmt.Println("===========================================================")
			fmt.Println(strings.Join(reciprocalList, "\n"))
			exitCode |= 1
		}
		if len(missingList) > 0 {
			fmt.Println("===========================================================")
			fmt.Println("The following packages have missing licenses:")
			fmt.Println("===========================================================")
			fmt.Println(strings.Join(missingList, "\n"))
			exitCode |= 2
		}
		if len(unknownMap) > 0 {
			fmt.Println("===========================================================")
			fmt.Println("The following packages have unknown status licenses (legal")
			fmt.Println("review required). ")
			fmt.Println("===========================================================")
			for k, v := range unknownMap {
				fmt.Printf("%s:%s\n", k, v)
			}
			exitCode |= 4
		}
		if len(restrictedList) > 0 {
			fmt.Println("===========================================================")
			fmt.Println("The following packages had RESTRICTED licenses!")
			fmt.Println("Packages MUST BE REMOVED! ")
			fmt.Println("===========================================================")
			fmt.Println(strings.Join(restrictedList, "\n"))
			exitCode |= 8
		}
		os.Exit(exitCode)
		return
	}

	sort.Sort(licenses)
	sort.Sort(exact)
	sort.Sort(inexact)

	if summary {
		for _, p := range missing {
			fmt.Printf("%s, MISSING\n", p)
		}
		for _, l := range append(inexact, exact...) {
			fmt.Printf("%s,%s,%s,%s\n", l.packageName, l.url, l.licenseTypeString, l.confidence)
		}
		return
	}

	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "===========================================================")
		fmt.Fprintln(os.Stderr, "The following packages were missing license files.")
		fmt.Fprintln(os.Stderr, "===========================================================")
		for _, p := range missing {
			fmt.Fprintln(os.Stderr, p)
		}
		os.Exit(2)
	}

	if matchDetail {
		fmt.Println()
		fmt.Println("===========================================================")
		fmt.Println("The following packages had inexact licenses:")
		fmt.Println("===========================================================")
		for _, l := range inexact {
			fmt.Printf("Package: %s\n", l.packageName)
			fmt.Printf("URL: %s\n", l.url)
			fmt.Printf("Match info:\n%s\n", l.licenseeOutput)
			fmt.Printf("License text:\n%s\n", l.licenseText)
			fmt.Println("-----------------------------------------------------------")
		}

		fmt.Println()
		fmt.Println("===========================================================")
		fmt.Println("The following packages had exact licenses:")
		fmt.Println("===========================================================")
		for t, ps := range licenseTypes {
			fmt.Printf("\nLicense type: %s\n", t)
			sort.Strings(ps)
			for _, p := range ps {
				fmt.Printf("  %s\n", p)
			}
		}
	} else {
		fmt.Println("===========================================================")
		fmt.Println("Package licenses")
		fmt.Println("===========================================================")

		for _, l := range append(exact, inexact...) {
			fmt.Printf("Package: %s\n", l.packageName)
			fmt.Printf("License URL: %s\n", l.url)
			fmt.Printf("License text:\n%s\n", l.licenseText)
			fmt.Println("-----------------------------------------------------------")
		}

		// Append manually added files.
		manualAppendDir := filepath.Join(istioRoot, "tools/license/manual_append")
		fs, err := ioutil.ReadDir(manualAppendDir)
		if err != nil {
			log.Fatalf("ReadDir: %s\n", err)
		}
		for _, f := range fs {
			b, err := ioutil.ReadFile(filepath.Join(manualAppendDir, f.Name()))
			if err != nil {
				log.Fatalf("ReadFile (%s): %s\n", f.Name(), err)
			}
			fmt.Print(string(b))
		}

	}
}

// runBash runs a bash command. If command is successful, returns output, otherwise returns stderr output as error.
func runBash(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf(fmt.Sprint(err) + ": " + stderr.String())
	}
	return out.String(), nil
}

// pathToURL returns a URL to a path within Istio github code.
func pathToURL(path string) string {
	return strings.Replace(path, istioRoot, "https://github.com/istio/istio/blob/"+istioReleaseBranch, 1)
}

func readFile(path string) string {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func getLicenseeInfo(path string) (*LicenseInfo, error) {
	outb, err := exec.Command("licensee", "detect", path).Output()
	if err != nil {
		return nil, err
	}
	out := string(outb)

	licenseTypeString := getMatchingValue(out, "License:")
	confidence := getMatchingValue(out, "  Confidence:")
	if licenseTypeString == "NOASSERTION" {
		licenseTypeString, confidence = getLicenseAndConfidence(out)
	}

	return &LicenseInfo{
		licenseeOutput:    out,
		licenseTypeString: licenseTypeString,
		confidence:        confidence,
		exact:             strings.Contains(out, "Licensee::Matchers::Exact"),
	}, nil
}

func getMatchingValue(in, match string) string {
	for _, l := range strings.Split(in, "\n") {
		if strings.Contains(l, match) {
			return strings.TrimSpace(strings.TrimPrefix(l, match))
		}
	}
	return ""
}

// For NOASSERTION license type, it means we are below the match threshold. Still grab the closest match and output
// confidence value.
func getLicenseAndConfidence(in string) (string, string) {
	for _, l := range strings.Split(in, "\n") {
		if strings.Contains(l, " similarity:") {
			fs := strings.Fields(l)
			return fs[0], fs[2]
		}
	}
	return "UNKNOWN", ""
}

func findLicenseFile(path string) ([]string, error) {
	path = filepath.Join(root, path)
	for i := 0; i <= maxLevelsToLicense; i++ {
		outb, err := exec.Command("find", path, "-maxdepth", "1",
			"-iname", "licen[sc]e*", "-o", "-iname", "copying").Output()
		if err != nil {
			return nil, err
		}
		out := string(outb)
		if strings.TrimSpace(out) != "" {
			return strings.Split(out, "\n"), nil
		}
		path = filepath.Join(path, "..")
		if strings.Count(path, "/") < strings.Count(istioRoot, "/")+2 {
			// go no further than the root of the package
			break
		}
	}
	return nil, nil
}

func filter(in []string) (keep, skip []string) {
	for _, s := range in {
		s = cleanString(s)

		if !hasAnyPrefix(s, mustStartWith) || hasAnyPrefix(s, skipPrefixes) {
			skip = append(skip, s)
			continue
		}
		keep = append(keep, s)
	}
	return keep, skip
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}

	}
	return false
}

func cleanString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "'")
	s = strings.TrimSuffix(s, "'")
	return s
}

func dedup(s []string) []string {
	return fromMap(toMap(s))
}

func toMap(ss []string) map[string]interface{} {
	out := make(map[string]interface{})
	for _, s := range ss {
		out[s] = nil
	}
	return out
}

func fromMap(m map[string]interface{}) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
