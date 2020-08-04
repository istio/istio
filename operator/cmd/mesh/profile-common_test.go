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
	"errors"
	"testing"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util/clog"
)

func TestParseYAMLFiles(t *testing.T) {
	tests := []struct {
		desc            string
		inYAML          []string
		inForce         bool
		inLogger        clog.Logger
		expectedOverlay string
		expectedProfile string
		expectedErr     error
	}{
		{
			desc:     "array-pilot-plugins",
			inYAML:   []string{"testdata/profile-dump/input/pilot_plugin_valid.yaml"},
			inForce:  false,
			inLogger: clog.NewDefaultLogger(),
			expectedOverlay: `apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
spec:
  values:
    pilot:
      plugins:
        - aa
        - bb `,
			expectedProfile: "",
			expectedErr:     nil,
		},
		{
			desc:            "invalid-pilot-plugins",
			inYAML:          []string{"testdata/profile-dump/input/pilot_plugin_invalid.yaml"},
			inForce:         false,
			inLogger:        clog.NewDefaultLogger(),
			expectedOverlay: "",
			expectedProfile: "",
			expectedErr:     errors.New("json: cannot unmarshal object into Go value of type string"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotOverlay, gotProfile, err := manifest.ParseYAMLFiles(tt.inYAML, tt.inForce, tt.inLogger)
			if tt.expectedProfile != gotProfile || tt.expectedOverlay != gotOverlay ||
				((tt.expectedErr != nil && err == nil) || (tt.expectedErr == nil && err != nil)) {
				t.Errorf("%s: expect overlay, profile,&err %v %v %v, got %v %v %v", tt.desc, tt.expectedOverlay,
					tt.expectedProfile, tt.expectedErr, gotOverlay, gotProfile, err)
			}
		})
	}
}
