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
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

func TestUpdateExistingContents(t *testing.T) {
	g := NewWithT(t)
	src := NewKubeSource(collections.Istio)

	applyAndValidate := func(version string) {
		configTemplate := `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: productpage
  labels:
    version: %s
spec:
  host: productpage
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
  subsets:
  - name: %s
    labels:
      version: %s`
		config := fmt.Sprintf(configTemplate, version, version, version)
		err := src.ApplyContent("test", config)
		g.Expect(err).To(BeNil())
		existing := src.Get(gvk.DestinationRule, "productpage", "")
		g.Expect(existing.Labels["version"]).To(Equal(version))
	}

	// Apply v1 config
	applyAndValidate("v1")
	// Apply v2 config and validate overwrite
	applyAndValidate("v2")
}

func TestUnknownSchema(t *testing.T) {
	src := NewKubeSource(collections.Istio)
	assert.NoError(t, src.ApplyContent("test", `apiVersion: networking.istio.io/v1
kind: WoKnows
`))
	assert.NoError(t, src.ApplyContent("test", `kind: List
apiVersion: v1
items:
- apiVersion: networking.istio.io/v1
  kind: WoKnows
`))
}
