// Copyright 2017 Istio Authors
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

package apigen

import (
	"strings"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/pkg/log"
)

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// DNS can populate the name to cluster VIP mapping using this response.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.
type ApiGenerator struct {
}

func (g *ApiGenerator) Generate(node *model.Proxy, push *model.PushContext, w *model.WatchedResource) []*any.Any {
	return g.handleConfigResource(node, push, w)
}

// Handle watching Istio config types. This provides similar functionality with MCP.
// Alternative to using 8080:/debug/configz
// Names are based on the current stable naming in istiod.
func (g *ApiGenerator) handleConfigResource(node *model.Proxy, push *model.PushContext, w *model.WatchedResource) []*any.Any {
	resp := []*any.Any{}

	// Example: networking.istio.io/v1alpha3/VirtualService
	// Note: this is the style used by MCP and its config. Pilot is using 'Group/Version/Kind' as the
	// key, which is similar.
	//
	// The actual type in the Any should be a real proto - which is based on the generated package name.
	// For example: type is for Any is 'type.googlepis.com/istio.networking.v1alpha3.EnvoyFilter
	// We use: networking.istio.io/v1alpha3/EnvoyFilter
	gvk := strings.SplitN(w.TypeURL, "/", 3)
	if len(gvk) == 3 {
		rgvk := resource.GroupVersionKind{
			Group:   gvk[0],
			Version: gvk[1],
			Kind:    gvk[2],
		}

		cfg, err := push.IstioConfigStore.List(rgvk, "")
		if err != nil {
			log.Warnf("ADS: Unknown watched resources %s %v", w.TypeURL, err)
			return resp
		}
		for _, c := range cfg {
			// Right now model.Config is not a proto - until we change it, mcp.Resource.
			// This also helps migrating MCP users.

			b, err := configToResource(&c)
			if err != nil {
				log.Warna("Resource error ", err, " ", c.Namespace, "/", c.Name)
				continue
			}
			bany, err := types.MarshalAny(b)
			if err == nil {
				resp = append(resp, &any.Any{
					TypeUrl: bany.TypeUrl,
					Value:  bany.Value,
				})
			} else {
				log.Warna("Any ", err)
			}
		}
		if w.TypeURL == collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String() {
			// Include 'synthetic' SE - but without the endpoints. Used to generate CDS, LDS.
			// EDS is pass-through.
			svcs := push.Services(node)
			for _, s := range svcs {
				c := external.ServiceToServiceEntry(s)
				b, err := configToResource(c)
				if err != nil {
					log.Warna("Resource error ", err, " ", c.Namespace, "/", c.Name)
					continue
				}
				bany, err := types.MarshalAny(b)
				if err == nil {
					resp = append(resp, &any.Any{
						TypeUrl: bany.TypeUrl,
						Value:  bany.Value,
					})
				} else {
					log.Warna("Any ", err)
				}
			}
		}
	}

	return resp
}

// Convert from model.Config, which has no associated proto, to MCP Resource proto.
// TODO: define a proto matching Config - to avoid useless superficial conversions.
func configToResource(c *model.Config) (*mcp.Resource, error) {
	r := &mcp.Resource{}

	// MCP, K8S and Istio configs use gogo configs
	// On the wire it's the same as golang proto.
	a, err := types.MarshalAny(c.Spec)
	if err != nil {
		return nil, err
	}
	r.Body = a
	ts, err := types.TimestampProto(c.CreationTimestamp)
	if err != nil {
		return nil, err
	}
	r.Metadata = &mcp.Metadata{
		Name:                 c.Namespace + "/" + c.Name,
		CreateTime:           ts,
		Version:              c.ResourceVersion,
		Labels: c.Labels,
		Annotations: c.Annotations,
	}

	return r, nil
}


