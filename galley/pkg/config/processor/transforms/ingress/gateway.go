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
	"path"

	ingress "k8s.io/api/extensions/v1beta1"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/synthesize"
)

type gatewayXform struct {
	*event.FnTransform

	options processing.ProcessorOptions
}

var _ event.Transformer = &gatewayXform{}

func getGatewayXformProvider() transformer.Provider {
	inputs := collection.Names{metadata.K8SExtensionsV1Beta1Ingresses}
	outputs := collection.Names{metadata.IstioNetworkingV1Alpha3Gateways}

	createFn := func(o processing.ProcessorOptions) event.Transformer {
		xform := &gatewayXform{}
		xform.FnTransform = event.NewFnTransform(
			inputs,
			outputs,
			nil, nil,
			xform.handle)
		xform.options = o

		return xform
	}
	return transformer.NewProvider(inputs, outputs, createFn)
}

func (g *gatewayXform) handle(e event.Event, h event.Handler) {

	if g.options.MeshConfig.IngressControllerMode == meshconfig.MeshConfig_OFF {
		// short circuit and return
		return
	}

	switch e.Kind {
	case event.Added, event.Updated:
		if !shouldProcessIngress(g.options.MeshConfig, e.Entry) {
			return
		}

		gw := g.convertIngressToGateway(e.Entry)
		evt := event.Event{
			Kind:   e.Kind,
			Source: metadata.IstioNetworkingV1Alpha3Gateways,
			Entry:  gw,
		}
		h.Handle(evt)

	case event.Deleted:
		gw := g.convertIngressToGateway(e.Entry)
		evt := event.Event{
			Kind:   e.Kind,
			Source: metadata.IstioNetworkingV1Alpha3Gateways,
			Entry:  gw,
		}
		evt.Entry.Metadata.Name = generateSyntheticGatewayName(e.Entry.Metadata.Name)
		evt.Entry.Metadata.Version = generateSyntheticVersion(e.Entry.Metadata.Version)

		h.Handle(evt)

	default:
		panic(fmt.Errorf("gatewayXform.handle: unknown event: %v", e))
	}
}

func (g *gatewayXform) convertIngressToGateway(e *resource.Entry) *resource.Entry {
	namespace, name := e.Metadata.Name.InterpretAsNamespaceAndName()

	var gateway *v1alpha3.Gateway
	if e.Item != nil {
		i := e.Item.(*ingress.IngressSpec)

		gateway = &v1alpha3.Gateway{
			Selector: IstioIngressWorkloadLabels,
		}

		// FIXME this is ingressAdapter temporary hack until all test templates are updated
		// for _, tls := range i.Spec.TLS {
		if len(i.TLS) > 0 {
			tls := i.TLS[0] // FIXME
			// TODO validation when multiple wildcard tls secrets are given
			if len(tls.Hosts) == 0 {
				tls.Hosts = []string{"*"}
			}
			gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   443,
					Protocol: https,
					Name:     fmt.Sprintf("https-443-i-%s-%s", name, namespace),
				},
				Hosts: tls.Hosts,
				// While we accept multiple certs, we expect them to be mounted in
				// /etc/certs/namespace/secretname/tls.crt|tls.key
				Tls: &v1alpha3.Server_TLSOptions{
					HttpsRedirect: false,
					Mode:          v1alpha3.Server_TLSOptions_SIMPLE,
					// TODO this is no longer valid for the new v2 stuff
					PrivateKey:        path.Join(IngressCertsPath, IngressKeyFilename),
					ServerCertificate: path.Join(IngressCertsPath, IngressCertFilename),
					// TODO: make sure this is mounted
					CaCertificates: path.Join(IngressCertsPath, RootCertFilename),
				},
			})
		}

		gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
			Port: &v1alpha3.Port{
				Number:   80,
				Protocol: http,
				Name:     fmt.Sprintf("http-80-i-%s-%s", name, namespace),
			},
			Hosts: []string{"*"},
		})
	}

	ann := e.Metadata.Annotations.Clone()
	ann.Delete(annotation.IoKubernetesIngressClass.Name)

	gw := &resource.Entry{
		Metadata: resource.Metadata{
			Name:        generateSyntheticGatewayName(e.Metadata.Name),
			Version:     generateSyntheticVersion(e.Metadata.Version),
			CreateTime:  e.Metadata.CreateTime,
			Annotations: ann,
			Labels:      e.Metadata.Labels,
		},
		Item:   gateway,
		Origin: e.Origin,
	}

	return gw
}

func generateSyntheticGatewayName(name resource.Name) resource.Name {
	_, n := name.InterpretAsNamespaceAndName()
	newName := n + "-" + IstioIngressGatewayName
	newNamespace := IstioIngressNamespace

	return resource.NewName(newNamespace, newName)
}

func generateSyntheticVersion(v resource.Version) resource.Version {
	return synthesize.Version("ing", v)
}
