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
package local

// Test helpers common to this package

import (
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
)

func createTestEvent(t *testing.T, k event.Kind, r *resource.Instance) event.Event {
	t.Helper()
	return event.Event{
		Kind:     k,
		Source:   basicmeta.K8SCollection1,
		Resource: r,
	}
}

func createTestResource(t *testing.T, ns, name, version string) *resource.Instance {
	t.Helper()
	rname := resource.NewFullName(resource.Namespace(ns), resource.LocalName(name))
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: rname,
			Version:  resource.Version(version),
		},
		Message: &types.Empty{},
		Origin: &rt.Origin{
			FullName: rname,
		},
	}
}
