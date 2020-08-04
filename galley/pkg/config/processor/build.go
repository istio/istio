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

package processor

import (
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema"
)

// Settings is the settings that are needed for creating a config processing pipeline that can read
// from a file system, or API Server, and can publish snapshots via MCP.
type Settings struct {
	Metadata           *schema.Metadata
	DomainSuffix       string
	Source             event.Source
	TransformProviders transformer.Providers
	Distributor        snapshotter.Distributor
	EnabledSnapshots   []string
}

// Initialize a processing runtime for Galley.
func Initialize(settings Settings) (*processing.Runtime, error) {
	var options []snapshotter.SnapshotOptions
	for _, s := range settings.Metadata.AllSnapshots() {
		if !isEnabled(s.Name, settings.EnabledSnapshots) {
			continue
		}

		str, err := strategy.Create(s.Strategy)
		if err != nil {
			return nil, err
		}

		opt := snapshotter.SnapshotOptions{
			Group:       s.Name,
			Distributor: settings.Distributor,
			Collections: s.Collections,
			Strategy:    str,
		}
		options = append(options, opt)
	}

	// TODO: Add a precondition test here to ensure the panic below will not fire during runtime.

	// This is passed as a provider so it can be evaluated once ProcessorOptions become available
	procProvider := func(o processing.ProcessorOptions) event.Processor {
		xforms := settings.TransformProviders.Create(o)

		s, err := snapshotter.NewSnapshotter(xforms, options)
		if err != nil {
			panic(err)
		}
		return s
	}

	rtOpt := processing.RuntimeOptions{
		ProcessorProvider: procProvider,
		DomainSuffix:      settings.DomainSuffix,
		Source:            settings.Source,
	}

	return processing.NewRuntime(rtOpt), nil
}

func isEnabled(snapshotName string, enabledSnapshots []string) bool {
	for _, es := range enabledSnapshots {
		if snapshotName == es {
			return true
		}
	}

	return false
}
