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

package telemetry

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
)

func TestBuildStatPrefix(t *testing.T) {
	tests := []struct {
		name        string
		statPattern string
		host        string
		subsetName  string
		port        *model.Port
		attributes  *model.ServiceAttributes
		want        string
	}{
		{
			"Service only pattern",
			"%SERVICE%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default",
		},
		{
			"Service only pattern from different namespace",
			"%SERVICE%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1",
		},
		{
			"Service with port pattern from different namespace",
			"%SERVICE%.%SERVICE_PORT%",
			"reviews.namespace1.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "namespace1",
			},
			"reviews.namespace1.7443",
		},
		{
			"Service FQDN only pattern",
			"%SERVICE_FQDN%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local",
		},
		{
			"Service With Port pattern",
			"%SERVICE%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_7443",
		},
		{
			"Service With Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc",
		},
		{
			"Service With Port and Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default_grpc-svc_7443",
		},
		{
			"Service FQDN With Port pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_7443",
		},
		{
			"Service FQDN With Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc",
		},
		{
			"Service FQDN With Port and Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Empty Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%.%SUBSET_NAME%.%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.v1.grpc-svc_7443",
		},
		{
			"Service FQDN With Unknown Pattern",
			"%SERVICE_FQDN%.%DUMMY%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "reviews",
				Namespace:       "default",
			},
			"reviews.default.svc.cluster.local.%DUMMY%",
		},
		{
			"Service FQDN without ServiceRegistry property",
			"%SERVICE_FQDN%",
			"reviews.default.svc.cluster.local",
			"v1",
			&model.Port{Name: "grpc-svc", Port: 7443, Protocol: "GRPC"},
			&model.ServiceAttributes{
				Name:      "reviews",
				Namespace: "default",
			},
			"reviews.default.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildStatPrefix(tt.statPattern, tt.host, tt.subsetName, tt.port, tt.attributes)
			if got != tt.want {
				t.Errorf("BuildStatPrefix:: Expected alt statname %s, but got %s", tt.want, got)
			}
		})
	}
}

func TestTraceOperation(t *testing.T) {
	tests := []struct {
		host  string
		port  int
		match string
	}{
		{"localhost", 3000, "localhost:3000/*"},
		{"127.0.0.1", 3000, "127.0.0.1:3000/*"},
		{"::1", 3000, "[::1]:3000/*"},
		{"2001:4860:0:2001::68", 3000, "[2001:4860:0:2001::68]:3000/*"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.host), func(t *testing.T) {
			if got := TraceOperation(tt.host, tt.port); got != tt.match {
				t.Fatalf("got %v wanted %v", got, tt.match)
			}
		})
	}
}

func TestBuildInboundStatPrefix(t *testing.T) {
	tests := []struct {
		name        string
		statPattern string
		tm          FilterChainMetadata
		subset      string
		port        uint32
		portName    string
		want        string
	}{
		{
			"Service only pattern",
			"%SERVICE%",
			FilterChainMetadata{InstanceHostname: "svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default",
		},
		{
			"Service only pattern with empty service name",
			"%SERVICE%",
			FilterChainMetadata{InstanceHostname: "svc.cluster.local"},
			"",
			7443,
			"grpc-svc",
			"svc.cluster.local",
		},
		{
			"Service only pattern from different namespace",
			"%SERVICE%",
			FilterChainMetadata{InstanceHostname: "reviews.namespace1.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "namespace1"},
			"",
			7443,
			"grpc-svc",
			"reviews.namespace1",
		},
		{
			"Service with port pattern from different namespace",
			"%SERVICE%.%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.namespace1.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "namespace1"},
			"",
			7443,
			"grpc-svc",
			"reviews.namespace1.7443",
		},
		{
			"Service FQDN only pattern",
			"%SERVICE_FQDN%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local",
		},
		{
			"Service With Port pattern",
			"%SERVICE%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default_7443",
		},
		{
			"Service With Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default_grpc-svc",
		},
		{
			"Service With Port and Port Name pattern",
			"%SERVICE%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default_grpc-svc_7443",
		},
		{
			"Service FQDN With Port pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local_7443",
		},
		{
			"Service FQDN With Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local_grpc-svc",
		},
		{
			"Service FQDN With Port and Port Name pattern",
			"%SERVICE_FQDN%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Empty Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%%SUBSET_NAME%_%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local_grpc-svc_7443",
		},
		{
			"Service FQDN With Subset, Port and Port Name pattern",
			"%SERVICE_FQDN%.%SUBSET_NAME%.%SERVICE_PORT_NAME%_%SERVICE_PORT%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"v1",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local.v1.grpc-svc_7443",
		},
		{
			"Service FQDN With Unknown Pattern",
			"%SERVICE_FQDN%.%DUMMY%",
			FilterChainMetadata{InstanceHostname: "reviews.default.svc.cluster.local", KubernetesServiceName: "reviews", KubernetesServiceNamespace: "default"},
			"",
			7443,
			"grpc-svc",
			"reviews.default.svc.cluster.local.%DUMMY%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildInboundStatPrefix(tt.statPattern, tt.tm, tt.subset, tt.port, tt.portName)
			if got != tt.want {
				t.Errorf("BuildInboundStatPrefix:: Expected alt statname %s, but got %s", tt.want, got)
			}
		})
	}
}
