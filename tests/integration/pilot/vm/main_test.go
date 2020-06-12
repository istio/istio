package vm

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance
	p pilot.Instance
	//ingr ingress.Instance
	ns namespace.Instance
)

// This tests VM mesh expansion. Rather than deal with the infra to get a real VM, we will use a pod
// with no Service, no DNS, no service account, etc to simulate a VM.
func TestMain(m *testing.M) {
	framework.
		NewSuite("vm_test", m).
		RequireSingleCluster().
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
# Add an additional TCP port, 31400
components:
  ingressGateways:
  - name: istio-ingressgateway
    enabled: true
    k8s:
      service:
        ports:
          - port: 15020
            targetPort: 15020
            name: status-port
          - port: 80
            targetPort: 8080
            name: http2
          - port: 443
            targetPort: 8443
            name: https
          - port: 31400
            targetPort: 31400
            name: tcp
values:
  global:
    meshExpansion:
      enabled: true`
			//meshConfig:
			// accessLogFile: "/dev/stdout"
			//grafana:
			// enabled: true
			//telemetry:
			// enabled: true
			//telemetry:
			// v1:
			//   enabled: true
			//telemetry:
			// v2:
			//   enabled: true
			//telemetry:
			// v2:
			//   prometheus:
			//     enabled: true`
		})).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			//if ingr, err = ingress.New(ctx, ingress.Config{
			//	Istio: i,
			//}); err != nil {
			//	return err
			//}
			return nil
		}).
		Run()
}
