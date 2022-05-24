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

package fuzz

import (
	"bytes"
	"os"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/scope"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/pkg/log"
)

var availableAnalyzers = analyzers.All()

// createRandomConfigFile creates a single fuzzed config file
func createRandomConfigFile(f *fuzz.ConsumeFuzzer) (string, error) {
	data, err := f.GetBytes()
	if err != nil {
		return "nobytes", err
	}
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		return "nofile", err
	}
	if _, err := tmpfile.Write(data); err != nil {
		return "nofile", err
	}
	if err := tmpfile.Close(); err != nil {
		return "nofile", err
	}
	return tmpfile.Name(), nil
}

// createRandomConfigFiles creates a slice of ReaderSources
func createRandomConfigFiles(f *fuzz.ConsumeFuzzer) ([]local.ReaderSource, error) {
	var files []local.ReaderSource

	numberOfFiles, err := f.GetInt()
	if err != nil {
		return files, err
	}
	maxFiles := numberOfFiles % 10

	// Gather test files
	for i := 0; i < maxFiles; i++ {
		name, err := f.GetString()
		if err != nil {
			return files, err
		}
		rBytes, err := f.GetBytes()
		if err != nil {
			return files, err
		}
		r := bytes.NewReader(rBytes)
		files = append(files, local.ReaderSource{Name: name, Reader: r})
	}
	return files, nil
}

// runAnalyzer runs the analyzer
func runAnalyzer(sa *local.IstiodAnalyzer) (local.AnalysisResult, error) {
	prevLogLevel := scope.Processing.GetOutputLevel()
	scope.Processing.SetOutputLevel(log.NoneLevel)
	defer scope.Processing.SetOutputLevel(prevLogLevel)

	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return local.AnalysisResult{}, err
	}
	return result, err
}

// FuzzAnalyzer implements the fuzzer
func FuzzAnalyzer(data []byte) int {
	f := fuzz.NewConsumer(data)
	analyzerIndex, err := f.GetInt()
	if err != nil {
		return 0
	}
	analyzer := availableAnalyzers[analyzerIndex%len(availableAnalyzers)]

	requestedInputsByAnalyzer := make(map[string]map[collection.Name]struct{})
	analyzerName := analyzer.Metadata().Name
	cr := func(col collection.Name) {
		if _, ok := requestedInputsByAnalyzer[analyzerName]; !ok {
			requestedInputsByAnalyzer[analyzerName] = make(map[collection.Name]struct{})
		}
		requestedInputsByAnalyzer[analyzerName][col] = struct{}{}
	}

	// Mesh config file
	addMeshConfig, err := f.GetBool()
	if err != nil {
		return 0
	}
	meshConfigFile := ""
	if addMeshConfig {
		meshConfigFile, err = createRandomConfigFile(f)
		if err != nil {
			return 0
		}
		defer os.Remove(meshConfigFile)
	}

	// Mesh networks file
	addMeshNetworks, err := f.GetBool()
	if err != nil {
		return 0
	}
	meshNetworkFile := ""
	if addMeshNetworks {
		meshNetworkFile, err = createRandomConfigFile(f)
		if err != nil {
			return 0
		}
		defer os.Remove(meshNetworkFile)
	}

	configFiles, err := createRandomConfigFiles(f)
	if err != nil {
		return 0
	}

	sa := local.NewSourceAnalyzer(analysis.Combine("testCase", analyzer), "", "istio-system", cr, true, 10*time.Second)
	if addMeshConfig {
		err = sa.AddFileKubeMeshConfig(meshConfigFile)
		if err != nil {
			return 0
		}
	}
	if addMeshNetworks {
		err = sa.AddFileKubeMeshNetworks(meshNetworkFile)
		if err != nil {
			return 0
		}
	}

	// Include default resources
	err = sa.AddDefaultResources()
	if err != nil {
		return 0
	}

	// Include resources from test files
	err = sa.AddReaderKubeSource(configFiles)
	if err != nil {
		return 0
	}
	_, _ = runAnalyzer(sa)
	return 1
}
