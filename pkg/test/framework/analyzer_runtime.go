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

package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"

	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
)

var (
	analyzeMode bool
	analysis    *suiteAnalysis
	analysisMu  sync.Mutex
)

func init() {
	flag.BoolVar(&analyzeMode, "istio.test.analyze", os.Getenv("ANALYZE_TESTS") != "", "Analyzes tests without actually running them")
}

func analyze() bool {
	if !flag.Parsed() {
		flag.Parse()
	}
	return analyzeMode
}

// initAnalysis sets up analysis for a single suite. If an analysis is already running,
// it will block until finishAnalysis is called.
func initAnalysis(a *suiteAnalysis) {
	analysisMu.Lock()
	analysis = a
}

// finishAnalysis marks the analysis for a suite as done and dumps the results.
func finishAnalysis() {
	scopes.Framework.Infof("=== DONE: Analysis of %s ===", analysis.SuiteID)

	dumpAnalysis()
	analysis = nil
	analysisMu.Unlock()
}

func dumpAnalysis() {
	s, err := getSettings(analysis.SuiteID)
	if err != nil {
		scopes.Framework.Errorf("failed to get settings for %s: %v", analysis.SuiteID, err)
	}
	if err := os.MkdirAll(s.RunDir(), os.ModePerm); err != nil {
		scopes.Framework.Errorf("failed to create analysis directory for %s: %v", analysis.SuiteID, err)
		return
	}

	marshaled, err := yaml.Marshal(analysis)
	if err != nil {
		scopes.Framework.Errorf("failed to marshaled analysis for %s: %v", analysis.SuiteID, err)
		return
	}
	scopes.Framework.Info("\n" + string(marshaled))

	outPath := path.Join(s.RunDir(), fmt.Sprintf("%s_analysis.yaml", analysis.SuiteID))
	if err := ioutil.WriteFile(outPath, marshaled, 0666); err != nil {
		scopes.Framework.Errorf("failed writing analysis to file for %s: %v", analysis.SuiteID, err)
		return
	}
	scopes.Framework.Infof("Wrote analysis to %s", outPath)
}

// suiteAnalysis captures the results of analyzing a Suite
type suiteAnalysis struct {
	SuiteID          string                   `yaml:"suiteID"`
	SkipReason       string                   `yaml:"skipReason,omitempty"`
	Labels           []label.Instance         `yaml:"labels,omitempty"`
	MultiCluster     bool                     `yaml:"multicluster,omitempty"`
	MultiClusterOnly bool                     `yaml:"multiclusterOnly,omitempty"`
	Tests            map[string]*testAnalysis `yaml:"tests"`
}

func (s *suiteAnalysis) addTest(id string, test *testAnalysis) {
	s.Tests[id] = test
}

// suiteAnalysis captures the results of analyzing a Test
type testAnalysis struct {
	SkipReason       string                        `yaml:"skipReason,omitempty"`
	Labels           []label.Instance              `yaml:"labels,omitempty"`
	Features         map[features.Feature][]string `yaml:"features,omitempty"`
	Invalid          bool                          `yaml:"invalid,omitempty"`
	MultiCluster     bool                          `yaml:"multicluster,omitempty"`
	MultiClusterOnly bool                          `yaml:"multiclusterOnly,omitempty"`
	NotImplemented   bool                          `yaml:"notImplemented,omitempty"`
}
