package outboundtrafficpolicy

import (
	"bytes"
	"fmt"
	"html/template"
	"reflect"
	"testing"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	ServiceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: https-service
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: https
    number: 90
    protocol: HTTPS
  resolution: DNS
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: http
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: DNS
`
	SidecarScope = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-service-entry-namespace
spec:
  egress:
  - hosts:
    - "{{.ImportNamespace}}/*"
    - "istio-system/*"
`
)

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createSidecarScope(t *testing.T, appsNamespace namespace.Instance, serviceNamespace namespace.Instance, g galley.Instance) {
	tmpl, err := template.New("SidecarScope").Parse(SidecarScope)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ImportNamespace": serviceNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(appsNamespace, buf.String()); err != nil {
		t.Errorf("failed to apply service entries: %v", err)
	}
}

// TODO support native environment. Blocked by #13177 because the listeners for native use static
// routes and this test relies on the dynamic routes sent through pilot to allow external traffic.

// Expected is a map of protocol -> expected response codes
func RunExternalRequestTest(expected map[string][]string, t *testing.T) {
	framework.
		NewTest(t).
		// TODO(https://github.com/istio/istio/issues/13813)
		Label(label.Flaky).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			appsNamespace := namespace.NewOrFail(t, ctx, "app", true)
			serviceNamespace := namespace.NewOrFail(t, ctx, "service", true)

			var client, dest echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echo.Config{
					Service:   "client",
					Namespace: appsNamespace,
					Pilot:     p,
					Galley:    g,
				}).
				With(&dest, echo.Config{
					Service:   "destination",
					Namespace: appsNamespace,
					Pilot:     p,
					Galley:    g,
					Ports: []echo.Port{
						{
							Name:     "http",
							Protocol: model.ProtocolHTTP,
						},
						{
							Name:     "https",
							Protocol: model.ProtocolHTTPS,
						},
					},
				}).BuildOrFail(t)

			// External traffic should work even if we have service entries on the same ports
			createSidecarScope(t, appsNamespace, serviceNamespace, g)
			if err := g.ApplyConfig(serviceNamespace, ServiceEntry); err != nil {
				t.Errorf("failed to apply service entries: %v", err)
			}

			if err := WaitUntilNotCallable(client, dest); err != nil {
				t.Fatalf("failed to apply sidecar, %v", err)
			}

			cases := []struct {
				name     string
				portName string
			}{
				{
					name:     "HTTP Traffic",
					portName: "http",
				},
				{
					name:     "HTTPS Traffic",
					portName: "https",
				},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					resp, err := client.Call(echo.CallOptions{
						Target:   dest,
						PortName: tc.portName,
						Scheme:   scheme.HTTP,
					})
					if err != nil && len(expected[tc.portName]) != 0 {
						t.Fatalf("request failed: %v", err)
					}
					codes := make([]string, 0, len(resp))
					for _, r := range resp {
						codes = append(codes, r.Code)
					}
					if !reflect.DeepEqual(codes, expected[tc.portName]) {
						t.Errorf("got codes %v, expected %v", codes, expected[tc.portName])
					}
				})
			}
		})
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

// Wait for the destination to NOT be callable by the client. This allows us to simulate external traffic.
// This essentially just waits for the Sidecar to be applied, without sleeping.
func WaitUntilNotCallable(c echo.Instance, dest echo.Instance) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		for _, port := range dest.Config().Ports {
			clusterName := clusterName(dest, port)
			// Ensure that we have an outbound configuration for the target port.
			err := validator.NotExists("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", clusterName).Check()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
		}
	}

	return nil
}
