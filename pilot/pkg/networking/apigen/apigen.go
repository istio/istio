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

package apigen

import (
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

// APIGenerator supports generation of high-level API resources, similar with the MCP
// protocol. This is a replacement for MCP, using XDS (and in future UDPA) as a transport.
// Based on lessons from MCP, the protocol allows incremental updates by
// default, using the same mechanism that EDS is using, i.e. sending only changed resources
// in a push. Incremental deletes are sent as a resource with empty body.
//
// Example: networking.istio.io/v1alpha3/VirtualService
//
// TODO: we can also add a special marker in the header)
type APIGenerator struct {
	// ConfigStore interface for listing istio api resources.
	store model.ConfigStore
}

func NewGenerator(store model.ConfigStore) *APIGenerator {
	return &APIGenerator{
		store: store,
	}
}

// TODO: take 'updates' into account, don't send pushes for resources that haven't changed
// TODO: support WorkloadEntry - to generate endpoints (equivalent with EDS)
// TODO: based on lessons from MCP, we want to send 'chunked' responses, like apiserver does.
// A first attempt added a 'sync' record at the end. Based on feedback and common use, a
// different approach can be used - for large responses, we can mark the last one as 'hasMore'
// by adding a field to the envelope.

// Generate implements the generate method for high level APIs, like Istio config types.
// This provides similar functionality with MCP and :8080/debug/configz.
//
// Names are based on the current resource naming in istiod stores.
func (g *APIGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	resp := model.Resources{}

	// Note: this is the style used by MCP and its config. Pilot is using 'Group/Version/Kind' as the
	// key, which is similar.
	//
	// The actual type in the Any should be a real proto - which is based on the generated package name.
	// For example: type is for Any is 'type.googlepis.com/istio.networking.v1alpha3.EnvoyFilter
	// We use: networking.istio.io/v1alpha3/EnvoyFilter
	kind := strings.SplitN(w.TypeUrl, "/", 3)
	if len(kind) != 3 {
		log.Warnf("ADS: Unknown watched resources %s", w.TypeUrl)
		// Still return an empty response - to not break waiting code. It is fine to not know about some resource.
		return resp, model.DefaultXdsLogDetails, nil
	}
	// TODO: extra validation may be needed - at least logging that a resource
	// of unknown type was requested. This should not be an error - maybe client asks
	// for a valid CRD we just don't know about. An empty set indicates we have no such config.
	rgvk := config.GroupVersionKind{
		Group:   kind[0],
		Version: kind[1],
		Kind:    kind[2],
	}
	if w.TypeUrl == collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind().String() {
		resp = append(resp, &discovery.Resource{
			Resource: util.MessageToAny(req.Push.Mesh),
		})
		return resp, model.DefaultXdsLogDetails, nil
	}

	// TODO: what is the proper way to handle errors ?
	// Normally once istio is 'ready' List can't return errors on a valid config -
	// even if k8s is disconnected, we still cache all previous results.
	// This needs further consideration - I don't think XDS or MCP transports
	// have a clear recommendation.
	cfg, err := g.store.List(rgvk, "")
	if err != nil {
		log.Warnf("ADS: Error reading resource %s %v", w.TypeUrl, err)
		return resp, model.DefaultXdsLogDetails, nil
	}
	for _, c := range cfg {
		// Right now model.Config is not a proto - until we change it, mcp.Resource.
		// This also helps migrating MCP users.

		b, err := config.PilotConfigToResource(&c)
		if err != nil {
			log.Warn("Resource error ", err, " ", c.Namespace, "/", c.Name)
			continue
		}
		resp = append(resp, &discovery.Resource{
			Name:     c.Namespace + "/" + c.Name,
			Resource: util.MessageToAny(b),
		})
	}

	// TODO: MeshConfig, current dynamic ProxyConfig (for this proxy), Networks

	if w.TypeUrl == gvk.ServiceEntry.String() {
		// Include 'synthetic' SE - but without the endpoints. Used to generate CDS, LDS.
		// EDS is pass-through.
		svcs := proxy.SidecarScope.Services()
		for _, s := range svcs {
			// Ignore services that are result of conversion from ServiceEntry.
			if s.Attributes.ServiceRegistry == provider.External {
				continue
			}
			c := serviceentry.ServiceToServiceEntry(s, proxy)
			b, err := config.PilotConfigToResource(c)
			if err != nil {
				log.Warn("Resource error ", err, " ", c.Namespace, "/", c.Name)
				continue
			}
			resp = append(resp, &discovery.Resource{
				Name:     c.Namespace + "/" + c.Name,
				Resource: util.MessageToAny(b),
			})
		}
	}

	return resp, model.DefaultXdsLogDetails, nil
}
