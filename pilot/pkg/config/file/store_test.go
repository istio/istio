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
)

func TestApplyingConflictingContents(t *testing.T) {
	yaml1 := `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
  host: productpage
  subsets:
  - name: v1
    labels:
      version: v1`

	yaml2 := `apiVersion: networking.istio.io/v1alpha3
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
      version: v1`

	yaml3 := `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage-1
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: v1
    labels:
      version: v1`

	yaml4 := `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage-1
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: v2
    labels:
      version: v2`

	tests := []struct {
		name string
		text string
	}{
		{
			name: "yaml1",
			text: yaml1,
		},
		{
			name: "yaml2",
			text: yaml2,
		},
		{
			name: "yaml3",
			text: yaml3,
		},
		{
			name: "yaml4",
			text: yaml4,
		},
	}

	g := NewWithT(t)
	for _, t := range tests {
		src := NewKubeSource(collections.Istio)
		err := src.ApplyContent(t.name, t.text)
		g.Expect(err).To(BeNil())
	}
}
