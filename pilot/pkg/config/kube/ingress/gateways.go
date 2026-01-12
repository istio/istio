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

package ingress

import (
	"fmt"

	knetworking "k8s.io/api/networking/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

func Gateways(
	ingress krt.Collection[*knetworking.Ingress],
	meshConfig meshwatcher.WatcherCollection,
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[config.Config] {
	return krt.NewCollection(
		ingress,
		func(ctx krt.HandlerContext, i *knetworking.Ingress) *config.Config {
			meshConfig := krt.FetchOne(ctx, meshConfig.AsCollection())
			cfg := convertIngressV1alpha3(*i, meshConfig.MeshConfig, domainSuffix)
			return &cfg
		},
		opts.WithName("DerivedVirtualServices")...,
	)
}

// convertIngressV1alpha3 converts from ingress spec to Istio Gateway
func convertIngressV1alpha3(ingress knetworking.Ingress, mesh *meshconfig.MeshConfig, domainSuffix string) config.Config {
	gateway := &networking.Gateway{}
	gateway.Selector = getIngressGatewaySelector(mesh.IngressSelector, mesh.IngressService)

	for i, tls := range ingress.Spec.TLS {
		if tls.SecretName == "" {
			log.Infof("invalid ingress rule %s:%s for hosts %q, no secretName defined", ingress.Namespace, ingress.Name, tls.Hosts)
			continue
		}
		// TODO validation when multiple wildcard tls secrets are given
		if len(tls.Hosts) == 0 {
			tls.Hosts = []string{"*"}
		}
		gateway.Servers = append(gateway.Servers, &networking.Server{
			Port: &networking.Port{
				Number:   443,
				Protocol: string(protocol.HTTPS),
				Name:     fmt.Sprintf("https-443-ingress-%s-%s-%d", ingress.Name, ingress.Namespace, i),
			},
			Hosts: tls.Hosts,
			Tls: &networking.ServerTLSSettings{
				HttpsRedirect:  false,
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: tls.SecretName,
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &networking.Server{
		Port: &networking.Port{
			Number:   80,
			Protocol: string(protocol.HTTP),
			Name:     fmt.Sprintf("http-80-ingress-%s-%s", ingress.Name, ingress.Namespace),
		},
		Hosts: []string{"*"},
	})

	gatewayConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Gateway,
			Name:             ingress.Name + "-" + constants.IstioIngressGatewayName + "-" + ingress.Namespace,
			Namespace:        IngressNamespace,
			Domain:           domainSuffix,
		},
		Spec: gateway,
	}

	return gatewayConfig
}

func getIngressGatewaySelector(ingressSelector, ingressService string) map[string]string {
	// Setup the selector for the gateway
	if ingressSelector != "" {
		// If explicitly defined, use this one
		return labels.Instance{constants.IstioLabel: ingressSelector}
	} else if ingressService != "istio-ingressgateway" && ingressService != "" {
		// Otherwise, we will use the ingress service as the default. It is common for the selector and service
		// to be the same, so this removes the need for two configurations
		// However, if its istio-ingressgateway we need to use the old values for backwards compatibility
		return labels.Instance{constants.IstioLabel: ingressService}
	}
	// If we have neither an explicitly defined ingressSelector or ingressService then use a selector
	// pointing to the ingressgateway from the default installation
	return labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue}
}
