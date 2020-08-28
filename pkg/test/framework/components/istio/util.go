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

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	dummyValidationVirtualServiceTemplate = `
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
)

func waitForValidationWebhook(ctx resource.Context, cluster resource.Cluster, cfg Config) error {
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

func (i *operatorComponent) RemoteDiscoveryAddressFor(cluster resource.Cluster) (net.TCPAddr, error) {
	cp, err := i.environment.GetControlPlaneCluster(cluster)
	if err != nil {
		return net.TCPAddr{}, err
	}
	addr := i.IngressFor(cp).DiscoveryAddress()
	if addr.IP.String() == "<nil>" {
		return net.TCPAddr{}, fmt.Errorf("failed to get ingress IP for %s", cp.Name())
	}
	return addr, nil
}
