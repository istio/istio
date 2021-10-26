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

package transformer

import (
	"testing"

	. "github.com/onsi/gomega"
	"istio.io/istio/pkg/config/legacy/processing"
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestSimpleTransformerProvider(t *testing.T) {
	g := NewWithT(t)

	input := basicmeta2.K8SCollection1
	output := basicmeta2.Collection2
	handleFn := func(e event.Event, h event.Handler) {}
	opts := util.ProcessorOptions{}

	providers := Providers{
		NewSimpleTransformerProvider(input, output, handleFn),
	}
	fixtures2.ExpectEqual(t, providers[0].Inputs(), collection.SchemasFor(input))
	fixtures2.ExpectEqual(t, providers[0].Outputs(), collection.SchemasFor(output))

	transformers := providers.Create(opts)
	g.Expect(transformers).To(HaveLen(len(providers)))

	fixtures2.ExpectEqual(t, transformers[0].Inputs(), collection.SchemasFor(input))
	fixtures2.ExpectEqual(t, transformers[0].Outputs(), collection.SchemasFor(output))
}
