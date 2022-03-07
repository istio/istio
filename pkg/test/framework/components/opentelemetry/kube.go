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

package opentelemetry

import (
	"fmt"
	"net"
	"os"
	"strings"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
)

type otel struct {
	id      resource.ID
	cluster cluster.Cluster
}

const (
	appName         = "opentelemetry-collector"
	remoteOtelEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: otel-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 55678
      name: http-tracing-span
      protocol: HTTP
    hosts:
    - "opentelemetry-collector.{INGRESS_DOMAIN}"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: opentelemetry-collector
  namespace: istio-system
spec:
  hosts:
  - "opentelemetry-collector.{INGRESS_DOMAIN}"
  gateways:
  - otel-gateway
  http:
  - match:
    - port: 55678
    route:
    - destination:
        host: opentelemetry-collector
        port:
          number: 55678
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: opentelemetry-collector
  namespace: istio-system
spec:
  host: opentelemetry-collector
  trafficPolicy:
    tls:
      mode: DISABLE
---`

	extServiceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: opentelemetry-collector
spec:
  hosts:
  # must be of form name.namespace.global
  - opentelemetry-collector.istio-system.global
  # Treat remote cluster services as part of the service mesh
  # as all clusters in the service mesh share the same root of trust.
  location: MESH_INTERNAL
  ports:
  - name: http-tracing-span
    number: 55678
    protocol: http
  resolution: DNS
  addresses:
  # the IP address to which opentelemetry-collector.istio-system.global will resolve to
  # must be unique for each remote service, within a given cluster.
  # This address need not be routable. Traffic for this IP will be captured
  # by the sidecar and routed appropriately.
  - 240.0.0.2
  endpoints:
  # This is the routable address of the ingress gateway in cluster1 that
  # sits in front of otel service. Traffic from the sidecar will be
  # routed to this address.
  - address: {INGRESS_DOMAIN}
    ports:
      http-tracing-span: 15443 # Do not change this port value
`
)

func getYaml() (string, error) {
	b, err := os.ReadFile(env.OtelCollectorInstallFilePath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func install(ctx resource.Context, ns string) error {
	y, err := getYaml()
	if err != nil {
		return err
	}
	return ctx.ConfigKube().YAML(y).Apply(ns)
}

func installServiceEntry(ctx resource.Context, ns, ingressAddr string) error {
	// Setup remote access to zipkin in cluster
	yaml := strings.ReplaceAll(remoteOtelEntry, "{INGRESS_DOMAIN}", ingressAddr)
	if err := ctx.ConfigIstio().YAML(yaml).Apply(ns); err != nil {
		return err
	}
	// For all other clusters, add a service entry so that can access
	// zipkin in cluster installed.
	yaml = strings.ReplaceAll(extServiceEntry, "{INGRESS_DOMAIN}", ingressAddr)
	if err := ctx.ConfigIstio().YAML(yaml).Apply(ns); err != nil {
		return err
	}
	return nil
}

func newCollector(ctx resource.Context, c Config) (*otel, error) {
	o := &otel{
		cluster: ctx.Clusters().GetOrDefault(c.Cluster),
	}
	ctx.TrackResource(o)

	istioCfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	ns := istioCfg.TelemetryNamespace
	if err := install(ctx, ns); err != nil {
		return nil, err
	}

	f := testKube.NewSinglePodFetch(o.cluster, ns, fmt.Sprintf("app=%s", appName))
	_, err = testKube.WaitUntilPodsAreReady(f)
	if err != nil {
		return nil, err
	}

	isIP := net.ParseIP(c.IngressAddr).String() != "<nil>"
	ingressDomain := c.IngressAddr
	if isIP {
		ingressDomain = fmt.Sprintf("%s.sslip.io", strings.ReplaceAll(c.IngressAddr, ":", "-"))
	}

	err = installServiceEntry(ctx, istioCfg.TelemetryNamespace, ingressDomain)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (o *otel) ID() resource.ID {
	return o.id
}
