package framework

import (
	"os"
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
	// TODO dump
	analysis = nil
	analysisMu.Unlock()
}

// suiteAnalysis captures the results of analyzing a Suite
type suiteAnalysis struct {
	ID string

	testMu sync.Mutex
	tests  []*testAnalysis
}

func (s *suiteAnalysis) addTest(test *testAnalysis) {
	s.testMu.Lock()
	defer s.testMu.Unlock()
	s.tests = append(s.tests, test)
}

// suiteAnalysis captures the results of analyzing a Test
type testAnalysis struct {
	ID string
}
