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

package sink

import (
	"testing"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
)

func TestInMemoryUpdater(t *testing.T) {
	u := NewInMemoryUpdater()

	o := u.Get("foo")
	if len(o) != 0 {
		t.Fatalf("Unexpected items in updater: %v", o)
	}

	c := Change{
		Collection: "foo",
		Objects: []*Object{
			{
				TypeURL: "foo",
				Metadata: &mcp.Metadata{
					Name: "bar",
				},
				Body: &types.Empty{},
			},
		},
	}

	err := u.Apply(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	o = u.Get("foo")
	if len(o) != 1 {
		t.Fatalf("expected item not found: %v", o)
	}

	if o[0].Metadata.Name != "bar" {
		t.Fatalf("expected name not found on object: %v", o)
	}
}
