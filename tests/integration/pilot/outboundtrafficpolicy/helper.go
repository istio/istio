package outboundtrafficpolicy

import (
	"bytes"
	"html/template"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

const (
	IstioSystemScope = `
`
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
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			appsNamespace := namespace.NewOrFail(t, ctx, "app", true)
			serviceNamespace := namespace.NewOrFail(t, ctx, "service", true)

			createSidecarScope(t, appsNamespace, serviceNamespace, g)

			// External traffic should work even if we have service entries on the same ports
			if err := g.ApplyConfig(serviceNamespace, ServiceEntry); err != nil {
				t.Errorf("failed to apply service entries: %v", err)
			}

			instance := apps.NewOrFail(t, ctx, apps.Config{
				Namespace: appsNamespace,
				Pilot:     p,
				Galley:    g,
				AppParams: []apps.AppParam{
					{Name: "client"},
					{Name: "destination"},
				},
			})

			client := instance.GetAppOrFail("client", t).(apps.KubeApp)
			dest := instance.GetAppOrFail("destination", t).(apps.KubeApp)

			// Wait for config to propagate
			time.Sleep(time.Second * 5)

			cases := []struct {
				name     string
				protocol string
				port     int
			}{
				{
					"HTTP Traffic",
					"http",
					80,
				},
				{
					"HTTPS Traffic",
					"https",
					90,
				},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					ep := dest.EndpointForPort(tc.port)
					resp, err := client.Call(ep, apps.AppCallOptions{})
					if err != nil && len(expected[tc.protocol]) != 0 {
						t.Fatalf("request failed: %v", err)
					}
					codes := make([]string, 0, len(resp))
					for _, r := range resp {
						codes = append(codes, r.Code)
					}
					if !reflect.DeepEqual(codes, expected[tc.protocol]) {
						t.Errorf("got codes %v, expected %v", codes, expected[tc.protocol])
					}
				})
			}
		})
}
