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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

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
		in       string
		want     string
		duration time.Duration
		tproxy   bool
	}{
		// "testdata/hello.yaml" is tested in http_test.go (with debug)
		{
			in:   "hello.yaml",
			want: "hello.yaml.injected",
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
			in:     "hello.yaml",
			want:   "hello-tproxy.yaml.injected",
			tproxy: true,
		},
		{
			in:   "hello-probes.yaml",
			want: "hello-probes.yaml.injected",
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
			in:   "hello-ignore.yaml",
			want: "hello-ignore.yaml.injected",
		},
		{
			in:   "hello-readiness.yaml",
			want: "hello-readiness.yaml.injected",
		},
		{
			in:   "hello-readiness-multi.yaml",
			want: "hello-readiness-multi.yaml.injected",
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
			in:       "format-duration.yaml",
			want:     "format-duration.yaml.injected",
			duration: 42 * time.Second,
		},
		{
			// Verifies that annotation values are applied properly.
			in:   "traffic-annotations.yaml",
			want: "traffic-annotations.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			in:   "traffic-annotations-wildcards.yaml",
			want: "traffic-annotations-wildcards.yaml.injected",
		},
		{
			// Verifies that the empty value behaves properly when used in annotations.
			in:   "traffic-annotations-empty-includes.yaml",
			want: "traffic-annotations-empty-includes.yaml.injected",
		},
		{
			// Verifies that the status annotation values are applied properly.
			in:   "status_annotations.yaml",
			want: "status_annotations.yaml.injected",
		},
	}

	for i, c := range cases {
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		t.Run(testName, func(t *testing.T) {
			mesh := model.DefaultMeshConfig()
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
			sidecarTemplate := loadConfigMapWithHelm(t)
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
			gotBytes := got.Bytes()
			wantedBytes := util.ReadGoldenFile(gotBytes, wantFilePath, t)

			wantBytes := stripVersion(wantedBytes)
			gotBytes = stripVersion(gotBytes)

			//ioutil.WriteFile(wantFilePath, gotBytes, 0644)

			util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)
		})
	}
}

func stripVersion(yaml []byte) []byte {
	return statusPattern.ReplaceAllLiteral(yaml, []byte(statusReplacement))
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
			mesh := model.DefaultMeshConfig()
			sidecarTemplate := loadConfigMapWithHelm(t)
			inputFilePath := "testdata/inject/" + c.in
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			defer func() { _ = in.Close() }()
			var got bytes.Buffer
			if err = IntoResourceFile(sidecarTemplate, &mesh, in, &got); err == nil {
				t.Fatalf("expected error")
			} else if !strings.Contains(strings.ToLower(err.Error()), c.annotation) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
