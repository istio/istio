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
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/processor/transforms"
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
	source event.Source,
	transformers *Transformers,
	distributor snapshotter.Distributor) (*processing.Runtime, error) {

	var options []snapshotter.SnapshotOptions
	for _, s := range m.Snapshots() {
		str, err := strategy.Create(s.Strategy)
		if err != nil {
			return nil, err
		}

		opt := snapshotter.SnapshotOptions{
			Group:       s.Name,
			Distributor: distributor,
			Collections: s.Collections,
			Strategy:    str,
		}
		options = append(options, opt)
	}

	// TODO: Add a precondition test here to ensure the panic below will not fire during runtime.

	provider := func(o processing.ProcessorOptions) event.Processor {
		// The provider will late-bind processing options before setting up the snapshotter.
		for _, xf := range transformers.optsXforms {
			xf.SetOptions(o)
		}

		s, err := snapshotter.NewSnapshotter(transformers.xforms, options)
		if err != nil {
			panic(err)
		}
		return s
	}

	rtOpt := processing.RuntimeOptions{
		ProcessorProvider: provider,
		DomainSuffix:      domainSuffix,
		Source:            source,
	}

	return processing.NewRuntime(rtOpt), nil
}

// TODO: Pull this to its own file and make fields readonly
type Transformers struct {
	optsXforms []transforms.ProcessorOptionsTransformer
	xforms     []event.Transformer
}

func NewTransformers(m *schema.Metadata) *Transformers {
	optsXforms := make([]transforms.ProcessorOptionsTransformer, 0)
	optsXforms = append(optsXforms, ingress.Create()...)
	optsXforms = append(optsXforms, serviceentry.Create()...)

	xforms := make([]event.Transformer, 0)
	for _, oxf := range optsXforms {
		xf := oxf.(event.Transformer)
		xforms = append(xforms, xf)
	}
	xforms = append(xforms, direct.Create(m.DirectTransform().Mapping())...)
	xforms = append(xforms, authpolicy.Create()...)

	return &Transformers{
		optsXforms: optsXforms,
		xforms:     xforms,
	}
}
