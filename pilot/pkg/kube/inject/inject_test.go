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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	"fmt"

	"regexp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
)

const (
	// This is the hub to expect in
	// platform/kube/inject/testdata/frontend.yaml.injected and the
	// other .injected "want" YAMLs
	unitTestHub = "docker.io/istio"

	// Tag name should be kept in sync with value in
	// platform/kube/inject/refresh.sh
	unitTestTag = "unittest"

	statusReplacement = "sidecar.istio.io/status: '{\"version\":\"\","
)

var (
	statusPattern = regexp.MustCompile("sidecar.istio.io/status: '{\"version\":\"([0-9a-f]+)\",")
)

func TestImageName(t *testing.T) {
	want := "docker.io/istio/proxy_init:latest"
	if got := InitImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("InitImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxy_debug:latest"
	if got := ProxyImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("ProxyImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxyv2:latest"
	if got := ProxyImageName("docker.io/istio", "latest", false); got != want {
		t.Errorf("ProxyImageName(debug:false) failed: got %q want %q", got, want)
	}
}

func TestIntoResourceFile(t *testing.T) {
	cases := []struct {
		enableAuth                   bool
		in                           string
		want                         string
		imagePullPolicy              string
		enableCoreDump               bool
		debugMode                    bool
		privileged                   bool
		duration                     time.Duration
		includeIPRanges              string
		excludeIPRanges              string
		includeInboundPorts          string
		excludeInboundPorts          string
		statusPort                   int
		readinessPath                string
		readinessInitialDelaySeconds uint32
		readinessPeriodSeconds       uint32
		readinessFailureThreshold    uint32
		tproxy                       bool
	}{
		// "testdata/hello.yaml" is tested in http_test.go (with debug)
		{
			in:                           "hello.yaml",
			want:                         "hello.yaml.injected",
			debugMode:                    true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-namespace.yaml",
			want:                         "hello-namespace.yaml.injected",
			debugMode:                    true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-proxy-override.yaml",
			want:                         "hello-proxy-override.yaml.injected",
			debugMode:                    true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:     "hello.yaml",
			want:   "hello-tproxy.yaml.injected",
			tproxy: true,
		},
		{
			in:        "hello.yaml",
			want:      "hello-tproxy-debug.yaml.injected",
			debugMode: true,
			tproxy:    true,
		},
		{
			in:                           "hello-probes.yaml",
			want:                         "hello-probes.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello.yaml",
			want:                         "hello-config-map-name.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "frontend.yaml",
			want:                         "frontend.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-service.yaml",
			want:                         "hello-service.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-multi.yaml",
			want:                         "hello-multi.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello.yaml",
			want:                         "hello-always.yaml.injected",
			imagePullPolicy:              "Always",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello.yaml",
			want:                         "hello-never.yaml.injected",
			imagePullPolicy:              "Never",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-ignore.yaml",
			want:                         "hello-ignore.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "multi-init.yaml",
			want:                         "multi-init.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "statefulset.yaml",
			want:                         "statefulset.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "enable-core-dump.yaml",
			want:                         "enable-core-dump.yaml.injected",
			enableCoreDump:               true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "auth.yaml",
			want:                         "auth.yaml.injected",
			enableAuth:                   true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "auth.non-default-service-account.yaml",
			want:                         "auth.non-default-service-account.yaml.injected",
			enableAuth:                   true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "auth.yaml",
			want:                         "auth.cert-dir.yaml.injected",
			enableAuth:                   true,
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "daemonset.yaml",
			want:                         "daemonset.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "job.yaml",
			want:                         "job.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "replicaset.yaml",
			want:                         "replicaset.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "replicationcontroller.yaml",
			want:                         "replicationcontroller.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "cronjob.yaml",
			want:                         "cronjob.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "pod.yaml",
			want:                         "pod.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "hello-host-network.yaml",
			want:                         "hello-host-network.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "list.yaml",
			want:                         "list.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "list-frontend.yaml",
			want:                         "list-frontend.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "deploymentconfig.yaml",
			want:                         "deploymentconfig.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "deploymentconfig-multi.yaml",
			want:                         "deploymentconfig-multi.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			in:                           "format-duration.yaml",
			want:                         "format-duration.yaml.injected",
			duration:                     time.Duration(42 * time.Second),
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			// Verifies that parameters are applied properly when no annotations are provided.
			in:                  "traffic-params.yaml",
			want:                "traffic-params.yaml.injected",
			includeIPRanges:     "127.0.0.1/24,10.96.0.1/24",
			excludeIPRanges:     "10.96.0.2/24,10.96.0.3/24",
			includeInboundPorts: "1,2,3",
			excludeInboundPorts: "4,5,6",
			statusPort:          0,
		},
		{
			// Verifies that empty include lists are applied properly from parameters.
			in:                           "traffic-params-empty-includes.yaml",
			want:                         "traffic-params-empty-includes.yaml.injected",
			includeIPRanges:              "",
			excludeIPRanges:              "",
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			// Verifies that annotation values are applied properly. This also tests that annotation values
			// override params when specified.
			in:                           "traffic-annotations.yaml",
			want:                         "traffic-annotations.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			in:                           "traffic-annotations-wildcards.yaml",
			want:                         "traffic-annotations-wildcards.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			in:                           "traffic-annotations-empty-includes.yaml",
			want:                         "traffic-annotations-empty-includes.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
		{
			// Verifies that the status params behave properly.
			in:                           "status_params.yaml",
			want:                         "status_params.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   123,
			readinessInitialDelaySeconds: 100,
			readinessPeriodSeconds:       200,
			readinessFailureThreshold:    300,
		},
		{
			// Verifies that the status annotations override the params.
			in:                           "status_annotations.yaml",
			want:                         "status_annotations.yaml.injected",
			includeIPRanges:              DefaultIncludeIPRanges,
			includeInboundPorts:          DefaultIncludeInboundPorts,
			statusPort:                   DefaultStatusPort,
			readinessInitialDelaySeconds: DefaultReadinessInitialDelaySeconds,
			readinessPeriodSeconds:       DefaultReadinessPeriodSeconds,
			readinessFailureThreshold:    DefaultReadinessFailureThreshold,
		},
	}

	for i, c := range cases {
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		t.Run(testName, func(t *testing.T) {
			mesh := model.DefaultMeshConfig()
			if c.enableAuth {
				mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
			}
			if c.duration != 0 {
				mesh.DefaultConfig.DrainDuration = types.DurationProto(c.duration)
				mesh.DefaultConfig.ParentShutdownDuration = types.DurationProto(c.duration)
				mesh.DefaultConfig.ConnectTimeout = types.DurationProto(c.duration)
			}
			if c.tproxy {
				mesh.DefaultConfig.InterceptionMode = meshconfig.ProxyConfig_TPROXY
			} else {
				mesh.DefaultConfig.InterceptionMode = meshconfig.ProxyConfig_REDIRECT
			}

			params := &Params{
				InitImage:                    InitImageName(unitTestHub, unitTestTag, c.debugMode),
				ProxyImage:                   ProxyImageName(unitTestHub, unitTestTag, c.debugMode),
				ImagePullPolicy:              "IfNotPresent",
				Verbosity:                    DefaultVerbosity,
				SidecarProxyUID:              DefaultSidecarProxyUID,
				Version:                      "12345678",
				EnableCoreDump:               c.enableCoreDump,
				Privileged:                   c.privileged,
				Mesh:                         &mesh,
				DebugMode:                    c.debugMode,
				IncludeIPRanges:              c.includeIPRanges,
				ExcludeIPRanges:              c.excludeIPRanges,
				IncludeInboundPorts:          c.includeInboundPorts,
				ExcludeInboundPorts:          c.excludeInboundPorts,
				StatusPort:                   c.statusPort,
				ReadinessInitialDelaySeconds: c.readinessInitialDelaySeconds,
				ReadinessPeriodSeconds:       c.readinessPeriodSeconds,
				ReadinessFailureThreshold:    c.readinessFailureThreshold,
			}
			if c.imagePullPolicy != "" {
				params.ImagePullPolicy = c.imagePullPolicy
			}
			sidecarTemplate, err := GenerateTemplateFromParams(params)
			if err != nil {
				t.Fatalf("GenerateTemplateFromParams(%v) failed: %v", params, err)
			}
			inputFilePath := "testdata/inject/" + c.in
			wantFilePath := "testdata/inject/" + c.want
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate, &mesh, in, &got); err != nil {
				t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
			}

			// The version string is a maintenance pain for this test. Strip the version string before comparing.
			wantBytes := stripVersion(util.ReadFile(wantFilePath, t))
			gotBytes := stripVersion(got.Bytes())

			util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)
		})
	}
}

func stripVersion(yaml []byte) []byte {
	return statusPattern.ReplaceAllLiteral(yaml, []byte(statusReplacement))
}

func TestInvalidParams(t *testing.T) {
	cases := []struct {
		annotation    string
		paramModifier func(p *Params)
	}{
		{
			annotation: "includeipranges",
			paramModifier: func(p *Params) {
				p.IncludeIPRanges = "bad"
			},
		},
		{
			annotation: "excludeipranges",
			paramModifier: func(p *Params) {
				p.ExcludeIPRanges = "*"
			},
		},
		{
			annotation: "includeinboundports",
			paramModifier: func(p *Params) {
				p.IncludeInboundPorts = "bad"
			},
		},
		{
			annotation: "excludeinboundports",
			paramModifier: func(p *Params) {
				p.ExcludeInboundPorts = "*"
			},
		},
	}

	for _, c := range cases {
		t.Run(c.annotation, func(t *testing.T) {
			params := newTestParams()
			c.paramModifier(params)

			if _, err := GenerateTemplateFromParams(params); err == nil {
				t.Fatalf("expected error")
			} else if !strings.Contains(strings.ToLower(err.Error()), c.annotation) {
				t.Fatalf("unexpected error: %v", err)
			}
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
	}

	for _, c := range cases {
		t.Run(c.annotation, func(t *testing.T) {
			params := newTestParams()
			sidecarTemplate, err := GenerateTemplateFromParams(params)
			if err != nil {
				t.Fatalf("GenerateTemplateFromParams(%v) failed: %v", params, err)
			}
			inputFilePath := "testdata/inject/" + c.in
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate, params.Mesh, in, &got); err == nil {
				t.Fatalf("expected error")
			} else if !strings.Contains(strings.ToLower(err.Error()), c.annotation) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func newTestParams() *Params {
	mesh := model.DefaultMeshConfig()
	return &Params{
		InitImage:           InitImageName(unitTestHub, unitTestTag, false),
		ProxyImage:          ProxyImageName(unitTestHub, unitTestTag, false),
		ImagePullPolicy:     "IfNotPresent",
		Verbosity:           DefaultVerbosity,
		SidecarProxyUID:     DefaultSidecarProxyUID,
		Version:             "12345678",
		EnableCoreDump:      false,
		Mesh:                &mesh,
		DebugMode:           false,
		IncludeIPRanges:     DefaultIncludeIPRanges,
		ExcludeIPRanges:     "",
		IncludeInboundPorts: DefaultIncludeInboundPorts,
		ExcludeInboundPorts: "",
	}
}
