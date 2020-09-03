package common

import (
	"fmt"
	"strconv"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace namespace.Instance
	// Namespace where external echo app will be deployed
	ExternalNamespace namespace.Instance

	// Standard echo app to be used by tests
	PodA echo.Instances
	// Standard echo app to be used by tests
	PodB echo.Instances
	// Standard echo app to be used by tests
	PodC echo.Instances
	// Headless echo app to be used by tests
	Headless echo.Instances
	// Echo app to be used by tests, with no sidecar injected
	Naked echo.Instances
	// A virtual machine echo app (only deployed to one cluster)
	VM echo.Instances

	// Echo app to be used by tests, with no sidecar injected
	External echo.Instances
	// Fake hostname of external service
	ExternalHost string

	All echo.Instances
}

const (
	PodASvc     = "a"
	PodBSvc     = "b"
	PodCSvc     = "c"
	VMSvc       = "vm"
	HeadlessSvc = "headless"
	NakedSvc    = "naked"
	ExternalSvc = "external"
)

var EchoPorts = []echo.Port{
	{Name: "http", Protocol: protocol.HTTP, InstancePort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, InstancePort: 17070},
	{Name: "tcp", Protocol: protocol.TCP, InstancePort: 19090},
	{Name: "tcp-server", Protocol: protocol.TCP, InstancePort: 16060, ServerFirst: true},
	{Name: "auto-tcp", Protocol: protocol.TCP, InstancePort: 19091},
	{Name: "auto-tcp-server", Protocol: protocol.TCP, InstancePort: 16061, ServerFirst: true},
	{Name: "auto-http", Protocol: protocol.HTTP, InstancePort: 18081},
	{Name: "auto-grpc", Protocol: protocol.GRPC, InstancePort: 17071},
}

func SetupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.ExternalNamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "external",
		Inject: false,
	})
	if err != nil {
		return err
	}
	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(EchoPorts))
	for i, p := range EchoPorts {
		p.ServicePort = p.InstancePort
		headlessPorts[i] = p
	}
	builder := echoboot.NewBuilder(ctx)
	for _, c := range ctx.Environment().Clusters() {
		builder.
			With(nil, echo.Config{
				Service:   PodASvc,
				Namespace: apps.Namespace,
				Ports:     EchoPorts,
				Subsets:   []echo.SubsetConfig{{}},
				Locality:  "region.zone.subzone",
				Cluster:   c,
			}).
			With(nil, echo.Config{
				Service:   PodBSvc,
				Namespace: apps.Namespace,
				Ports:     EchoPorts,
				Subsets:   []echo.SubsetConfig{{}},
				Cluster:   c,
			}).
			With(nil, echo.Config{
				Service:   PodCSvc,
				Namespace: apps.Namespace,
				Ports:     EchoPorts,
				Subsets:   []echo.SubsetConfig{{}},
				Cluster:   c,
			}).
			With(nil, echo.Config{
				Service:   HeadlessSvc,
				Headless:  true,
				Namespace: apps.Namespace,
				Ports:     headlessPorts,
				Subsets:   []echo.SubsetConfig{{}},
				Cluster:   c,
			}).
			With(nil, echo.Config{
				Service:   NakedSvc,
				Namespace: apps.Namespace,
				Ports:     EchoPorts,
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarInject: {
								Value: strconv.FormatBool(false)},
						},
					},
				},
				Cluster: c,
			}).
			With(nil, echo.Config{
				Service:   ExternalSvc,
				Namespace: apps.ExternalNamespace,
				Ports:     EchoPorts,
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarInject: {
								Value: strconv.FormatBool(false)},
						},
					},
				},
				Cluster: c,
			})
	}

	for _, c := range ctx.Clusters().ByNetwork() {
		builder.With(nil, echo.Config{
			Service:    VMSvc,
			Namespace:  apps.Namespace,
			Ports:      EchoPorts,
			DeployAsVM: true,
			Subsets:    []echo.SubsetConfig{{}},
			Cluster:    c[0],
		})
	}

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.PodA = echos.Match(echo.Service(PodASvc))
	apps.PodB = echos.Match(echo.Service(PodBSvc))
	apps.PodC = echos.Match(echo.Service(PodCSvc))
	apps.Headless = echos.Match(echo.Service(HeadlessSvc))
	apps.Naked = echos.Match(echo.Service(NakedSvc))
	apps.External = echos.Match(echo.Service(ExternalSvc))
	apps.VM = echos.Match(echo.Service(VMSvc))

	apps.ExternalHost = "fake.example.com"
	if err := ctx.Config().ApplyYAML(apps.Namespace.Name(), fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-namespace
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service
spec:
  hosts:
  - %s
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: grpc
    number: 7070
    protocol: GRPC
  resolution: DNS
  endpoints:
  - address: external.%s
`, apps.ExternalHost, apps.ExternalNamespace.Name())); err != nil {
		return err
	}
	return nil
}
