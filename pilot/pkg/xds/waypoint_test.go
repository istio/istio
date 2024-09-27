package xds_test

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestWaypoint(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbient, true)
	test.SetForTest(t, &features.EnableDualStack, true)
	waypointSvc := `apiVersion: v1
kind: Service
metadata:
  labels:
    gateway.istio.io/managed: istio.io-mesh-controller
    gateway.networking.k8s.io/gateway-name: waypoint
    istio.io/gateway-name: waypoint
  name: waypoint
  namespace: default
spec:
  clusterIP: 3.0.0.0
  ports:
  - appProtocol: hbone
    name: mesh
    port: 15008
  selector:
    gateway.networking.k8s.io/gateway-name: waypoint
`
	waypointInstance := `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: waypoint-a
  namespace: default
spec:
  address: 3.0.0.1
  labels:
    gateway.networking.k8s.io/gateway-name: waypoint
`
	waypointGateway := `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: waypoint
  namespace: default
spec:
  gatewayClassName: waypoint
  listeners:
    - name: mesh
      port: 15009
      protocol: HBONE
status:
  addresses:
  - type: Hostname
    value: waypoint.default.svc.cluster.local
`
	appServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app
`
	appWorkloadEntry := `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: app-a
  namespace: default
  labels:
    app: app
  annotations:
    ambient.istio.io/redirection: enabled
spec:
  address: 1.1.1.1
`
	appPod := `apiVersion: v1
kind: Pod
metadata:
  name: app-b
  namespace: default
  labels:
    app: app
  annotations:
    ambient.istio.io/redirection: enabled
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 1.1.1.2
  podIPs:
  - ip: 1.1.1.2
  - ip: 2001:20::2
`
	appPodNoMesh := `apiVersion: v1
kind: Pod
metadata:
  name: app-c
  namespace: default
  labels:
    app: app
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 1.1.1.3
  podIPs:
  - ip: 1.1.1.3
  - ip: 2001:20::3
`
	d := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: joinYaml(waypointInstance, appServiceEntry, appWorkloadEntry),
		KubeClientModifier: func(c kube.Client) {
			// Because we are hitting the ambient controller, we want to create actual objects
			createString[*gateway.Gateway](t, c, waypointGateway)
			createString[*corev1.Service](t, c, waypointSvc)
			createString[*corev1.Pod](t, c, appPod, appPodNoMesh)
			createString[*networkingv1.WorkloadEntry](t, c, waypointInstance, appWorkloadEntry)
			createString[*networkingv1.ServiceEntry](t, c, appServiceEntry)
		},
	})
	proxy := d.SetupProxy(&model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		IPAddresses:     []string{"3.0.0.1"}, // match the WE
	})

	eps := slices.Sort(xdstest.ExtractEndpoints(d.Endpoints(proxy)[0]))
	assert.Equal(t, eps, []string{
		// No tunnel, should get dual IPs
		"1.1.1.3:80,[2001:20::3]:80",
		"connect_originate;1.1.1.1:80",
		// Tunnel doesn't support multiple IPs
		"connect_originate;1.1.1.2:80",
	})
}

func kubernetesObjectFromString(s string) (runtime.Object, error) {
	decode := kube.IstioCodec.UniversalDeserializer().Decode
	if len(strings.TrimSpace(s)) == 0 {
		return nil, fmt.Errorf("empty kubernetes object")
	}
	o, _, err := decode([]byte(s), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed deserializing kubernetes object: %v (%v)", err, s)
	}
	return o, nil
}

func createString[T controllers.ComparableObject](t test.Failer, c kube.Client, objs ...string) {
	for _, s := range objs {
		obj, err := kubernetesObjectFromString(s)
		assert.NoError(t, err)
		clienttest.NewWriter[T](t, c).Create(obj.(T))
	}
}

func joinYaml(s ...string) string {
	return strings.Join(s, "\n---\n")
}
