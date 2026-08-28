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

package multixds

import "testing"

func TestMakeSan(t *testing.T) {
	cases := []struct {
		revision string
		want     string
	}{
		{"", "istiod.istio-system.svc"},
		{"default", "istiod.istio-system.svc"},
		{"canary", "istiod-canary.istio-system.svc"},
	}
	for _, c := range cases {
		if got := makeSan("istio-system", c.revision); got != c.want {
			t.Errorf("makeSan(%q) = %q, want %q", c.revision, got, c.want)
		}
	}
}
