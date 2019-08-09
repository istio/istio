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

package translate

import (
	"testing"

	"github.com/kr/pretty"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/version"
)

func TestProtoToValuesV13(t *testing.T) {
	tests := []struct {
		desc    string
		yamlStr string
		want    string
		wantErr string
	}{
		{
			desc: "default success",
			yamlStr: `
defaultNamespace: istio-system
`,
			want: `
certmanager:
  enabled: false
  namespace: istio-system
citadel:
  enabled: false
  namespace: istio-system
galley:
  enabled: false
  namespace: istio-system
gateways:
  istio-egressgateway:
    enabled: false
    namespace: istio-system
  istio-ingressgateway:
    enabled: false
    namespace: istio-system
global:
  enabled: true
  namespace: istio-system
  istioNamespace: istio-system
  configNamespace: istio-system
  policyNamespace: istio-system
  prometheusNamespace: istio-system
  securityNamespace: istio-system
  telemetryNamespace: istio-system

mixer:
  policy:
    enabled: false
    namespace: istio-system
  telemetry:
    enabled: false
    namespace: istio-system
nodeagent:
  enabled: false
  namespace: istio-system
pilot:
  enabled: false
  namespace: istio-system
sidecarInjectorWebhook:
  enabled: false
  namespace: istio-system

`,
		},
		{
			desc: "global",
			yamlStr: `
hub: docker.io/istio
tag: 1.2.3
defaultNamespace: istio-system
`,
			want: `
certmanager:
  enabled: false
  namespace: istio-system
citadel:
  enabled: false
  namespace: istio-system
galley:
  enabled: false
  namespace: istio-system
gateways:
  istio-egressgateway:
    enabled: false
    namespace: istio-system
  istio-ingressgateway:
    enabled: false
    namespace: istio-system
global:
  enabled: true
  hub: docker.io/istio
  istioNamespace: istio-system
  configNamespace: istio-system
  policyNamespace: istio-system
  prometheusNamespace: istio-system
  securityNamespace: istio-system
  telemetryNamespace: istio-system
  namespace: istio-system
  tag: 1.2.3
mixer:
  policy:
    enabled: false
    namespace: istio-system
  telemetry:
    enabled: false
    namespace: istio-system
nodeagent:
  enabled: false
  namespace: istio-system
pilot:
  enabled: false
  namespace: istio-system
sidecarInjectorWebhook:
  enabled: false
  namespace: istio-system

`,
		},
		{
			desc: "security",
			yamlStr: `
defaultNamespace: istio-system
security:
  enabled: true
  controlPlaneMtls: true
  dataPlaneMtlsStrict: false
`,
			want: `
certmanager:
  enabled: true
  namespace: istio-system
citadel:
  enabled: true
  namespace: istio-system
galley:
  enabled: false
  namespace: istio-system
gateways:
  istio-egressgateway:
    enabled: false
    namespace: istio-system
  istio-ingressgateway:
    enabled: false
    namespace: istio-system
global:
  controlPlaneSecurityEnabled: true
  enabled: true
  istioNamespace: istio-system
  configNamespace: istio-system
  policyNamespace: istio-system
  prometheusNamespace: istio-system
  securityNamespace: istio-system
  telemetryNamespace: istio-system
  mtls:
    enabled: false
  namespace: istio-system
mixer:
  policy:
    enabled: false
    namespace: istio-system
  telemetry:
    enabled: false
    namespace: istio-system
nodeagent:
  enabled: true
  namespace: istio-system
pilot:
  enabled: false
  namespace: istio-system
sidecarInjectorWebhook:
  enabled: false
  namespace: istio-system
`,
		},
	}

	tr, err := NewTranslator(version.NewMinorVersion(1, 3))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ispec := &v1alpha2.IstioControlPlaneSpec{}
			err := util.UnmarshalWithJSONPB(tt.yamlStr, ispec)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s", tt.desc, err)
			}
			scope.Debugf("ispec: \n%s\n", pretty.Sprint(ispec))
			got, err := tr.ProtoToValues(ispec)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Fatalf("ProtoToValues(%s)(%v): gotErr:%s, wantErr:%s", tt.desc, tt.yamlStr, gotErr, wantErr)
			}
			if want := tt.want; !util.IsYAMLEqual(got, want) {
				t.Errorf("ProtoToValues(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
