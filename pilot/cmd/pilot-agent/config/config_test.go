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

package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
)

func TestGetMeshConfig(t *testing.T) {
	meshOverride := `
defaultConfig:
  discoveryAddress: foo:123
  controlPlaneAuthPolicy: NONE
  proxyMetadata:
    SOME: setting
  drainDuration: 1s`
	proxyOverride := `discoveryAddress: foo:123
proxyMetadata:
  SOME: setting
drainDuration: 1s
controlPlaneAuthPolicy: NONE`
	overridesExpected := func() *meshconfig.ProxyConfig {
		m := mesh.DefaultProxyConfig()
		m.DiscoveryAddress = "foo:123"
		m.ProxyMetadata = map[string]string{"SOME": "setting"}
		m.DrainDuration = durationpb.New(time.Second)
		m.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
		return m
	}()
	cases := []struct {
		name        string
		annotation  string
		environment string
		file        string
		expect      *meshconfig.ProxyConfig
	}{
		{
			name: "Defaults",
			expect: func() *meshconfig.ProxyConfig {
				m := mesh.DefaultProxyConfig()
				return m
			}(),
		},
		{
			name:       "Annotation Override",
			annotation: proxyOverride,
			expect:     overridesExpected,
		},
		{
			name:   "File Override",
			file:   meshOverride,
			expect: overridesExpected,
		},
		{
			name:        "Environment Override",
			environment: proxyOverride,
			expect:      overridesExpected,
		},
		{
			// Hopefully no one actually has all three of these set in a real system, but we will still
			// test them all together.
			name: "Multiple Override",
			// Order is file < env < annotation
			file: `
defaultConfig:
  discoveryAddress: file:123
  proxyMetadata:
    SOME: setting
  drainDuration: 1s
  extraStatTags: ["a"]
  proxyStatsMatcher:
    inclusionPrefixes: ["a"]
    inclusionSuffixes: ["b"]
    inclusionRegexps: ["c"]
  controlPlaneAuthPolicy: NONE`,
			environment: `
discoveryAddress: environment:123
proxyMetadata:
OTHER: option`,
			annotation: `
discoveryAddress: annotation:123
proxyMetadata:
  ANNOTATION: something
drainDuration: 5s
extraStatTags: ["b"]
proxyStatsMatcher:
  inclusionPrefixes: ["a"]
  inclusionSuffixes: ["e"]
  inclusionRegexps: ["f"]
`,
			expect: func() *meshconfig.ProxyConfig {
				m := mesh.DefaultProxyConfig()
				m.DiscoveryAddress = "annotation:123"
				m.ProxyMetadata = map[string]string{"ANNOTATION": "something", "SOME": "setting"}
				m.DrainDuration = durationpb.New(5 * time.Second)
				m.ExtraStatTags = []string{"b"}
				m.ProxyStatsMatcher = &meshconfig.ProxyConfig_ProxyStatsMatcher{}
				m.ProxyStatsMatcher.InclusionPrefixes = []string{"a"}
				m.ProxyStatsMatcher.InclusionSuffixes = []string{"e"}
				m.ProxyStatsMatcher.InclusionRegexps = []string{"f"}
				m.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
				return m
			}(),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxyConfigEnv := tt.environment
			got, err := getMeshConfig(tt.file, tt.annotation, proxyConfigEnv, true)
			if err != nil {
				t.Fatal(err)
			}
			if !cmp.Equal(got.DefaultConfig, tt.expect, protocmp.Transform()) {
				t.Fatalf("got \n%v expected \n%v", got.DefaultConfig, tt.expect)
			}
		})
	}
}

func writeAnnotations(t *testing.T, annotations string) {
	// Setup pod cpu limit
	err := os.MkdirAll(filepath.Dir(constants.PodInfoAnnotationsPath), 0777)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(constants.PodInfoAnnotationsPath, []byte(annotations), 0666)
	if err != nil {
		t.Fatal(err)
	}
}

func writeCPULimit(t *testing.T, limit int) {
	// Setup pod cpu limit
	err := os.MkdirAll(filepath.Dir(constants.PodInfoCPULimitsPath), 0777)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(constants.PodInfoCPULimitsPath, []byte(strconv.Itoa(limit)), 0666)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConstructProxyConfig_Concurrency(t *testing.T) {
	cases := []struct {
		name        string
		concurrency int
		configEnv   string
		annotations string
		cpuLimit    int
		isSidecar   bool
		expect      int32
	}{
		{
			name:      "Ignore sidecar proxy (use defaults)",
			isSidecar: true,
			cpuLimit:  2000,
			expect:    mesh.DefaultProxyConfig().Concurrency.GetValue(),
		},
		{
			name:     "CPU limit unset",
			cpuLimit: 0,
			expect:   0, // 0 means "all" cores on host
		},
		{
			name:     "CPU limit is 100m",
			cpuLimit: 100,
			expect:   1,
		},
		{
			name:     "CPU limit is 1500m",
			cpuLimit: 1500,
			expect:   2,
		},
		{
			name:     "CPU limit is 4000m",
			cpuLimit: 4000,
			expect:   4,
		},
		{
			name:        "Override in commandline arguments",
			concurrency: 5,
			cpuLimit:    4000,
			expect:      5,
		},
		{
			name: "Override in environment variable",
			configEnv: `
concurrency: 5
`,
			cpuLimit: 4000,
			expect:   5,
		},
		{
			name:        "Override in annotation",
			annotations: `proxy.istio.io/config="concurrency: 5"`,
			cpuLimit:    4000,
			expect:      5,
		},
	}

	setupTempDir := func(t *testing.T) func() {
		prevDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}

		tempDir := t.TempDir()
		err = os.Chdir(tempDir)
		if err != nil {
			t.Fatal(err)
		}

		return func() {
			err := os.Chdir(prevDir)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTempDir(t)
			defer cleanup()

			writeCPULimit(t, tt.cpuLimit)
			writeAnnotations(t, tt.annotations)

			role := &model.Proxy{}
			if tt.isSidecar {
				role.Type = model.SidecarProxy
			} else {
				role.Type = model.Router
			}

			got, err := ConstructProxyConfig("", constants.ServiceClusterName, tt.configEnv, tt.concurrency, role)
			if err != nil {
				t.Fatal(err)
			}
			concurrency := got.Concurrency.GetValue()
			if !cmp.DeepEqual(concurrency, tt.expect) {
				t.Fatalf("got %v, expected %v", concurrency, tt.expect)
			}
		})
	}
}
