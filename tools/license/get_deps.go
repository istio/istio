package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"strings"
	"io/ioutil"
)

const (
	// maxLevelsToLicense is the maximum levels to go up to the root to find
	// license in parent directories.
	maxLevelsToLicense = 7
	// istioReleaseBranch is the name of the release to check licenses for.
	istioReleaseBranch = "release-0.8"
)

var (
	// Ignore package paths that don't start with this.
	mustStartWith = []string{
		"istio.io/istio/vendor",
		"vendor",
	}
	skipPrefixes = []string{
		"istio.io/istio/vendor/github.com/gogo",
		"vendor/golang_org",
	}
	root      = filepath.Join(os.Getenv("GOPATH"), "src")
	istioRoot = filepath.Join(root, "istio.io/istio")
)

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
type LicenseInfos []*LicenseInfo

func (s LicenseInfos) Len() int {
	return len(s)
}

func (s LicenseInfos) Less(i, j int) bool {
	return s[i].packageName < s[j].packageName
}

func (s LicenseInfos) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func main() {
	var summary bool
	flag.BoolVar(&summary, "summary", false, "Generate a summary report.")
	flag.Parse()

	if err := os.Chdir(istioRoot); err != nil {
		log.Fatal(err)
	}

	out, err := exec.Command("go", "list", "-f", `'{{ join .Deps  "\n"}}'`, "./vendor/...").Output()
	if err != nil {
		log.Fatal(err)
	}
	outv := strings.Split(string(out), "\n")
	outv, skipv := filter(dedup(outv))
	sort.Strings(outv)
	sort.Strings(skipv)
	var missing []string

	// TODO: detect multiple licenses.
	licensePath := make(map[string]string, 0)
	for _, p := range outv {
		lf, err := findLicenseFile(p)
		if err != nil || lf == nil {
			missing = append(missing, p)
			continue
		}
		licensePath[p] = lf[0]
	}

	licenseTypes := make(map[string][]string, 0)
	var licenses, inexact LicenseInfos
	for p, lp := range licensePath {
		linfo, err := getLicenseInfo(lp)
		if err != nil {
			log.Printf("licensee error: %s", err)
			continue
		}
		linfo.packageName = strings.TrimPrefix(p, "istio.io/istio/vendor/")
		licenses = append(licenses, linfo)
		if linfo.exact {
			licenseTypes[linfo.licenseTypeString] = append(licenseTypes[linfo.licenseTypeString], p)
		} else {
			linfo.licenseText = readFile(lp)
			inexact = append(inexact, linfo)
		}
	}

	sort.Sort(licenses)

	if summary {
		for _, p := range missing {
			fmt.Printf("%s, MISSING\n", p)
		}
		for _, l := range licenses {
			fmt.Printf("%s,%s,%s,%s\n", l.packageName, l.url, l.licenseTypeString, l.confidence)
		}
	} else {
		fmt.Println("===========================================================")
		fmt.Println("The following packages were missing license files:")
		fmt.Println("===========================================================")
		for _, p := range missing {
			fmt.Println(p)
		}

		fmt.Println("\n\n")
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

		fmt.Println("\n\n")
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
	}
}

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

func getLicenseInfo(path string) (*LicenseInfo, error) {
	outb, err := exec.Command("licensee", "detect", path).Output()
	if err != nil {
		return nil, err
	}
	out := string(outb)

	return &LicenseInfo{
		path:              path,
		url:               pathToURL(path),
		licenseeOutput:    out,
		licenseTypeString: getMatchingValue(out, "License:"),
		confidence:        getMatchingValue(out, "  Confidence:"),
		exact:             strings.Contains(out, "Licensee::Matchers::Exact"),
	}, nil
}

func getMatchingValue(in, match string) string {
	for _, l := range strings.Split(in, "\n") {
		if strings.Contains(l, match) {
			return strings.TrimSpace(strings.TrimPrefix(l, match))
		}
	}
	return "ERROR"
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
		//sv := strings.Split(s, "/")

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
