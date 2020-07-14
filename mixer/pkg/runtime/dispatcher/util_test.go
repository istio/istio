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

package dispatcher

import (
	"testing"

	"istio.io/pkg/attribute"
)

func TestGetIdentityNamespace(t *testing.T) {
	cases := []struct {
		bag  attribute.Bag
		want string
	}{{
		bag: attribute.GetMutableBagForTesting(map[string]interface{}{
			"destination.namespace": "value",
			"context.reporter.kind": "inbound",
		}),
		want: "value",
	}, {
		bag: attribute.GetMutableBagForTesting(map[string]interface{}{
			"source.namespace":      "value",
			"context.reporter.kind": "outbound",
		}),
		want: "value",
	}, {
		bag:  attribute.GetMutableBagForTesting(map[string]interface{}{}),
		want: "",
	}}

	for _, cs := range cases {
		result := getIdentityNamespace(cs.bag)
		if result != cs.want {
			t.Errorf("got %q, want %q", result, cs.want)
		}
	}
}
