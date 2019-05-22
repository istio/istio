// Copyright 2019 Istio Authors
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

package framework

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestGalley_Kubernetes(t *testing.T) {
	const simpleResource = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gw
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`

	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			gal := galley.NewOrFail(t, ctx, galley.Config{})

			ns := namespace.NewOrFail(t, ctx, "res", true)

			t.Run("ApplyConfig", func(t *testing.T) {
				gal.ApplyConfigOrFail(t, ns, simpleResource)
			})

			t.Run("WaitForExistingResource", func(t *testing.T) {
				waitForResourceAppearance(t, gal, ns)
			})

			t.Run("ClearConfig", func(t *testing.T) {
				if err := gal.ClearConfig(); err != nil {
					t.Fatalf("Error clearing config: %v", err)
				}
			})

			t.Run("WaitForMissingResource", func(t *testing.T) {
				waitForResourceDisappearance(t, gal, ns)
			})

			t.Run("DeleteConfig", func(t *testing.T) {
				gal.ApplyConfigOrFail(t, ns, simpleResource)
				waitForResourceAppearance(t, gal, ns)
				gal.DeleteConfigOrFail(t, ns, simpleResource)
				waitForResourceDisappearance(t, gal, ns)
			})
		})
}

func waitForResourceAppearance(t *testing.T, gal galley.Instance, ns namespace.Instance) {
	waitForResource(t, gal, ns, true)
}

func waitForResourceDisappearance(t *testing.T, gal galley.Instance, ns namespace.Instance) {
	waitForResource(t, gal, ns, false)
}

func waitForResource(t *testing.T, gal galley.Instance, ns namespace.Instance, shouldExist bool) {
	gal.WaitForSnapshotOrFail(t, "istio/networking/v1alpha3/gateways", func(actuals []*galley.SnapshotObject) error {
		var gw *galley.SnapshotObject

		for _, candidate := range actuals {
			if candidate.Metadata.Name == fmt.Sprintf("%s/gw", ns.Name()) {
				gw = candidate
				break
			}
		}

		if shouldExist {
			if gw == nil {
				return fmt.Errorf("resource not found in set: %+v", actuals)
			}
		} else {
			if gw != nil {
				return fmt.Errorf("resource was expected *not* to be found in set: %+v", actuals)
			}
		}

		return nil
	})
}
