package framework

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
	"os"
	"path"
	"sync"
)

var (
	analyzeMode bool
	analysis    *suiteAnalysis
	analysisMu  sync.Mutex
)

func init() {
	analyzeMode = os.Getenv("ANALYZE_TESTS") != ""
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
		scopes.Framework.Errorf("failed to marshal analysis for %s: %v", analysis.SuiteID, err)
		return
	}

	marshalled, err := yaml.Marshal(analysis)
	if err != nil {
		scopes.Framework.Errorf("failed to marshal analysis for %s: %v", analysis.SuiteID, err)
		return
	}
	scopes.Framework.Info("\n" + string(marshalled))

	outPath := path.Join(s.RunDir(), fmt.Sprintf("%s_analysis.yaml", analysis.SuiteID))
	if err := ioutil.WriteFile(outPath, marshalled, 0666); err != nil {
		scopes.Framework.Errorf("failed writing analysis to file for %s: %v", analysis.SuiteID, err)
		return
	}
	scopes.Framework.Infof("Wrote analysis to %s", outPath)
}

// suiteAnalysis captures the results of analyzing a Suite
type suiteAnalysis struct {
	SuiteID       string
	Labels        []label.Instance
	SingleCluster bool
	MultiCluster  bool
	Tests         []*testAnalysis
}

func (s *suiteAnalysis) addTest(test *testAnalysis) {
	s.Tests = append(s.Tests, test)
}

// suiteAnalysis captures the results of analyzing a Test
type testAnalysis struct {
	TestID         string
	Labels         []label.Instance
	Features       map[features.Feature][]string
	Valid          bool
	SingleCluster  bool
	MultiCluster   bool
	NotImplemented bool
}
