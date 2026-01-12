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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

var (
	port9999 = []*Port{
		{
			Name:     "uds",
			Port:     9999,
			Protocol: "HTTP",
		},
	}

	port7000 = []*Port{
		{
			Name:     "uds",
			Port:     7000,
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

	port744x = []*Port{
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

	port803x = []*Port{
		{
			Port:     8031,
			Protocol: "TCP",
			Name:     "tcp-1",
		},
		{
			Port:     8032,
			Protocol: "TCP",
			Name:     "tcp-2",
		},
		{
			Port:     8033,
			Protocol: "TCP",
			Name:     "tcp-3",
		},
		{
			Port:     8034,
			Protocol: "TCP",
			Name:     "tcp-4",
		},
		{
			Port:     8035,
			Protocol: "TCP",
			Name:     "tcp-5",
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
				{
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar.foo"},
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
					Port: &networking.SidecarPort{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar.foo", "ns2/foo.svc.cluster.local"},
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
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.SidecarPort{
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
					Port: &networking.SidecarPort{
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

	configs21 = &config.Config{
		Meta: config.Meta{
			Name: "virtual-service-destinations-matching-http-virtual-service-ports",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"foo/virtualbar.foo"},
				},
			},
		},
	}

	configs22 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-multiple-ports",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Number:   8031,
						Protocol: "TCP",
						Name:     "tcp-ipc1",
					},
					Hosts: []string{"*/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8032,
						Protocol: "TCP",
						Name:     "tcp-ipc2",
					},
					Hosts: []string{"*/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8033,
						Protocol: "TCP",
						Name:     "tcp-ipc3",
					},
					Hosts: []string{"*/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8034,
						Protocol: "TCP",
						Name:     "tcp-ipc4",
					},
					Hosts: []string{"*/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8035,
						Protocol: "TCP",
						Name:     "tcp-ipc5",
					},
					Hosts: []string{"*/foobar.svc.cluster.local"},
				},
			},
		},
	}

	configs23 = &config.Config{
		Meta: config.Meta{
			Name: "sidecar-scope-with-multiple-ports",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.SidecarPort{
						Number:   8031,
						Protocol: "TCP",
						Name:     "tcp-ipc1",
					},
					Hosts: []string{"ns1/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8032,
						Protocol: "TCP",
						Name:     "tcp-ipc2",
					},
					Hosts: []string{"ns1/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8033,
						Protocol: "TCP",
						Name:     "tcp-ipc3",
					},
					Hosts: []string{"ns1/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8034,
						Protocol: "TCP",
						Name:     "tcp-ipc4",
					},
					Hosts: []string{"ns1/foobar.svc.cluster.local"},
				},
				{
					Port: &networking.SidecarPort{
						Number:   8035,
						Protocol: "TCP",
						Name:     "tcp-ipc5",
					},
					Hosts: []string{"ns1/foobar.svc.cluster.local"},
				},
			},
		},
	}

	configs24 = &config.Config{
		Meta: config.Meta{
			Name: "virtual-service-destinations-matching-http-source-namespace",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"foo/virtualbar.foo"},
				},
			},
		},
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
				ExportTo:  sets.New(visibility.Private),
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

	services21 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services22 = []*Service{
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services23 = []*Service{
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port803x,
			Attributes: ServiceAttributes{
				Name:            "foo",
				Namespace:       "ns1",
				ServiceRegistry: provider.Kubernetes,
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:            "foo",
				Namespace:       "ns2",
				ServiceRegistry: provider.Kubernetes,
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:            "baz",
				Namespace:       "ns3",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}
	services24 = []*Service{
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port803x,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns1", // same ns as foo
			},
		},
	}
	services25 = []*Service{
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port803x,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:            "baz",
				Namespace:       "ns3",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}

	services26 = []*Service{
		{
			Hostname: "foobar.svc.cluster.local",
			Ports:    port803x,
			Attributes: ServiceAttributes{
				Name:            "foo",
				Namespace:       "ns1",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}

	services27 = []*Service{
		{
			Hostname: "baz.svc.cluster.local",
			Attributes: ServiceAttributes{
				Name:            "baz",
				Namespace:       "ns1",
				ServiceRegistry: provider.Kubernetes,
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Attributes: ServiceAttributes{
				Name:            "foo",
				Namespace:       "ns1",
				ServiceRegistry: provider.Kubernetes,
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Attributes: ServiceAttributes{
				Name:            "bar",
				Namespace:       "ns2",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}

	virtualServices1 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
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
				GroupVersionKind: gvk.VirtualService,
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

	virtualServices3 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "baz.svc.cluster.local", Port: &networking.PortSelector{Number: 7000},
								},
							},
						},
						Mirror: &networking.Destination{Host: "foo.svc.cluster.local", Port: &networking.PortSelector{Number: 7000}},
					},
				},
			},
		},
	}

	virtualServices4 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Tcp: []*networking.TCPRoute{
					{
						Route: []*networking.RouteDestination{
							{
								Destination: &networking.Destination{
									Host: "baz.svc.cluster.local", Port: &networking.PortSelector{Number: 7000},
								},
							},
						},
					},
				},
			},
		},
	}

	virtualServices5 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Tls: []*networking.TLSRoute{
					{
						Route: []*networking.RouteDestination{
							{
								Destination: &networking.Destination{
									Host: "baz.svc.cluster.local", Port: &networking.PortSelector{Number: 7000},
								},
							},
						},
					},
				},
			},
		},
	}

	virtualServices6 = []config.Config{
		{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								SourceNamespace: "mynamespace",
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "baz.svc.cluster.local",
								},
							},
						},
					},
					{
						Match: []*networking.HTTPMatchRequest{
							{
								SourceNamespace: "foo",
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "foo.svc.cluster.local",
								},
							},
						},
					},
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "bar.svc.cluster.local",
								},
							},
						},
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
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
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
		expectedServices []*Service
		expectedDr       *config.Config
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
			"no-sidecar-config-not-merge-service-in-diff-namespaces",
			nil,
			services23,
			nil,
			[]*Service{services23[2]},
			nil,
		},
		{
			"no-sidecar-config-merge-service-ports",
			nil,
			services24,
			nil,
			[]*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					// We merge the 'foo' in ns1 with the 'baz' ports also in ns1
					Ports: append(slices.Clone(port7443), port803x...),
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns1",
					},
				},
			},
			nil,
		},
		{
			"no-sidecar-config-k8s-service-take-precedence",
			nil,
			services25,
			nil,
			[]*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:            "baz",
						Namespace:       "ns3",
						ServiceRegistry: provider.Kubernetes,
					},
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
			services3,
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
			services2,
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
			[]*Service{services5[0]},
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
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "foo",
					},
				},
			},
			nil,
		},
		{
			"sidecar-with-egress-port-merges-service-ports",
			configs6,
			services6,
			nil,
			services6,
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
					Ports:    twoPorts,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "foo",
					},
				},
			},
			nil,
		},
		{
			"two-egresslisteners-one-with-port-and-without-port",
			configs7,
			services8,
			nil,
			services8,
			nil,
		},
		// Validates when service is scoped to Sidecar, it uses service port rather than listener port.
		{
			"service-port-used-while-cloning",
			configs8,
			services9,
			nil,
			services9,
			nil,
		},
		{
			"wild-card-egress-listener-match",
			configs9,
			services10,
			nil,
			[]*Service{
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
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
					Hostname: "bar.svc.cluster.local",
					Ports:    twoMatchingPorts,
					Attributes: ServiceAttributes{
						Name:      "bar",
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
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,

					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
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
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
			},
			nil,
		},
		{
			"virtual-service-destinations-matching-http-virtual-service-ports",
			configs21,
			services21,
			virtualServices3,
			[]*Service{
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
			},
			nil,
		},
		{
			"virtual-service-destinations-matching-tcp-virtual-service-ports",
			configs21,
			services21,
			virtualServices4,
			[]*Service{
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
			},
			nil,
		},
		{
			"virtual-service-destinations-matching-tls-virtual-service-ports",
			configs21,
			services21,
			virtualServices5,
			[]*Service{
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					// Ports should not be merged even though virtual service will select the service with 7443
					// as ns1 comes before ns2, because 8000 was already picked explicitly and is in different namespace
					Ports: port8000,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns2", // Pick the service with 8000
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port8000,
					// Service is defined in multiple namespaces, but we prefer the local one
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "mynamespace",
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
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
			},
			nil,
		},
		{
			"virtual-service-bad-host",
			configs11,
			services9,
			virtualServices1,
			services9,
			nil,
		},
		{
			"virtual-service-destination-port-missing-from-service",
			configs21,
			services22,
			virtualServices3,
			[]*Service{},
			nil,
		},
		{
			"virtual-service-2-match-service",
			configs11,
			services18,
			virtualServices2,
			[]*Service{
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "*",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "*",
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "*",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "*",
					},
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
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "*",
					},
				},
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "*",
					},
				},
			},
			nil,
		},
		{
			"virtual-service-6-match-source-namespace",
			configs24,
			services27,
			virtualServices6,
			[]*Service{
				{
					Hostname: "bar.svc.cluster.local",
					Attributes: ServiceAttributes{
						Name:            "bar",
						Namespace:       "ns2",
						ServiceRegistry: provider.Kubernetes,
					},
				},
				{
					Hostname: "baz.svc.cluster.local",
					Attributes: ServiceAttributes{
						Name:            "baz",
						Namespace:       "ns1",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
			nil,
		},
		{
			"sidecar-scope-with-illegal-host",
			configs13,
			services14,
			nil,
			services14,
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
					Attributes: ServiceAttributes{
						Name:      "en.wikipedia.org",
						Namespace: "ns1",
					},
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
					Hostname: "*.wikipedia.org",
					Attributes: ServiceAttributes{
						Name:      "*.wikipedia.org",
						Namespace: "ns1",
					},
				},
				{
					Hostname: "en.wikipedia.org",
					Attributes: ServiceAttributes{
						Name:      "en.wikipedia.org",
						Namespace: "ns1",
					},
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
		{
			name: "multi-service-merge",
			sidecarConfig: &config.Config{
				Meta: config.Meta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: &networking.Sidecar{},
			},
			services: []*Service{
				{
					Hostname: "proxy",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:      "s1",
						Namespace: "default",
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "s2",
						Namespace: "default",
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:      "s3",
						Namespace: "default",
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "proxy",
					Ports:    PortList{port7000[0], port7443[0], port7442[0]},
					Attributes: ServiceAttributes{
						Name:      "s1",
						Namespace: "default",
					},
				},
			},
		},
		{
			name: "k8s service take precedence over external service",
			sidecarConfig: &config.Config{
				Meta: config.Meta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: &networking.Sidecar{},
			},
			services: []*Service{
				{
					Hostname: "proxy",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:            "s1",
						Namespace:       "default",
						ServiceRegistry: provider.External,
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:            "s2",
						Namespace:       "default",
						ServiceRegistry: provider.External,
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:            "s3",
						Namespace:       "default",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "proxy",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:            "s3",
						Namespace:       "default",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
		},
		{
			name: "k8s service take precedence over external service, but not over k8s service",
			sidecarConfig: &config.Config{
				Meta: config.Meta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: &networking.Sidecar{},
			},
			services: []*Service{
				{
					Hostname: "proxy",
					Ports:    port7000,
					Attributes: ServiceAttributes{
						Name:            "s1",
						Namespace:       "default",
						ServiceRegistry: provider.External,
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:            "s2",
						Namespace:       "default",
						ServiceRegistry: provider.Kubernetes,
					},
				},
				{
					Hostname: "proxy",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:            "s3",
						Namespace:       "default",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "proxy",
					Ports:    port744x,
					Attributes: ServiceAttributes{
						Name:            "s2",
						Namespace:       "default",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
		},
		{
			name:          "multi-port-merge",
			sidecarConfig: configs22,
			services:      services23,
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:            "foo",
						Namespace:       "ns1",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
		},
		{
			name:          "multi-port-merge-in-same-namespace",
			sidecarConfig: configs23,
			services:      services26,
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:            "foo",
						Namespace:       "ns1",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
		},
		{
			name:          "multi-port-merge: serviceentry not merge with another namespace",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[3:],
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:          "multi-port-merge: k8s service take precedence",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:            "bar",
						Namespace:       "ns2",
						ServiceRegistry: provider.Kubernetes,
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:            "bar",
						Namespace:       "ns2",
						ServiceRegistry: provider.Kubernetes,
					},
				},
			},
		},
		{
			name:          "multi-port-merge: serviceentry merge",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[3:],
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns1",
					},
				},
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port7443,
					Attributes: ServiceAttributes{
						Name:      "baz",
						Namespace: "ns3",
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x,
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:          "serviceentry not merge when resolution is different",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
				{
					Hostname:   "foobar.svc.cluster.local",
					Ports:      port803x[3:],
					Resolution: DNSLB,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns1",
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:          "serviceentry not merge when label selector is different",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
						LabelSelectors: map[string]string{
							"app": "foo",
						},
					},
				},
				{
					Hostname:   "foobar.svc.cluster.local",
					Ports:      port803x[3:],
					Resolution: DNSLB,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns1",
						LabelSelectors: map[string]string{
							"app": "bar",
						},
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
						LabelSelectors: map[string]string{
							"app": "foo",
						},
					},
				},
			},
		},
		{
			name:          "serviceentry not merge when exportTo is different",
			sidecarConfig: configs22,
			services: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
						ExportTo:  sets.New(visibility.Public),
					},
				},
				{
					Hostname:   "foobar.svc.cluster.local",
					Ports:      port803x[3:],
					Resolution: DNSLB,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns1",
						ExportTo:  sets.New(visibility.Private),
					},
				},
			},
			expectedServices: []*Service{
				{
					Hostname: "foobar.svc.cluster.local",
					Ports:    port803x[:3],
					Attributes: ServiceAttributes{
						Name:      "foo",
						Namespace: "ns1",
						ExportTo:  sets.New(visibility.Public),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			env := NewEnvironment()
			env.Watcher = meshwatcher.NewTestWatcher(meshConfig)
			ps.Mesh = env.Mesh()

			env.ServiceDiscovery = &localServiceDiscovery{services: tt.services}
			ps.initDefaultExportMaps()
			ps.initServiceRegistry(env, nil)
			ps.setDestinationRules([]config.Config{destinationRule1, destinationRule2, destinationRule3, nonWorkloadSelectorDr})
			configStore := NewFakeStore()
			for _, c := range tt.virtualServices {
				if _, err := configStore.Create(c); err != nil {
					t.Fatalf("could not create %v", c.Name)
				}
			}
			env.ConfigStore = configStore
			ps.initVirtualServices(env)
			sidecarConfig := tt.sidecarConfig
			configuredListeneres := 1
			if sidecarConfig != nil {
				r := sidecarConfig.Spec.(*networking.Sidecar)
				if len(r.Egress) > 0 {
					configuredListeneres = len(r.Egress)
				}
			}

			sidecarScope := convertToSidecarScope(ps, sidecarConfig, "mynamespace")
			if features.EnableLazySidecarEvaluation {
				sidecarScope.initFunc()
			}

			numberListeners := len(sidecarScope.EgressListeners)
			if numberListeners != configuredListeneres {
				t.Errorf("Expected %d listeners, Got: %d", configuredListeneres, numberListeners)
			}

			if tt.virtualServices != nil {
				// VirtualService ordering is unstable. This is acceptable because its never selecting multiple services for the same
				// hostname, which is where ordering is critical
				slices.SortBy(sidecarScope.services, func(a *Service) string {
					return a.Attributes.Name
				})
			}
			assert.Equal(t, tt.expectedServices, sidecarScope.services)
			if tt.sidecarConfig != nil {
				dr := sidecarScope.DestinationRule(TrafficDirectionOutbound,
					&Proxy{
						Labels:          tt.sidecarConfig.Labels,
						Metadata:        &NodeMetadata{Labels: tt.sidecarConfig.Labels},
						ConfigNamespace: tt.sidecarConfig.Namespace,
					}, host.Name("httpbin.org")).GetRule()
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

	serviceBWildcard := &Service{
		Hostname:   "*.test.wildcard.com",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	allServices := []*Service{serviceA8000, serviceA9000, serviceAalt, serviceB8000, serviceB9000, serviceBalt}

	tests := []struct {
		name          string
		listenerHosts map[string]hostClassification
		services      []*Service
		expected      []*Service
		namespace     string
	}{
		{
			name: "*/* imports only those in a",
			listenerHosts: map[string]hostClassification{
				wildcardNamespace: {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  allServices,
			expected:  []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace: "a",
		},
		{
			name: "*/* will bias towards configNamespace",
			listenerHosts: map[string]hostClassification{
				wildcardNamespace: {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  []*Service{serviceB8000, serviceB9000, serviceBalt, serviceA8000, serviceA9000, serviceAalt},
			expected:  []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace: "a",
		},
		{
			name: "a/* imports only those in a",
			listenerHosts: map[string]hostClassification{
				"a": {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  allServices,
			expected:  []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace: "a",
		},
		{
			name: "b/*, b/* imports only those in b",
			listenerHosts: map[string]hostClassification{
				"b": {allHosts: []host.Name{wildcardService, wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  allServices,
			expected:  []*Service{serviceB8000, serviceB9000, serviceBalt},
			namespace: "a",
		},
		{
			name: "*/alt imports alt in namespace a",
			listenerHosts: map[string]hostClassification{
				wildcardNamespace: {allHosts: []host.Name{"alt"}, exactHosts: sets.New[host.Name]("alt")},
			},
			services:  allServices,
			expected:  []*Service{serviceAalt},
			namespace: "a",
		},
		{
			name: "b/alt imports alt in a namespaces",
			listenerHosts: map[string]hostClassification{
				"b": {allHosts: []host.Name{"alt"}, exactHosts: sets.New[host.Name]("alt")},
			},
			services:  allServices,
			expected:  []*Service{serviceBalt},
			namespace: "a",
		},
		{
			name: "b/* imports doesn't import in namespace a with proxy in a",
			listenerHosts: map[string]hostClassification{
				"b": {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  []*Service{serviceA8000},
			expected:  []*Service{},
			namespace: "a",
		},
		{
			name: "multiple hosts selected same service",
			listenerHosts: map[string]hostClassification{
				"a": {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
				"*": {allHosts: []host.Name{wildcardService}, exactHosts: sets.New[host.Name]()},
			},
			services:  []*Service{serviceA8000},
			expected:  []*Service{serviceA8000},
			namespace: "a",
		},
		{
			name: "fall back to wildcard namespace",
			listenerHosts: map[string]hostClassification{
				wildcardNamespace: {allHosts: []host.Name{"host"}, exactHosts: sets.New[host.Name]("host")},
				"a":               {allHosts: []host.Name{"alt"}, exactHosts: sets.New[host.Name]("alt")},
			},
			services:  allServices,
			expected:  []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace: "a",
		},
		{
			name: "service is wildcard, but not listener's subset",
			listenerHosts: map[string]hostClassification{
				"b": {allHosts: []host.Name{"wildcard.com"}, exactHosts: sets.New[host.Name]("wildcard.com")},
			},
			services:  []*Service{serviceBWildcard},
			expected:  []*Service{},
			namespace: "b",
		},
		{
			name: "service is wildcard",
			listenerHosts: map[string]hostClassification{
				"b": {allHosts: []host.Name{"*.wildcard.com"}, exactHosts: sets.New[host.Name]()},
			},
			services:  []*Service{serviceBWildcard},
			expected:  []*Service{serviceBWildcard},
			namespace: "b",
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
			{kind.ServiceEntry, svcName, ns}:   contains,
			{kind.VirtualService, vsName, ns}:  contains,
			{kind.DestinationRule, drName, ns}: contains,
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
			{kind.AuthorizationPolicy, "authz", "default"}: true,
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
			ps.setDestinationRules(destinationRules)
			sidecarScope := convertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = convertToSidecarScope(ps, nil, "default")
			}
			if features.EnableLazySidecarEvaluation {
				sidecarScope.initFunc()
			}

			for k, v := range tt.contains {
				if ok := sidecarScope.DependsOnConfig(k, ps.Mesh.RootNamespace); ok != v {
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
		{"AuthorizationPolicy in the same ns as workload", []string{"*/*"}, map[ConfigKey]bool{
			{kind.AuthorizationPolicy, "authz", "default"}: true,
		}},
		{"AuthorizationPolicy in a different ns from workload", []string{"*/*"}, map[ConfigKey]bool{
			{kind.AuthorizationPolicy, "authz", "ns1"}: false,
		}},
		{"AuthorizationPolicy in the root namespace", []string{"*/*"}, map[ConfigKey]bool{
			{kind.AuthorizationPolicy, "authz", constants.IstioSystemNamespace}: true,
		}},
		{"WasmPlugin in same ns as workload", []string{"*/*"}, map[ConfigKey]bool{
			{kind.WasmPlugin, "wasm", "default"}: true,
		}},
		{"WasmPlugin in different ns from workload", []string{"*/*"}, map[ConfigKey]bool{
			{kind.WasmPlugin, "wasm", "ns1"}: false,
		}},
		{"WasmPlugin in the root namespace", []string{"*/*"}, map[ConfigKey]bool{
			{kind.WasmPlugin, "wasm", constants.IstioSystemNamespace}: true,
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
			sidecarScope := convertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = convertToSidecarScope(ps, nil, "default")
			}
			if features.EnableLazySidecarEvaluation {
				sidecarScope.initFunc()
			}

			for k, v := range tt.contains {
				if ok := sidecarScope.DependsOnConfig(k, ps.Mesh.RootNamespace); ok != v {
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
				sidecarScope = convertToSidecarScope(ps, nil, "not-default")
			} else {
				sidecarScope = convertToSidecarScope(ps, test.sidecar, test.sidecar.Namespace)
			}

			if features.EnableLazySidecarEvaluation {
				sidecarScope.initFunc()
			}

			if !reflect.DeepEqual(test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy) {
				t.Errorf("Unexpected sidecar outbound traffic, want %v, found %v",
					test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy)
			}
		})
	}
}

func TestInboundConnectionPoolForPort(t *testing.T) {
	connectionPoolSettings := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests:  1024,
			Http2MaxRequests:         1024,
			MaxRequestsPerConnection: 1024,
			MaxRetries:               1024,
			IdleTimeout:              durationpb.New(5 * time.Second),
			H2UpgradePolicy:          networking.ConnectionPoolSettings_HTTPSettings_UPGRADE,
		},
		Tcp: &networking.ConnectionPoolSettings_TCPSettings{
			MaxConnections: 1024,
			ConnectTimeout: durationpb.New(6 * time.Second),
			TcpKeepalive: &networking.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
				Probes:   3,
				Time:     durationpb.New(7 * time.Second),
				Interval: durationpb.New(8 * time.Second),
			},
			MaxConnectionDuration: durationpb.New(9 * time.Second),
		},
	}

	overrideConnectionPool := &networking.ConnectionPoolSettings{
		Http: &networking.ConnectionPoolSettings_HTTPSettings{
			Http1MaxPendingRequests:  1,
			Http2MaxRequests:         2,
			MaxRequestsPerConnection: 3,
			MaxRetries:               4,
			IdleTimeout:              durationpb.New(1 * time.Second),
			H2UpgradePolicy:          networking.ConnectionPoolSettings_HTTPSettings_DO_NOT_UPGRADE,
		},
	}

	tests := map[string]struct {
		sidecar *networking.Sidecar
		// port to settings map
		want map[int]*networking.ConnectionPoolSettings
	}{
		"no settings": {
			sidecar: &networking.Sidecar{},
			want: map[int]*networking.ConnectionPoolSettings{
				22:  nil,
				80:  nil,
				443: nil,
			},
		},
		"no settings multiple ports": {
			sidecar: &networking.Sidecar{
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
					},
					{
						Port: &networking.SidecarPort{
							Number:   443,
							Protocol: "HTTPS",
							Name:     "https",
						},
					},
				},
			},
			want: map[int]*networking.ConnectionPoolSettings{
				22:  nil,
				80:  nil,
				443: nil,
			},
		},
		"single port with settings": {
			sidecar: &networking.Sidecar{
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						ConnectionPool: connectionPoolSettings,
					},
				},
			},
			want: map[int]*networking.ConnectionPoolSettings{
				22:  nil,
				80:  connectionPoolSettings,
				443: nil,
			},
		},
		"top level settings": {
			sidecar: &networking.Sidecar{
				InboundConnectionPool: connectionPoolSettings,
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
					},
				},
			},
			want: map[int]*networking.ConnectionPoolSettings{
				// with a default setting on the sidecar, we'll return it for any port we're asked about
				22:  connectionPoolSettings,
				80:  connectionPoolSettings,
				443: connectionPoolSettings,
			},
		},
		"port settings override top level": {
			sidecar: &networking.Sidecar{
				InboundConnectionPool: connectionPoolSettings,
				Ingress: []*networking.IstioIngressListener{
					{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						ConnectionPool: overrideConnectionPool,
					},
				},
			},
			want: map[int]*networking.ConnectionPoolSettings{
				// with a default setting on the sidecar, we'll return it for any port we're asked about
				22:  connectionPoolSettings,
				80:  overrideConnectionPool,
				443: connectionPoolSettings,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ps := NewPushContext()
			ps.Mesh = mesh.DefaultMeshConfig()

			sidecar := &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.Sidecar,
					Name:             "sidecar",
					Namespace:        strings.Replace(name, " ", "-", -1),
				},
				Spec: tt.sidecar,
			}
			scope := convertToSidecarScope(ps, sidecar, sidecar.Namespace)
			if features.EnableLazySidecarEvaluation {
				scope.initFunc()
			}

			for port, expected := range tt.want {
				actual := scope.InboundConnectionPoolForPort(port)
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("for port %d, wanted %#v but got: %#v", port, expected, actual)
				}
			}
		})
	}
}

func BenchmarkConvertIstioListenerToWrapper(b *testing.B) {
	b.Run("small-exact", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 10, 3, "", false)
	})
	b.Run("small-wildcard", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 10, 3, "*.", false)
	})
	b.Run("small-match-all", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 10, 3, "", true)
	})

	b.Run("middle-exact", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 100, 10, "", false)
	})
	b.Run("middle-wildcard", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 100, 10, "*.", false)
	})
	b.Run("middle-match-all", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 100, 10, "", true)
	})

	b.Run("big-exact", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 300, 15, "", false)
	})
	b.Run("big-wildcard", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 300, 15, "*.", false)
	})
	b.Run("big-match-all", func(b *testing.B) {
		benchmarkConvertIstioListenerToWrapper(b, 300, 15, "", true)
	})
}

func benchmarkConvertIstioListenerToWrapper(b *testing.B, vsNum int, hostNum int, wildcard string, matchAll bool) {
	// virtual service
	cfgs := make([]config.Config, 0)
	for i := 0; i < vsNum; i++ {
		cfgs = append(cfgs, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "vs-name-" + strconv.Itoa(i),
				Namespace:        "default",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"host-" + strconv.Itoa(i) + ".com"},
			},
		})
	}
	ps := NewPushContext()
	ps.virtualServiceIndex.publicByGateway[constants.IstioMeshGateway] = cfgs

	// service
	svcList := make([]*Service, 0, vsNum)
	for i := 0; i < vsNum; i++ {
		svcList = append(svcList, &Service{
			Attributes: ServiceAttributes{Namespace: "default"},
			Hostname:   host.Name("host-" + strconv.Itoa(i) + ".com"),
		})
	}
	ps.ServiceIndex.public = svcList

	hosts := make([]string, 0)
	if matchAll {
		// default/*
		hosts = append(hosts, "default/*")
	} else {
		// default/xx or default/*.xx
		for i := 0; i < hostNum; i++ {
			h := "default/" + wildcard + "host-" + strconv.Itoa(i) + ".com"
			hosts = append(hosts, h)
		}
	}

	istioListener := &networking.IstioEgressListener{
		Hosts: hosts,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		convertIstioListenerToWrapper(ps, "default", istioListener)
	}
}

func TestComputeWildcardHostVirtualServiceIndex(t *testing.T) {
	oldestTime := time.Now().Add(-2 * time.Hour)
	olderTime := time.Now().Add(-1 * time.Hour)
	newerTime := time.Now()
	virtualServices := []config.Config{
		{
			Meta: config.Meta{
				Name:              "foo",
				Namespace:         "default",
				CreationTimestamp: newerTime,
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"foo.example.com"},
			},
		},
		{
			Meta: config.Meta{
				Name:              "foo2",
				Namespace:         "default",
				CreationTimestamp: olderTime.Add(30 * time.Minute), // This should not be used despite being older than foo
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"foo.example.com"},
			},
		},
		{
			Meta: config.Meta{
				Name:              "wild",
				Namespace:         "default",
				CreationTimestamp: olderTime,
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"*.example.com"},
			},
		},
		{
			Meta: config.Meta{
				Name:              "barwild",
				Namespace:         "default",
				CreationTimestamp: oldestTime,
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"*.bar.example.com"},
			},
		},
		{
			Meta: config.Meta{
				Name:              "barwild2",
				Namespace:         "default",
				CreationTimestamp: olderTime,
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"*.bar.example.com"},
			},
		},
	}

	services := []*Service{
		{
			Hostname: "foo.example.com",
		},
		{
			Hostname: "baz.example.com",
		},
		{
			Hostname: "qux.bar.example.com",
		},
		{
			Hostname: "*.bar.example.com",
		},
	}

	tests := []struct {
		name            string
		virtualServices []config.Config
		services        []*Service
		expectedIndex   map[host.Name]types.NamespacedName
	}{
		{
			name:            "most specific",
			virtualServices: virtualServices,
			services:        services,
			expectedIndex: map[host.Name]types.NamespacedName{
				"foo.example.com":     {Name: "foo", Namespace: "default"},
				"baz.example.com":     {Name: "wild", Namespace: "default"},
				"qux.bar.example.com": {Name: "barwild", Namespace: "default"},
				"*.bar.example.com":   {Name: "barwild", Namespace: "default"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := computeWildcardHostVirtualServiceIndex(tt.virtualServices, tt.services)
			if !reflect.DeepEqual(tt.expectedIndex, index) {
				t.Errorf("Expected index %v, got %v", tt.expectedIndex, index)
			}
		})
	}
}
