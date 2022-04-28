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

package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	port9999 = []*Port{
		{
			Name:     "uds",
			Port:     9999,
			Protocol: "HTTP",
		},
	}

	port7443 = []*Port{
		{
			Port:     7443,
			Protocol: "GRPC",
			Name:     "service-grpc-tls",
		},
	}

	port7442 = []*Port{
		{
			Port:     7442,
			Protocol: "HTTP",
			Name:     "http-tls",
		},
	}

	twoMatchingPorts = []*Port{
		{
			Port:     7443,
			Protocol: "GRPC",
			Name:     "service-grpc-tls",
		},
		{
			Port:     7442,
			Protocol: "HTTP",
			Name:     "http-tls",
		},
	}

	port8000 = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
	}

	port9000 = []*Port{
		{
			Name: "port1",
			Port: 9000,
		},
	}

	twoPorts = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
		{
			Name:     "uds",
			Port:     7000,
			Protocol: "HTTP",
		},
	}

	allPorts = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
		{
			Name:     "uds",
			Port:     7000,
			Protocol: "HTTP",
		},
		{
			Name: "port1",
			Port: 9000,
		},
	}

	configs1 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	configs2 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{},
	}

	configs3 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"foo/bar", "*/*"},
				},
			},
		},
	}

	configs4 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs5 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs6 = &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
				{
					Port: &networking.Port{
						Number:   7000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs7 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-ns1-ns2",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   23145,
						Protocol: "TCP",
						Name:     "outbound-tcp",
					},
					Bind: "7.7.7.7",
					Hosts: []string{
						"*/bookinginfo.com",
						"*/private.com",
					},
				},
				{
					Hosts: []string{
						"ns1/*",
						"*/*.tcp.com",
					},
				},
			},
		},
	}

	configs8 = &config.Config{
		Meta: config.Meta{
			Name: "different-port-name",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "listener-grpc-tls",
					},
					Hosts: []string{"mesh/*"},
				},
			},
		},
	}

	configs9 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-wildcards",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   7442,
						Protocol: "HTTP",
						Name:     "http-tls",
					},
					Hosts: []string{"ns2/*"},
				},
			},
		},
	}

	configs10 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-http-proxy",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	configs11 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-http-proxy-match-virtual-service",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar"},
				},
			},
		},
	}

	configs12 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-http-proxy-match-virtual-service-and-service",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar", "ns2/foo.svc.cluster.local"},
				},
			},
		},
	}

	configs13 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-illegal-host",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo", "foo/bar"},
				},
			},
		},
	}

	configs14 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-wildcards",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   7442,
						Protocol: "HTTP",
						Name:     "http-tls",
					},
					Hosts: []string{"ns2/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	configs15 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-virtual-service",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	configs16 = &config.Config{
		Meta: config.Meta{
			Name:      "sidecar-scope-with-specific-host",
			Namespace: "ns1",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/en.wikipedia.org"},
				},
			},
		},
	}

	configs17 = &config.Config{
		Meta: config.Meta{
			Name:      "sidecar-scope-with-wildcard-host",
			Namespace: "ns1",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*.wikipedia.org"},
				},
			},
		},
	}

	configs18 = &config.Config{
		Meta: config.Meta{
			Name:      "sidecar-scope-with-workloadselector-specific-dr-match",
			Namespace: "mynamespace",
			Labels:    map[string]string{"app": "app2"},
		},
		Spec: &networking.Sidecar{},
	}

	configs19 = &config.Config{
		Meta: config.Meta{
			Name:      "sidecar-scope-with-workloadselector-specific-dr-no-match",
			Namespace: "mynamespace",
			Labels:    map[string]string{"app": "app5"},
		},
		Spec: &networking.Sidecar{},
	}

	configs20 = &config.Config{
		Meta: config.Meta{
			Name:      "sidecar-scope-with-same-workloadselector-label-drs-merged",
			Namespace: "mynamespace",
			Labels:    map[string]string{"app": "app1"},
		},
		Spec: &networking.Sidecar{},
	}

	services1 = []*Service{
		{
			Hostname: "bar",
		},
	}

	services2 = []*Service{
		{
			Hostname: "bar",
			Ports:    port8000,
		},
		{
			Hostname: "barprime",
		},
	}

	services3 = []*Service{
		{
			Hostname: "bar",
			Ports:    port9000,
		},
		{
			Hostname: "barprime",
		},
	}

	services4 = []*Service{
		{
			Hostname: "bar",
		},
		{
			Hostname: "barprime",
		},
	}

	services5 = []*Service{
		{
			Hostname: "bar",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
		{
			Hostname: "barprime",
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "foo",
			},
		},
	}

	services6 = []*Service{
		{
			Hostname: "bar",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
	}

	services7 = []*Service{
		{
			Hostname: "bar",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
		{
			Hostname: "barprime",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "foo",
			},
		},
		{
			Hostname: "foo",
			Ports:    allPorts,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "foo",
			},
		},
	}

	services8 = []*Service{
		{
			Hostname: "bookinginfo.com",
			Ports:    port9999,
			Attributes: ServiceAttributes{
				Name:      "bookinginfo.com",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "private.com",
			Attributes: ServiceAttributes{
				Name:      "private.com",
				Namespace: "ns1",
			},
		},
	}

	services9 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "mesh",
			},
		},
	}

	services10 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
	}

	services11 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Ports:    twoMatchingPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
	}

	services12 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services13 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "mynamespace",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services14 = []*Service{
		{
			Hostname: "bar",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
	}

	services15 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
				ExportTo:  map[visibility.Instance]bool{visibility.Private: true},
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services16 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "random.svc.cluster.local",
			Ports:    port9999,
			Attributes: ServiceAttributes{
				Name:      "random",
				Namespace: "randomns", // nolint
			},
		},
	}

	services17 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services18 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "*",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "*",
			},
		},
	}

	services19 = []*Service{
		{
			Hostname: "en.wikipedia.org",
			Attributes: ServiceAttributes{
				Name:      "en.wikipedia.org",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "*.wikipedia.org",
			Attributes: ServiceAttributes{
				Name:      "*.wikipedia.org",
				Namespace: "ns1",
			},
		},
	}

	services20 = []*Service{
		{
			Hostname: "httpbin.org",
			Attributes: ServiceAttributes{
				Namespace: "mynamespace",
			},
		},
	}

	virtualServices1 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Http: []*networking.HTTPRoute{
					{
						Mirror: &networking.Destination{Host: "foo.svc.cluster.local"},
						Route:  []*networking.HTTPRouteDestination{{Destination: &networking.Destination{Host: "baz.svc.cluster.local"}}},
					},
				},
			},
		},
	}

	virtualServices2 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar", "*"},
				Http: []*networking.HTTPRoute{
					{
						Mirror: &networking.Destination{Host: "foo.svc.cluster.local"},
						Route:  []*networking.HTTPRouteDestination{{Destination: &networking.Destination{Host: "baz.svc.cluster.local"}}},
					},
				},
			},
		},
	}
	destinationRule1 = config.Config{
		Meta: config.Meta{
			Name:      "drRule1",
			Namespace: "mynamespace",
		},
		Spec: &networking.DestinationRule{
			Host: "httpbin.org",
			WorkloadSelector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app1"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
			},
		},
	}
	destinationRule2 = config.Config{
		Meta: config.Meta{
			Name:      "drRule2",
			Namespace: "mynamespace",
		},
		Spec: &networking.DestinationRule{
			Host: "httpbin.org",
			WorkloadSelector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app2"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrappers.UInt32Value{Value: 3},
				},
			},
		},
	}
	mergedDr1and3 = config.Config{
		Meta: config.Meta{
			Name:      "drRule1",
			Namespace: "mynamespace",
		},
		Spec: &networking.DestinationRule{
			Host: "httpbin.org",
			WorkloadSelector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app1"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
			},
			Subsets: []*networking.Subset{
				{
					Name: "subset1",
				},
				{
					Name: "subset2",
				},
			},
		},
	}
	destinationRule3 = config.Config{
		Meta: config.Meta{
			Name:      "drRule3",
			Namespace: "mynamespace",
		},
		Spec: &networking.DestinationRule{
			Host: "httpbin.org",
			WorkloadSelector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app1"},
			},
			Subsets: []*networking.Subset{
				{
					Name: "subset1",
				},
				{
					Name: "subset2",
				},
			},
		},
	}
	nonWorkloadSelectorDr = config.Config{
		Meta: config.Meta{
			Name:      "drRule3",
			Namespace: "mynamespace",
		},
		Spec: &networking.DestinationRule{
			Host: "httpbin.org",
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
			},
		},
	}
)

func TestCreateSidecarScope(t *testing.T) {
	tests := []struct {
		name          string
		sidecarConfig *config.Config
		// list of available service for a given proxy
		services        []*Service
		virtualServices []config.Config
		// list of services expected to be in the listener
		excpectedServices []*Service
		expectedDr        *config.Config
	}{
		{
			"no-sidecar-config",
			nil,
			nil,
			nil,
			nil,
			nil,
		},
		{
			"no-sidecar-config-with-service",
			nil,
			services1,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
			},
			nil,
		},
		{
			"sidecar-with-multiple-egress",
			configs1,
			nil,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-with-service",
			configs1,
			services1,
			nil,

			[]*Service{
				{
					Hostname: "bar",
				},
			},
			nil,
		},
		{
			"sidecar-with-multiple-egress-with-service-on-same-port",
			configs1,
			services3,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
			nil,
		},
		{
			"sidecar-with-multiple-egress-with-multiple-service",
			configs1,
			services4,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
			nil,
		},
		{
			"sidecar-with-zero-egress",
			configs2,
			nil,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-zero-egress-multiple-service",
			configs2,
			services4,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport",
			configs3,
			nil,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport-with-specific-service",
			configs3,
			services2,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport-with-services",
			configs3,
			services4,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
			nil,
		},
		{
			"sidecar-with-egress-port-match-with-services-with-and-without-port",
			configs4,
			services5,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
			},
			nil,
		},
		{
			"sidecar-with-egress-port-trims-service-non-matching-ports",
			configs5,
			services6,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    port8000,
				},
			},
			nil,
		},
		{
			"sidecar-with-egress-port-merges-service-ports",
			configs6,
			services6,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    twoPorts,
				},
			},
			nil,
		},
		{
			"sidecar-with-egress-port-trims-and-merges-service-ports",
			configs6,
			services7,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    twoPorts,
				},
				{
					Hostname: "barprime",
					Ports:    port8000,
				},
				{
					Hostname: "foo",
					Ports:    twoPorts,
				},
			},
			nil,
		},
		{
			"two-egresslisteners-one-with-port-and-without-port",
			configs7,
			services8,
			nil,
			[]*Service{
				{
					Hostname: "bookinginfo.com",
					Ports:    port9999,
				},
				{
					Hostname: "private.com",
				},
			},
			nil,
		},
		// Validates when service is scoped to Sidecar, it uses service port rather than listener port.
		{
			"service-port-used-while-cloning",
			configs8,
			services9,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"wild-card-egress-listener-match",
			configs9,
			services10,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "bar.svc.cluster.local",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
			},
			nil,
		},
		{
			"wild-card-egress-listener-match-and-all-hosts",
			configs14,
			services16,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "bar.svc.cluster.local",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
				{
					Hostname: "barprime.svc.cluster.local",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:      "barprime",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "random.svc.cluster.local",
					Ports:    port9999,
					Attributes: ServiceAttributes{
						Name:      "random",
						Namespace: "randomns", // nolint
					},
				},
			},
			nil,
		},
		{
			"wild-card-egress-listener-match-with-two-ports",
			configs9,
			services11,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "bar.svc.cluster.local",
					Ports:    twoMatchingPorts,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
			},
			nil,
		},
		{
			"http-proxy-protocol-matches-any-port",
			configs10,
			services7,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
				{
					Hostname: "foo",
				},
			},
			nil,
		},
		{
			"virtual-service",
			configs11,
			services11,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-destinations-matching-ports",
			configs15,
			services17,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-prefer-required",
			configs12,
			services12,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					// Ports should not be merged even though virtual service will select the service with 7443
					// as ns1 comes before ns2, because 8000 was already picked explicitly and is in different namespace
					Ports: port8000,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-prefer-config-namespace",
			configs11,
			services13,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port8000,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-pick-alphabetical",
			configs11,
			// Ambiguous; same hostname in ns1 and ns2, neither is config namespace
			// ns1 should always win
			services12,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-pick-public",
			configs11,
			// Ambiguous; same hostname in ns1 and ns2, neither is config namespace
			// ns1 should always win
			services15,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port8000,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-bad-host",
			configs11,
			services9,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-2-match-service",
			configs11,
			services18,
			virtualServices2,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-2-match-service-and-domain",
			configs12,
			services18,
			virtualServices2,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"virtual-service-2-match-all-services",
			configs15,
			services18,
			virtualServices2,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"sidecar-scope-with-illegal-host",
			configs13,
			services14,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    port7443,
				},
			},
			nil,
		},
		{
			"sidecar-scope-with-specific-host",
			configs16,
			services19,
			nil,
			[]*Service{
				{
					Hostname: "en.wikipedia.org",
				},
			},
			nil,
		},
		{
			"sidecar-scope-with-wildcard-host",
			configs17,
			services19,
			nil,
			[]*Service{
				{
					Hostname: "en.wikipedia.org",
				},
				{
					Hostname: "*.wikipedia.org",
				},
			},
			nil,
		},
		{
			"sidecar-scope-with-matching-workloadselector-dr",
			configs18,
			services20,
			nil,
			[]*Service{
				{
					Hostname: "httpbin.org",
					Attributes: ServiceAttributes{
						Namespace: "mynamespace",
					},
				},
			},
			&destinationRule2,
		},
		{
			"sidecar-scope-with-non-matching-workloadselector-dr",
			configs19,
			services20,
			nil,
			[]*Service{
				{
					Hostname: "httpbin.org",
					Attributes: ServiceAttributes{
						Namespace: "mynamespace",
					},
				},
			},
			&nonWorkloadSelectorDr,
		},
		{
			"sidecar-scope-same-workloadselector-labels-drs-should-be-merged",
			configs20,
			services20,
			nil,
			[]*Service{
				{
					Hostname: "httpbin.org",
					Attributes: ServiceAttributes{
						Namespace: "mynamespace",
					},
				},
			},
			&mergedDr1and3,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			var found bool
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			ps.Mesh = meshConfig
			ps.SetDestinationRules([]config.Config{destinationRule1, destinationRule2, destinationRule3, nonWorkloadSelectorDr})
			if tt.services != nil {
				ps.ServiceIndex.public = append(ps.ServiceIndex.public, tt.services...)

				for _, s := range tt.services {
					if _, f := ps.ServiceIndex.HostnameAndNamespace[s.Hostname]; !f {
						ps.ServiceIndex.HostnameAndNamespace[s.Hostname] = map[string]*Service{}
					}
					ps.ServiceIndex.HostnameAndNamespace[s.Hostname][s.Attributes.Namespace] = s
				}
			}
			if tt.virtualServices != nil {
				// nolint lll
				ps.virtualServiceIndex.publicByGateway[constants.IstioMeshGateway] = append(ps.virtualServiceIndex.publicByGateway[constants.IstioMeshGateway], tt.virtualServices...)
			}

			ps.exportToDefaults = exportToDefaults{
				virtualService:  map[visibility.Instance]bool{visibility.Public: true},
				service:         map[visibility.Instance]bool{visibility.Public: true},
				destinationRule: map[visibility.Instance]bool{visibility.Public: true},
			}

			sidecarConfig := tt.sidecarConfig
			sidecarScope := ConvertToSidecarScope(ps, sidecarConfig, "mynamespace")
			configuredListeneres := 1
			if sidecarConfig != nil {
				r := sidecarConfig.Spec.(*networking.Sidecar)
				if len(r.Egress) > 0 {
					configuredListeneres = len(r.Egress)
				}
			}

			numberListeners := len(sidecarScope.EgressListeners)
			if numberListeners != configuredListeneres {
				t.Errorf("Expected %d listeners, Got: %d", configuredListeneres, numberListeners)
			}

			for _, s1 := range sidecarScope.services {
				found = false
				for _, s2 := range tt.excpectedServices {
					if s1.Hostname == s2.Hostname {
						if len(s2.Ports) > 0 {
							if reflect.DeepEqual(s2.Ports, s1.Ports) {
								found = true
								break
							}
						} else {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("Expected service %v in SidecarScope but not found", s1.Hostname)
				}
			}

			for _, s1 := range tt.excpectedServices {
				found = false
				for _, s2 := range sidecarScope.services {
					if s1.Hostname == s2.Hostname {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("UnExpected service %v in SidecarScope", s1.Hostname)
				}
			}

			if tt.sidecarConfig != nil {
				dr := sidecarScope.DestinationRule(TrafficDirectionOutbound,
					&Proxy{
						Metadata:        &NodeMetadata{Labels: tt.sidecarConfig.Labels},
						ConfigNamespace: tt.sidecarConfig.Namespace,
					}, host.Name("httpbin.org"))
				assert.Equal(t, dr, tt.expectedDr)
			}
		})
	}
}

func TestIstioEgressListenerWrapper(t *testing.T) {
	serviceA8000 := &Service{
		Hostname:   "host",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}
	serviceA9000 := &Service{
		Hostname:   "host",
		Ports:      port9000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}
	serviceAalt := &Service{
		Hostname:   "alt",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}

	serviceB8000 := &Service{
		Hostname:   "host",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	serviceB9000 := &Service{
		Hostname:   "host",
		Ports:      port9000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	serviceBalt := &Service{
		Hostname:   "alt",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	allServices := []*Service{serviceA8000, serviceA9000, serviceAalt, serviceB8000, serviceB9000, serviceBalt}

	tests := []struct {
		name          string
		listenerHosts map[string][]host.Name
		services      []*Service
		expected      []*Service
		namespace     string
	}{
		{
			name:          "*/* imports only those in a",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "*/* will bias towards configNamespace",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {wildcardService}},
			services:      []*Service{serviceB8000, serviceB9000, serviceBalt, serviceA8000, serviceA9000, serviceAalt},
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "a/* imports only those in a",
			listenerHosts: map[string][]host.Name{"a": {wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "b/*, b/* imports only those in b",
			listenerHosts: map[string][]host.Name{"b": {wildcardService, wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceB8000, serviceB9000, serviceBalt},
			namespace:     "a",
		},
		{
			name:          "*/alt imports alt in namespace a",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {"alt"}},
			services:      allServices,
			expected:      []*Service{serviceAalt},
			namespace:     "a",
		},
		{
			name:          "b/alt imports alt in a namespaces",
			listenerHosts: map[string][]host.Name{"b": {"alt"}},
			services:      allServices,
			expected:      []*Service{serviceBalt},
			namespace:     "a",
		},
		{
			name:          "b/* imports doesn't import in namespace a with proxy in a",
			listenerHosts: map[string][]host.Name{"b": {wildcardService}},
			services:      []*Service{serviceA8000},
			expected:      []*Service{},
			namespace:     "a",
		},
		{
			name:          "multiple hosts selected same service",
			listenerHosts: map[string][]host.Name{"a": {wildcardService}, "*": {wildcardService}},
			services:      []*Service{serviceA8000},
			expected:      []*Service{serviceA8000},
			namespace:     "a",
		},
		{
			name:          "fall back to wildcard namespace",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {"host"}, "a": {"alt"}},
			services:      allServices,
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ilw := &IstioEgressListenerWrapper{}
			got := ilw.selectServices(tt.services, tt.namespace, tt.listenerHosts)
			if !reflect.DeepEqual(got, tt.expected) {
				gots, _ := json.MarshalIndent(got, "", "  ")
				expecteds, _ := json.MarshalIndent(tt.expected, "", "  ")
				t.Errorf("Got %v, expected %v", string(gots), string(expecteds))
			}
		})
	}
}

func TestContainsEgressDependencies(t *testing.T) {
	const (
		svcName = "svc1.com"
		nsName  = "ns"
		drName  = "dr1"
		vsName  = "vs1"
	)

	allContains := func(ns string, contains bool) map[ConfigKey]bool {
		return map[ConfigKey]bool{
			{gvk.ServiceEntry, svcName, ns}:   contains,
			{gvk.VirtualService, vsName, ns}:  contains,
			{gvk.DestinationRule, drName, ns}: contains,
		}
	}

	cases := []struct {
		name   string
		egress []string

		contains map[ConfigKey]bool
	}{
		{"Just wildcard", []string{"*/*"}, allContains(nsName, true)},
		{"Namespace and wildcard", []string{"ns/*", "*/*"}, allContains(nsName, true)},
		{"Just Namespace", []string{"ns/*"}, allContains(nsName, true)},
		{"Wrong Namespace", []string{"ns/*"}, allContains("other-ns", false)},
		{"No Sidecar", nil, allContains("ns", true)},
		{"No Sidecar Other Namespace", nil, allContains("other-ns", false)},
		{"clusterScope resource", []string{"*/*"}, map[ConfigKey]bool{
			{gvk.AuthorizationPolicy, "authz", "default"}: true,
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: &networking.Sidecar{
					Egress: []*networking.IstioEgressListener{
						{
							Hosts: tt.egress,
						},
					},
				},
			}
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			ps.Mesh = meshConfig

			services := []*Service{
				{
					Hostname:   "nomatch",
					Attributes: ServiceAttributes{Namespace: "nomatch"},
				},
				{
					Hostname:   svcName,
					Attributes: ServiceAttributes{Namespace: nsName},
				},
			}
			virtualServices := []config.Config{
				{
					Meta: config.Meta{
						Name:      vsName,
						Namespace: nsName,
					},
					Spec: &networking.VirtualService{
						Hosts: []string{svcName},
					},
				},
			}
			destinationRules := []config.Config{
				{
					Meta: config.Meta{
						Name:      drName,
						Namespace: nsName,
					},
					Spec: &networking.DestinationRule{
						Host:     svcName,
						ExportTo: []string{"*"},
					},
				},
			}
			ps.ServiceIndex.public = append(ps.ServiceIndex.public, services...)
			// nolint lll
			ps.virtualServiceIndex.publicByGateway[constants.IstioMeshGateway] = append(ps.virtualServiceIndex.publicByGateway[constants.IstioMeshGateway], virtualServices...)
			ps.SetDestinationRules(destinationRules)
			sidecarScope := ConvertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "default")
			}

			for k, v := range tt.contains {
				if ok := sidecarScope.DependsOnConfig(k); ok != v {
					t.Fatalf("Expected contains %v-%v, but no match", k, v)
				}
			}
		})
	}
}

func TestRootNsSidecarDependencies(t *testing.T) {
	cases := []struct {
		name   string
		egress []string

		contains map[ConfigKey]bool
	}{
		{"authorizationPolicy in same ns with workload", []string{"*/*"}, map[ConfigKey]bool{
			{gvk.AuthorizationPolicy, "authz", "default"}: true,
		}},
		{"authorizationPolicy in different ns with workload", []string{"*/*"}, map[ConfigKey]bool{
			{gvk.AuthorizationPolicy, "authz", "ns1"}: false,
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: constants.IstioSystemNamespace,
				},
				Spec: &networking.Sidecar{
					Egress: []*networking.IstioEgressListener{
						{
							Hosts: tt.egress,
						},
					},
				},
			}
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			ps.Mesh = meshConfig
			sidecarScope := ConvertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "default")
			}

			for k, v := range tt.contains {
				if ok := sidecarScope.DependsOnConfig(k); ok != v {
					t.Fatalf("Expected contains %v-%v, but no match", k, v)
				}
			}
		})
	}
}

func TestSidecarOutboundTrafficPolicy(t *testing.T) {
	configWithoutOutboundTrafficPolicy := &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{},
	}
	configRegistryOnly := &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
	}
	configAllowAny := &config.Config{
		Meta: config.Meta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	meshConfigWithRegistryOnly, err := mesh.ApplyMeshConfigDefaults(`
outboundTrafficPolicy: 
  mode: REGISTRY_ONLY
`)
	if err != nil {
		t.Fatalf("unexpected error reading test mesh config: %v", err)
	}

	tests := []struct {
		name                  string
		meshConfig            *v1alpha1.MeshConfig
		sidecar               *config.Config
		outboundTrafficPolicy *networking.OutboundTrafficPolicy
	}{
		{
			name:       "default MeshConfig, no Sidecar",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    nil,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "default MeshConfig, sidecar without OutboundTrafficPolicy",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configWithoutOutboundTrafficPolicy,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "default MeshConfig, Sidecar with registry only",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configRegistryOnly,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "default MeshConfig, Sidecar with allow any",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configAllowAny,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "MeshConfig registry only, no Sidecar",
			meshConfig: meshConfigWithRegistryOnly,
			sidecar:    nil,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, sidecar without OutboundTrafficPolicy",
			meshConfig: meshConfigWithRegistryOnly,
			sidecar:    configWithoutOutboundTrafficPolicy,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, Sidecar with registry only",
			meshConfig: meshConfigWithRegistryOnly,
			sidecar:    configRegistryOnly,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, Sidecar with allow any",
			meshConfig: meshConfigWithRegistryOnly,
			sidecar:    configAllowAny,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ps := NewPushContext()
			ps.Mesh = tests[i].meshConfig

			var sidecarScope *SidecarScope
			if test.sidecar == nil {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "not-default")
			} else {
				sidecarScope = ConvertToSidecarScope(ps, test.sidecar, test.sidecar.Namespace)
			}

			if !reflect.DeepEqual(test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy) {
				t.Errorf("Unexpected sidecar outbound traffic, want %v, found %v",
					test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy)
			}
		})
	}
}
