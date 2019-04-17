//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package dialout

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/mcpserver"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

var yamlCfg = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: helloworld-gateway
spec:
  selector:
    istio: ingressgateway # use istio default controller
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`

func TestDialout_Basic(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		srv := mcpserver.NewSinkOrFail(t, ctx, mcpserver.SinkConfig{Collections: []string{"istio/networking/v1alpha3/gateways"}})

		g := galley.NewOrFail(t, ctx, galley.Config{SinkAddress: srv.Address()})

		ns := namespace.NewOrFail(t, ctx, "dialout", true)

		g.ApplyConfigOrFail(t, ns, yamlCfg)

		retry.UntilSuccessOrFail(t, func() error {
			objects := srv.GetCollectionStateOrFail(t, "istio/networking/v1alpha3/gateways")
			if len(objects) != 1 {
				return fmt.Errorf("expected number of objects not found (current: %v)", len(objects))
			}

			structpath.ForProto(objects[0].Body).
				Equals("{.Metadata.Name}", "helloworld-gateway")

			return nil
		})
	})
}

func TestMain(m *testing.M) {
	// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
	// component
	framework.
		NewSuite("galley_dialout", m).
		RequireEnvironment(environment.Native).
		Run()
}
