// Copyright 2019 Istio Authors
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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	tmpDirPrefix = "test-infra_package-coverage-"
)

var (
	tmpDir string
)

func TestParseHtml(t *testing.T) {
	exampleHTML :=
		"<html>\n" +
			"    <option value=\"file2\">istio.io/istio/galley/pkg/crd/validation/endpoint.go (62.8%)</option>\n" +
			"\n" +
			"    <option value=\"file3\">istio.io/istio/galley/pkg/crd/validation/monitoring.go (61.2%)</option>\n" +
			"</html>\n"
	reportFile := filepath.Join(tmpDir, "reportFile")
	if err := ioutil.WriteFile(reportFile, []byte(exampleHTML), 0644); err != nil {
		t.Errorf("Failed to write example reportFile file, %v", err)
	}

	codeCoverage, err := parseReport(reportFile)
	if err != nil {
		t.Errorf("Failed to parse reportFile, %v", err)
	} else {
		if len(codeCoverage) != 2 {
			t.Error("Wrong result count from parseReport()")
		}
		if codeCoverage["istio.io/istio/galley/pkg/crd/validation/endpoint.go"] != 62.8 {
			t.Error("Wrong result from parseReport()")
		}
		if codeCoverage["istio.io/istio/galley/pkg/crd/validation/monitoring.go"] != 61.2 {
			t.Error("Wrong result from parseReport()")
		}
	}
}

func TestParseThreshold(t *testing.T) {
	example :=
		"#Some comments\n" +
			"  # more comments\n" +
			"istio.io/istio/galley/pkg/crd=10.5\n" +
			" istio.io/istio/pilot = 20.2\n" +
			"\n"
	outFile := filepath.Join(tmpDir, "outFile")
	if err := ioutil.WriteFile(outFile, []byte(example), 0644); err != nil {
		t.Errorf("Failed to write example file, %v", err)
	}
	example2 :=
		"#Some comments\n" +
			"  # more comments\n" +
			"istio.io/istio/mixer/pkg\n" +
			" istio.io/istio/pilot/test\n" +
			"\n"
	outFile2 := filepath.Join(tmpDir, "outFile2")
	if err := ioutil.WriteFile(outFile2, []byte(example2), 0644); err != nil {
		t.Errorf("Failed to write example file, %v", err)
	}

	thresholds, err := parseThreshold(outFile + "," + outFile2)
	if err != nil {
		t.Errorf("Failed to parse outFile, %v", err)
	} else {
		if len(thresholds) != 4 {
			t.Error("Wrong result count from parseThresholds()")
		}
		if thresholds["istio.io/istio/galley/pkg/crd"] != 10.5 {
			t.Error("Wrong result from parseThreshold()")
		}
		if thresholds["istio.io/istio/pilot"] != 20.2 {
			t.Error("Wrong result from parseThreshold()")
		}
		if thresholds["istio.io/istio/mixer/pkg"] != 100 {
			t.Error("Wrong result from parseThreshold()")
		}
		if thresholds["istio.io/istio/pilot/test"] != 100 {
			t.Error("Wrong result from parseThreshold()")
		}
	}
}

func TestGetThreshold(t *testing.T) {
	thresholds := map[string]float64{
		"istio.io/istio/galley/pkg/crd": 20,
		"istio.io/istio/galley/pkg":     30,
		"istio.io/istio/pilot":          40,
	}
	if getThreshold(thresholds, "istio.io/istio/galley/pkg/crd/foobar") != 20 {
		t.Error("Unexpected threshold")
	}
	if getThreshold(thresholds, "istio.io/istio/galley/pkg/foobar") != 30 {
		t.Error("Unexpected threshold")
	}
	if getThreshold(thresholds, "istio.io/istio/pilot/pkg/crd/foobar") != 40 {
		t.Error("Unexpected threshold")
	}
	if getThreshold(thresholds, "istio.io/istio/mixer/pkg/crd/foobar") != 0 {
		t.Error("Unexpected threshold")
	}
}

func TestFindDelta(t *testing.T) {
	dettas := findDelta(
		// report
		map[string]float64{
			"P1": 30,
			"P2": 90,
			"P3": 100,
			"P4": 90,
		},
		// baseline
		map[string]float64{
			"P1": 50,
			"P2": 60,
			"P3": 100,
			"P5": 60,
		},
	)
	expected := map[string]float64{
		"P1": -20,
		"P2": 30,
		"P3": 0,
		"P4": 90,
		"P5": -60,
	}
	if !reflect.DeepEqual(dettas, expected) {
		t.Errorf("Actual: %s; expected: %s", fmt.Sprint(dettas), fmt.Sprint(expected))
	}
}

func TestCheckDeltaError(t *testing.T) {
	result := checkDelta(
		// Delta
		map[string]float64{
			"P1": -20,
			"P2": 30,
			"P3": 0,
			"P4": 90,
			"P5": -60,
		},
		// report
		map[string]float64{
			"P1": 30,
			"P2": 90,
			"P3": 100,
			"P4": 90,
		},
		// baseline
		map[string]float64{
			"P1": 50,
			"P2": 60,
			"P3": 100,
			"P5": 60,
		},
		// thresholds
		map[string]float64{
			// Default threshold
			"P": 5,
		}, false)
	if len(result) == 0 {
		t.Error("Expecting error")
	}
}

func TestCheckDeltaGood(t *testing.T) {
	result := checkDelta(
		// Delta
		map[string]float64{
			"P1": -1,
			"P2": 30,
			"P3": 0,
			"P4": 90,
		},
		// report
		map[string]float64{
			"P1": 30,
			"P2": 90,
			"P3": 100,
			"P4": 90,
		},
		// baseline
		map[string]float64{
			"P1": 31,
			"P2": 60,
			"P3": 100,
		},
		// thresholds
		map[string]float64{
			// Default threshold
			"P": 5,
		}, false)
	if len(result) > 0 {
		t.Errorf("Expecting success")
	}
}

// Actual codecov diff test
func TestCheckCoverage(t *testing.T) {
	if len(*reportFile) == 0 || len(*baselineFile) == 0 || len(*thresholdFiles) == 0 {
		t.Skip("Test files are not provided.")
	}
	err := checkCoverage(*reportFile, *baselineFile, *thresholdFiles, *skipDeleted)

	if err != nil {
		t.Errorf("%v", err)
	}
}

func TestGoPath(t *testing.T) {
	if len(os.Getenv("GOPATH")) == 0 {
		t.Error("${GOPATH} is not set")
	}
}

func TestMain(m *testing.M) {
	var err error
	if tmpDir, err = ioutil.TempDir("", tmpDirPrefix); err != nil {
		log.Printf("Failed to create tmp directory: %s, %s", tmpDir, err)
		os.Exit(4)
	}

	exitCode := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Printf("Failed to remove tmpDir %s", tmpDir)
	}

	os.Exit(exitCode)
}
