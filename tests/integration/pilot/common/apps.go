// +build integ
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

package common

import (
	"strconv"
	"strings"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace namespace.Instance
	// Namespace where external echo app will be deployed
	ExternalNamespace namespace.Instance

	// Ingressgateway instance
	Ingress ingress.Instance

	// Standard echo app to be used by tests
	PodA echo.Instances
	// Standard echo app to be used by tests
	PodB echo.Instances
	// Standard echo app to be used by tests
	PodC echo.Instances
	// Standard echo app with TPROXY interception mode to be used by tests
	PodTproxy echo.Instances
	// Headless echo app to be used by tests
	Headless echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	Naked echo.Instances
	// A virtual machine echo app (only deployed to one cluster)
	VM echo.Instances

	// Echo app to be used by tests, with no sidecar injected
	External echo.Instances

	All echo.Instances
}

const (
	PodASvc      = "a"
	PodBSvc      = "b"
	PodCSvc      = "c"
	PodTproxySvc = "tproxy"
	VMSvc        = "vm"
	HeadlessSvc  = "headless"
	NakedSvc     = "naked"
	ExternalSvc  = "external"

	externalHostname = "fake.external.com"
)

var EchoPorts = []echo.Port{
	{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, InstancePort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, InstancePort: 17070},
	{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, InstancePort: 19090},
	{Name: "https", Protocol: protocol.HTTPS, ServicePort: 443, InstancePort: 18443, TLS: true},
	{Name: "tcp-server", Protocol: protocol.TCP, ServicePort: 9091, InstancePort: 16060, ServerFirst: true},
	{Name: "auto-tcp", Protocol: protocol.TCP, ServicePort: 9092, InstancePort: 19091},
	{Name: "auto-tcp-server", Protocol: protocol.TCP, ServicePort: 9093, InstancePort: 16061, ServerFirst: true},
	{Name: "auto-http", Protocol: protocol.HTTP, ServicePort: 81, InstancePort: 18081},
	{Name: "auto-grpc", Protocol: protocol.GRPC, ServicePort: 7071, InstancePort: 17071},
	{Name: "auto-https", Protocol: protocol.HTTPS, ServicePort: 9443, InstancePort: 19443},
	{Name: "http-instance", Protocol: protocol.HTTP, ServicePort: 82, InstancePort: 18082, InstanceIP: true},
	{Name: "http-localhost", Protocol: protocol.HTTP, ServicePort: 84, InstancePort: 18084, LocalhostIP: true},
}

var WorkloadPorts = []echo.WorkloadPort{
	{Protocol: protocol.TCP, Port: 19092},
	{Protocol: protocol.HTTP, Port: 18083},
}

func FindPortByName(name string) echo.Port {
	for _, p := range EchoPorts {
		if p.Name == name {
			return p
		}
	}
	return echo.Port{}
}

func serviceEntryPorts() []echo.Port {
	res := []echo.Port{}
	for _, p := range EchoPorts {
		if strings.HasPrefix(p.Name, "auto") {
			// The protocol needs to be set in EchoPorts to configure the echo deployment
			// But for service entry, we want to ensure we set it to "" which will use sniffing
			p.Protocol = ""
		}
		res = append(res, p)
	}
	return res
}

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.ExternalNamespace, err = namespace.New(t, namespace.Config{
		Prefix: "external",
		Inject: false,
	})
	if err != nil {
		return err
	}

	apps.Ingress = i.IngressFor(t.Clusters().Default())

	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(EchoPorts))
	for i, p := range EchoPorts {
		p.ServicePort = p.InstancePort
		headlessPorts[i] = p
	}
	builder := echoboot.NewBuilder(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:           PodASvc,
			Namespace:         apps.Namespace,
			Ports:             EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			Locality:          "region.zone.subzone",
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           PodBSvc,
			Namespace:         apps.Namespace,
			Ports:             EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           PodCSvc,
			Namespace:         apps.Namespace,
			Ports:             EchoPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           HeadlessSvc,
			Headless:          true,
			Namespace:         apps.Namespace,
			Ports:             headlessPorts,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:   NakedSvc,
			Namespace: apps.Namespace,
			Ports:     EchoPorts,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           ExternalSvc,
			Namespace:         apps.ExternalNamespace,
			DefaultHostHeader: externalHostname,
			Ports:             EchoPorts,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false),
						},
					},
				},
			},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:   PodTproxySvc,
			Namespace: apps.Namespace,
			Ports:     EchoPorts,
			Subsets: []echo.SubsetConfig{{
				Annotations: echo.NewAnnotations().Set(echo.SidecarInterceptionMode, "TPROXY"),
			}},
			WorkloadOnlyPorts: WorkloadPorts,
		}).
		WithConfig(echo.Config{
			Service:           VMSvc,
			Namespace:         apps.Namespace,
			Ports:             EchoPorts,
			DeployAsVM:        true,
			AutoRegisterVM:    true,
			Subsets:           []echo.SubsetConfig{{}},
			WorkloadOnlyPorts: WorkloadPorts,
		})

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.PodA = echos.Match(echo.Service(PodASvc))
	apps.PodB = echos.Match(echo.Service(PodBSvc))
	apps.PodC = echos.Match(echo.Service(PodCSvc))
	apps.PodTproxy = echos.Match(echo.Service(PodTproxySvc))
	apps.Headless = echos.Match(echo.Service(HeadlessSvc))
	apps.Naked = echos.Match(echo.Service(NakedSvc))
	apps.External = echos.Match(echo.Service(ExternalSvc))
	if !t.Settings().SkipVM {
		apps.VM = echos.Match(echo.Service(VMSvc))
	}

	if err := t.Config().ApplyYAML(apps.Namespace.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
`); err != nil {
		return err
	}

	se, err := tmpl.Evaluate(`apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  hosts:
  - {{.Hostname}}
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: external.{{.Namespace}}.svc.cluster.local
  ports:
  - name: http-tls-origination
    number: 8888
    protocol: http
    targetPort: 443
  - name: http2-tls-origination
    number: 8882
    protocol: http2
    targetPort: 443
{{- range $i, $p := .Ports }}
  - name: {{$p.Name}}
    number: {{$p.ServicePort}}
    protocol: "{{$p.Protocol}}"
{{- end }}
`, map[string]interface{}{"Namespace": apps.ExternalNamespace.Name(), "Hostname": externalHostname, "Ports": serviceEntryPorts()})
	if err != nil {
		return err
	}
	if err := t.Config().ApplyYAML(apps.Namespace.Name(), se); err != nil {
		return err
	}
	return nil
}

func (d EchoDeployments) IsMulticluster() bool {
	return d.All.Clusters().IsMulticluster()
}
