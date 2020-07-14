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

package data

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/resource"
)

var (
	// EntryN1I1V1 is a test resource.Instance
	EntryN1I1V1 = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n1", "i1"),
			Version:  "v1",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`
{
	"n1_i1": "v1"
}`),
	}

	// EntryN1I1V1ClusterScoped is a test resource.Instance that is cluster scoped.
	EntryN1I1V1ClusterScoped = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("", "i1"),
			Version:  "v1",
			Schema:   K8SCollection2.Resource(),
		},
		Message: parseStruct(`
{
	"n1_i1": "v1"
}`),
	}

	// EntryN1I1V1Broken is a test resource.Instance
	EntryN1I1V1Broken = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n1", "i1"),
			Version:  "v1",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: nil,
	}

	// EntryN1I1V2 is a test resource.Instance
	EntryN1I1V2 = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n1", "i1"),
			Version:  "v2",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`
{
	"n1_i1": "v2"
}`),
	}

	// EntryN2I2V1 is a test resource.Instance
	EntryN2I2V1 = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n2", "i2"),
			Version:  "v1",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`
{
	"n2_i2": "v1"
}`),
	}

	// EntryN2I2V2 is a test resource.Instance
	EntryN2I2V2 = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n2", "i2"),
			Version:  "v2",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`{
	"n2_i2": "v2"
}`),
	}

	// EntryN3I3V1 is a test resource.Instance
	EntryN3I3V1 = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("n3", "i3"),
			Version:  "v1",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`{
	"n3_i3": "v1"
}`),
	}

	// EntryI1V1NoNamespace is a test resource.Instance
	EntryI1V1NoNamespace = &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("", "i1"),
			Version:  "v1",
			Schema:   basicmeta.K8SCollection1.Resource(),
		},
		Message: parseStruct(`{
		"n1_i1": "v1"
	}`),
	}
)

func parseStruct(s string) *types.Struct {
	m := jsonpb.Unmarshaler{}

	str := &types.Struct{}
	err := m.Unmarshal(bytes.NewReader([]byte(s)), str)
	if err != nil {
		panic(err)
	}

	return str
}
