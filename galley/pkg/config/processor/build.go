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

package processor

import (
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/monitoring"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/processor/transforms/authpolicy"
	"istio.io/istio/galley/pkg/config/processor/transforms/direct"
	"istio.io/istio/galley/pkg/config/processor/transforms/ingress"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry"
	"istio.io/istio/galley/pkg/config/schema"
)

// Initialize a processing runtime for Galley.
func Initialize(
	m *schema.Metadata,
	domainSuffix string,
	sources []event.Source,
	distributor snapshotter.Distributor,
	reporter monitoring.Reporter) (*processing.Runtime, error) {

	var xforms []event.Transformer

	xf := direct.Create(m.DirectTransform().Mapping())
	xforms = append(xforms, xf...)

	xf = ingress.Create()
	xforms = append(xforms, xf...)

	xf = authpolicy.Create()
	xforms = append(xforms, xf...)

	xf = serviceentry.Create()
	xforms = append(xforms, xf...)

	var snOpts []snapshotter.SnapshotOption
	for _, s := range m.Snapshots() {
		str, err := strategy.Create(s.Strategy, reporter)
		if err != nil {
			return nil, err
		}

		opt := snapshotter.SnapshotOption{
			Group:       s.Name,
			Distributor: distributor,
			Collections: s.Collections,
			Strategy:    str,
		}
		snOpts = append(snOpts, opt)
	}
	snapshotter := snapshotter.NewSnapshotter(xforms, snOpts, reporter)

	rtopt := processing.RuntimeOptions{
		Processor:    snapshotter,
		DomainSuffix: domainSuffix,
		Sources:      sources,
	}

	return processing.NewRuntime(rtopt), nil
}
