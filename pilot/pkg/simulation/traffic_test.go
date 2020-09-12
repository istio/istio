package simulation

import (
	"testing"

	"istio.io/istio/pilot/pkg/xds"
)

func TestBasic(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{KubernetesObjectString: `apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: default
spec:
  selector:
    app: httpbin
  clusterIP: 1.2.3.4
  ports:
  - name: http
    port: 80
    targetPort: 80`})
	RunSimulation(t, s, s.SetupProxy(nil), Call{
		Address:  "1.2.3.4",
		Port:     80,
		Protocol: HTTP,
		Headers:  map[string][]string{"Host": {"httpbin"}},
	}).Matches(t, Result{
		ListenerMatched:    "0.0.0.0_80",
		RouteMatched:       "default",
		RouteConfigMatched: "80",
		VirtualHostMatched: "httpbin.default.svc.cluster.local:80",
		ClusterMatched:     "outbound|80||httpbin.default.svc.cluster.local",
	})
	result := RunSimulation(t, s, s.SetupProxy(nil), Call{
		Address:  "1.2.3.4",
		Port:     80,
		Protocol: HTTP,
		Headers:  map[string][]string{"Host": {"foobar"}},
	})
	result.Matches(t, Result{
		ListenerMatched:    "0.0.0.0_80",
		RouteMatched:       "allow_any",
		RouteConfigMatched: "80",
		ClusterMatched:     "PassthroughCluster",
	})
}
