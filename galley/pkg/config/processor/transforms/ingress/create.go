// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain ingressAdapter copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ingress

import (
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing"
)

// Create transformer for Ingress resource
func Create(options processing.ProcessorOptions) []event.Transformer {
	return []event.Transformer{
		newGatewayXform(options),
		newVirtualServiceXform(options),
	}
}
