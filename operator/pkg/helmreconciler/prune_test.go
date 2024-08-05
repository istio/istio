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

package helmreconciler

import (
	_ "embed"
	"istio.io/istio/pkg/kube"
	"testing"

	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	//go:embed testdata/iop-test-gw-1.yaml
	iopTestGwData1 []byte
	//go:embed testdata/iop-test-gw-2.yaml
	iopTestGwData2 []byte
)

// TODO
func TestGetPrunedResources(t *testing.T) {
	cl := kube.NewFakeClient()
	// init two custom gateways with revision
	gateways := [][]byte{iopTestGwData1, iopTestGwData2}
	for i, data := range gateways {
		iop := &v1alpha1.IstioOperator{}
		err := yaml.UnmarshalStrict(data, iop)
		assert.NoError(t, err)
		_ = i
		//h := &HelmReconciler{
		//	client:     cl,
		//	kubeClient: kube.NewFakeClientWithVersion("24"),
		//	opts: &Options{
		//		ProgressLog: progress.NewLog(),
		//		Log:         clog.NewDefaultLogger(),
		//	},
		//	iop: iop,
		//}
		//if i == 0 {
		//	h1 = h
		//}
		//manifestMap, err := h.RenderCharts()
		//if err != nil {
		//	t.Fatalf("failed to render manifest: %v", err)
		//}
		//applyResourcesIntoCluster(t, h, manifestMap)
	}
	// delete one iop: iop-test-gw-1, get its pruned resources
	componentName := string(name.IngressComponentName)
	resources, err := GetPrunedResources(cl, "name", "ns", "rev", false)
	assert.NoError(t, err)
	assert.Equal(t, true, len(resources) > 0)
	// check resources, only associated with iop-test-gw-1 istiooperator CR,
	// otherwise, the resources of all IngressGateways components will be deleted.
	// See https://github.com/istio/istio/issues/40577 for more details.
	for _, uslist := range resources {
		for _, u := range uslist.Items {
			assert.Equal(t, "rev", u.GetLabels()[label.IoIstioRev.Name])
			assert.Equal(t, componentName, u.GetLabels()[IstioComponentLabelStr])
			assert.Equal(t, "name", u.GetLabels()[OwningResourceName])
			assert.Equal(t, "ns", u.GetLabels()[OwningResourceNamespace])
		}
	}
}
