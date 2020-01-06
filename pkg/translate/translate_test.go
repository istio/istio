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
cni:
  components:
    cni:
      namespace: kube-system
`,
			want: `certmanager:
  enabled: false
  namespace: istio-system
cni:
  namespace: kube-system
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
  configNamespace: istio-system
  enabled: false
  istioNamespace: istio-system
  namespace: istio-system
  policyNamespace: istio-system
  prometheusNamespace: istio-system
  securityNamespace: istio-system
  telemetryNamespace: istio-system
grafana:
  enabled: false
  namespace: istio-system
istio_cni:
  enabled: false
kiali:
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
  enabled: false
  namespace: istio-system
pilot:
  enabled: false
  namespace: istio-system
prometheus:
  enabled: false
  namespace: istio-system
security:
  enabled: false
  namespace: istio-system
sidecarInjectorWebhook:
  enabled: false
  namespace: istio-system
tracing:
  jaeger:
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
cni:
  components:
    cni:
      namespace: kube-system
`,
			want: `certmanager:
  enabled: false
  namespace: istio-system
cni:
  namespace: kube-system
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
  configNamespace: istio-system
  enabled: false
  hub: docker.io/istio
  istioNamespace: istio-system
  namespace: istio-system
  policyNamespace: istio-system
  prometheusNamespace: istio-system
  securityNamespace: istio-system
  tag: 1.2.3
  telemetryNamespace: istio-system
grafana:
  enabled: false
  namespace: istio-system
istio_cni:
  enabled: false
kiali:
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
  enabled: false
  namespace: istio-system
pilot:
  enabled: false
  namespace: istio-system
prometheus:
  enabled: false
  namespace: istio-system
security:
  enabled: false
  namespace: istio-system
sidecarInjectorWebhook:
  enabled: false
  namespace: istio-system
tracing:
  jaeger:
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

func TestNewTranslator(t *testing.T) {
	tests := []struct {
		name         string
		minorVersion version.MinorVersion
		wantVer      string
		wantErr      bool
	}{
		{
			name:         "version 1.3",
			minorVersion: version.NewMinorVersion(1, 3),
			wantVer:      "1.3",
			wantErr:      false,
		},
		{
			name:         "version 1.4",
			minorVersion: version.NewMinorVersion(1, 4),
			wantVer:      "1.4",
			wantErr:      false,
		},
		{
			name:         "version 1.5",
			minorVersion: version.NewMinorVersion(1, 5),
			wantVer:      "1.5",
			wantErr:      false,
		},
		{
			name:         "version 1.6",
			minorVersion: version.NewMinorVersion(1, 6),
			wantVer:      "1.5",
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTranslator(tt.minorVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTranslator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != nil && tt.wantVer != got.Version.String() {
				t.Errorf("NewTranslator() got = %v, want %v", got.Version.String(), tt.wantVer)
			}
		})
	}
}
