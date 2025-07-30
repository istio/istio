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

package validation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/apis/validation"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

// nolint: lll
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name     string
		value    *apis.IstioOperatorSpec
		values   string
		errors   error
		warnings validation.Warnings
	}{
		{
			name: "unset target port",
			values: `
spec:
  components:
    ingressGateways:
      - name: istio-ingressgateway
        enabled: true
      - name: cluster-local-gateway
        enabled: true
        k8s:
          service:
            type: ClusterIP
            ports:
            - port: 15020
              name: status-port
            - port: 80
              name: http2
`,
			errors: fmt.Errorf(`port http2/80 in gateway cluster-local-gateway invalid: targetPort is set to 0, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`),
		},
		{
			name: "explicitly invalid target port",
			values: `
spec:
  components:
    ingressGateways:
      - name: istio-ingressgateway
        enabled: true
      - name: cluster-local-gateway
        enabled: true
        k8s:
          service:
            type: ClusterIP
            ports:
            - port: 15020
              name: status-port
            - port: 80
              name: http2
              targetPort: 90
`,
			errors: fmt.Errorf(`port http2/80 in gateway cluster-local-gateway invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`),
		},
		{
			name: "explicitly invalid target port for egress",
			values: `
spec:
  components:
    egressGateways:
      - name: egress-gateway
        enabled: true
        k8s:
          service:
            type: ClusterIP
            ports:
            - port: 15020
              name: status-port
            - port: 80
              name: http2
              targetPort: 90
`,
			errors: fmt.Errorf(`port http2/80 in gateway egress-gateway invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-egressgateway.runAsRoot=true`),
		},
		{
			name: "low target port with root",
			values: `
spec:
  components:
    ingressGateways:
      - name: istio-ingressgateway
        enabled: true
      - name: cluster-local-gateway
        enabled: true
        k8s:
          service:
            type: ClusterIP
            ports:
            - port: 15020
              name: status-port
            - port: 80
              name: http2
              targetPort: 90
  values:
    gateways:
      istio-ingressgateway:
        runAsRoot: true
`,
			errors: nil,
		},
		{
			name: "legacy values ports config empty targetPort",
			values: `
spec:
  values:
    gateways:
      istio-ingressgateway:
        ingressPorts:
        - name: http
          port: 80
`,
			errors: fmt.Errorf(`port 80 is invalid: targetPort is set to 0, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`),
		},
		{
			name: "legacy values ports config explicit targetPort",
			values: `
spec:
  values:
    gateways:
      istio-ingressgateway:
        ingressPorts:
        - name: http
          port: 80
          targetPort: 90
`,
			errors: fmt.Errorf(`port 80 is invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`),
		},
		{
			name: "legacy values ports valid",
			values: `
spec:
  values:
    gateways:
      istio-ingressgateway:
        ingressPorts:
        - name: http
          port: 80
          targetPort: 8080
`,
			errors: nil,
		},
		{
			name: "replicaCount set when autoscaleEnabled is true",
			values: `
spec:
  values:
    pilot:
      autoscaleEnabled: true
    gateways:
      istio-ingressgateway:
        autoscaleEnabled: true
      istio-egressgateway:
        autoscaleEnabled: true
  components:
    pilot:
      k8s:
        replicaCount: 2
    ingressGateways:
      - name: istio-ingressgateway
        enabled: true
        k8s:
          replicaCount: 2
    egressGateways:
      - name: istio-egressgateway
        enabled: true
        k8s:
          replicaCount: 2
`,
			warnings: validation.Warnings{
				errors.New(`components.pilot.k8s.replicaCount should not be set when values.pilot.autoscaleEnabled is true`),
				errors.New(`components.ingressGateways[name=istio-ingressgateway].k8s.replicaCount should not be set when values.gateways.istio-ingressgateway.autoscaleEnabled is true`),
				errors.New(`components.egressGateways[name=istio-egressgateway].k8s.replicaCount should not be set when values.gateways.istio-egressgateway.autoscaleEnabled is true`),
			},
		},
		{
			name: "pilot.k8s.replicaCount is default value set when autoscaleEnabled is true",
			values: `
spec:
  values:
    pilot:
      autoscaleEnabled: true
    gateways:
      istio-ingressgateway:
        autoscaleEnabled: true
      istio-egressgateway:
        autoscaleEnabled: true
  components:
    pilot:
      k8s:
        replicaCount: 1
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := values.MapFromYaml([]byte(tt.values))
			assert.NoError(t, err)
			warnings, errors := validation.ParseAndValidateIstioOperator(m, nil)
			assert.Equal(t, tt.errors, errors.ToError(), "errors")
			assert.Equal(t, tt.warnings.ToError(), warnings.ToError(), "warnings")
		})
	}
}

// nolint: lll
func TestValidateValues(t *testing.T) {
	tests := []struct {
		desc        string
		yamlStr     string
		wantErrs    util.Errors
		wantWarning util.Errors
	}{
		{
			desc: "nil success",
		},
		{
			desc: "StarIPRange",
			yamlStr: `
global:
  proxy:
    includeIPRanges: "*"
    excludeIPRanges: "*"
`,
		},
		{
			desc: "ProxyConfig",
			yamlStr: `
global:
  podDNSSearchNamespaces:
  - "my-namespace"
  proxy:
    includeIPRanges: "1.1.0.0/16,2.2.0.0/16"
    excludeIPRanges: "3.3.0.0/16,4.4.0.0/16"
    excludeInboundPorts: "333,444"
    clusterDomain: "my.domain"
    lifecycle:
      preStop:
        exec:
          command: ["/bin/sh", "-c", "sleep 30"]
`,
		},
		{
			desc: "CNIConfig",
			yamlStr: `
cni:
  cniBinDir: "/var/lib/cni/bin"
  cniConfDir: "/var/run/multus/cni/net.d"
`,
		},
		{
			desc: "CNIReconcileIptablesOnStartup",
			yamlStr: `
cni:
  ambient:
    enabled: true
    reconcileIptablesOnStartup: true
`,
		},
		{
			desc: "BadIPRange",
			yamlStr: `
global:
  proxy:
    includeIPRanges: "1.1.0.256/16,2.2.0.257/16"
    excludeIPRanges: "3.3.0.0/33,4.4.0.0/34"
`,
			wantErrs: makeErrors([]string{
				`global.proxy.excludeIPRanges netip.ParsePrefix("3.3.0.0/33"): prefix length out of range`,
				`global.proxy.excludeIPRanges netip.ParsePrefix("4.4.0.0/34"): prefix length out of range`,
				`global.proxy.includeIPRanges netip.ParsePrefix("1.1.0.256/16"): ParseAddr("1.1.0.256"): IPv4 field has value >255`,
				`global.proxy.includeIPRanges netip.ParsePrefix("2.2.0.257/16"): ParseAddr("2.2.0.257"): IPv4 field has value >255`,
			}),
		},
		{
			desc: "BadIPMalformed",
			yamlStr: `
global:
  proxy:
    includeIPRanges: "1.2.3/16,1.2.3.x/16"
`,
			wantErrs: makeErrors([]string{
				`global.proxy.includeIPRanges netip.ParsePrefix("1.2.3/16"): ParseAddr("1.2.3"): IPv4 address too short`,
				`global.proxy.includeIPRanges netip.ParsePrefix("1.2.3.x/16"): ParseAddr("1.2.3.x"): unexpected character (at "x")`,
			}),
		},
		{
			desc: "BadIPWithStar",
			yamlStr: `
global:
  proxy:
    includeIPRanges: "*,1.1.0.0/16,2.2.0.0/16"
`,
			wantErrs: makeErrors([]string{`global.proxy.includeIPRanges netip.ParsePrefix("*"): no '/'`}),
		},
		{
			desc: "BadPortRange",
			yamlStr: `
global:
  proxy:
    excludeInboundPorts: "-1,444"
`,
			wantErrs: makeErrors([]string{`value global.proxy.excludeInboundPorts:-1 falls outside range [0, 65535]`}),
		},
		{
			desc: "BadPortMalformed",
			yamlStr: `
global:
  proxy:
    excludeInboundPorts: "111,222x"
`,
			wantErrs: makeErrors([]string{`global.proxy.excludeInboundPorts : strconv.ParseInt: parsing "222x": invalid syntax`}),
		},
		{
			desc: "unknown field",
			yamlStr: `
global:
  proxy:
    foo: "bar"
`,
			wantErrs: makeErrors([]string{`could not unmarshal: error unmarshaling JSON: while decoding JSON: unknown field "foo" in istio.operator.v1alpha1.ProxyConfig`}),
		},
		{
			desc: "unknown cni field",
			yamlStr: `
cni:
  foo: "bar"
`,
			wantErrs: makeErrors([]string{`could not unmarshal: error unmarshaling JSON: while decoding JSON: unknown field "foo" in istio.operator.v1alpha1.CNIConfig`}),
		},
		{
			desc: "nativeNftables with distroless",
			yamlStr: `
global:
  variant: "distroless"
  nativeNftables: true
`,
			wantWarning: makeErrors([]string{
				"nativeNftables is enabled, but the image is distroless. The 'nft' CLI binary is not available in distroless images, which is required for nativeNftables to work. Please either disable nativeNftables or use a non-distroless image",
			}),
		},
		{
			desc: "nativeNftables with debug image",
			yamlStr: `
global:
  variant: "debug"
  nativeNftables: true
`,
		},
		{
			desc: "nativeNftables disabled with distroless",
			yamlStr: `
global:
  variant: "distroless"
  nativeNftables: false
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m, err := values.MapFromYaml([]byte(tt.yamlStr))
			assert.NoError(t, err)
			warnings, errs := validation.ParseAndValidateIstioOperator(values.MakeMap(m, "spec", "values"), nil)
			if gotErr, wantErr := errs, tt.wantErrs; !util.EqualErrors(gotErr, wantErr) {
				t.Errorf("CheckValues(%s)(%v): gotErr:%s, wantErr:%s", tt.desc, tt.yamlStr, gotErr, wantErr)
			}
			if gotWarning, wantWarning := warnings, tt.wantWarning; !util.EqualErrors(gotWarning, wantWarning) {
				t.Errorf("CheckValues(%s)(%v): gotWarning:%s, wantWarning:%s", tt.desc, tt.yamlStr, gotWarning, wantWarning)
			}
		})
	}
}

// nolint: lll
func TestDetectCniIncompatibility(t *testing.T) {
	tests := []struct {
		desc         string
		ioYamlStr    string
		calicoConfig *unstructured.Unstructured
		ciliumConfig *v1.ConfigMap
		wantWarnings util.Errors
	}{
		{
			desc: "Calico bpfConnectTimeLoadBalancing TCP",
			calicoConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "projectcalico.org/v3",
					"kind":       "FelixConfiguration",
					"metadata": map[string]interface{}{
						"name": "default",
					},
					"spec": map[string]interface{}{
						"bpfConnectTimeLoadBalancing": "TCP",
					},
				},
			},

			wantWarnings: makeErrors([]string{`detected Calico CNI with 'bpfConnectTimeLoadBalancing=TCP'; this must be set to 'bpfConnectTimeLoadBalancing=Disabled' in the Calico configuration`}),
		},
		{
			desc: "Calico bpfConnectTimeLoadBalancingEnabled true",
			calicoConfig: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "projectcalico.org/v3",
					"kind":       "FelixConfiguration",
					"metadata": map[string]interface{}{
						"name": "default",
					},
					"spec": map[string]interface{}{
						"bpfConnectTimeLoadBalancingEnabled": true,
					},
				},
			},

			wantWarnings: makeErrors([]string{`detected Calico CNI with 'bpfConnectTimeLoadBalancingEnabled=true'; this must be set to 'bpfConnectTimeLoadBalancingEnabled=false' in the Calico configuration`}),
		},
		{
			desc: "Cilium enable-bpf-masquerade true",
			ioYamlStr: `
spec:
  components:
    ztunnel:
      enabled: true
`,
			ciliumConfig: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cilium-config"},
				Data: map[string]string{
					"enable-bpf-masquerade": "true",
				},
			},
			wantWarnings: makeErrors([]string{`detected Cilium CNI with 'enable-bpf-masquerade=true'; this must be set to 'false' when using ambient mode`}),
		},
		{
			desc: "Cilium bpf-lb-sock=true + bpf-lb-sock-hostns-only=false => error",
			ioYamlStr: `
spec:
  components:
    cni:
      enabled: true
`,
			ciliumConfig: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cilium-config"},
				Data: map[string]string{
					"bpf-lb-sock":             "true",
					"bpf-lb-sock-hostns-only": "false",
				},
			},
			wantWarnings: makeErrors([]string{
				"detected Cilium CNI with 'bpf-lb-sock=true'; this requires 'bpf-lb-sock-hostns-only=true' to be set",
			}),
		},
		{
			desc: "Cilium kube-proxy-replacement=strict + bpf-lb-sock-hostns-only=false => error",
			ioYamlStr: `
spec:
  components:
    cni:
      enabled: true
`,
			ciliumConfig: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cilium-config"},
				Data: map[string]string{
					"kube-proxy-replacement":  "strict",
					"bpf-lb-sock-hostns-only": "false",
				},
			},
			wantWarnings: makeErrors([]string{
				"detected Cilium CNI with 'kube-proxy-replacement=strict' and 'bpf-lb-sock-hostns-only=false'; please set 'bpf-lb-sock-hostns-only=true' to avoid conflicts with Istio",
			}),
		},
		{
			desc: "Cilium kube-proxy-replacement=strict + bpf-lb-sock-hostns-only=true => no error",
			ioYamlStr: `
spec:
  components:
    cni:
      enabled: true
`,
			ciliumConfig: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cilium-config"},
				Data: map[string]string{
					"kube-proxy-replacement":  "strict",
					"bpf-lb-sock-hostns-only": "true",
				},
			},
			wantWarnings: nil,
		},
	}

	calicoResource := schema.GroupVersionResource{Group: "projectcalico.org", Version: "v3", Resource: "felixconfigurations"}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			m, err := values.MapFromYaml([]byte(tt.ioYamlStr))
			assert.NoError(t, err)

			client := kube.NewFakeClient()
			if tt.calicoConfig != nil {
				client.Dynamic().Resource(calicoResource).Create(context.Background(), tt.calicoConfig, metav1.CreateOptions{})
			}
			if tt.ciliumConfig != nil {
				client.Kube().CoreV1().ConfigMaps("kube-system").Create(context.Background(), tt.ciliumConfig, metav1.CreateOptions{})
			}
			warnings, _ := validation.ParseAndValidateIstioOperator(m, client)
			if gotWarnings, wantWarnings := warnings, tt.wantWarnings; !util.EqualErrors(gotWarnings, wantWarnings) {
				t.Errorf("CheckValues(%s): gotWarnings:%s, wantWarnings:%s", tt.desc, gotWarnings, wantWarnings)
			}
		})
	}
}

func makeErrors(estr []string) util.Errors {
	var errs util.Errors
	for _, s := range estr {
		errs = util.AppendErr(errs, fmt.Errorf("%s", s))
	}
	return errs
}
