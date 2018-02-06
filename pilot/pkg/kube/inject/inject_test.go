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
	"testing"

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
	want = "docker.io/istio/proxy:latest"
	if got := ProxyImageName("docker.io/istio", "latest", false); got != want {
		t.Errorf("ProxyImageName(debug:false) failed: got %q want %q", got, want)
	}
}

func TestIntoResourceFile(t *testing.T) {
	cases := []struct {
		enableAuth      bool
		in              string
		want            string
		imagePullPolicy string
		enableCoreDump  bool
		debugMode       bool
	}{
		// "testdata/hello.yaml" is tested in http_test.go (with debug)
		{
			in:        "testdata/hello.yaml",
			want:      "testdata/hello.yaml.injected",
			debugMode: true,
		},
		{
			in:   "testdata/hello-probes.yaml",
			want: "testdata/hello-probes.yaml.injected",
		},
		{
			in:   "testdata/hello.yaml",
			want: "testdata/hello-config-map-name.yaml.injected",
		},
		{
			in:   "testdata/frontend.yaml",
			want: "testdata/frontend.yaml.injected",
		},
		{
			in:   "testdata/hello-service.yaml",
			want: "testdata/hello-service.yaml.injected",
		},
		{
			in:   "testdata/hello-multi.yaml",
			want: "testdata/hello-multi.yaml.injected",
		},
		{
			imagePullPolicy: "Always",
			in:              "testdata/hello.yaml",
			want:            "testdata/hello-always.yaml.injected",
		},
		{
			imagePullPolicy: "Never",
			in:              "testdata/hello.yaml",
			want:            "testdata/hello-never.yaml.injected",
		},
		{
			in:   "testdata/hello-ignore.yaml",
			want: "testdata/hello-ignore.yaml.injected",
		},
		{
			in:   "testdata/multi-init.yaml",
			want: "testdata/multi-init.yaml.injected",
		},
		{
			in:   "testdata/statefulset.yaml",
			want: "testdata/statefulset.yaml.injected",
		},
		{
			in:             "testdata/enable-core-dump.yaml",
			want:           "testdata/enable-core-dump.yaml.injected",
			enableCoreDump: true,
		},
		{
			enableAuth: true,
			in:         "testdata/auth.yaml",
			want:       "testdata/auth.yaml.injected",
		},
		{
			enableAuth: true,
			in:         "testdata/auth.non-default-service-account.yaml",
			want:       "testdata/auth.non-default-service-account.yaml.injected",
		},
		{
			enableAuth: true,
			in:         "testdata/auth.yaml",
			want:       "testdata/auth.cert-dir.yaml.injected",
		},
		{
			in:   "testdata/daemonset.yaml",
			want: "testdata/daemonset.yaml.injected",
		},
		{
			in:   "testdata/job.yaml",
			want: "testdata/job.yaml.injected",
		},
		{
			in:   "testdata/replicaset.yaml",
			want: "testdata/replicaset.yaml.injected",
		},
		{
			in:   "testdata/replicationcontroller.yaml",
			want: "testdata/replicationcontroller.yaml.injected",
		},
		{
			in:   "testdata/cronjob.yaml",
			want: "testdata/cronjob.yaml.injected",
		},
		{
			in:   "testdata/hello-host-network.yaml",
			want: "testdata/hello-host-network.yaml.injected",
		},
		{
			in:   "testdata/list.yaml",
			want: "testdata/list.yaml.injected",
		},
		{
			in:   "testdata/list-frontend.yaml",
			want: "testdata/list-frontend.yaml.injected",
		},
	}

	for _, c := range cases {
		mesh := model.DefaultMeshConfig()
		if c.enableAuth {
			mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
		}

		params := &Params{
			InitImage:       InitImageName(unitTestHub, unitTestTag, c.debugMode),
			ProxyImage:      ProxyImageName(unitTestHub, unitTestTag, c.debugMode),
			ImagePullPolicy: "IfNotPresent",
			Verbosity:       DefaultVerbosity,
			SidecarProxyUID: DefaultSidecarProxyUID,
			Version:         "12345678",
			EnableCoreDump:  c.enableCoreDump,
			Mesh:            &mesh,
			DebugMode:       c.debugMode,
		}
		if c.imagePullPolicy != "" {
			params.ImagePullPolicy = c.imagePullPolicy
		}
		sidecarTemplate, err := GenerateTemplateFromParams(params)
		if err != nil {
			t.Fatalf("GenerateTemplateFromParams(%v) failed: %v", params, err)
		}
		in, err := os.Open(c.in)
		if err != nil {
			t.Fatalf("Failed to open %q: %v", c.in, err)
		}
		defer func() { _ = in.Close() }()
		var got bytes.Buffer
		if err = IntoResourceFile(sidecarTemplate, &mesh, in, &got); err != nil {
			t.Fatalf("IntoResourceFile(%v) returned an error: %v", c.in, err)
		}

		util.CompareContent(got.Bytes(), c.want, t)
	}
}
