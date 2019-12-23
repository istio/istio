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
package schema

import (
	"fmt"
	"testing"

	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/gomega"

	"github.com/gogo/protobuf/proto"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/analysis/testing/fixtures"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
)

func TestCorrectArgs(t *testing.T) {
	g := NewGomegaWithT(t)

	m1 := &v1alpha3.VirtualService{}

	testSchema := schema.Instance{
		Collection: metadata.IstioNetworkingV1Alpha3Virtualservices.String(),
		Validate: func(name, ns string, msg proto.Message) (errs error) {
			g.Expect(name).To(Equal("name"))
			g.Expect(ns).To(Equal("ns"))
			g.Expect(msg).To(Equal(m1))

			return nil
		},
	}
	ctx := &fixtures.Context{
		Resources: []*resource.Instance{
			{
				Message: &v1alpha3.VirtualService{},
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("ns", "name"),
				},
			},
		},
	}
	a := ValidationAnalyzer{s: testSchema}
	a.Analyze(ctx)
}

func TestSchemaValidationWrapper(t *testing.T) {
	testCol := metadata.IstioNetworkingV1Alpha3Virtualservices

	m1 := &v1alpha3.VirtualService{}
	m2 := &v1alpha3.VirtualService{}
	m3 := &v1alpha3.VirtualService{}

	testSchema := schema.Instance{
		Collection: testCol.String(),
		Validate: func(_, _ string, msg proto.Message) (errs error) {
			if msg == m1 {
				return nil
			}
			if msg == m2 {
				return fmt.Errorf("")
			}
			if msg == m3 {
				return multierror.Append(fmt.Errorf(""), fmt.Errorf(""))
			}
			return nil
		},
	}

	a := ValidationAnalyzer{s: testSchema}

	t.Run("CheckMetadataInputs", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(a.Metadata().Inputs).To(ConsistOf(testCol))
	})

	t.Run("NoErrors", func(t *testing.T) {
		g := NewGomegaWithT(t)
		ctx := &fixtures.Context{
			Resources: []*resource.Instance{
				{
					Message: m1,
				},
			},
		}
		a.Analyze(ctx)
		g.Expect(ctx.Reports).To(BeEmpty())
	})

	t.Run("SingleError", func(t *testing.T) {
		g := NewGomegaWithT(t)

		ctx := &fixtures.Context{
			Resources: []*resource.Instance{
				{
					Message: m2,
				},
			},
		}
		a.Analyze(ctx)
		g.Expect(ctx.Reports).To(HaveLen(1))
		g.Expect(ctx.Reports[0].Type).To(Equal(msg.SchemaValidationError))
	})

	t.Run("MultiError", func(t *testing.T) {
		g := NewGomegaWithT(t)
		ctx := &fixtures.Context{
			Resources: []*resource.Instance{
				{
					Message: m3,
				},
			},
		}
		a.Analyze(ctx)
		g.Expect(ctx.Reports).To(HaveLen(2))
		g.Expect(ctx.Reports[0].Type).To(Equal(msg.SchemaValidationError))
		g.Expect(ctx.Reports[1].Type).To(Equal(msg.SchemaValidationError))
	})
}
