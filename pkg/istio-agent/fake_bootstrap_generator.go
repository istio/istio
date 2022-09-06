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

package istioagent

import (
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
)

// FakeBootstrapGenerator is an delayed Envoy bootstrap generator.
type FakeBootstrapGenerator struct {
}

var _ model.XdsResourceGenerator = &FakeBootstrapGenerator{}

// Generate returns a bootstrap discovery response.
func (e *FakeBootstrapGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	time.Sleep(500 * time.Millisecond)
	return (&xds.BootstrapGenerator{}).Generate(proxy, w, req)
}
