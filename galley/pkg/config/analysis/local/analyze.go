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

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
)

const domainSuffix = "svc.local"

// Patch table
var (
	apiserverNew = apiserver.New
)

// SourceAnalyzer handles local analysis of k8s and file based event sources
type SourceAnalyzer struct {
	m                    *schema.Metadata
	sources              []event.Source
	analyzer             analysis.Analyzer
	transformerProviders transformer.Providers

	// Which collections are used by this analysis
	// Derived from the specified analyzer and transformer providers
	inputCollections map[collection.Name]struct{}

	collectionReporter snapshotter.CollectionReporterFn
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewSourceAnalyzer(m *schema.Metadata, analyzer analysis.Analyzer, cr snapshotter.CollectionReporterFn) *SourceAnalyzer {
	// collectionReporter hook function defaults to no-op
	if cr == nil {
		cr = func(collection.Name) {}
	}

	transformerProviders := transforms.Providers(m)

	return &SourceAnalyzer{
		m:                    m,
		sources:              make([]event.Source, 0),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		inputCollections:     getUpstreamCollections(analyzer, transformerProviders),
		collectionReporter:   cr,
	}
}

// Analyze loads the sources and executes the analysis
func (sa *SourceAnalyzer) Analyze(cancel chan struct{}) (diag.Messages, error) {
	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(meshcfg.Default())

	if len(sa.sources) == 0 {
		return nil, fmt.Errorf("at least one file and/or kubernetes source must be provided")
	}
	src := newPrecedenceSource(sa.sources)

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributor := snapshotter.NewAnalyzingDistributor(updater, sa.analyzer, snapshotter.NewInMemoryDistributor(), sa.collectionReporter)
	rt, err := processor.Initialize(sa.m, domainSuffix, event.CombineSources(src, meshsrc), sa.transformerProviders, distributor)
	if err != nil {
		return nil, err
	}
	rt.Start()
	defer rt.Stop()

	if updater.WaitForReport(cancel) {
		return updater.Get(), nil
	}

	return nil, errors.New("cancelled")
}

// AddFileKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddFileKubeSource(files []string, defaultNs string) error {
	src := inmemory.NewKubeSource(sa.m.KubeSource().Resources())
	src.SetDefaultNamespace(defaultNs)

	for _, file := range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			return err
		}
		if err = src.ApplyContent(file, string(by)); err != nil {
			return err
		}
	}

	sa.sources = append(sa.sources, src)
	return nil
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddRunningKubeSource(k kube.Interfaces) {
	// As an optimization, filter out the resources we won't need for the current analysis.
	// This matters because getting a snapshot from k8s is relatively time-expensive,
	// so removing unnecessary resources makes a useful difference.
	filteredResources := make([]schema.KubeResource, 0)
	for _, r := range sa.m.KubeSource().Resources() {
		if _, ok := sa.inputCollections[r.Collection.Name]; !ok {
			scope.Analysis.Debugf("Disabling resource %q since it isn't necessary for the current analysis", r.Collection.Name)
			r.Disabled = true
		}

		filteredResources = append(filteredResources, r)
	}

	o := apiserver.Options{
		Client:    k,
		Resources: filteredResources,
	}
	src := apiserverNew(o)

	sa.sources = append(sa.sources, src)
}

func getUpstreamCollections(analyzer analysis.Analyzer, xformProviders transformer.Providers) map[collection.Name]struct{} {
	// For each transform, map output to inputs
	outToIn := make(map[collection.Name]map[collection.Name]struct{})
	for _, xfp := range xformProviders {
		for _, out := range xfp.Outputs() {
			if _, ok := outToIn[out]; !ok {
				outToIn[out] = make(map[collection.Name]struct{})
			}
			for _, in := range xfp.Inputs() {
				outToIn[out][in] = struct{}{}
			}
		}
	}

	// 2. For each collection used by the analyzer, get its inputs using the above mapping and include them in the output set
	upstreamCollections := make(map[collection.Name]struct{})
	for _, c := range analyzer.Metadata().Inputs {
		for in := range outToIn[c] {
			upstreamCollections[in] = struct{}{}
		}
	}

	return upstreamCollections
}
