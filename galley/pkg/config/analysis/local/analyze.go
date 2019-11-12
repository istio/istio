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

package local

import (
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
)

const domainSuffix = "cluster.local"

// Patch table
var (
	apiserverNew = apiserver.New
)

// SourceAnalyzer handles local analysis of k8s event sources, both live and file-based
type SourceAnalyzer struct {
	m                    *schema.Metadata
	sources              []event.Source
	analyzer             *analysis.CombinedAnalyzer
	transformerProviders transformer.Providers
	namespace            string

	// Which kube resources are used by this analyzer
	// Derived from metadata and the specified analyzer and transformer providers
	kubeResources schema.KubeResources

	// Hook function called when a collection is used in analysis
	collectionReporter snapshotter.CollectionReporterFn
}

// AnalysisResult represents the returnable results of an analysis execution
type AnalysisResult struct {
	Messages          diag.Messages
	SkippedAnalyzers  []string
	ExecutedAnalyzers []string
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewSourceAnalyzer(m *schema.Metadata, analyzer *analysis.CombinedAnalyzer, namespace string,
	cr snapshotter.CollectionReporterFn, serviceDiscovery bool) *SourceAnalyzer {

	// collectionReporter hook function defaults to no-op
	if cr == nil {
		cr = func(collection.Name) {}
	}

	transformerProviders := transforms.Providers(m)

	// Get the closure of all input collections for our analyzer, paying attention to transforms
	kubeResources := kuberesource.DisableExcludedKubeResources(
		m.KubeSource().Resources(),
		transformerProviders,
		analyzer.Metadata().Inputs,
		kuberesource.DefaultExcludedResourceKinds(),
		serviceDiscovery)

	return &SourceAnalyzer{
		m:                    m,
		sources:              make([]event.Source, 0),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		namespace:            namespace,
		kubeResources:        kubeResources,
		collectionReporter:   cr,
	}
}

// Analyze loads the sources and executes the analysis
func (sa *SourceAnalyzer) Analyze(cancel chan struct{}) (AnalysisResult, error) {
	var result AnalysisResult

	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(meshcfg.Default())

	if len(sa.sources) == 0 {
		return result, fmt.Errorf("at least one file and/or Kubernetes source must be provided")
	}
	src := newPrecedenceSource(sa.sources)

	var namespaces []string
	if sa.namespace != "" {
		namespaces = []string{sa.namespace}
	}

	result.SkippedAnalyzers = sa.analyzer.RemoveDisabled(sa.kubeResources.DisabledCollections(), sa.transformerProviders)
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributorSettings := snapshotter.AnalyzingDistributorSettings{
		StatusUpdater:      updater,
		Analyzer:           sa.analyzer,
		Distributor:        snapshotter.NewInMemoryDistributor(),
		AnalysisSnapshots:  []string{metadata.LocalAnalysis, metadata.SyntheticServiceEntry},
		TriggerSnapshot:    metadata.LocalAnalysis,
		CollectionReporter: sa.collectionReporter,
		AnalysisNamespaces: namespaces,
	}
	distributor := snapshotter.NewAnalyzingDistributor(distributorSettings)

	processorSettings := processor.Settings{
		Metadata:           sa.m,
		DomainSuffix:       domainSuffix,
		Source:             event.CombineSources(src, meshsrc),
		TransformProviders: sa.transformerProviders,
		Distributor:        distributor,
		EnabledSnapshots:   []string{metadata.LocalAnalysis, metadata.SyntheticServiceEntry},
	}
	rt, err := processor.Initialize(processorSettings)
	if err != nil {
		return result, err
	}
	rt.Start()
	defer rt.Stop()

	scope.Analysis.Debugf("Waiting for analysis messages to be available...")
	if updater.WaitForReport(cancel) {
		result.Messages = updater.Get()
		return result, nil
	}

	return result, errors.New("cancelled")
}

// AddFileKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddFileKubeSource(files []string) error {
	src := inmemory.NewKubeSource(sa.kubeResources)
	src.SetDefaultNamespace(sa.namespace)

	var errs error

	// If we encounter any errors reading or applying files, track them but attempt to continue
	for _, file := range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		if err = src.ApplyContent(file, string(by)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	sa.sources = append(sa.sources, src)

	return errs
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddRunningKubeSource(k kube.Interfaces) {
	o := apiserver.Options{
		Client:    k,
		Resources: sa.kubeResources,
	}
	src := apiserverNew(o)

	sa.sources = append(sa.sources, src)
}
