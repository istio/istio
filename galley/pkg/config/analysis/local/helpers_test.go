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

// Test helpers common to this package

import (
	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/data"
)

func createTestEvent(k event.Kind, r *resource.Entry) event.Event {
	return event.Event{
		Kind:   k,
		Source: data.Collection1,
		Entry:  r,
	}
}

func createTestResource(ns, name, version string) *resource.Entry {
	rname := resource.NewName(ns, name)
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name:    rname,
			Version: resource.Version(version),
		},
		Item: &types.Empty{},
		Origin: &rt.Origin{
			Name: rname,
		},
	}
}
