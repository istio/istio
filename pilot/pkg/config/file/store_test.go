/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package file

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestUpdateExistingContents(t *testing.T) {
	cases := []struct {
		existingConfig          string
		newConfigs              []string
		expectedResourceVersion string
	}{
		{
			existingConfig: `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  subsets:
  - name: v1
    labels:
      version: v1`,
			newConfigs: []string{`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: v1
    labels:
      version: v1`},
			expectedResourceVersion: "v2",
		},
		{
			existingConfig: `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: v1
    labels:
      version: v1`,
			newConfigs: []string{
				`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: v2
    labels:
      version: v2`,
				`apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: DISABLE 
  subsets:
  - name: v2
    labels:
      version: v2`,
			},
			expectedResourceVersion: "v3",
		},
	}
	g := NewWithT(t)
	for _, c := range cases {
		src := NewKubeSource(collections.Istio)

		err := src.ApplyContent("test", c.existingConfig)
		g.Expect(err).To(BeNil())

		// apply the same resource, should overwrite the existing one
		for _, cfg := range c.newConfigs {
			err = src.ApplyContent("test", cfg)
			g.Expect(err).To(BeNil())
		}
		existing := src.Get(gvk.DestinationRule, "productpage", "")
		g.Expect(c.expectedResourceVersion, existing.ResourceVersion)
	}
}
