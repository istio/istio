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

	gogotypes "github.com/gogo/protobuf/types"
	golangany "github.com/golang/protobuf/ptypes/any"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/pkg/log"
)

// ApiGenerator supports generation of high-level API resources, similar with the MCP
// protocol. This is a replacement for MCP, using XDS (and in future UDPA) as a transport.
// Based on lessons from MCP, the protocol allows 'chunking' and 'incremental' updates by
// default, using the same mechanism that EDS is using, i.e. sending only changed resources
// in a push.
type ApiGenerator struct {
}

// TODO: take 'updates' into account, don't send pushes for resources that haven't changed
// TODO: support WorkloadEntry - to generate endpoints (equivalent with EDS)

// Generate implements the generate method for high level APIs, like Istio config types.
// This provides similar functionality with MCP and :8080/debug/configz.
//
// Names are based on the current resource naming in istiod.
// IMPORTANT: based on lessons from MCP, the response has as last element a 'sync' resource.
// Currently a 'sync' just an empty one.
// This will allow avoiding extremely large packet sizes, and chunking. Not yet reflected in
// the XDS implementation, but useful to get into the initial protocol. The behavior is specific
// to this type of resources - and not prohibited by the ADS protocol, EDS has a similar behavior.
func (g *ApiGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	resp := []*golangany.Any{}

	// Example: networking.istio.io/v1alpha3/VirtualService
	// Note: this is the style used by MCP and its config. Pilot is using 'Group/Version/Kind' as the
	// key, which is similar.
	//
	// The actual type in the Any should be a real proto - which is based on the generated package name.
	// For example: type is for Any is 'type.googlepis.com/istio.networking.v1alpha3.EnvoyFilter
	// We use: networking.istio.io/v1alpha3/EnvoyFilter
	gvk := strings.SplitN(w.TypeUrl, "/", 3)
	if len(gvk) == 3 {
		rgvk := resource.GroupVersionKind{
			Group:   gvk[0],
			Version: gvk[1],
			Kind:    gvk[2],
		}
		if w.TypeUrl == collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind().String() {
			meshAny, err := gogotypes.MarshalAny(push.Mesh)
			if err == nil {
				resp = append(resp, &golangany.Any{
					TypeUrl: w.TypeUrl,
					Value:   meshAny.Value,
				})
			}
			return resp
		}

		cfg, err := push.IstioConfigStore.List(rgvk, "")
		if err != nil {
			log.Warnf("ADS: Unknown watched resources %s %v", w.TypeUrl, err)
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
			bany, err := gogotypes.MarshalAny(b)
			if err == nil {
				resp = append(resp, &golangany.Any{
					TypeUrl: bany.TypeUrl,
					Value:   bany.Value,
				})
			} else {
				log.Warna("Any ", err)
			}
		}

		// TODO: MeshConfig, current dynamic ProxyConfig (for this proxy), Networks

		if w.TypeUrl == collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String() {
			// Include 'synthetic' SE - but without the endpoints. Used to generate CDS, LDS.
			// EDS is pass-through.
			svcs := push.Services(proxy)
			for _, s := range svcs {
				c := external.ServiceToServiceEntry(s)
				b, err := configToResource(c)
				if err != nil {
					log.Warna("Resource error ", err, " ", c.Namespace, "/", c.Name)
					continue
				}
				bany, err := gogotypes.MarshalAny(b)
				if err == nil {
					resp = append(resp, &golangany.Any{
						TypeUrl: bany.TypeUrl,
						Value:   bany.Value,
					})
				} else {
					log.Warna("Any ", err)
				}
			}
		}
	}

	resp = append(resp, &golangany.Any{})

	return resp
}

// Convert from model.Config, which has no associated proto, to MCP Resource proto.
// TODO: define a proto matching Config - to avoid useless superficial conversions.
func configToResource(c *model.Config) (*mcp.Resource, error) {
	r := &mcp.Resource{}

	// MCP, K8S and Istio configs use gogo configs
	// On the wire it's the same as golang proto.
	a, err := gogotypes.MarshalAny(c.Spec)
	if err != nil {
		return nil, err
	}
	r.Body = a
	ts, err := gogotypes.TimestampProto(c.CreationTimestamp)
	if err != nil {
		return nil, err
	}
	r.Metadata = &mcp.Metadata{
		Name:        c.Namespace + "/" + c.Name,
		CreateTime:  ts,
		Version:     c.ResourceVersion,
		Labels:      c.Labels,
		Annotations: c.Annotations,
	}

	return r, nil
}
