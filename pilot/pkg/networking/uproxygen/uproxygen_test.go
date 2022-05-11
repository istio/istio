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
package uproxygen_test

import (
	"context"
	"strings"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

// On node 1:
// * One client pod using its own ServiceAccount
// * One server pod using a different service account
// On node 2:
// * A different server pod
const testData = `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sleep
---
apiVersion: v1
kind: Pod
metadata:
  name: sleep-pod-1
  namespace: ns1
  labels:
    app: sleep
    asm-type: workload
spec:
  nodeName: worker-1
  serviceAccountName: sleep
status:
  podIP: 10.0.0.1
  conditions:
  - type: Ready
    status: "True"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: ns1
---
apiVersion: v1
kind: Service
metadata:
  name: helloworld
  namespace: ns1
  labels:
    app: helloworld
    service: helloworld
spec:
  ports:
  - port: 5000
    name: http
  selector:
    app: helloworld
---
apiVersion: v1
kind: Pod
metadata:
  name: helloworld-pod-1
  namespace: ns1
  labels:
    app: helloworld
    asm-type: workload
spec:
  nodeName: worker-1
  serviceAccountName: default
status:
  podIP: 10.0.0.2
  conditions:
  - type: Ready
    status: "True"
---
apiVersion: v1
kind: Pod
metadata:
  name: helloworld-pod-2
  namespace: ns1
  labels:
    app: helloworld
    asm-type: workload
spec:
  nodeName: worker-2
  serviceAccountName: default
status:
  podIP: 10.0.0.3
  conditions:
  - type: Ready
    status: "True"
`

func TestUproxygen(t *testing.T) {
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesObjectString: testData,
	})
	ads := ds.ConnectADS().WithMetadata(model.NodeMetadata{
		Generator: "uproxy-envoy",
		NodeName:  "worker-1",
	})

	type NamedMessage interface {
		proto.Message
		GetName() string
	}

	ctx := context.TODO()
	serviceAccounts, _ := ds.KubeClient().CoreV1().ServiceAccounts(v1.NamespaceAll).List(ctx, v1.ListOptions{})
	sa := len(serviceAccounts.Items)
	services, _ := ds.KubeClient().CoreV1().Services(v1.NamespaceAll).List(ctx, v1.ListOptions{})
	svc := len(services.Items)
	testCases := []struct {
		msg     NamedMessage
		typeURL string
		want    int
	}{
		{
			msg:     &listenerv3.Listener{},
			typeURL: v3.ListenerType,
			// inbound, outbound + outbound_tunnel (per-sa)
			want: 2 + sa,
			// TODO inspect filter chains
		},
		{
			msg:     &clusterv3.Cluster{},
			typeURL: v3.ClusterType,
			// blackhole + passthrough + inbound + outbound_tunnel (per-sa), outbound_internal (sa * svc)
			want: 3 + sa + (sa * svc),
		},
		// TODO cover eds
	}

	for _, tc := range testCases {
		t.Run(tc.typeURL, func(t *testing.T) {
			ads = ads.WithType(tc.typeURL)
			res := ads.RequestResponseAck(t, &discoveryv3.DiscoveryRequest{})
			if n := len(res.Resources); n != tc.want {
				var names []string
				if tc.msg != nil {
					for _, resource := range res.Resources {
						_ = resource.UnmarshalTo(tc.msg)
						names = append(names, ""+tc.msg.GetName())
					}
				}
				t.Errorf("expected %d; got %d\n- %s", tc.want, n, strings.Join(names, "\n- "))
			}
		})
	}
}
