package util

import (
	"github.com/gogo/protobuf/proto"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"reflect"
	"testing"
)

func TestToYAML(t *testing.T) {
	tests := []struct{
		desc string
		inVals interface{}
		expectedOut string
	}{
		{
			desc: "valid-yaml",
			inVals: map[string]interface{}{
				"foo": "bar",
				"yo": map[string]interface{}{
					"istio": "bar",
				},
			},
			expectedOut: `foo: bar
yo:
  istio: bar
`,
		},
		{
			desc: "alphabetical",
			inVals: map[string]interface{}{
				"foo": "yaml",
				"abc": "f",
			},
			expectedOut: `abc: f
foo: yaml
`,
		},
		{
			desc: "expected-err-nil",
			inVals: nil,
			expectedOut: "null\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := ToYAML(tt.inVals); got != tt.expectedOut {
				t.Errorf("%s: expected out %v got %s", tt.desc, tt.expectedOut, got)
			}
		})
	}
}

func TestToYAMLWithJSONPB(t *testing.T) {
	tests := []struct{
		desc string
		in proto.Message
		expectedOut string
	}{
		{
			desc: "valid-istio-op-with-missing-fields",
			in: &v1alpha1.IstioOperator{
				ApiVersion: "v1",
				Kind: "operator",
			},
			expectedOut: `apiVersion: v1
kind: operator
metadata:
  creationTimestamp: null
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := ToYAMLWithJSONPB(tt.in); got != tt.expectedOut {
				t.Errorf("%s: expected %v got %v", tt.desc, tt.expectedOut, got)
			}
		})
	}
}

func TestOverlayTrees(t *testing.T) {
	tests := []struct{
		desc string
		inBase map[string]interface{}
		inOverlays map[string]interface{}
		expectedOverlay map[string]interface{}
		expectedErr error
	}{
		{
			desc: "overlay-valid",
			inBase: map[string]interface{}{
				"foo": "bar",
				"baz": "naz",
			},
			inOverlays: map[string]interface{}{
				"foo": "laz",
			},
			expectedOverlay: map[string]interface{}{
				"baz": "naz",
				"foo": "laz",
			},
			expectedErr: nil,
		},
		{
			desc : "overlay-key-does-not-exist",
			inBase: map[string]interface{}{
				"foo": "bar",
				"baz": "naz",
			},
			inOverlays: map[string]interface{}{
				"i-dont-exist": "i-really-dont-exist",
			},
			expectedOverlay: map[string]interface{}{
				"baz": "naz",
				"foo": "bar",
				"i-dont-exist": "i-really-dont-exist",
			},
			expectedErr: nil,
		},
		{
			desc: "remove-key-val",
			inBase: map[string]interface{}{
				"foo": "bar",
			},
			inOverlays: map[string]interface{}{
				"foo": nil,
			},
			expectedOverlay: map[string]interface{}{},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if gotOverlays, err := OverlayTrees(tt.inBase, tt.inOverlays); !reflect.DeepEqual(gotOverlays, tt.expectedOverlay) ||
				((err != nil && tt.expectedErr == nil) || (err == nil && tt.expectedErr != nil)) {
				t.Errorf("%s: expected overlay & err %v %v got %v %v", tt.desc, tt.expectedOverlay, tt.expectedErr,
					gotOverlays, err)
			}
		})
	}
}