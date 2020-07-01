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

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
)

var (
	reportFile     = flag.String("report_file", "", "Code coverage report file")
	baselineFile   = flag.String("baseline_file", "", "Code coverage baseline file")
	thresholdFiles = flag.String("threshold_files", "", "File containing package to threshold mappings, as overrides")
	skipDeleted    = flag.Bool("skip_deleted", true, "Whether deleted files should be skipped")

	// report line format (e.g., <option value="file0">istio.io/istio/galley/cmd/shared/shared.go (0.0%)</option>)
	reportRegexp = regexp.MustCompile(` *<option value="(.*)">(.*) \((.*)%\)</option>`)
	// threshold line format
	thresholdRegxep = regexp.MustCompile(`(.*)=(.*)`)
)

func parseReportLine(line string) (string, float64, error) {
	if m := reportRegexp.FindStringSubmatch(line); len(m) != 0 {
		cov, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			return "", 0, err
		}
		return m[2], cov, nil
	}
	return "", 0, fmt.Errorf("no coverage in %s", line)
}

func parseReport(filename string) (map[string]float64, error) {
	coverage := make(map[string]float64)

	f, err := os.Open(filename)
	if err != nil {
		return coverage, fmt.Errorf("failed to open file %s, %v", filename, err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			glog.Warningf("failed to close file %s, %v", filename, err)
		}
	}()

	inFileList := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096*8), bufio.MaxScanTokenSize*8)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		} else if line == "<select id=\"files\">" { // report file list starts
			inFileList = true
			continue
		} else if inFileList && line == "</select>" { // end of file list, bail
			break
		}
		if !inFileList { // ignore
			continue
		}

		if pkg, cov, err := parseReportLine(line); err == nil {
			coverage[pkg] = cov
		}
	}
	return coverage, scanner.Err()
}

func parseThreshold(thresholdFiles string) (map[string]float64, error) {
	files := strings.Split(thresholdFiles, ",")
	thresholds := make(map[string]float64)

	for _, thresholdFile := range files {
		f, err := os.Open(thresholdFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open threshold file, %s, %v", thresholdFile, err)
		}
		defer func() {
			if err = f.Close(); err != nil {
				glog.Errorf("failed to close file %s, %v", thresholdFile, err)
			}
		}()

		scanner := bufio.NewScanner(f)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") { // Skip comments
				continue
			}
			m := thresholdRegxep.FindStringSubmatch(line)
			if len(m) == 3 {
				threshold, err := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse threshold to float64 for package %s: %s, %v",
						m[1], m[2], err)
				}
				thresholds[strings.TrimSpace(m[1])] = threshold
			} else if len(line) > 0 { // The line is the package being ignored.
				thresholds[strings.TrimSpace(line)] = 100
			}
			if scanner.Err() != nil {
				return thresholds, scanner.Err()
			}
		}
	}
	return thresholds, nil
}

func findDelta(report, baseline map[string]float64) map[string]float64 {
	deltas := make(map[string]float64)

	for pkg, cov := range report {
		deltas[pkg] = cov - baseline[pkg]
	}
	// Find the remaining packages that exist in baseline but not in report.
	for pkg, base := range baseline {
		if _, exist := report[pkg]; !exist {
			deltas[pkg] = 0 - base
		}
	}
	return deltas
}

func checkDelta(deltas, report, baseline, thresholds map[string]float64, skipDeleted bool) []string {
	dropMsgs := []string{}
	for pkg, delta := range deltas {
		if delta+getThreshold(thresholds, pkg) < 0 {
			if skipDeleted {
				if _, err := os.Stat(os.Getenv("GOPATH") + "/src/" + pkg); os.IsNotExist(err) {
					// Don't report if the file has been deleted
					continue
				}
			}
			dropMsg := fmt.Sprintf("%s:%f%% (%f%% to %f%%)", pkg, delta, baseline[pkg], report[pkg])
			dropMsgs = append(dropMsgs, dropMsg)
		}
	}
	return dropMsgs
}

func getThreshold(thresholds map[string]float64, path string) float64 {
	matchedThreshold := 0.0
	matchedPackageLebgth := 0
	for pkg, threshold := range thresholds {
		// Find the threshold that matches the longest package prefix.
		if strings.HasPrefix(path, pkg) && len(pkg) > matchedPackageLebgth {
			matchedPackageLebgth = len(pkg)
			matchedThreshold = threshold
		}
	}
	return matchedThreshold
}

func checkCoverage(reportFile, baselineFile, thresholdFile string, skipMissingFile bool) error {
	report, err := parseReport(reportFile)
	if err != nil {
		return fmt.Errorf("cannot open or parse report file: %s, %v", reportFile, err)
	}
	baseline, err := parseReport(baselineFile)
	if err != nil {
		return fmt.Errorf("cannot open or parse baseline file: %s, %v", baselineFile, err)
	}
	thresholds, err := parseThreshold(thresholdFile)
	if err != nil {
		return fmt.Errorf("cannot open or parse threshold file: %s, %v", thresholdFile, err)
	}
	deltas := findDelta(report, baseline)

	// Then generate errors for reduced coverage.
	dropMsgs := checkDelta(deltas, report, baseline, thresholds, skipMissingFile)
	if len(dropMsgs) > 0 {
		errMsgs := []string{"Coverage dropped:"}
		errMsgs = append(errMsgs, dropMsgs...)
		return fmt.Errorf("%s", strings.Join(errMsgs, "\n"))
	}
	return nil
}

// This takes codecov reports generated from PR HEAD abd base and generates errors in case
// code coverage has dropped above the given threshold.
func main() {
	flag.Parse()
	err := checkCoverage(*reportFile, *baselineFile, *thresholdFiles, *skipDeleted)
	if err != nil {
		glog.Error(err)
		os.Exit(1)
	}
	os.Exit(0)
}
