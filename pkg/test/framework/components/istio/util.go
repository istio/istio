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
	"fmt"
	"net"
	"time"

	kubeenv "istio.io/istio/pkg/test/framework/components/environment/kube"

	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	dummyValidationRuleTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: validation-readiness-dummy-rule
  namespace: %s
spec:
  match: request.headers["foo"] == "bar"
  actions:
  - handler: validation-readiness-dummy
    instances:
    - validation-readiness-dummy
`
)

var (
	igwServiceName = "istio-ingressgateway"
	discoveryPort  = 15012
)

func waitForValidationWebhook(accessor *kube.Accessor, cfg Config) error {
	dummyValidationRule := fmt.Sprintf(dummyValidationRuleTemplate, cfg.SystemNamespace)
	defer func() {
		e := accessor.DeleteContents("", dummyValidationRule)
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy rule for waiting the validation webhook: %v", e)
		}
	}()

	scopes.CI.Info("Creating dummy rule to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		_, err := accessor.ApplyContents("", dummyValidationRule)
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

func getRemoteDiscoveryAddress(cfg Config, cluster kubeenv.Cluster) (net.TCPAddr, error) {
	// If running in KinD, MetalLB must be installed to enable LoadBalancer resources
	svc, err := cluster.GetService(cfg.SystemNamespace, igwServiceName)
	if err != nil {
		return net.TCPAddr{}, err
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return net.TCPAddr{}, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: discoveryPort}, nil
}
