// Copyright Istio Authors. All Rights Reserved.
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

package model

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/util/identifier"
	"istio.io/istio/pkg/util/sets"
)

// ProxyView provides a restricted view of mesh endpoints for a Proxy.
type ProxyView interface {
	fmt.Stringer
	IsVisible(ep *IstioEndpoint) bool
}

// ProxyViewAll is a ProxyView where all endpoints are visible.
var ProxyViewAll ProxyView = proxyViewAll{}

type proxyViewAll struct{}

func (v proxyViewAll) IsVisible(*IstioEndpoint) bool {
	return true
}

func (v proxyViewAll) String() string {
	return ""
}

func newProxyView(node *Proxy) ProxyView {
	if node == nil || node.Metadata == nil || len(node.Metadata.RequestedNetworkView) == 0 {
		return ProxyViewAll
	}

	// Restrict the view to the requested networks.
	return &proxyViewImpl{
		visible: sets.New(node.Metadata.RequestedNetworkView...).Insert(identifier.Undefined),
		getValue: func(ep *IstioEndpoint) string {
			return ep.Network.String()
		},
	}
}

type proxyViewImpl struct {
	visible  sets.Set
	getValue func(ep *IstioEndpoint) string
}

func (v *proxyViewImpl) IsVisible(ep *IstioEndpoint) bool {
	return v.visible.Contains(v.getValue(ep))
}

func (v *proxyViewImpl) String() string {
	return strings.Join(v.visible.SortedList(), ",")
}
