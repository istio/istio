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

package authpolicy

import (
	"reflect"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processor/metadata"
)

var scope = log.RegisterScope("processing", "", 0)

type xformer struct {
	source      collection.Name
	destination collection.Name
	handler     event.Handler
}

var _ event.Transformer = &xformer{}

// Create a new Direct transformer.
func Create() []event.Transformer {
	return []event.Transformer{
		&xformer{
			source:      metadata.K8SAuthenticationIstioIoV1Alpha1Policies,
			destination: metadata.IstioAuthenticationV1Alpha1Policies,
		},
		&xformer{
			source:      metadata.K8SAuthenticationIstioIoV1Alpha1Meshpolicies,
			destination: metadata.IstioAuthenticationV1Alpha1Meshpolicies,
		},
	}
}

// Inputs implements processing.Transformer
func (x *xformer) Inputs() collection.Names {
	return []collection.Name{x.source}
}

// Outputs implements processing.Transformer
func (x *xformer) Outputs() collection.Names {
	return []collection.Name{x.destination}
}

// Start implements event.Transformer
func (x *xformer) Start(o interface{}) {}

// Stop implements processing.Transformer
func (x *xformer) Stop() {}

// Select implements event.Transformer
func (x *xformer) Select(c collection.Name, h event.Handler) {
	if c == x.destination {
		x.handler = event.CombineHandlers(x.handler, h)
	}
}

// Handle implements processing.Transformer
func (x *xformer) Handle(e event.Event) {
	if x.handler == nil {
		return
	}

	switch e.Kind {
	case event.Reset:
		x.handler.Handle(e)
		return
	}

	if e.Source != x.source {
		scope.Warnf("authpolicy.xformer.Handle: Unexpected event: %v", e)
		return
	}

	e.Source = x.destination

	if e.Entry != nil && e.Entry.Item != nil {
		policy, ok := e.Entry.Item.(*authn.Policy)
		if !ok {
			scope.Errorf("unexpected proto found when converting authn.Policy: %v", reflect.TypeOf(e.Entry.Item))
			return
		}

		// The pilot authentication plugin's config handling allows the mtls
		// peer method object value to be nil. See pilot/pkg/networking/plugin/authn/authentication.go#L68
		//
		// For example,
		//
		//     metadata:
		//       name: d-ports-mtls-enabled
		//     spec:
		//       targets:
		//       - name: d
		//         ports:
		//         - number: 80
		//       peers:
		//       - mtls:
		//
		// This translates to the following in-memory representation:
		//
		//     policy := &authn.Policy{
		//       Peers: []*authn.PeerAuthenticationMethod{{
		//         &authn.PeerAuthenticationMethod_Mtls{},
		//       }},
		//     }
		//
		// The PeerAuthenticationMethod_Mtls object with nil field is lost when
		// the proto is re-encoded for transport via MCP. As a workaround, fill
		// in the missing field value which is functionality equivalent.
		for _, peer := range policy.Peers {
			if mtls, ok := peer.Params.(*authn.PeerAuthenticationMethod_Mtls); ok && mtls.Mtls == nil {
				mtls.Mtls = &authn.MutualTls{}
			}
		}
	}

	x.handler.Handle(e)
}
