/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package local

import (
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/source/kube"
)

type SourceAnalyzerInterface interface {
	Analyze(cancel chan struct{}) (AnalysisResult, error)

	// SetSuppressions will set the list of suppressions for the analyzer. Any
	// resource that matches the provided suppression will not be included in the
	// final message output.
	SetSuppressions(suppressions []snapshotter.AnalysisSuppression)

	// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
	AddReaderKubeSource(readers []ReaderSource) error

	// AddRunningKubeSource adds a source based on a running k8s cluster to the current SourceAnalyzer
	// Also tries to get mesh config from the running cluster, if it can
	AddRunningKubeSource(k kube.Interfaces)

	// AddFileKubeMeshConfig gets mesh config from the specified yaml file
	AddFileKubeMeshConfig(file string) error

	// AddFileKubeMeshNetworks gets a file meshnetworks and add it to the analyzer.
	AddFileKubeMeshNetworks(file string) error

	// AddDefaultResources adds some basic dummy Istio resources, based on mesh configuration.
	// This is useful for files-only analysis cases where we don't expect the user to be including istio system resources
	// and don't want to generate false positives because they aren't there.
	// Respect mesh config when deciding which default resources should be generated
	AddDefaultResources() error
}
