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

package mesh

import (
	"fmt"
	"testing"

	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

func Test_compareIOPWithInstalledIOP(t *testing.T) {
	findOperatorInCluster = func(client dynamic.Interface, name, namespace string) (*v1alpha1.IstioOperator, error) {
		return generateIOP("ambient"), nil
	}

	diff, err := compareIOPWithInstalledIOP(kube.NewFakeClient(), generateIOP("demo"))
	assert.Equal(t, err, nil)

	out, err := readFile("testdata/upgrade/expected-out.yaml")
	assert.NoError(t, err)
	assert.Equal(t, diff, out)
}

func generateIOP(profile string) *v1alpha1.IstioOperator {
	demo, err := readFile(fmt.Sprintf("testdata/upgrade/generated-%s.yaml", profile))
	if err != nil {
		return nil
	}
	var iop *v1alpha1.IstioOperator
	if err := yaml.Unmarshal([]byte(demo), &iop); err != nil {
		return nil
	}
	return iop
}
