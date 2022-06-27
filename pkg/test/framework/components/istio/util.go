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

package istio

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

var dummyValidationVirtualServiceTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: validation-readiness-dummy-virtual-service
  namespace: %s
spec:
  hosts:
    - non-existent-host
  http:
    - route:
      - destination:
          host: non-existent-host
          subset: v1
        weight: 75
      - destination:
          host: non-existent-host
          subset: v2
        weight: 25
`

func waitForValidationWebhook(ctx resource.Context, cluster cluster.Cluster, cfg Config) error {
	dummyValidationVirtualService := fmt.Sprintf(dummyValidationVirtualServiceTemplate, cfg.SystemNamespace)
	defer func() {
		e := ctx.ConfigKube(cluster).YAML("", dummyValidationVirtualService).Delete()
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy virtual service for waiting the validation webhook: %v", e)
		}
	}()

	scopes.Framework.Info("Creating dummy virtual service to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		err := ctx.ConfigKube(cluster).YAML("", dummyValidationVirtualService).Apply()
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

func getRemoteServiceAddress(s *kube.Settings, cluster cluster.Cluster, ns, label, svcName string,
	port int,
) (interface{}, bool, error) {
	if !s.LoadBalancerSupported {
		pods, err := cluster.PodsForSelector(context.TODO(), ns, label)
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying remote service %s, pods:%v", svcName, names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no remote service pod found")
		}

		scopes.Framework.Debugf("Found pod: %v", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the remote service node yet")
		}

		svc, err := cluster.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
		if err != nil {
			return nil, false, err
		}

		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", ns, svcName)
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(port) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return nil, false, fmt.Errorf("no port %d found in service: %s/%s", port, ns, svcName)
		}

		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := cluster.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil, false, fmt.Errorf("service %s/%s is not available yet: no ingress", svc.Namespace, svc.Name)
	}
	ingr := svc.Status.LoadBalancer.Ingress[0]
	if ingr.IP == "" && ingr.Hostname == "" {
		return nil, false, fmt.Errorf("service %s/%s is not available yet: no ingress", svc.Namespace, svc.Name)
	}
	if ingr.IP != "" {
		return net.TCPAddr{IP: net.ParseIP(ingr.IP), Port: port}, true, nil
	}
	return net.JoinHostPort(ingr.Hostname, strconv.Itoa(port)), true, nil
}

func removeCRDsSlice(raw []string) string {
	res := make([]string, 0)
	for _, r := range raw {
		res = append(res, removeCRDs(r))
	}
	return yml.JoinString(res...)
}
