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

// Create a new Direct transformer.
func Create() []event.Transformer {
	return []event.Transformer{
		event.NewFnTransform(
			collection.Names{metadata.K8SAuthenticationIstioIoV1Alpha1Policies},
			collection.Names{metadata.IstioAuthenticationV1Alpha1Policies},
			nil,
			nil,
			handler(metadata.IstioAuthenticationV1Alpha1Policies),
		),
		event.NewFnTransform(
			collection.Names{metadata.K8SAuthenticationIstioIoV1Alpha1Meshpolicies},
			collection.Names{metadata.IstioAuthenticationV1Alpha1Meshpolicies},
			nil,
			nil,
			handler(metadata.IstioAuthenticationV1Alpha1Meshpolicies),
		),
	}
}

func handler(destination collection.Name) func(e event.Event, h event.Handler) {
	return func(e event.Event, h event.Handler) {
		e = e.WithSource(destination)

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

		h.Handle(e)
	}
}
