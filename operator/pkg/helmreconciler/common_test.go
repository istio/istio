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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHelmReconciler_saveClusterIP(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		overlay    string
		expectedIP string
		wantErr    bool
	}{
		{
			name:       "keeps the cluster IP set by the cluster",
			current:    "testdata/current.yaml",
			overlay:    "testdata/overlay.yaml",
			expectedIP: "10.0.171.239",
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := loadData(t, tt.current)
			current := obj.UnstructuredObject()
			objOverlay := loadData(t, tt.overlay)
			overlay := objOverlay.UnstructuredObject()
			if err := saveClusterIP(current, overlay); (err != nil) != tt.wantErr {
				t.Errorf("HelmReconciler.saveClusterIP() error = %v, wantErr %v", err, tt.wantErr)
			}
			clusterIP, _, _ := unstructured.NestedString(current.Object, "spec",
				"clusterIP")
			if clusterIP != tt.expectedIP {
				t.Errorf("wanted:\n%v\ngot:\n%v", tt.expectedIP, clusterIP)
			}
		})
	}
}

func TestHelmReconciler_saveNodePorts(t *testing.T) {
	tests := []struct {
		name    string
		current string
		overlay string
		want    string
	}{
		{
			name:    "keeps the nodePorts set by the cluster",
			current: "testdata/current.yaml",
			overlay: "testdata/overlay.yaml",
			want:    "testdata/clusterIP-changed.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := loadData(t, tt.current)
			current := obj.UnstructuredObject()
			objOverlay := loadData(t, tt.overlay)
			overlay := objOverlay.UnstructuredObject()
			overlayMap := createPortMap(overlay)
			saveNodePorts(current, overlay)
			currentMap := createPortMap(current)
			overlayUpdatedMap := createPortMap(overlay)
			for key, element := range overlayMap {
				if element != 0 {
					expectedVal, ok := overlayUpdatedMap[key]
					if !ok {
						t.Errorf("HelmReconciler.saveNodePorts() error = unmatched key %v between initial overlay "+
							"and updated overlay.", key)
						break
					}
					if element != expectedVal {
						t.Errorf("wanted:\n%v\ngot:\n%v", expectedVal, element)
					}
					continue
				}
				val, ok := overlayUpdatedMap[key]
				if !ok {
					t.Errorf("HelmReconciler.saveNodePorts() error = unmatched key %v between initial overlay "+
						"and updated overlay.", key)
					break
				}
				expectedVal, ok := currentMap[key]
				if !ok {
					t.Errorf("HelmReconciler.saveNodePorts() error = unmatched key %v between initial overlay "+
						"and current.", key)
					break
				}
				if val != expectedVal {
					t.Errorf("wanted:\n%v\ngot:\n%v", expectedVal, val)
				}
			}
		})
	}
}
