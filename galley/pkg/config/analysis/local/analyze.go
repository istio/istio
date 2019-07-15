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

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
)

// AnalyzeFiles loads yaml files from given paths, parses and analyzes them.
func AnalyzeFiles(domainSuffix string, cancel chan struct{}, files ...string) (diag.Messages, error) {
	o := log.DefaultOptions()
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("processor", log.ErrorLevel)
	o.SetOutputLevel("source", log.ErrorLevel)
	err := log.Configure(o)
	if err != nil {
		return nil, fmt.Errorf("unable to configure logging: %v", err)
	}

	m := metadata.MustGet()

	src := inmemory.NewKubeSource(m.KubeSource().Resources())
	for _, file := range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err = src.ApplyContent(file, string(by)); err != nil {
			return nil, err
		}
	}

	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(meshcfg.Default())

	distributor := snapshotter.NewInMemoryDistributor()
	reporter := &processing.InMemoryStatusReporter{}
	rt, err := processor.Initialize(m, domainSuffix, src, distributor, analyzers.All(), reporter)
	if err != nil {
		return nil, err
	}
	rt.Start()
	defer rt.Stop()

	if reporter.WaitForReport(cancel) {
		return reporter.Get(), nil
	}

	return nil, errors.New("cancelled")
}
