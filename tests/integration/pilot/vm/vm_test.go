package vm

import (
	"testing"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/cert"
)

var p pilot.Instance
var ns namespace.Instance

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite("vm_test", m).
		RequireSingleCluster().
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, func(ctx resource.Context) error {
			var err error
			ns, err = namespace.New(ctx, namespace.Config{
				Prefix: "virtual-machine",
				Inject: true,
			})
			return err
		}).
		SetupOnEnv(environment.Kube, istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
		}, func(ctx resource.Context) error {
			return cert.CreateCASecret(ctx, ns.Name())
		})).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

// TODO:
// * Use workload entry (ie pod has no labels)
// * Dynamically generate certs for namespace. Maybe do this for cacerts as well. Stop using default then
// * Clean up all the debug stuff
// * Investigate if the changes to istio-start are safe
func TestVmTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Set up strict mTLS. This gives a bit more assurance the calls are actually going through envoy,
			// and certs are set up correctly.
			ctx.ApplyConfigOrFail(ctx, ns.Name(), `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: send-mtls
spec:
  host: "*.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`)
			ports := []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
			}
			var pod, vm echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&vm, echo.Config{
					Service:    "vm",
					Namespace:  ns,
					Ports:      ports,
					Pilot:      p,
					DeployAsVM: true,
				}).
				With(&pod, echo.Config{
					Service:   "pod",
					Namespace: ns,
					Ports:     ports,
					Pilot:     p,
				}).
				BuildOrFail(t)

			retry.UntilSuccessOrFail(ctx, func() error {
				r, err := pod.Call(echo.CallOptions{Target: vm, PortName: "http"})
				log.Errorf("howardjohn: %v -> %v", r.String(), err)
				if err != nil {
					return err
				}
				return r.CheckOK()
			}, retry.Delay(100*time.Millisecond))
			retry.UntilSuccessOrFail(ctx, func() error {
				r, err := vm.Call(echo.CallOptions{Target: pod, PortName: "http"})
				log.Errorf("howardjohn: %v -> %v", r.String(), err)
				if err != nil {
					return err
				}
				return r.CheckOK()
			}, retry.Delay(100*time.Millisecond))
		})
}
