package mesh

import (
	"istio.io/istio/operator/pkg/util/clog"
	"testing"
)

func TestParseYAMLFiles(t *testing.T) {
	tests := []struct{
		desc string
		inYAML []string
		inForce bool
		inLogger clog.Logger
		expectedOverlay string
		expectedProfile string
		expectedErr error
	}{
		{
			desc: "array-pilot-plugins",
			inYAML: []string{"testdata/profile-dump/input/pilot_plugin.yaml"},
			inForce: false,
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
        - bb`,
        	expectedProfile: "",
        	expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if gotOverlay, gotProfile, err := parseYAMLFiles(tt.inYAML, tt.inForce, tt.inLogger); tt.expectedProfile != gotProfile || tt.expectedOverlay != gotOverlay || ((tt.expectedErr != nil && err == nil)  || (tt.expectedErr == nil && err != nil)) {
				t.Errorf("%s: expected overlay, profile, and err %v %v %v, got %v %v %v", tt.desc, tt.expectedOverlay, tt.expectedProfile, tt.expectedErr, gotOverlay, gotProfile, err)
			}
		})
	}
}