// Copyright 2018 Istio Authors
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

package genbinding

import (
	kube_v1 "k8s.io/api/core/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

// ConvertBindingsAndExposures2 converts desired multicluster state into Kubernetes and Istio state
func CreateBinding(service string, clusters []string, subset string) ([]model.Config, []kube_v1.Service, error) { // nolint: lll
	istioConfig, k8sSvcs, err := dummyBinding() // TODO
	return istioConfig, k8sSvcs, err
}

func dummyBinding() ([]model.Config, []kube_v1.Service, error) {
	return []model.Config{
		model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.DestinationRule.Type,
				Group:     model.DestinationRule.Group + model.IstioAPIGroupDomain,
				Version:   model.DestinationRule.Version,
				Name:      "dest-rule-dummy",
				Namespace: "default",
			},
			Spec: &v1alpha3.DestinationRule{
				Host: "dummy",
				TrafficPolicy: &v1alpha3.TrafficPolicy{
					Tls: &v1alpha3.TLSSettings{
						Mode:              v1alpha3.TLSSettings_MUTUAL,
						ClientCertificate: "/etc/certs/cert-chain.pem",
						PrivateKey:        "/etc/certs/key.pem",
						CaCertificates:    "/etc/certs/root-cert.pem",
						Sni:               "dummy-sni.default.svc.cluster.local",
					},
				},
			},
		}}, nil, nil
}
