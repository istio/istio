//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package ingress

import (
	"fmt"
	"path"

	"istio.io/istio/galley/pkg/runtime/processing"
	"k8s.io/api/extensions/v1beta1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
)

type gwConverter struct {
	p *processing.StoredProjection
}

var _ processing.Handler = &gwConverter{}

func (g *gwConverter) Handle(event resource.Event) {
	switch event.Kind {
	case resource.Added, resource.Updated:
		e, err := ToGateway(event.Entry)
		if err != nil {
			scope.Errorf("Error transforming ingress: %v", err)
			return
		}
		r, err := resource.ToMcpResource(e)
		if err != nil {
			scope.Errorf("Error wrapping gateway: %v", err)
			return
		}
		g.p.Set(e.ID.FullName, r)
	case resource.Deleted:
		g.p.Remove(toGatewayName(event.Entry.ID.FullName))
	default:
		scope.Errorf("Unrecognized event received: %v", event.Kind)
	}
}

// ToGateway converts from ingress spec to Istio Gateway
func ToGateway(entry resource.Entry) (resource.Entry, error) {
	i, ok := entry.Item.(*v1beta1.IngressSpec)
	if !ok {
		return resource.Entry{}, fmt.Errorf("error converting entry proto to IngressSpec: %v", entry.Item)
	}
	namespace, name := entry.ID.Key.FullName.InterpretAsNamespaceAndName()

	gateway := &v1alpha3.Gateway{
		Selector: model.IstioIngressWorkloadLabels,
	}

	// FIXME this is a temporary hack until all test templates are updated
	//for _, tls := range i.Spec.TLS {
	if len(i.TLS) > 0 {
		tls := i.TLS[0] // FIXME
		// TODO validation when multiple wildcard tls secrets are given
		if len(tls.Hosts) == 0 {
			tls.Hosts = []string{"*"}
		}
		gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
			Port: &v1alpha3.Port{
				Number:   443,
				Protocol: string(model.ProtocolHTTPS),
				Name:     fmt.Sprintf("https-443-i-%s-%s", name, namespace),
			},
			Hosts: tls.Hosts,
			// While we accept multiple certs, we expect them to be mounted in
			// /etc/certs/namespace/secretname/tls.crt|tls.key
			Tls: &v1alpha3.Server_TLSOptions{
				HttpsRedirect: false,
				Mode:          v1alpha3.Server_TLSOptions_SIMPLE,
				// TODO this is no longer valid for the new v2 stuff
				PrivateKey:        path.Join(model.IngressCertsPath, model.IngressKeyFilename),
				ServerCertificate: path.Join(model.IngressCertsPath, model.IngressCertFilename),
				// TODO: make sure this is mounted
				CaCertificates: path.Join(model.IngressCertsPath, model.RootCertFilename),
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
		Port: &v1alpha3.Port{
			Number:   80,
			Protocol: string(model.ProtocolHTTP),
			Name:     fmt.Sprintf("http-80-i-%s-%s", name, namespace),
		},
		Hosts: []string{"*"},
	})

	gw := resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName: toGatewayName(entry.ID.FullName),
				TypeURL:  metadata.Gateway.TypeURL,
			},
			Version: entry.ID.Version,
		},
		Item:     gateway,
		Metadata: resource.Metadata{},
	}

	return gw, nil
}

func toGatewayName(ingressName resource.FullName) resource.FullName {
	_, name := ingressName.InterpretAsNamespaceAndName()
	newName := name + "-" + model.IstioIngressGatewayName
	newNamespace := model.IstioIngressNamespace
	return resource.FullNameFromNamespaceAndName(newNamespace, newName)
}
