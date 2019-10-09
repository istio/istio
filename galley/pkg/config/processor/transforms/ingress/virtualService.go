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
	"fmt"
	"strings"
	"sync"

	"k8s.io/api/extensions/v1beta1"
	ingress "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/scope"
)

type virtualServiceXform struct {
	*event.FnTransform

	options processing.ProcessorOptions

	mu sync.Mutex

	ingresses map[resource.Name]*resource.Entry
	vsByHost  map[string]*syntheticVirtualService
}

func getVirtualServiceXformProvider() transformer.Provider {
	inputs := collection.Names{metadata.K8SExtensionsV1Beta1Ingresses}
	outputs := collection.Names{metadata.IstioNetworkingV1Alpha3Virtualservices}

	createFn := func(o processing.ProcessorOptions) event.Transformer {
		xform := &virtualServiceXform{
			options: o,
		}
		xform.FnTransform = event.NewFnTransform(
			inputs,
			outputs,
			xform.start,
			xform.stop,
			xform.handle)

		return xform
	}
	return transformer.NewProvider(inputs, outputs, createFn)
}

// Start implements processing.Transformer
func (g *virtualServiceXform) start() {
	g.vsByHost = make(map[string]*syntheticVirtualService)

	g.ingresses = make(map[resource.Name]*resource.Entry)
}

// Stop implements processing.Transformer
func (g *virtualServiceXform) stop() {
	g.vsByHost = nil

	g.ingresses = nil
}

// Handle implements event.Handler
func (g *virtualServiceXform) handle(e event.Event, h event.Handler) {
	if g.options.MeshConfig.IngressControllerMode == meshconfig.MeshConfig_OFF {
		// short circuit and return
		return
	}

	switch e.Kind {
	case event.Added, event.Updated:
		if !shouldProcessIngress(g.options.MeshConfig, e.Entry) {
			scope.Processing.Debugf("virtualServiceXform: Skipping ingress event: %v", e)
			return
		}

		g.processIngress(e.Entry, h)

	case event.Deleted:
		ing, exists := g.ingresses[e.Entry.Metadata.Name]
		if exists {
			g.removeIngress(ing, h)
			delete(g.ingresses, e.Entry.Metadata.Name)
		}

	default:
		panic(fmt.Errorf("virtualServiceXForm.handle: unknown event: %v", e))
	}
}

func (g *virtualServiceXform) processIngress(newIngress *resource.Entry, h event.Handler) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ingresses[newIngress.Metadata.Name] = newIngress

	// Extract the hosts from Ingress and find all relevant Synthetic Virtual Service entries.
	iterateHosts(newIngress, func(host string) {
		svs, exists := g.vsByHost[host]
		if !exists {
			svs = &syntheticVirtualService{
				host: host,
			}
			g.vsByHost[host] = svs
		}

		// Associate the Ingress resource with the Synthetic Virtual Service. This may or may not
		// cause a change in the resource state.
		oldName, oldVersion := svs.attachIngress(newIngress)
		if oldName != svs.name {
			if exists {
				g.notifyDelete(h, oldName, oldVersion)
			}
			g.notifyUpdate(h, event.Added, svs)
		} else {
			if exists {
				g.notifyUpdate(h, event.Updated, svs)
			} else {
				g.notifyUpdate(h, event.Added, svs)
			}
		}
	})

	// It is possible that the ingress may have been removed from a Synthetic Virtual Service. Find and
	// update/remove those
	oldIngress, found := g.ingresses[newIngress.Metadata.Name]
	if found {
		iterateRemovedHosts(oldIngress, newIngress, func(host string) {
			svs := g.vsByHost[host]
			oldName, oldVersion := svs.detachIngress(oldIngress)
			if oldName != svs.name {
				g.notifyDelete(h, oldName, oldVersion)
				if svs.isEmpty() {
					delete(g.vsByHost, host)
				} else {
					g.notifyUpdate(h, event.Added, svs)
				}
			} else {
				if svs.isEmpty() {
					delete(g.vsByHost, host)
					g.notifyDelete(h, oldName, oldVersion)
				} else {
					g.notifyUpdate(h, event.Updated, svs)
				}
			}
		})
	}
}

func (g *virtualServiceXform) removeIngress(oldIngress *resource.Entry, h event.Handler) {
	g.mu.Lock()
	defer g.mu.Unlock()

	iterateRemovedHosts(oldIngress, nil, func(host string) {
		svs := g.vsByHost[host]
		oldName, oldVersion := svs.detachIngress(oldIngress)
		if oldName != svs.name {
			g.notifyDelete(h, oldName, oldVersion)
			if svs.isEmpty() {
				delete(g.vsByHost, host)
			} else {
				g.notifyUpdate(h, event.Added, svs)
			}
		} else {
			if svs.isEmpty() {
				delete(g.vsByHost, host)
				g.notifyDelete(h, oldName, oldVersion)
			} else {
				g.notifyUpdate(h, event.Updated, svs)
			}
		}
	})
}

func iterateHosts(i *resource.Entry, fn func(string)) {
	spec := i.Item.(*v1beta1.IngressSpec)
	for _, r := range spec.Rules {
		host := getHost(&r)
		fn(host)
	}
}

func iterateRemovedHosts(o, n *resource.Entry, fn func(string)) {
	// Use N^2 algorithm, to avoid garbage generation.
loop:
	for _, ro := range o.Item.(*v1beta1.IngressSpec).Rules {
		if n != nil {
			for _, rn := range n.Item.(*v1beta1.IngressSpec).Rules {
				if getHost(&ro) == getHost(&rn) {
					continue loop
				}
			}
		}

		fn(getHost(&ro))
	}
}

func (g *virtualServiceXform) notifyUpdate(h event.Handler, k event.Kind, svs *syntheticVirtualService) {
	e := event.Event{
		Kind:   k,
		Source: metadata.IstioNetworkingV1Alpha3Virtualservices,
		Entry:  svs.generateEntry(g.options.DomainSuffix),
	}
	h.Handle(e)
}

func (g *virtualServiceXform) notifyDelete(h event.Handler, name resource.Name, v resource.Version) {
	e := event.Event{
		Kind:   event.Deleted,
		Source: metadata.IstioNetworkingV1Alpha3Virtualservices,
		Entry: &resource.Entry{
			Metadata: resource.Metadata{
				Name:    name,
				Version: v,
			},
		},
	}
	h.Handle(e)
}

func getHost(r *v1beta1.IngressRule) string {
	host := r.Host
	if host == "" {
		host = "*"
	}
	return host
}

func createStringMatch(s string) *v1alpha3.StringMatch {
	if s == "" {
		return nil
	}

	// Note that this implementation only converts prefix and exact matches, not regexps.

	// Replace e.g. "foo.*" with prefix match
	if strings.HasSuffix(s, ".*") {
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, ".*")},
		}
	}
	if strings.HasSuffix(s, "/*") {
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "/*")},
		}
	}

	// Replace e.g. "foo" with ingressAdapter exact match
	return &v1alpha3.StringMatch{
		MatchType: &v1alpha3.StringMatch_Exact{Exact: s},
	}
}

func ingressBackendToHTTPRoute(backend *ingress.IngressBackend, namespace string, domainSuffix string) *v1alpha3.HTTPRoute {
	if backend == nil {
		return nil
	}

	port := &v1alpha3.PortSelector{}

	if backend.ServicePort.Type == intstr.Int {
		port.Number = uint32(backend.ServicePort.IntVal)
	} else {
		// Port names are not allowed in destination rules.
		return nil
	}

	return &v1alpha3.HTTPRoute{
		Route: []*v1alpha3.HTTPRouteDestination{
			{
				Destination: &v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.%s", backend.ServiceName, namespace, domainSuffix),
					Port: port,
				},
				Weight: 100,
			},
		},
	}
}
