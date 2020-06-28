// Copyright 2018 Istio Authors
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

package inject

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
)

func TestIntoResourceFile(t *testing.T) {
	mesh.TestMode = true
	cases := []struct {
		in         string
		want       string
		setFlags   []string
		inFilePath string
		mesh       func(m *meshapi.MeshConfig)
	}{
		//"testdata/hello.yaml" is tested in http_test.go (with debug)
		{
			in:   "hello.yaml",
			want: "hello.yaml.injected",
		},
		// verify cni
		{
			in:   "hello.yaml",
			want: "hello.yaml.cni.injected",
			setFlags: []string{
				"components.cni.enabled=true",
				"values.istio_cni.chained=true",
			},
		},
		{
			in:   "hello-mtls-not-ready.yaml",
			want: "hello-mtls-not-ready.yaml.injected",
		},
		{
			in:   "hello-namespace.yaml",
			want: "hello-namespace.yaml.injected",
		},
		{
			in:   "hello-proxy-override.yaml",
			want: "hello-proxy-override.yaml.injected",
		},
		{
			in:   "hello.yaml",
			want: "hello-tproxy.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.InterceptionMode = meshapi.ProxyConfig_TPROXY
			},
		},
		{
			in:   "hello.yaml",
			want: "hello-config-map-name.yaml.injected",
		},
		{
			in:   "frontend.yaml",
			want: "frontend.yaml.injected",
		},
		{
			in:   "hello-service.yaml",
			want: "hello-service.yaml.injected",
		},
		{
			in:   "hello-multi.yaml",
			want: "hello-multi.yaml.injected",
		},
		{
			in:       "hello.yaml",
			want:     "hello-always.yaml.injected",
			setFlags: []string{"values.global.imagePullPolicy=Always"},
		},
		{
			in:       "hello.yaml",
			want:     "hello-never.yaml.injected",
			setFlags: []string{"values.global.imagePullPolicy=Never"},
		},
		{
			in:   "hello-ignore.yaml",
			want: "hello-ignore.yaml.injected",
		},
		{
			in:   "multi-init.yaml",
			want: "multi-init.yaml.injected",
		},
		{
			in:   "statefulset.yaml",
			want: "statefulset.yaml.injected",
		},
		{
			in:       "enable-core-dump.yaml",
			want:     "enable-core-dump.yaml.injected",
			setFlags: []string{"values.global.proxy.enableCoreDump=true"},
		},
		{
			in:   "enable-core-dump-annotation.yaml",
			want: "enable-core-dump-annotation.yaml.injected",
		},
		{
			in:   "auth.yaml",
			want: "auth.yaml.injected",
		},
		{
			in:   "auth.non-default-service-account.yaml",
			want: "auth.non-default-service-account.yaml.injected",
		},
		{
			in:   "auth.yaml",
			want: "auth.cert-dir.yaml.injected",
		},
		{
			in:   "daemonset.yaml",
			want: "daemonset.yaml.injected",
		},
		{
			in:   "job.yaml",
			want: "job.yaml.injected",
		},
		{
			in:   "replicaset.yaml",
			want: "replicaset.yaml.injected",
		},
		{
			in:   "replicationcontroller.yaml",
			want: "replicationcontroller.yaml.injected",
		},
		{
			in:   "cronjob.yaml",
			want: "cronjob.yaml.injected",
		},
		{
			in:   "pod.yaml",
			want: "pod.yaml.injected",
		},
		{
			in:   "hello-host-network.yaml",
			want: "hello-host-network.yaml.injected",
		},
		{
			in:   "list.yaml",
			want: "list.yaml.injected",
		},
		{
			in:   "list-frontend.yaml",
			want: "list-frontend.yaml.injected",
		},
		{
			in:   "deploymentconfig.yaml",
			want: "deploymentconfig.yaml.injected",
		},
		{
			in:   "deploymentconfig-multi.yaml",
			want: "deploymentconfig-multi.yaml.injected",
		},
		{
			in:   "format-duration.yaml",
			want: "format-duration.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.DrainDuration = types.DurationProto(time.Second * 23)
				m.DefaultConfig.ParentShutdownDuration = types.DurationProto(time.Second * 42)
			},
		},
		{
			// Verifies that parameters are applied properly when no annotations are provided.
			in:   "traffic-params.yaml",
			want: "traffic-params.yaml.injected",
			setFlags: []string{
				`values.global.proxy.includeIPRanges=127.0.0.1/24,10.96.0.1/24`,
				`values.global.proxy.excludeIPRanges=10.96.0.2/24,10.96.0.3/24`,
				`values.global.proxy.excludeInboundPorts=4,5,6`,
				`values.global.proxy.statusPort=0`,
			},
		},
		{
			// Verifies that empty include lists are applied properly from parameters.
			in:   "traffic-params-empty-includes.yaml",
			want: "traffic-params-empty-includes.yaml.injected",
		},
		{
			// Verifies that annotation values are applied properly. This also tests that annotation values
			// override params when specified.
			in:   "traffic-annotations.yaml",
			want: "traffic-annotations.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			in:   "traffic-annotations-wildcards.yaml",
			want: "traffic-annotations-wildcards.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			in:   "traffic-annotations-empty-includes.yaml",
			want: "traffic-annotations-empty-includes.yaml.injected",
		},
		{
			// Verifies that pods can have multiple containers
			in:   "multi-container.yaml",
			want: "multi-container.yaml.injected",
		},
		{
			// Verifies that the status params behave properly.
			in:   "status_params.yaml",
			want: "status_params.yaml.injected",
			setFlags: []string{
				`values.global.proxy.statusPort=123`,
				`values.global.proxy.readinessInitialDelaySeconds=100`,
				`values.global.proxy.readinessPeriodSeconds=200`,
				`values.global.proxy.readinessFailureThreshold=300`,
			},
		},
		{
			// Verifies that the status annotations override the params.
			in:   "status_annotations.yaml",
			want: "status_annotations.yaml.injected",
		},
		{
			// Verifies that the status annotations override the params.
			in:   "status_annotations_zeroport.yaml",
			want: "status_annotations_zeroport.yaml.injected",
		},
		{
			// Verifies that the kubevirtInterfaces list are applied properly from parameters..
			in:   "kubevirtInterfaces.yaml",
			want: "kubevirtInterfaces.yaml.injected",
			setFlags: []string{
				`values.global.proxy.statusPort=123`,
				`values.global.proxy.readinessInitialDelaySeconds=100`,
				`values.global.proxy.readinessPeriodSeconds=200`,
				`values.global.proxy.readinessFailureThreshold=300`,
			},
		},
		{
			// Verifies that the kubevirtInterfaces list are applied properly from parameters..
			in:   "kubevirtInterfaces_list.yaml",
			want: "kubevirtInterfaces_list.yaml.injected",
		},
		{
			// Verifies that global.imagePullSecrets are applied properly
			in:         "hello.yaml",
			want:       "hello-image-secrets-in-values.yaml.injected",
			inFilePath: "hello-image-secrets-in-values-iop.yaml",
		},
		{
			// Verifies that global.imagePullSecrets are appended properly
			in:         "hello-image-pull-secret.yaml",
			want:       "hello-multiple-image-secrets.yaml.injected",
			inFilePath: "hello-image-secrets-in-values-iop.yaml",
		},
		{
			// Verifies that global.podDNSSearchNamespaces are applied properly
			in:         "hello.yaml",
			want:       "hello-template-in-values.yaml.injected",
			inFilePath: "hello-template-in-values-iop.yaml",
		},
		{
			// Verifies that global.mountMtlsCerts is applied properly
			in:       "hello.yaml",
			want:     "hello-mount-mtls-certs.yaml.injected",
			setFlags: []string{`values.global.mountMtlsCerts=true`},
		},
		{
			// Verifies that k8s.v1.cni.cncf.io/networks is set to istio-cni when not chained
			in:   "hello.yaml",
			want: "hello-cncf-networks.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.istio_cni.chained=false`,
			},
		},
		{
			// Verifies that istio-cni is appended to k8s.v1.cni.cncf.io/networks value if set
			in:   "hello-existing-cncf-networks.yaml",
			want: "hello-existing-cncf-networks.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.istio_cni.chained=false`,
			},
		},
	}

	for i, c := range cases {
		c := c
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			m := mesh.DefaultMeshConfig()
			if c.mesh != nil {
				c.mesh(&m)
			}
			sidecarTemplate, valuesConfig := loadInjectionConfigMap(t, c.setFlags, c.inFilePath)
			inputFilePath := "testdata/inject/" + c.in
			wantFilePath := "testdata/inject/" + c.want
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate.Template, valuesConfig, "", &m, in, &got); err != nil {
				t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
			}

			// The version string is a maintenance pain for this test. Strip the version string before comparing.
			gotBytes := got.Bytes()
			wantedBytes := util.ReadGoldenFile(gotBytes, wantFilePath, t)

			wantBytes := util.StripVersion(wantedBytes)
			gotBytes = util.StripVersion(gotBytes)

			util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)

			if util.Refresh() {
				util.RefreshGoldenFile(gotBytes, wantFilePath, t)
			}
		})
	}
}

// TestRewriteAppProbe tests the feature for pilot agent to take over app health check traffic.
func TestRewriteAppProbe(t *testing.T) {
	mesh.TestMode = true
	cases := []struct {
		in                  string
		rewriteAppHTTPProbe bool
		want                string
	}{
		{
			in:                  "hello-probes.yaml",
			rewriteAppHTTPProbe: true,
			want:                "hello-probes.yaml.injected",
		},
		{
			in:                  "hello-readiness.yaml",
			rewriteAppHTTPProbe: true,
			want:                "hello-readiness.yaml.injected",
		},
		{
			in:                  "named_port.yaml",
			rewriteAppHTTPProbe: true,
			want:                "named_port.yaml.injected",
		},
		{
			in:                  "one_container.yaml",
			rewriteAppHTTPProbe: true,
			want:                "one_container.yaml.injected",
		},
		{
			in:                  "two_container.yaml",
			rewriteAppHTTPProbe: true,
			want:                "two_container.yaml.injected",
		},
		{
			in:                  "ready_only.yaml",
			rewriteAppHTTPProbe: true,
			want:                "ready_only.yaml.injected",
		},
		{
			in:                  "https-probes.yaml",
			rewriteAppHTTPProbe: true,
			want:                "https-probes.yaml.injected",
		},
		{
			in:                  "hello-probes-with-flag-set-in-annotation.yaml",
			rewriteAppHTTPProbe: true,
			want:                "hello-probes-with-flag-set-in-annotation.yaml.injected",
		},
		{
			in:                  "hello-probes-with-flag-unset-in-annotation.yaml",
			rewriteAppHTTPProbe: false,
			want:                "hello-probes-with-flag-unset-in-annotation.yaml.injected",
		},
		{
			in:                  "ready_live.yaml",
			rewriteAppHTTPProbe: true,
			want:                "ready_live.yaml.injected",
		},
		// TODO(incfly): add more test case covering different -statusPort=123, --statusPort=123
		// No statusport, --statusPort 123.
	}

	for i, c := range cases {
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		t.Run(testName, func(t *testing.T) {
			m := mesh.DefaultMeshConfig()
			sidecarTemplate, valuesConfig := loadInjectionConfigMap(t, nil, "")
			inputFilePath := "testdata/inject/app_probe/" + c.in
			wantFilePath := "testdata/inject/app_probe/" + c.want
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate.Template, valuesConfig, "", &m, in, &got); err != nil {
				t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
			}

			// The version string is a maintenance pain for this test. Strip the version string before comparing.
			gotBytes := got.Bytes()
			gotBytes = util.StripVersion(gotBytes)

			wantedBytes := util.ReadGoldenFile(gotBytes, wantFilePath, t)
			wantBytes := util.StripVersion(wantedBytes)

			util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)
		})
	}
}

func TestInvalidAnnotations(t *testing.T) {
	cases := []struct {
		annotation string
		in         string
	}{
		{
			annotation: "includeipranges",
			in:         "traffic-annotations-bad-includeipranges.yaml",
		},
		{
			annotation: "excludeipranges",
			in:         "traffic-annotations-bad-excludeipranges.yaml",
		},
		{
			annotation: "includeinboundports",
			in:         "traffic-annotations-bad-includeinboundports.yaml",
		},
		{
			annotation: "excludeinboundports",
			in:         "traffic-annotations-bad-excludeinboundports.yaml",
		},
		{
			annotation: "excludeoutboundports",
			in:         "traffic-annotations-bad-excludeoutboundports.yaml",
		},
	}
	m := mesh.DefaultMeshConfig()
	for _, c := range cases {
		t.Run(c.annotation, func(t *testing.T) {
			sidecarTemplate, valuesConfig := loadInjectionConfigMap(t, nil, "")
			inputFilePath := "testdata/inject/" + c.in
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate.Template, valuesConfig, "", &m, in, &got); err == nil {
				t.Fatalf("expected error")
			} else if !strings.Contains(strings.ToLower(err.Error()), c.annotation) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSkipUDPPorts(t *testing.T) {
	cases := []struct {
		c     corev1.Container
		ports []string
	}{
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{},
			},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 8080,
						Protocol:      corev1.ProtocolTCP,
					},
				},
			},
			ports: []string{"80", "8080"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
			ports: []string{"53"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
			ports: []string{"80"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
		},
	}
	for i := range cases {
		expectPorts := cases[i].ports
		ports := getPortsForContainer(cases[i].c)
		if len(ports) != len(expectPorts) {
			t.Fatalf("unexpect ports result for case %d", i)
		}
		for j := 0; j < len(ports); j++ {
			if ports[j] != expectPorts[j] {
				t.Fatalf("unexpect ports result for case %d: expect %v, got %v", i, expectPorts, ports)
			}
		}
	}
}

func TestCleanProxyConfig(t *testing.T) {
	overrides := mesh.DefaultProxyConfig()
	overrides.ConfigPath = "/foo/bar"
	overrides.DrainDuration = types.DurationProto(7 * time.Second)
	overrides.ProxyMetadata = map[string]string{
		"foo": "barr",
	}
	explicit := mesh.DefaultProxyConfig()
	explicit.ConfigPath = constants.ConfigPathDir
	explicit.DrainDuration = types.DurationProto(45 * time.Second)
	cases := []struct {
		name   string
		proxy  meshapi.ProxyConfig
		expect string
	}{
		{
			"default",
			mesh.DefaultProxyConfig(),
			`{}`,
		},
		{
			"explicit default",
			explicit,
			`{}`,
		},
		{
			"overrides",
			overrides,
			`{"configPath":"/foo/bar","drainDuration":"7s","proxyMetadata":{"foo":"barr"}}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := protoToJSON(&tt.proxy)
			if got != tt.expect {
				t.Fatalf("incorrect output: got %v, expected %v", got, tt.expect)
			}
			roundTrip, err := mesh.ApplyProxyConfig(got, mesh.DefaultMeshConfig())
			if err != nil {
				t.Fatal(err)
			}
			if !cmp.Equal(*roundTrip.GetDefaultConfig(), tt.proxy) {
				t.Fatalf("round trip is not identical: got \n%+v, expected \n%+v", *roundTrip.GetDefaultConfig(), tt.proxy)
			}
		})
	}
}
