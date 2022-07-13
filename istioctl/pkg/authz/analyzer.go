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

// The auth package provides support for checking the authentication and authorization policy applied
// in the mesh. It aims to increase the debuggability and observability of auth policies.
// Note: this is still under active development and is not ready for real use.
package authz

import (
	"fmt"
	"io"

	envoy_admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"istio.io/istio/istioctl/pkg/util/configdump"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

// Analyzer that can be used to check authorization policy.
type Analyzer struct {
	listenerDump *envoy_admin.ListenersConfigDump
}

// NewAnalyzer creates a new analyzer for a given pod based on its envoy config.
func NewAnalyzer(envoyConfig *configdump.Wrapper) (*Analyzer, error) {
	listeners, err := envoyConfig.GetDynamicListenerDump(true)
	if err != nil {
		return nil, fmt.Errorf("failed to get dynamic listener dump: %s", err)
	}

	return &Analyzer{listenerDump: listeners}, nil
}

// Print print the analysis results.
func (a *Analyzer) Print(writer io.Writer) {
	var listeners []*listener.Listener
	for _, l := range a.listenerDump.DynamicListeners {
		listenerTyped := &listener.Listener{}
		// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
		l.ActiveState.Listener.TypeUrl = v3.ListenerType
		err := l.ActiveState.Listener.UnmarshalTo(listenerTyped)
		if err != nil {
			return
		}
		listeners = append(listeners, listenerTyped)
	}
	Print(writer, listeners)
}
