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
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	istiodLabel = "pilot"
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
		e := ctx.Config(cluster).DeleteYAML("", dummyValidationVirtualService)
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy virtual service for waiting the validation webhook: %v", e)
		}
	}()

	scopes.Framework.Info("Creating dummy virtual service to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		err := ctx.Config(cluster).ApplyYAML("", dummyValidationVirtualService)
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

func (i *operatorComponent) RemoteDiscoveryAddressFor(cluster cluster.Cluster) (net.TCPAddr, error) {
	var addr net.TCPAddr
	primary := cluster.Primary()
	if !primary.IsConfig() {
		// istiod is exposed via LoadBalancer since we won't have ingress outside of a cluster;a cluster that is;
		// a control cluster, but not config cluster is supposed to simulate istiod outside of k8s or "external"
		address, err := retry.Do(func() (interface{}, bool, error) {
			return getRemoteServiceAddress(i.environment.Settings(), primary, i.settings.SystemNamespace, istiodLabel,
				istiodSvcName, discoveryPort)
		}, getAddressTimeout, getAddressDelay)
		if err != nil {
			return net.TCPAddr{}, err
		}
		addr = address.(net.TCPAddr)
	} else {
		addr = i.CustomIngressFor(primary, eastWestIngressServiceName, eastWestIngressIstioLabel).DiscoveryAddress()
	}
	if addr.IP.String() == "<nil>" {
		return net.TCPAddr{}, fmt.Errorf("failed to get ingress IP for %s", primary.Name())
	}
	return addr, nil
}

func getRemoteServiceAddress(s *kube.Settings, cluster cluster.Cluster, ns, label, svcName string,
	port int) (interface{}, bool, error) {
	if !s.LoadBalancerSupported {
		pods, err := cluster.PodsForSelector(context.TODO(), ns, fmt.Sprintf("istio=%s", label))
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying remote service %s, pods:\n%v\n", svcName, names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no remote service pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the remote service node yet")
		}

		svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), svcName, v1.GetOptions{})
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
	svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), svcName, v1.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service %s is not available yet: %s/%s", svcName, svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: port}, true, nil
}

func (i *operatorComponent) isExternalControlPlane() bool {
	for _, cluster := range i.ctx.Clusters() {
		if cluster.IsPrimary() && !cluster.IsConfig() {
			return true
		}
	}
	return false
}
